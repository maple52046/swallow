// Package hostenroll enrolls the host the CLI runs on into a provisioner while the host keeps its
// operating system (Server Enrollment, decision 053; contract server-enrollment.md).
//
// Component boundary: this is the one part of the CLI that talks to a provisioner instead of
// api-server, because hardware facts can only be read on the host itself. It wraps the
// provisioner's own enrollment tooling (for MAAS: maas-run-scripts register-machine, then
// report-results) and never calls api-server; the Server appears there through reconciliation.
// When asked, it also sets up the host's BMC for the provisioner's power driver from the host's
// local IPMI interface, writing the power parameters to the provisioner directly so the BMC
// password never reaches swallow (decision 054). The endpoint and credential come from
// api-server's enrollment bundle, which an admin copies to the host. Nothing here may persist the
// credential or a BMC password, or print either.
package hostenroll

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProviderMAAS is the only provisioner kind with existing-host enrollment today.
const ProviderMAAS = "maas"

// Options is one enrollment request.
type Options struct {
	// Provisioner is the provisioner kind, e.g. "maas".
	Provisioner string
	// Endpoint is the provisioner address the host reaches, for MAAS its region URL.
	Endpoint string
	// Token is the provisioner credential from the enrollment bundle. It is a secret.
	Token string
	// Hostname is the name to register; empty uses the host's short name.
	Hostname string
	// ConfigureBMC also sets up the host's BMC for the provisioner's power driver (for MAAS: an
	// IPMI user over the local KCS interface, then the Machine's ipmi power parameters). It
	// changes one user of the BMC's user table and nothing else: not the host's power, network, or
	// packages, and not the BMC's network settings.
	ConfigureBMC bool
	// SystemID names the Machine this host is already registered as. Registration is skipped and
	// the hardware is reported again under that Machine (which also repairs an address MAAS left
	// out); with ConfigureBMC the BMC is then set up. The Machine must share a MAC address with
	// this host, or its hostname when MAAS recorded no interfaces for it.
	SystemID string
	// Stdout and Stderr receive progress and the provider tool's own output.
	Stdout, Stderr io.Writer
}

// enroller holds the host-facing effects so tests can replace them.
type enroller struct {
	httpClient *http.Client
	geteuid    func() int
	lookPath   func(string) (string, error)
	hostname   func() (string, error)
	// run executes name with args in dir, writing to stdout and stderr.
	run func(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) error
	// stat reports a host path, such as the IPMI device or the EFI firmware directory.
	stat func(path string) (os.FileInfo, error)
	// hostMACs lists the MAC addresses of the host's network interfaces, lower-case.
	hostMACs func() ([]string, error)
	// routeAddress returns the local address this host uses to reach the provisioner endpoint,
	// the address the provisioner must record for the Server to have one in swallow.
	routeAddress func(endpoint string) (string, error)
}

// Enroll registers this host with the provisioner and records its hardware, without a reboot,
// and with ConfigureBMC sets up its BMC so the provisioner can switch and read its power.
//
// It must run as root on the host being enrolled: the provider's hardware report reads devices
// only root can, and so does the local IPMI interface. Every check that needs no change (the
// options, ipmitool, the IPMI device, the BMC's address, the provider's BMC policy) runs before
// the host is registered, so a host that cannot be set up is refused before it is enrolled. A
// BMC step that fails after registration returns an error saying the host is enrolled. Errors
// name the step that failed; the provider tool's own output is passed through on Stderr. ctx
// cancellation stops the running step. Temporary files, including the Machine credentials the
// provider returns and the BMC password, are removed before Enroll returns.
func Enroll(ctx context.Context, opts Options) error {
	return defaultEnroller().enroll(ctx, opts)
}

func defaultEnroller() enroller {
	return enroller{
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		geteuid:    os.Geteuid,
		lookPath:   exec.LookPath,
		hostname:   os.Hostname,
		run: func(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) error {
			command := exec.CommandContext(ctx, name, args...)
			command.Dir, command.Stdout, command.Stderr = dir, stdout, stderr
			return command.Run()
		},
		stat:         os.Stat,
		hostMACs:     interfaceMACs,
		routeAddress: sourceAddress,
	}
}

