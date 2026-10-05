package domain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// BootISO is a swallow-built iPXE boot ISO for one provisioner Integration (decision 049): a
// machine that boots it takes an address from the site's own DHCP and then chains to that
// provisioner's MAAS rack. A Server's Boot Media mounts one through its BMC.
//
// Script is rendered by swallow from its fixed template (RenderBootISOScript); operators never
// write it. The file lives outside this record, in the Boot Media directory, under the ISO's ID.
type BootISO struct {
	ID            string
	Name          string
	IntegrationID string
	// RackAddress is the MAAS rack as the operator gave it: an IPv4 address or hostname with an
	// optional ":port".
	RackAddress string
	// ChainURL is where the script chains after DHCP, derived from RackAddress.
	ChainURL    string
	Script      string
	IPXEVersion string
	SizeBytes   int64
	SHA256      string
	CreatedAt   time.Time
	CreatedBy   string
}

// BootISOFilter narrows a Boot ISO listing. Zero values mean no constraint.
type BootISOFilter struct {
	IntegrationID  string
	IntegrationIDs []string
}

// BootISORepository persists Boot ISO records. Names are unique per Integration, compared
// case-insensitively; Create returns ErrBootISONameTaken for a duplicate. FindByID and Delete
// return ErrBootISONotFound for an unknown id. List orders by name.
type BootISORepository interface {
	Create(ctx context.Context, iso *BootISO) error
	FindByID(ctx context.Context, id string) (*BootISO, error)
	List(ctx context.Context, filter BootISOFilter) ([]*BootISO, error)
	Delete(ctx context.Context, id string) error
}

// BootISOArtifact describes a built ISO file.
type BootISOArtifact struct {
	SizeBytes   int64
	SHA256      string
	IPXEVersion string
}

// BootISOBuilder packages a rendered script into an ISO file and manages the files (decision
// 049). Implementations write the file atomically, so a URL never serves a half-written ISO,
// and are safe for concurrent builds of different ids.
type BootISOBuilder interface {
	// Available returns nil when builds can run, otherwise a *BootISOBuilderUnavailableError
	// that says what is missing (base URL, a writable directory, iPXE assets, packaging tools).
	Available() error
	// Build writes the ISO for id with script as its autoexec.ipxe. A failure wraps
	// ErrBootISOBuildFailed with a short summary of the tool's output and leaves no file.
	Build(ctx context.Context, id, script string) (BootISOArtifact, error)
	// Remove deletes id's file; a missing file is not an error.
	Remove(id string) error
	// URL is where BMCs mount id's ISO, or "" when no Boot Media base URL is configured.
	URL(id string) string
}

// BootISOUsage counts the Servers whose enabled Boot Media uses a Boot ISO; a Boot ISO in use
// cannot be deleted.
type BootISOUsage interface {
	CountEnabledUsing(ctx context.Context, isoID string) (int, error)
}

var (
	// ErrBootISONotFound means the Boot ISO id is unknown.
	ErrBootISONotFound = errors.New("boot ISO not found")
	// ErrBootISONameTaken means another Boot ISO of the same Integration has the name.
	ErrBootISONameTaken = errors.New("a boot ISO with this name already exists for the integration")
	// ErrInvalidBootISO marks invalid build input (name, rack address).
	ErrInvalidBootISO = errors.New("invalid boot ISO")
	// ErrBootISOInUse means a Server's enabled Boot Media uses the Boot ISO.
	ErrBootISOInUse = errors.New("boot ISO is in use")
	// ErrBootISOBuilderUnavailable means this installation cannot build Boot ISOs.
	ErrBootISOBuilderUnavailable = errors.New("boot ISOs cannot be built on this installation")
	// ErrBootISOBuildFailed means packaging the ISO failed.
	ErrBootISOBuildFailed = errors.New("building the boot ISO failed")
)

