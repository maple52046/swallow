// Package libvirt implements serverdomain.LibvirtHost (decision 055): swallow's access to a
// Hypervisor's system libvirt daemon, by running virsh over an SSH login made with the Deployment
// Key. It reads domains, stops one the operator allowed to stop, and gives a domain libvirt Boot
// Media by uploading the Boot ISO as a storage volume and rewriting the domain's persistent
// definition. It never switches power for the provisioner and holds no hypervisor credential.
package libvirt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const (
	// virsh is every command's prefix: C locale (no setlocale warnings in the output, English state
	// words), quiet output, and the system daemon whatever the account's default URI is.
	virsh = "LC_ALL=C virsh -q -c qemu:///system"
	// domainMarker separates domains in a listing; it cannot occur in libvirt XML.
	domainMarker = "@@swallow-domain@@"
	// poolTarget is where a missing `default` pool is defined, as virt-install does.
	poolTarget = "/var/lib/libvirt/images"
	// uploadTimeout bounds one login that streams a Boot ISO, a few MiB on an internal network.
	uploadTimeout = 5 * time.Minute
	// maxDetail bounds libvirt's own explanation carried into an operator message.
	maxDetail = 400
)

// HostAccess is the SSH access the adapter needs: serverinfra.SSHHostAccess.
type HostAccess interface {
	RunWithDeploymentKey(ctx context.Context, target serverdomain.HostLoginTarget, user, script string, stdin io.Reader, timeout time.Duration) (string, error)
	AuthorizePublicKey(ctx context.Context, target serverdomain.HostLoginTarget, user, publicKey string) error
}

// exitError is a command that ran and exited non-zero (serverinfra.CommandError).
type exitError interface {
	ExitStatus() int
	ErrorOutput() string
}

// Host implements serverdomain.LibvirtHost. It is stateless and safe for concurrent use; each call
// makes its own logins.
type Host struct {
	access HostAccess
}

// NewHost builds the adapter over the Deployment Key SSH access.
func NewHost(access HostAccess) *Host {
	return &Host{access: access}
}

var _ serverdomain.LibvirtHost = (*Host)(nil)

// ListDomains implements serverdomain.LibvirtHost in one login: every domain's state and persistent
// definition, separated by domainMarker. A domain removed while the listing runs is skipped.
func (h *Host) ListDomains(ctx context.Context, hypervisor serverdomain.HypervisorLogin) ([]serverdomain.VirtualMachine, error) {
	script := `names=$(` + virsh + ` list --all --name) || exit $?
printf "%s\n" "$names" | while IFS= read -r name; do
  [ -n "$name" ] || continue
  state=$(` + virsh + ` domstate --domain "$name" 2>/dev/null) || continue
  definition=$(` + virsh + ` dumpxml --inactive --domain "$name" 2>/dev/null) || continue
  printf "%s\n%s\n%s\n" "` + domainMarker + `" "$state" "$definition"
done`
	output, err := h.run(ctx, hypervisor, "", script, nil, 0)
	if err != nil {
		return nil, err
	}
	var machines []serverdomain.VirtualMachine
	for _, chunk := range strings.Split(output, domainMarker+"\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		state, definition, _ := strings.Cut(chunk, "\n")
		machine, err := virtualMachine(strings.TrimSpace(state), []byte(definition))
		if err != nil {
			return nil, fmt.Errorf("read a domain of hypervisor %s: %w", hypervisor.Name, err)
		}
		machines = append(machines, machine)
	}
	return machines, nil
}

// Domain implements serverdomain.LibvirtHost.
func (h *Host) Domain(ctx context.Context, hypervisor serverdomain.HypervisorLogin, name string) (*serverdomain.VirtualMachine, error) {
	state, definition, err := h.definition(ctx, hypervisor, name)
	if err != nil {
		return nil, err
	}
	machine, err := virtualMachine(state, definition)
	if err != nil {
		return nil, fmt.Errorf("read domain %s of hypervisor %s: %w", name, hypervisor.Name, err)
	}
	return &machine, nil
}