func (e enroller) enroll(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	switch strings.ToLower(strings.TrimSpace(opts.Provisioner)) {
	case ProviderMAAS:
		return e.enrollMAAS(ctx, opts)
	case "":
		return errors.New("--provisioner is required (supported: maas)")
	default:
		return fmt.Errorf("provisioner %q has no existing-host enrollment (supported: maas)", opts.Provisioner)
	}
}

// enrollMAAS mirrors the enrollment script api-server serves: download maas-run-scripts from the
// region, register-machine (the Machine is created as Deployed), then report-results; with
// SystemID it reports again under that Machine instead of registering. MAAS can leave the host's
// address out of a report, so the report is then checked and, if needed, sent once more
// (maas_inventory.go). MAAS runs only its deploy-info scripts there, not its BMC script, so the
// Machine has no power driver; with ConfigureBMC the IPMI setup in maas_bmc.go gives it one,
// otherwise the output says how to.
func (e enroller) enrollMAAS(ctx context.Context, opts Options) error {
	endpoint := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/"), "/api/2.0")
	if endpoint == "" {
		return errors.New("--endpoint is required: the MAAS region URL, e.g. http://10.0.0.5:5240/MAAS")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return fmt.Errorf("--endpoint %q must be an http or https URL", endpoint)
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return errors.New("--token is required: the MAAS API key from the enrollment bundle (or set SWALLOW_ENROLL_TOKEN)")
	}
	systemID := strings.TrimSpace(opts.SystemID)
	register := systemID == ""
	if e.geteuid() != 0 {
		return errors.New("run as root (sudo): recording the hardware and the local IPMI interface need root")
	}
	if _, err := e.lookPath("python3"); err != nil {
		return errors.New("python3 is required by maas-run-scripts; install it and retry")
	}
	name := strings.TrimSpace(opts.Hostname)
	if name == "" {
		full, err := e.hostname()
		if err != nil {
			return fmt.Errorf("read the host name: %w; pass --hostname", err)
		}
		name, _, _ = strings.Cut(full, ".")
	}

	workdir, err := os.MkdirTemp("", "swallow-enroll-")
	if err != nil {
		return fmt.Errorf("create a working directory: %w", err)
	}
	defer os.RemoveAll(workdir)

	var bmc *bmcSetup
	if opts.ConfigureBMC {
		if bmc, err = e.prepareBMC(ctx, workdir, endpoint, token, opts.Stdout); err != nil {
			return err
		}
	}
	// The admin API reads the Machine back. The enrollment bundle's token is always a MAAS API key;
	// a token that is not one can still register, but nothing is read back to check.
	api, apiErr := newMAASAPI(e.httpClient, endpoint, token)
	if !register && apiErr != nil {
		return apiErr
	}
	if !register {
		// The Machine must be this host before anything is reported under it.
		if _, err := e.findMachine(ctx, api, name, systemID); err != nil {
			return err
		}
	}
	script := filepath.Join(workdir, "maas-run-scripts")
	if err := e.download(ctx, endpoint+"/maas-run-scripts", script); err != nil {
		return err
	}
	var creds string
	if register {
		if creds, err = e.registerMAAS(ctx, workdir, script, endpoint, token, name, opts); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "Enrolled %s. It appears in swallow as Deployed after the next inventory sync.\n", name)
	} else {
		if creds, err = e.reportAgain(ctx, workdir, script, endpoint, token, name, systemID, opts); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "Reported the hardware of %s again under Machine %s.\n", name, systemID)
	}
	if apiErr == nil {
		e.ensureAddressRecorded(ctx, api, name, systemID, endpoint, func() error {
			return e.reportResults(ctx, workdir, script, creds, opts)
		}, opts.Stdout)
	}
	if bmc == nil {
		fmt.Fprint(opts.Stdout, powerHint(name))
		return nil
	}
	if err := e.configureBMC(ctx, workdir, bmc, name, systemID, opts.Stdout); err != nil {
		if register {
			return fmt.Errorf("%s is enrolled, but its BMC was not set up: %w", name, err)
		}
		return fmt.Errorf("set up the BMC of %s: %w", name, err)
	}
	return nil
}

