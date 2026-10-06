package application

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// The unauthenticated download paths of Server Enrollment (contract server-enrollment.md). A host
// that keeps its OS fetches the script, and the script fetches the CLI, from the installation
// itself; neither file holds a secret.
const (
	HostEnrollmentScriptPath = "/downloads/swallow-enroll.sh"
	CLIDownloadPath          = "/downloads/swallow"
)

// HostEnrollmentBundle is what an admin hands to a host that keeps its OS so it enrolls itself
// into one provisioner (contract server-enrollment.md). Token is the provisioner's credential: it
// leaves swallow on purpose, because the provider's registration needs it, and must never be
// logged or cached. Command embeds it.
type HostEnrollmentBundle struct {
	IntegrationID string `json:"integrationId"`
	ProviderKind  string `json:"providerKind"`
	Endpoint      string `json:"endpoint"`
	Token         string `json:"token"`
	// Command is the one line to run on the host: download the enrollment script from swallow and
	// run it as root, which downloads the swallow CLI and runs `swallow servers enroll`.
	Command string `json:"command"`
}

// HostEnrollmentUseCase produces the existing-OS enrollment bundle of one provisioner Integration
// (decision 053). The provider adapter says where the provider is and which credential it needs;
// this use case phrases the command an operator copies.
//
// Authorization is the caller's concern: the bundle carries a credential, so only admin routes may
// expose it. A provisioner without existing-host enrollment is a ProviderErrorRejected refusal.
type HostEnrollmentUseCase struct {
	providers provisioningdomain.ProviderFactory
}

// NewHostEnrollmentUseCase wires existing-OS enrollment.
func NewHostEnrollmentUseCase(providers provisioningdomain.ProviderFactory) *HostEnrollmentUseCase {
	return &HostEnrollmentUseCase{providers: providers}
}

// Bundle returns the endpoint, credential, and command for integrationID. swallowURL is the
// address the host reaches this installation at (the Dashboard sends its own origin); it must be
// an absolute http(s) URL without path, query, or credentials, else ErrInvalidEnrollmentRequest.
// Other errors are the factory's (unknown Integration, not a provisioner, no credential) or the
// rejection above.
func (uc *HostEnrollmentUseCase) Bundle(ctx context.Context, integrationID, swallowURL string) (*HostEnrollmentBundle, error) {
	base, err := NormalizeSwallowURL(swallowURL)
	if err != nil {
		return nil, err
	}
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	enroller, ok := provider.(provisioningdomain.ExistingHostEnroller)
	if !ok {
		return nil, unsupported("enrolling a host that keeps its operating system")
	}
	enrollment, err := enroller.ExistingHostEnrollment(ctx)
	if err != nil {
		return nil, err
	}
	kind := provider.Name()
	return &HostEnrollmentBundle{
		IntegrationID: integrationID,
		ProviderKind:  kind,
		Endpoint:      enrollment.Endpoint,
		Token:         enrollment.Token,
		Command: "curl -fsSL " + shellQuote(base+HostEnrollmentScriptPath) + " | sudo sh -s -- --provisioner=" + kind +
			" --endpoint " + shellQuote(enrollment.Endpoint) + " --token " + shellQuote(enrollment.Token),
	}, nil
}

// NormalizeSwallowURL validates the address a host reaches swallow at and drops a trailing slash.
func NormalizeSwallowURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "", fmt.Errorf("%w: swallowUrl must be an absolute http(s) URL without path, query, or credentials", provisioningdomain.ErrInvalidEnrollmentRequest)
	}
	return trimmed, nil
}

// HostEnrollmentScript renders the enrollment script served at HostEnrollmentScriptPath, with the
// CLI download address built in. swallowURL is the address the host used to fetch the script, so
// the CLI is fetched the same way. The script holds no credential: the provider, endpoint, and
// token are its arguments, passed straight to `swallow servers enroll`.
func HostEnrollmentScript(swallowURL string) string {
	return strings.Replace(hostEnrollmentScriptTemplate, "@SWALLOW_URL@", shellQuote(strings.TrimRight(swallowURL, "/")), 1)
}

// hostEnrollmentScriptTemplate is the fixed enrollment script (contract server-enrollment.md):
// download the swallow CLI from the installation into a private temporary directory, run
// `swallow servers enroll` with the given arguments, and remove the CLI again.
const hostEnrollmentScriptTemplate = `#!/bin/sh
# swallow Server Enrollment. Downloads the swallow CLI from this installation, enrolls this host
# into its provisioner while it keeps its OS, then removes the CLI. Run as root:
#   curl -fsSL <swallow>/downloads/swallow-enroll.sh | sudo sh -s -- --provisioner=maas --endpoint <url> --token <key>
set -eu

swallow_url=@SWALLOW_URL@

case "$(uname -m)" in
  x86_64|amd64) ;;
  *) echo "swallow publishes its CLI for x86_64 hosts only" >&2; exit 1 ;;
esac
[ "$(id -u)" -eq 0 ] || { echo "run as root: curl ... | sudo sh -s -- ..." >&2; exit 1; }

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
trap 'exit 1' INT TERM

echo "Downloading the swallow CLI from $swallow_url"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "$workdir/swallow" "$swallow_url/downloads/swallow"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "$workdir/swallow" "$swallow_url/downloads/swallow"
else
  echo "curl or wget is required" >&2
  exit 1
fi
chmod 0700 "$workdir/swallow"

HOME="$workdir" "$workdir/swallow" servers enroll "$@"
`

// shellQuote quotes value for a POSIX shell so a pasted command passes it as one argument.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