// DestroyDomain implements serverdomain.LibvirtHost.
func (h *Host) DestroyDomain(ctx context.Context, hypervisor serverdomain.HypervisorLogin, name string) error {
	script := `state=$(` + virsh + ` domstate --domain ` + quote(name) + `) || exit $?
if [ "$state" != "shut off" ]; then ` + virsh + ` destroy --domain ` + quote(name) + ` >/dev/null; fi`
	_, err := h.run(ctx, hypervisor, name, script, nil, 0)
	return err
}

// AuthorizePublicKey implements serverdomain.LibvirtHost.
func (h *Host) AuthorizePublicKey(ctx context.Context, hypervisor serverdomain.HypervisorLogin, publicKey string) error {
	if err := h.access.AuthorizePublicKey(ctx, hypervisor.Target, hypervisor.Account, publicKey); err != nil {
		return h.explain(err, hypervisor, "")
	}
	return nil
}

// ReadBootMedia implements serverdomain.LibvirtHost.
func (h *Host) ReadBootMedia(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain, isoID string) (serverdomain.BootMediaState, error) {
	_, definition, err := h.definition(ctx, hypervisor, domain)
	if err != nil {
		return serverdomain.BootMediaState{}, err
	}
	parsed, err := parseDomain(definition)
	if err != nil {
		return serverdomain.BootMediaState{}, fmt.Errorf("read domain %s of hypervisor %s: %w", domain, hypervisor.Name, err)
	}
	return bootMediaState(parsed, serverdomain.BootMediaVolumeName(isoID)), nil
}

// ApplyBootMedia implements serverdomain.LibvirtHost.
func (h *Host) ApplyBootMedia(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain string, iso serverdomain.BootISOFile) (serverdomain.BootMediaState, error) {
	if _, _, err := h.definition(ctx, hypervisor, domain); err != nil {
		return serverdomain.BootMediaState{}, err
	}
	volumePath, err := h.ensureVolume(ctx, hypervisor, domain, iso)
	if err != nil {
		return serverdomain.BootMediaState{}, err
	}
	if err := h.rewrite(ctx, hypervisor, domain, func(d *domainDefinition) error { return d.setBootMedia(volumePath) }); err != nil {
		return serverdomain.BootMediaState{}, err
	}
	state, err := h.ReadBootMedia(ctx, hypervisor, domain, iso.ID)
	if err != nil {
		return serverdomain.BootMediaState{}, err
	}
	if !state.Ready() {
		return state, &serverdomain.HypervisorError{Err: serverdomain.ErrLibvirtRefused, Hypervisor: hypervisor.Name, Domain: domain,
			Detail: "The definition read back does not boot the Boot ISO first."}
	}
	return state, nil
}

// ClearBootMedia implements serverdomain.LibvirtHost.
func (h *Host) ClearBootMedia(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain string) error {
	return h.rewrite(ctx, hypervisor, domain, func(d *domainDefinition) error {
		d.clearBootMedia()
		return nil
	})
}

// definition reads a domain's state and persistent definition.
func (h *Host) definition(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain string) (string, []byte, error) {
	script := `state=$(` + virsh + ` domstate --domain ` + quote(domain) + `) || exit $?
printf "%s\n" "$state"
` + virsh + ` dumpxml --inactive --domain ` + quote(domain)
	output, err := h.run(ctx, hypervisor, domain, script, nil, 0)
	if err != nil {
		return "", nil, err
	}
	state, definition, _ := strings.Cut(output, "\n")
	return strings.TrimSpace(state), []byte(definition), nil
}