// registerMAAS registers the host under name as a Deployed Machine with the downloaded
// maas-run-scripts, and reports its hardware with the Machine credentials register-machine
// returned. It returns the path of those credentials, which stay in workdir (the caller removes
// it) so the report can be sent again.
func (e enroller) registerMAAS(ctx context.Context, workdir, script, endpoint, token, name string, opts Options) (string, error) {
	fmt.Fprintf(opts.Stdout, "Registering %s with MAAS at %s as a deployed machine\n", name, endpoint)
	var registered bytes.Buffer
	// The token is an argument because that is how register-machine takes it.
	if err := e.run(ctx, workdir, &registered, opts.Stderr, script, "register-machine", "--hostname", name, endpoint, token); err != nil {
		return "", fmt.Errorf("maas-run-scripts register-machine failed: %w", err)
	}
	creds, err := machineCredentials(workdir, registered.Bytes())
	if err != nil {
		return "", err
	}
	fmt.Fprintf(opts.Stdout, "Recording the hardware of %s\n", name)
	if err := e.reportResults(ctx, workdir, script, creds, opts); err != nil {
		return "", err
	}
	return creds, nil
}

// reportResults runs maas-run-scripts report-results with the Machine credentials at creds: it
// runs MAAS's deploy-info scripts on this host and sends their results, which MAAS processes
// before the call returns.
func (e enroller) reportResults(ctx context.Context, workdir, script, creds string, opts Options) error {
	if err := e.run(ctx, workdir, opts.Stdout, opts.Stderr, script, "report-results", "--config", creds); err != nil {
		return fmt.Errorf("maas-run-scripts report-results failed: %w", err)
	}
	return nil
}

// powerHint is the next step after an enrollment without ConfigureBMC: register-machine leaves
// the Machine without a power driver, so swallow can neither switch nor read the Server's power
// until the user sets one in swallow or sets up the BMC from this host.
func powerHint(name string) string {
	return "MAAS registered " + name + " without a power driver, so swallow cannot switch or read its power yet. Either:\n" +
		"  - set its Power Configuration in swallow, from the power button on the Server list or with\n" +
		"      swallow servers power-configuration set <serverId> --driver ipmi --address <BMC address> --username <account> --password-stdin\n" +
		"  - or set up its BMC over IPMI from this host: run the enrollment command again with\n" +
		"      --configure-bmc --system-id <its Provider machine ID in swallow>\n"
}

// download saves url to path as an owner-only executable.
func (e enroller) download(ctx context.Context, url, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download maas-run-scripts: %w", err)
	}
	response, err := e.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download maas-run-scripts from %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download maas-run-scripts from %s: HTTP %d", url, response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("save maas-run-scripts: %w", err)
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()
		return fmt.Errorf("save maas-run-scripts: %w", err)
	}
	return file.Close()
}

// machineCredentials returns the path of the Machine credentials register-machine produced. It
// prints them as YAML; a version that writes a *-creds.yaml file instead is accepted too.
func machineCredentials(workdir string, printed []byte) (string, error) {
	if bytes.Contains(printed, []byte("reporting:")) {
		path := filepath.Join(workdir, "creds.yaml")
		if err := os.WriteFile(path, printed, 0o600); err != nil {
			return "", fmt.Errorf("save the machine credentials: %w", err)
		}
		return path, nil
	}
	matches, err := filepath.Glob(filepath.Join(workdir, "*-creds.yaml"))
	if err == nil && len(matches) > 0 {
		return matches[0], nil
	}
	return "", errors.New("maas-run-scripts register-machine returned no machine credentials")
}

// interfaceMACs lists the hardware addresses of the host's non-loopback interfaces, which is how
// a Machine named by --system-id is matched to this host.
func interfaceMACs() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list the network interfaces: %w", err)
	}
	var macs []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		macs = append(macs, strings.ToLower(iface.HardwareAddr.String()))
	}
	return macs, nil
}
