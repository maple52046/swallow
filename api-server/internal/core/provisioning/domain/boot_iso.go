package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BootISO is a swallow-built network boot ISO for one provisioner Integration (decision 047): a
// machine that boots it takes an address from the site's own DHCP and then chains to that
// provisioner's boot server. A Server's Boot Media mounts one through its BMC.
//
// Script (the script BIOS firmware runs) and GRUBConfig (the GRUB configuration UEFI firmware runs)
// are rendered by the provisioner's BootChainRenderer from its fixed templates; users never
// write them. The file lives outside this record, in the Boot Media directory, under the ISO's ID.
// A rebuild re-renders both with the current templates and replaces the file in place, so the ID
// and the URL Boot Media mounts never change (decision 047).
type BootISO struct {
	ID            string
	Name          string
	IntegrationID string
	// RackAddress is the provisioner's boot server as the user gave it; its format is the
	// provisioner's (BootChainRenderer).
	RackAddress string
	// ChainURL is where the script chains after DHCP on BIOS firmware, derived from RackAddress.
	ChainURL string
	// UEFIChainURL is what the ISO's GRUB loads on UEFI firmware. It is meaningful only when
	// GRUBConfig is set: an ISO built before decision 047 has no GRUB and sends UEFI firmware
	// through the BIOS path, so it needs a rebuild.
	UEFIChainURL string
	// Script is the iPXE script (BIOS); GRUBConfig the GRUB configuration (UEFI).
	Script      string
	GRUBConfig  string
	IPXEVersion string
	SizeBytes   int64
	SHA256      string
	CreatedAt   time.Time
	CreatedBy   string
	// RebuiltAt and RebuiltBy record the latest rebuild; zero and "" for an ISO never rebuilt.
	RebuiltAt time.Time
	RebuiltBy string
}

// BootISOFilter narrows a Boot ISO listing. Zero values mean no constraint.
type BootISOFilter struct {
	IntegrationID  string
	IntegrationIDs []string
}

// BootISORepository persists Boot ISO records. Names are unique per Integration, compared
// case-insensitively; Create returns ErrBootISONameTaken for a duplicate. FindByID, Update, and
// Delete return ErrBootISONotFound for an unknown id. List orders by name.
type BootISORepository interface {
	Create(ctx context.Context, iso *BootISO) error
	FindByID(ctx context.Context, id string) (*BootISO, error)
	List(ctx context.Context, filter BootISOFilter) ([]*BootISO, error)
	// Update stores a rebuild: the script, GRUB configuration, chain URLs, artifact facts, and
	// RebuiltAt/RebuiltBy of iso. Identity, name, Integration, rack address, and creation fields
	// are never changed.
	Update(ctx context.Context, iso *BootISO) error
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
	// Build writes the ISO for id — content.IPXEScript as the BIOS path's autoexec.ipxe and a GRUB
	// with content.GRUBConfig embedded as the UEFI path's boot loader — atomically replacing a file
	// id already has (a rebuild). A failure wraps ErrBootISOBuildFailed with a short summary of the
	// tool's output and leaves no new file; a previous file stays served.
	Build(ctx context.Context, id string, content BootISOContent) (BootISOArtifact, error)
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
// is only Reason — one user-facing sentence such as "the Boot Media directory … is not
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

// BootISOContent is what a Boot ISO is built from: the iPXE script BIOS firmware runs and the GRUB
// configuration UEFI firmware runs (decision 047).
type BootISOContent struct {
	IPXEScript string
	GRUBConfig string
}

// BootChain is what a Boot ISO chains to on one provisioner: the URL each firmware path loads and
// the content that loads them.
type BootChain struct {
	ChainURL     string
	UEFIChainURL string
	Content      BootISOContent
}

// BootChainRenderer is the provisioner capability Boot ISOs are built from: it validates the
// user's boot server address in the provisioner's own format and renders the content that
// chains to it. A failure wraps ErrInvalidBootISO.
type BootChainRenderer interface {
	RenderBootChain(address string) (BootChain, error)
}

// BootChainRenderers are the renderers of the provisioners this installation includes, by the
// provider kind of their Integration; a provisioner without one cannot have Boot ISOs.
type BootChainRenderers map[string]BootChainRenderer
