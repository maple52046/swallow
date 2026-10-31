package genfsimg

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

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
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

// grubModuleDir is where grub-efi-amd64-bin installs the x86_64-efi modules grub-mkstandalone packs.
const grubModuleDir = "/usr/lib/grub/x86_64-efi"

// GenfsimgBuilder builds Boot ISOs with iPXE's genfsimg (decisions 049, 057) and owns their files
// under dir/<isoId>/swallow-ipxe.iso.
//
// BIOS firmware boots the prebuilt ipxe.lkrn with the iPXE script as initrd=autoexec.ipxe (genfsimg
// -s). UEFI firmware boots a GRUB built for the ISO by grub-mkstandalone with the GRUB
// configuration embedded in its memdisk, which genfsimg places as EFI/BOOT/BOOTX64.EFI; iPXE's own
// EFI build is not used, because a GRUB it chains cannot reach the network (decision 057). No
// compiler is needed: both tools only package prebuilt binaries and modules. Each build
// works in a private directory inside dir and renames the finished file into place, so the
// served URL never exposes a half-written ISO and concurrent builds of different ids do not
// interfere. Building an id that already has a file is a rebuild: the rename replaces the file
// atomically, and a failure leaves the previous file served. It is safe for concurrent use;
// concurrent builds of the same id are serialized by the caller. The API process is its only
// user; the worker never touches the files.
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
	for _, asset := range []string{"ipxe.lkrn", "genfsimg"} {
		info, err := b.statFile(filepath.Join(b.ipxeDir, asset))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return unavailable("the iPXE asset %s is missing from %s; use the swallow api-server image", asset, b.ipxeDir)
		}
	}
	if _, err := b.lookPath("grub-mkstandalone"); err != nil {
		return unavailable("grub-mkstandalone (grub-common) is not installed")
	}
	if info, err := b.statFile(filepath.Join(grubModuleDir, "normal.mod")); err != nil || !info.Mode().IsRegular() {
		return unavailable("the GRUB x86_64-efi modules (grub-efi-amd64-bin) are missing from %s", grubModuleDir)
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

// Build implements provisioningdomain.BootISOBuilder. It runs grub-mkstandalone, then genfsimg, in
// one private work directory; both share the build timeout.
func (b *GenfsimgBuilder) Build(ctx context.Context, id string, content provisioningdomain.BootISOContent) (provisioningdomain.BootISOArtifact, error) {
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
	if err := os.WriteFile(scriptPath, []byte(content.IPXEScript), 0o644); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: write script: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	grubConfigPath := filepath.Join(work, "grub.cfg")
	if err := os.WriteFile(grubConfigPath, []byte(content.GRUBConfig), 0o644); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: write GRUB configuration: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	grubImage := filepath.Join(work, "grubx64.efi")
	output := filepath.Join(work, BootISOFileName)
	buildCtx, cancel := context.WithTimeout(ctx, bootISOBuildTimeout)
	defer cancel()
	failed := func(tool string, out []byte, err error) error {
		summary := summarizeToolOutput(out, err)
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			summary = "timed out after " + bootISOBuildTimeout.String()
		}
		return fmt.Errorf("%w: %s: %s", provisioningdomain.ErrBootISOBuildFailed, tool, summary)
	}
	// The configuration goes into GRUB's memdisk as /boot/grub/grub.cfg, which GRUB reads at start;
	// locales, fonts, and themes are left out because the UEFI path only prints status lines.
	if out, err := b.run(buildCtx, work, "grub-mkstandalone",
		"-O", "x86_64-efi", "-d", grubModuleDir, "-o", grubImage,
		"--locales=", "--fonts=", "--themes=",
		"boot/grub/grub.cfg="+grubConfigPath,
	); err != nil {
		return provisioningdomain.BootISOArtifact{}, failed("grub-mkstandalone", out, err)
	}
	if out, err := b.run(buildCtx, work, filepath.Join(b.ipxeDir, "genfsimg"),
		"-o", output, "-s", scriptPath,
		filepath.Join(b.ipxeDir, "ipxe.lkrn"), grubImage,
	); err != nil {
		return provisioningdomain.BootISOArtifact{}, failed("genfsimg", out, err)
	}

	size, sum, err := fileDigest(output)
	if err != nil || size == 0 {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: genfsimg produced no ISO", provisioningdomain.ErrBootISOBuildFailed)
	}
	if err := os.Chmod(output, 0o644); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	final := filepath.Join(b.dir, id)
	_, statErr := b.statFile(final)
	existed := statErr == nil
	if err := os.MkdirAll(final, 0o755); err != nil {
		return provisioningdomain.BootISOArtifact{}, fmt.Errorf("%w: %v", provisioningdomain.ErrBootISOBuildFailed, err)
	}
	if err := os.Rename(output, filepath.Join(final, BootISOFileName)); err != nil {
		// A rebuild must keep serving the previous ISO, so only a directory this build created is
		// removed.
		if !existed {
			_ = os.RemoveAll(final)
		}
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