// rewrite reads the domain's persistent definition, changes it, and defines it again. libvirt
// validates the result; a running domain uses it from its next start.
func (h *Host) rewrite(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain string, change func(*domainDefinition) error) error {
	_, definition, err := h.definition(ctx, hypervisor, domain)
	if err != nil {
		return err
	}
	parsed, err := parseDomain(definition)
	if err != nil {
		return fmt.Errorf("read domain %s of hypervisor %s: %w", domain, hypervisor.Name, err)
	}
	if err := change(parsed); err != nil {
		return &serverdomain.HypervisorError{Err: serverdomain.ErrLibvirtRefused, Hypervisor: hypervisor.Name, Domain: domain, Detail: err.Error()}
	}
	_, err = h.run(ctx, hypervisor, domain, virsh+" define --file /dev/stdin >/dev/null", bytes.NewReader(parsed.root.marshal()), 0)
	return err
}

// ensureVolume makes the Boot ISO's volume in the Boot Media pool hold exactly the ISO file and
// returns its path. The pool is defined and started when needed. The content is compared by
// SHA-256 rather than trusted by size, so an interrupted upload is replaced by the next apply.
func (h *Host) ensureVolume(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain string, iso serverdomain.BootISOFile) (string, error) {
	sum, err := fileSHA256(iso.Path)
	if err != nil {
		return "", fmt.Errorf("%w: the file of Boot ISO %q cannot be read; build it again", serverdomain.ErrBootMediaNotConfigured, iso.Name)
	}
	pool, volume := quote(serverdomain.BootMediaPool), quote(serverdomain.BootMediaVolumeName(iso.ID))
	ensurePool := `if ! ` + virsh + ` pool-info --pool ` + pool + ` >/dev/null 2>&1; then
  ` + virsh + ` pool-define-as --name ` + pool + ` --type dir --target ` + quote(poolTarget) + ` >/dev/null || exit $?
  ` + virsh + ` pool-build --pool ` + pool + ` >/dev/null 2>&1 || true
  ` + virsh + ` pool-autostart --pool ` + pool + ` >/dev/null || exit $?
fi
state=$(` + virsh + ` pool-info --pool ` + pool + ` | awk '/^State:/ {print $2}')
if [ "$state" != "running" ]; then ` + virsh + ` pool-start --pool ` + pool + ` >/dev/null || exit $?; fi
if ` + virsh + ` vol-info --pool ` + pool + ` --vol ` + volume + ` >/dev/null 2>&1; then
  ` + virsh + ` vol-download --pool ` + pool + ` --vol ` + volume + ` --file /dev/stdout | sha256sum | cut -d" " -f1
fi`
	current, err := h.run(ctx, hypervisor, domain, ensurePool, nil, uploadTimeout)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != sum {
		file, err := os.Open(iso.Path)
		if err != nil {
			return "", fmt.Errorf("%w: the file of Boot ISO %q cannot be read; build it again", serverdomain.ErrBootMediaNotConfigured, iso.Name)
		}
		defer file.Close()
		size := strconv.FormatInt(iso.Size, 10)
		upload := virsh + ` vol-delete --pool ` + pool + ` --vol ` + volume + ` >/dev/null 2>&1
` + virsh + ` vol-create-as --pool ` + pool + ` --name ` + volume + ` --capacity ` + size + ` --format raw >/dev/null || exit $?
` + virsh + ` vol-upload --pool ` + pool + ` --vol ` + volume + ` --file /dev/stdin --length ` + size + ` || exit $?
` + virsh + ` vol-download --pool ` + pool + ` --vol ` + volume + ` --file /dev/stdout | sha256sum | cut -d" " -f1`
		uploaded, err := h.run(ctx, hypervisor, domain, upload, io.LimitReader(file, iso.Size), uploadTimeout)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(uploaded) != sum {
			return "", &serverdomain.HypervisorError{Err: serverdomain.ErrLibvirtRefused, Hypervisor: hypervisor.Name, Domain: domain,
				Detail: "The Boot ISO volume read back differs from the uploaded file."}
		}
	}
	volumePath, err := h.run(ctx, hypervisor, domain, virsh+` vol-path --pool `+pool+` --vol `+volume, nil, 0)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(volumePath), nil
}

