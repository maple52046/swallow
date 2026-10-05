package infra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// BootISOFileName is the file name of every Boot ISO, under its own directory. The directory
// component is load-bearing: AMI MegaRAC BMCs do not mount an image at a server's root, so the
// URL is .../<isoId>/swallow-ipxe.iso (decision 047).
const BootISOFileName = "swallow-ipxe.iso"

// bootISOBuildTimeout bounds one genfsimg run; packaging a few MiB takes seconds.
const bootISOBuildTimeout = 2 * time.Minute

// bootISOIDPattern is the only id shape a Boot ISO has (uuid.NewString). Checking it before any
// path is built keeps file access confined to the Boot Media directory: an id can never carry a
// separator or "..".
var bootISOIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isolinuxCandidates are where Debian and other distributions install the ISOLINUX boot sector
// genfsimg needs; the list mirrors genfsimg's own search.
var isolinuxCandidates = []string{
	"/usr/lib/ISOLINUX/isolinux.bin",
	"/usr/lib/syslinux/isolinux.bin",
	"/usr/lib/syslinux/bios/isolinux.bin",
	"/usr/share/syslinux/isolinux.bin",
	"/usr/local/share/syslinux/isolinux.bin",
}

// commandRunner runs one command in dir and returns its combined output; tests replace it.
type commandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// GenfsimgBuilder builds Boot ISOs with iPXE's genfsimg from prebuilt ipxe.lkrn and ipxe.efi
// (decision 049) and owns their files under dir/<isoId>/swallow-ipxe.iso.
//
// genfsimg -s copies the script to the ESP root as autoexec.ipxe, which iPXE's EFI build runs,
// and passes it to the BIOS kernel as initrd=autoexec.ipxe, so no compiler is needed. Each build
// works in a private directory inside dir and renames the finished file into place, so the
// served URL never exposes a half-written ISO and concurrent builds of different ids do not
// interfere. It is safe for concurrent use. The API process is its only user; the worker never
// touches the files.
type GenfsimgBuilder struct {
	dir      string
	ipxeDir  string
	baseURL  string
	run      commandRunner
	lookPath func(string) (string, error)
	statFile func(string) (os.FileInfo, error)
}

// NewGenfsimgBuilder returns a builder writing under dir with the iPXE assets in ipxeDir;
// baseURL is the Boot Media base URL BMCs reach the API at. Nothing is checked here: a missing
// piece only makes Available report why, it never stops the API.
func NewGenfsimgBuilder(dir, ipxeDir, baseURL string) *GenfsimgBuilder {
	return &GenfsimgBuilder{
		dir: strings.TrimSpace(dir), ipxeDir: strings.TrimSpace(ipxeDir),
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		run:     execRunner, lookPath: exec.LookPath, statFile: os.Stat,
	}
}

// Available implements provisioningdomain.BootISOBuilder.
func (b *GenfsimgBuilder) Available() error {
	unavailable := func(format string, args ...any) error {
		return &provisioningdomain.BootISOBuilderUnavailableError{Reason: fmt.Sprintf(format, args...)}
	}
	if b.baseURL == "" {
		return unavailable("set api.bootMedia.baseURL (SWALLOW_API_BOOT_MEDIA_BASE_URL) to the HTTP address BMCs use to reach swallow")
	}
	if b.dir == "" {
		return unavailable("set api.bootMedia.dir (SWALLOW_API_BOOT_MEDIA_DIR) to a writable directory")
	}
	if err := b.checkWritable(); err != nil {
		return unavailable("the Boot Media directory %s is not writable", b.dir)
	}
	for _, asset := range []string{"ipxe.lkrn", "ipxe.efi", "genfsimg"} {
		info, err := b.statFile(filepath.Join(b.ipxeDir, asset))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return unavailable("the iPXE asset %s is missing from %s; use the swallow api-server image", asset, b.ipxeDir)
		}
	}
	for _, tool := range []string{"mformat", "mcopy"} {
		if _, err := b.lookPath(tool); err != nil {
			return unavailable("%s (mtools) is not installed", tool)
		}
	}
	if !b.anyTool("xorrisofs", "genisoimage", "mkisofs") {
		return unavailable("xorriso is not installed")
	}
	if !b.anyFile(isolinuxCandidates...) {
		return unavailable("ISOLINUX (isolinux.bin) is not installed")
	}
	return nil
}