// BootISOBuilderUnavailableError says why this installation cannot build Boot ISOs. Its message
// is only Reason — one operator-facing sentence such as "the Boot Media directory … is not
// writable" — because clients show it verbatim under their own "cannot build" heading. It
// matches ErrBootISOBuilderUnavailable with errors.Is.
type BootISOBuilderUnavailableError struct {
	Reason string
}

func (e *BootISOBuilderUnavailableError) Error() string { return e.Reason }

// Is makes errors.Is(err, ErrBootISOBuilderUnavailable) hold.
func (e *BootISOBuilderUnavailableError) Is(target error) bool {
	return target == ErrBootISOBuilderUnavailable
}

// DefaultMAASRackPort is the MAAS rack controller's HTTP boot port that serves ipxe.cfg.
const DefaultMAASRackPort = 5248

// maxBootISONameLength bounds a Boot ISO name.
const maxBootISONameLength = 63

// ValidateBootISOName trims and checks a Boot ISO name: 1 to 63 characters, no control
// characters. It returns the trimmed name.
func ValidateBootISOName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidBootISO)
	}
	if len([]rune(name)) > maxBootISONameLength {
		return "", fmt.Errorf("%w: name must be at most %d characters", ErrInvalidBootISO, maxBootISONameLength)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: name must not contain control characters", ErrInvalidBootISO)
		}
	}
	return name, nil
}

// ParseMAASRackAddress validates a rack address as an operator types it — an IPv4 address or a
// hostname, optionally followed by ":port" — and returns the host and port (DefaultMAASRackPort
// when none is given). The result is safe to place into an iPXE script and a URL: only
// letters, digits, '-', '.', and a numeric port pass. It neither resolves nor contacts the host.
func ParseMAASRackAddress(raw string) (string, int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", 0, fmt.Errorf("%w: rackAddress is required", ErrInvalidBootISO)
	}
	host, port := value, DefaultMAASRackPort
	if i := strings.LastIndex(value, ":"); i >= 0 {
		host = value[:i]
		parsed, err := strconv.Atoi(value[i+1:])
		if err != nil || parsed < 1 || parsed > 65535 {
			return "", 0, fmt.Errorf("%w: rackAddress port must be a number from 1 to 65535", ErrInvalidBootISO)
		}
		port = parsed
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return "", 0, fmt.Errorf("%w: rackAddress must be an IPv4 address or a hostname", ErrInvalidBootISO)
		}
		return ip.To4().String(), port, nil
	}
	if !validHostname(host) {
		return "", 0, fmt.Errorf("%w: rackAddress must be an IPv4 address or a hostname", ErrInvalidBootISO)
	}
	return strings.ToLower(host), port, nil
}

// validHostname accepts RFC 1123 hostnames: dot-separated labels of letters, digits, and '-',
// each 1 to 63 characters, not starting or ending with '-', 253 characters in total.
func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// MAASChainURL is the URL a Boot ISO chains to after DHCP: the rack's iPXE pre-loader.
func MAASChainURL(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/ipxe.cfg", host, port)
}

// RenderBootISOScript renders the Boot ISO's iPXE script for a MAAS rack (decision 049). It is
// the template verified on networks whose DHCP is not MAAS's (MAAS knowledge base: iPXE 2.0.0,
// MAAS 3.7, OVMF): take a DHCP lease, set next-server to the rack because MAAS's follow-up
// scripts build their URLs from it, and chain the rack's ipxe.cfg without using the DHCP boot
// filename, so a site PXE server never takes over. A failed DHCP retries (Ctrl-B opens a shell);
// on UEFI a chain that returns exits to the firmware's next boot device. host and port must
// come from ParseMAASRackAddress.
func RenderBootISOScript(host string, port int) string {
	return fmt.Sprintf(`#!ipxe

set maas_rack %s

:start
dhcp || goto retry
set next-server ${maas_rack}
chain http://${next-server}:%d/ipxe.cfg || goto returned

:retry
prompt --key 0x02 --timeout 10000 Press Ctrl-B for shell, or wait to retry... && shell ||
goto start

:returned
iseq ${platform} efi && exit 1 || goto retry
`, host, port)
}