// run executes script on the Hypervisor and classifies a failure for domain ("" when none).
func (h *Host) run(ctx context.Context, hypervisor serverdomain.HypervisorLogin, domain, script string, stdin io.Reader, timeout time.Duration) (string, error) {
	output, err := h.access.RunWithDeploymentKey(ctx, hypervisor.Target, hypervisor.Account, script, stdin, timeout)
	if err != nil {
		return "", h.explain(err, hypervisor, domain)
	}
	return output, nil
}

// explain turns an access or virsh failure into a *HypervisorError.
func (h *Host) explain(err error, hypervisor serverdomain.HypervisorLogin, domain string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	failure := &serverdomain.HypervisorError{Hypervisor: hypervisor.Name, Account: hypervisor.Account, Domain: domain}
	var exit exitError
	switch {
	case errors.Is(err, serverdomain.ErrDeploymentKeyMissing):
		failure.Err = serverdomain.ErrDeploymentKeyMissing
	case errors.Is(err, serverdomain.ErrDeploymentKeyRejected):
		failure.Err = serverdomain.ErrDeploymentKeyRejected
	case errors.Is(err, serverdomain.ErrHostUnreachable):
		failure.Err = serverdomain.ErrHostUnreachable
	case errors.As(err, &exit):
		output := exit.ErrorOutput()
		lower := strings.ToLower(output)
		failure.Detail = detail(output)
		switch {
		case exit.ExitStatus() == 127 || strings.Contains(lower, "virsh: not found") || strings.Contains(lower, "command not found"):
			failure.Err = serverdomain.ErrLibvirtUnavailable
		case strings.Contains(lower, "failed to connect to the hypervisor") || strings.Contains(lower, "permission denied") ||
			strings.Contains(lower, "authentication unavailable") || strings.Contains(lower, "authentication failed"):
			failure.Err = serverdomain.ErrLibvirtUnavailable
		case domain != "" && (strings.Contains(lower, "failed to get domain") || strings.Contains(lower, "domain not found")):
			failure.Err, failure.Detail = serverdomain.ErrDomainNotFound, ""
		default:
			failure.Err = serverdomain.ErrLibvirtRefused
		}
	default:
		failure.Err, failure.Detail = serverdomain.ErrHostUnreachable, detail(err.Error())
	}
	return failure
}

// detail is libvirt's explanation for an operator: its error lines, without virsh's prefix,
// bounded in length.
func detail(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "error:"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	text := strings.Join(lines, " ")
	if len(text) > maxDetail {
		text = text[:maxDetail] + "…"
	}
	return text
}

// virtualMachine reads a domain's facts from its state and persistent definition.
func virtualMachine(state string, definition []byte) (serverdomain.VirtualMachine, error) {
	parsed, err := parseDomain(definition)
	if err != nil {
		return serverdomain.VirtualMachine{}, err
	}
	return serverdomain.VirtualMachine{
		Name: parsed.name(), UUID: parsed.uuid(), State: state, Architecture: parsed.architecture(),
		MACAddresses: parsed.macAddresses(), CDROM: parsed.cdrom() != nil,
	}, nil
}

// bootMediaState reads Boot Media from a domain's persistent definition: the CD-ROM's image, and
// whether it is the volume and boots first. The boot direction persists, so it is Continuous.
func bootMediaState(parsed *domainDefinition, volume string) serverdomain.BootMediaState {
	state := serverdomain.BootMediaState{MediaImage: cdromSource(parsed.cdrom()), MediaInserted: parsed.holdsVolume(volume)}
	if parsed.cdromBootsFirst() {
		state.OverrideEnabled, state.OverrideTarget, state.OverrideReady = "Continuous", "Cd", true
	}
	return state
}

// quote single-quotes s for the shell that runs the script.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
