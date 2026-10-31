package genfsimg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

const testBootISOID = "6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11"

// fakeIPXEDir is an iPXE asset directory with placeholder assets and a VERSION file.
func fakeIPXEDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"ipxe.lkrn": "lkrn", "ipxe.efi": "efi", "genfsimg": "#!/bin/sh\n", "VERSION": "v2.0.0 (12798ec)\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeTools makes every tool, the ISOLINUX boot sector, and the GRUB modules look installed.
func fakeTools(b *GenfsimgBuilder) {
	b.lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	realStat := b.statFile
	b.statFile = func(path string) (os.FileInfo, error) {
		if path == isolinuxCandidates[0] || path == filepath.Join(grubModuleDir, "normal.mod") {
			return realStat(filepath.Join(b.ipxeDir, "ipxe.lkrn"))
		}
		return realStat(path)
	}
}

// fakeBuildRunner stands in for grub-mkstandalone and genfsimg: it records the GRUB configuration
// and iPXE script each was given and writes the image each would produce, with iso as the ISO's
// bytes. failGenfsimg makes genfsimg fail with a tool-like output.
type fakeBuildRunner struct {
	t            *testing.T
	iso          string
	failGenfsimg bool
	grubArgs     []string
	genfsimgArgs []string
	grubConfig   string
	script       string
}

func (f *fakeBuildRunner) run(_ context.Context, workDir, name string, args ...string) ([]byte, error) {
	if !strings.HasPrefix(filepath.Base(workDir), ".build-") {
		f.t.Errorf("%s ran in %s, want the private build directory", name, workDir)
	}
	if name == "grub-mkstandalone" {
		f.grubArgs = args
		config := strings.TrimPrefix(args[len(args)-1], "boot/grub/grub.cfg=")
		raw, err := os.ReadFile(config)
		if err != nil {
			return nil, err
		}
		f.grubConfig = string(raw)
		return nil, os.WriteFile(args[5], []byte("GRUB-EFI"), 0o600)
	}
	f.genfsimgArgs = append([]string{name}, args...)
	raw, err := os.ReadFile(args[3])
	if err != nil {
		return nil, err
	}
	f.script = string(raw)
	if f.failGenfsimg {
		return []byte("line 1\nline 2\nline 3\nxorriso : FAILURE : no space\n"), errors.New("exit status 1")
	}
	return nil, os.WriteFile(args[1], []byte(f.iso), 0o600)
}

var testBootISOContent = provisioningdomain.BootISOContent{IPXEScript: "#!ipxe\nshell\n", GRUBConfig: "net_bootp\n"}

// A build packs the GRUB configuration into a standalone x86_64-efi GRUB, runs genfsimg with the
// iPXE script, the BIOS iPXE kernel, and that GRUB in a private directory, then moves the ISO under
// its id and reports its size, digest and the iPXE version.
func TestGenfsimgBuilderBuild(t *testing.T) {
	dir, ipxeDir := t.TempDir(), fakeIPXEDir(t)
	builder := NewGenfsimgBuilder(dir, ipxeDir, "http://192.0.2.1/")
	fakeTools(builder)
	runner := &fakeBuildRunner{t: t, iso: "ISO-BYTES"}
	builder.run = runner.run

	artifact, err := builder.Build(context.Background(), testBootISOID, testBootISOContent)
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if len(runner.grubArgs) < 6 || runner.grubArgs[0] != "-O" || runner.grubArgs[1] != "x86_64-efi" || runner.grubArgs[3] != grubModuleDir ||
		filepath.Base(runner.grubArgs[5]) != "grubx64.efi" || runner.grubConfig != "net_bootp\n" {
		t.Errorf("grub-mkstandalone args = %v with config %q, want an x86_64-efi image embedding the GRUB configuration", runner.grubArgs, runner.grubConfig)
	}
	got := runner.genfsimgArgs
	if len(got) != 7 || got[0] != filepath.Join(ipxeDir, "genfsimg") || got[1] != "-o" || got[3] != "-s" ||
		got[5] != filepath.Join(ipxeDir, "ipxe.lkrn") || got[6] != runner.grubArgs[5] {
		t.Errorf("genfsimg args = %v, want the iPXE BIOS kernel and the built GRUB", got)
	}
	if filepath.Base(got[4]) != "autoexec.ipxe" || runner.script != "#!ipxe\nshell\n" {
		t.Errorf("script %s = %q, want autoexec.ipxe with the rendered script", got[4], runner.script)
	}
	sum := sha256.Sum256([]byte("ISO-BYTES"))
	if artifact.SizeBytes != 9 || artifact.SHA256 != hex.EncodeToString(sum[:]) || artifact.IPXEVersion != "v2.0.0 (12798ec)" {
		t.Errorf("artifact = %+v", artifact)
	}
	path, ok := builder.FilePath(testBootISOID)
	if !ok || path != filepath.Join(dir, testBootISOID, BootISOFileName) || !builder.Served(testBootISOID) {
		t.Errorf("FilePath = %q, %v; Served = %v; want the stored ISO", path, ok, builder.Served(testBootISOID))
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("ISO file = %v, %v; want world-readable for the file route", info, err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".build-*")); len(leftovers) != 0 {
		t.Errorf("build directories left behind: %v", leftovers)
	}
	if got := builder.URL(testBootISOID); got != "http://192.0.2.1/boot-media/ipxe/"+testBootISOID+"/swallow-ipxe.iso" {
		t.Errorf("URL = %q", got)
	}

	if err := builder.Remove(testBootISOID); err != nil || builder.Served(testBootISOID) {
		t.Errorf("Remove = %v, served afterwards = %v", err, builder.Served(testBootISOID))
	}
}