func (b *GenfsimgBuilder) checkWritable() error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(b.dir, ".write-check-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name)
}

func (b *GenfsimgBuilder) anyTool(names ...string) bool {
	for _, name := range names {
		if _, err := b.lookPath(name); err == nil {
			return true
		}
	}
	return false
}

func (b *GenfsimgBuilder) anyFile(paths ...string) bool {
	for _, path := range paths {
		if info, err := b.statFile(path); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// Build implements provisioningdomain.BootISOBuilder.
func (b *GenfsimgBuilder) Build(ctx context.Context, id, script string) (provisioningdomain.BootISOArtifact, error) {
	if !bootISOIDPattern.MatchString(id) {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: invalid id", provisioningdomain.ErrBootISOBuildFailed)
	}
	if err := b.Available(); err != nil {
		return provisioningdomain.BootISOArtifact{}, err
	}
	work, err := os.MkdirTemp(b.dir, ".build-")
	if err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: create build directory: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	defer os.RemoveAll(work)

	scriptPath := filepath.Join(work, "autoexec.ipxe")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: write script: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	output := filepath.Join(work, BootISOFileName)
	buildCtx, cancel := context.WithTimeout(ctx, bootISOBuildTimeout)
	defer cancel()
	if out, err := b.run(buildCtx, work, filepath.Join(b.ipxeDir, "genfsimg"),
		"-o", output, "-s", scriptPath,
		filepath.Join(b.ipxeDir, "ipxe.lkrn"), filepath.Join(b.ipxeDir, "ipxe.efi"),
	); err != nil {
		summary := summarizeToolOutput(out, err)
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			summary = "timed out after " + bootISOBuildTimeout.String()
		}
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: genfsimg: %s", provisioningdomain.ErrBootISOBuildFailed, summary)
	}

	size, sum, err := fileDigest(output)
	if err != nil || size == 0 {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: genfsimg produced no ISO", provisioningdomain.ErrBootISOBuildFailed)
	}
	if err := os.Chmod(output, 0o644); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	final := filepath.Join(b.dir, id)
	if err := os.MkdirAll(final, 0o755); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	if err := os.Rename(output, filepath.Join(final, BootISOFileName)); err != nil {
		_ = os.RemoveAll(final)
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	return provisioningdomain.BootISOArtifact{SizeBytes: size, SHA256: sum, IPXEVersion: b.ipxeVersion()}, nil
}

// Remove implements provisioningdomain.BootISOBuilder.
func (b *GenfsimgBuilder) Remove(id string) error {
	if !bootISOIDPattern.MatchString(id) || b.dir == "" {
		return nil
	}
	return os.RemoveAll(filepath.Join(b.dir, id))
}

// URL implements provisioningdomain.BootISOBuilder.
func (b *GenfsimgBuilder) URL(id string) string {
	if b.baseURL == "" {
		return ""
	}
	return b.baseURL + "/boot-media/ipxe/" + id + "/" + BootISOFileName
}

// FilePath returns where id's ISO is stored and whether id is a well-formed Boot ISO id. It
// does not check the file exists. The unauthenticated file route uses it, so it must never
// resolve outside the Boot Media directory.
func (b *GenfsimgBuilder) FilePath(id string) (string, bool) {
	if !bootISOIDPattern.MatchString(id) || b.dir == "" {
		return "", false
	}
	return filepath.Join(b.dir, id, BootISOFileName), true
}

// Served reports whether id's ISO file exists and is non-empty.
func (b *GenfsimgBuilder) Served(id string) bool {
	path, ok := b.FilePath(id)
	if !ok {
		return false
	}
	info, err := b.statFile(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// ipxeVersion reads the VERSION file the image writes next to the iPXE assets.
func (b *GenfsimgBuilder) ipxeVersion() string {
	raw, err := os.ReadFile(filepath.Join(b.ipxeDir, "VERSION"))
	if version := strings.TrimSpace(string(raw)); err == nil && version != "" {
		return version
	}
	return "unknown"
}

// fileDigest returns a file's size and SHA-256.
func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

// summarizeToolOutput keeps the last few lines of a failed tool's output for the error message,
// bounded so a noisy tool cannot flood the API response; the output holds no secret.
func summarizeToolOutput(out []byte, err error) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	summary := strings.Join(lines, "; ")
	if summary == "" {
		summary = err.Error()
	}
	if len(summary) > 400 {
		summary = summary[:400] + "…"
	}
	return summary
}
