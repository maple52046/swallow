package infra

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

// fakeTools makes every tool and the ISOLINUX boot sector look installed.
func fakeTools(b *GenfsimgBuilder) {
	b.lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	realStat := b.statFile
	b.statFile = func(path string) (os.FileInfo, error) {
		if path == isolinuxCandidates[0] {
			return realStat(filepath.Join(b.ipxeDir, "ipxe.lkrn"))
		}
		return realStat(path)
	}
}

// A build runs genfsimg with the script and both iPXE binaries in a private directory, then moves
// the ISO under its id and reports its size, digest and the iPXE version.
func TestGenfsimgBuilderBuild(t *testing.T) {
	dir, ipxeDir := t.TempDir(), fakeIPXEDir(t)
	builder := NewGenfsimgBuilder(dir, ipxeDir, "http://192.0.2.1/")
	fakeTools(builder)
	var gotArgs []string
	var gotScript string
	builder.run = func(_ context.Context, workDir, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		raw, err := os.ReadFile(args[3])
		if err != nil {
			return nil, err
		}
		gotScript = string(raw)
		if filepath.Dir(args[1]) != workDir || !strings.HasPrefix(filepath.Base(workDir), ".build-") {
			t.Errorf("output %s not in the private build directory %s", args[1], workDir)
		}
		return nil, os.WriteFile(args[1], []byte("ISO-BYTES"), 0o600)
	}

	artifact, err := builder.Build(context.Background(), testBootISOID, "#!ipxe\nshell\n")
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	want := []string{filepath.Join(ipxeDir, "genfsimg"), "-o", "", "-s", "", filepath.Join(ipxeDir, "ipxe.lkrn"), filepath.Join(ipxeDir, "ipxe.efi")}
	if len(gotArgs) != len(want) || gotArgs[0] != want[0] || gotArgs[1] != "-o" || gotArgs[3] != "-s" || gotArgs[5] != want[5] || gotArgs[6] != want[6] {
		t.Errorf("genfsimg args = %v, want %v", gotArgs, want)
	}
	if filepath.Base(gotArgs[4]) != "autoexec.ipxe" || gotScript != "#!ipxe\nshell\n" {
		t.Errorf("script %s = %q, want autoexec.ipxe with the rendered script", gotArgs[4], gotScript)
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

// A failed genfsimg run reports the tool's last lines and leaves nothing behind; a malformed id
// never reaches the file system.
func TestGenfsimgBuilderBuildFailures(t *testing.T) {
	dir := t.TempDir()
	builder := NewGenfsimgBuilder(dir, fakeIPXEDir(t), "http://192.0.2.1")
	fakeTools(builder)
	builder.run = func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte("line 1\nline 2\nline 3\nxorriso : FAILURE : no space\n"), errors.New("exit status 1")
	}
	_, err := builder.Build(context.Background(), testBootISOID, "#!ipxe\n")
	if !errors.Is(err, provisioningdomain.ErrBootISOBuildFailed) || !strings.Contains(err.Error(), "no space") || strings.Contains(err.Error(), "line 1") {
		t.Errorf("Build error = %v, want ErrBootISOBuildFailed with the last output lines", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left behind %v after a failed build", entries)
	}

	for _, id := range []string{"../escape", "", "6B3F0C1E-4F7A-4F53-9D2A-2A7F1D0C9E11"} {
		if _, err := builder.Build(context.Background(), id, "#!ipxe\n"); !errors.Is(err, provisioningdomain.ErrBootISOBuildFailed) {
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
		{name: "missing asset", mutate: func(b *GenfsimgBuilder) { _ = os.Remove(filepath.Join(b.ipxeDir, "ipxe.efi")) }, reason: "ipxe.efi"},
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

// With real iPXE assets (SWALLOW_TEST_IPXE_DIR, as in the api-server image) and the packaging
// tools installed, the builder produces an ISO that carries the script as autoexec.ipxe.
func TestGenfsimgBuilderRealBuild(t *testing.T) {
	ipxeDir := os.Getenv("SWALLOW_TEST_IPXE_DIR")
	if ipxeDir == "" {
		t.Skip("SWALLOW_TEST_IPXE_DIR is not set")
	}
	builder := NewGenfsimgBuilder(t.TempDir(), ipxeDir, "http://192.0.2.1")
	if err := builder.Available(); err != nil {
		t.Skipf("builder unavailable: %v", err)
	}
	script := provisioningdomain.RenderBootISOScript("10.1.0.5", provisioningdomain.DefaultMAASRackPort)
	artifact, err := builder.Build(context.Background(), testBootISOID, script)
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
		t.Error("the ISO does not carry the rendered script")
	}
}