// Building an id that already has an ISO replaces the file in place, and a rebuild whose tool
// fails keeps serving the previous file.
func TestGenfsimgBuilderRebuild(t *testing.T) {
	dir := t.TempDir()
	builder := NewGenfsimgBuilder(dir, fakeIPXEDir(t), "http://192.0.2.1")
	fakeTools(builder)
	runner := &fakeBuildRunner{t: t, iso: "ISO-V1"}
	builder.run = runner.run
	if _, err := builder.Build(context.Background(), testBootISOID, testBootISOContent); err != nil {
		t.Fatalf("first Build error = %v", err)
	}
	runner.iso = "ISO-V2"
	if _, err := builder.Build(context.Background(), testBootISOID, testBootISOContent); err != nil {
		t.Fatalf("rebuild error = %v", err)
	}
	path, _ := builder.FilePath(testBootISOID)
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "ISO-V2" {
		t.Errorf("after rebuild the file holds %q, %v; want the new ISO", raw, err)
	}

	runner.failGenfsimg = true
	if _, err := builder.Build(context.Background(), testBootISOID, testBootISOContent); !errors.Is(err, provisioningdomain.ErrBootISOBuildFailed) {
		t.Fatalf("failed rebuild error = %v, want ErrBootISOBuildFailed", err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "ISO-V2" {
		t.Errorf("after a failed rebuild the file holds %q, %v; want the previous ISO", raw, err)
	}
}

// A failed genfsimg run reports the tool's last lines and leaves nothing behind; a malformed id
// never reaches the file system.
func TestGenfsimgBuilderBuildFailures(t *testing.T) {
	dir := t.TempDir()
	builder := NewGenfsimgBuilder(dir, fakeIPXEDir(t), "http://192.0.2.1")
	fakeTools(builder)
	builder.run = (&fakeBuildRunner{t: t, failGenfsimg: true}).run
	_, err := builder.Build(context.Background(), testBootISOID, testBootISOContent)
	if !errors.Is(err, provisioningdomain.ErrBootISOBuildFailed) || !strings.Contains(err.Error(), "no space") || strings.Contains(err.Error(), "line 1") {
		t.Errorf("Build error = %v, want ErrBootISOBuildFailed with the last output lines", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left behind %v after a failed build", entries)
	}

	for _, id := range []string{"../escape", "", "6B3F0C1E-4F7A-4F53-9D2A-2A7F1D0C9E11"} {
		if _, err := builder.Build(context.Background(), id, testBootISOContent); !errors.Is(err, provisioningdomain.ErrBootISOBuildFailed) {
			t.Errorf("Build(%q) error = %v, want ErrBootISOBuildFailed", id, err)
		}
		if _, ok := builder.FilePath(id); ok {
			t.Errorf("FilePath(%q) accepted a malformed id", id)
		}
	}
}

// Available names the first missing piece so the dashboard can tell the operator what to fix.
func TestGenfsimgBuilderAvailable(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(b *GenfsimgBuilder)
		reason string
	}{
		{name: "ready", mutate: func(*GenfsimgBuilder) {}},
		{name: "no base URL", mutate: func(b *GenfsimgBuilder) { b.baseURL = "" }, reason: "SWALLOW_API_BOOT_MEDIA_BASE_URL"},
		{name: "no directory", mutate: func(b *GenfsimgBuilder) { b.dir = "" }, reason: "SWALLOW_API_BOOT_MEDIA_DIR"},
		{name: "missing asset", mutate: func(b *GenfsimgBuilder) { _ = os.Remove(filepath.Join(b.ipxeDir, "ipxe.lkrn")) }, reason: "ipxe.lkrn"},
		{name: "no grub-mkstandalone", mutate: func(b *GenfsimgBuilder) {
			b.lookPath = func(name string) (string, error) {
				if name == "grub-mkstandalone" {
					return "", exec.ErrNotFound
				}
				return "/usr/bin/" + name, nil
			}
		}, reason: "grub-mkstandalone"},
		{name: "no GRUB modules", mutate: func(b *GenfsimgBuilder) {
			b.statFile = func(path string) (os.FileInfo, error) {
				if path == filepath.Join(grubModuleDir, "normal.mod") {
					return nil, os.ErrNotExist
				}
				return os.Stat(filepath.Join(b.ipxeDir, "ipxe.lkrn"))
			}
		}, reason: "grub-efi-amd64-bin"},
		{name: "no mtools", mutate: func(b *GenfsimgBuilder) {
			b.lookPath = func(name string) (string, error) {
				if name == "mformat" {
					return "", exec.ErrNotFound
				}
				return "/usr/bin/" + name, nil
			}
		}, reason: "mtools"},
		{name: "no xorriso", mutate: func(b *GenfsimgBuilder) {
			b.lookPath = func(name string) (string, error) {
				if strings.Contains(name, "iso") {
					return "", exec.ErrNotFound
				}
				return "/usr/bin/" + name, nil
			}
		}, reason: "xorriso"},
		{name: "no isolinux", mutate: func(b *GenfsimgBuilder) {
			b.statFile = func(path string) (os.FileInfo, error) {
				for _, candidate := range isolinuxCandidates {
					if path == candidate {
						return nil, os.ErrNotExist
					}
				}
				return os.Stat(path)
			}
		}, reason: "ISOLINUX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewGenfsimgBuilder(t.TempDir(), fakeIPXEDir(t), "http://192.0.2.1")
			fakeTools(builder)
			tc.mutate(builder)
			err := builder.Available()
			if tc.reason == "" {
				if err != nil {
					t.Fatalf("Available() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, provisioningdomain.ErrBootISOBuilderUnavailable) || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("Available() = %v, want ErrBootISOBuilderUnavailable naming %q", err, tc.reason)
			}
		})
	}
}

// The reason is exactly the one sentence clients show; neither the sentinel's wording nor the
// underlying OS error is prepended or appended.
func TestGenfsimgBuilderUnavailableReasonIsOneSentence(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	builder := NewGenfsimgBuilder(notADir, fakeIPXEDir(t), "http://192.0.2.1")
	fakeTools(builder)
	err := builder.Available()
	if want := "the Boot Media directory " + notADir + " is not writable"; err == nil || err.Error() != want {
		t.Errorf("Available() = %v, want %q", err, want)
	}
	if !errors.Is(err, provisioningdomain.ErrBootISOBuilderUnavailable) {
		t.Errorf("Available() = %v, want it to match ErrBootISOBuilderUnavailable", err)
	}
}

// With real iPXE assets (SWALLOW_TEST_IPXE_DIR, as in the api-server image), GRUB, and the
// packaging tools installed, the builder produces an ISO that carries the iPXE script as
// autoexec.ipxe and a GRUB embedding the UEFI configuration (stored uncompressed in its memdisk).
func TestGenfsimgBuilderRealBuild(t *testing.T) {
	ipxeDir := os.Getenv("SWALLOW_TEST_IPXE_DIR")
	if ipxeDir == "" {
		t.Skip("SWALLOW_TEST_IPXE_DIR is not set")
	}
	builder := NewGenfsimgBuilder(t.TempDir(), ipxeDir, "http://192.0.2.1")
	if err := builder.Available(); err != nil {
		t.Skipf("builder unavailable: %v", err)
	}
	content := provisioningdomain.RenderBootISOContent("10.1.0.5", provisioningdomain.DefaultMAASRackPort)
	artifact, err := builder.Build(context.Background(), testBootISOID, content)
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if artifact.SizeBytes < 1<<20 {
		t.Errorf("ISO is %d bytes, want an iPXE image of at least 1 MiB", artifact.SizeBytes)
	}
	path, _ := builder.FilePath(testBootISOID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "set maas_rack 10.1.0.5") {
		t.Error("the ISO does not carry the rendered iPXE script")
	}
	if !strings.Contains(string(raw), "set maas_rack=10.1.0.5") || !strings.Contains(string(raw), "configfile /grub/grub.cfg-$mac") {
		t.Error("the ISO does not carry a GRUB with the rendered UEFI configuration")
	}
}
