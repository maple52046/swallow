package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	testDigest      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testFingerprint = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
)

// testSigningKey returns a valid signing key a test may change.
func testSigningKey() *ArtifactSigningKey {
	return &ArtifactSigningKey{URL: "https://packages.example.com/debian/gpg", Fingerprint: testFingerprint}
}

func validArtifacts() Artifacts {
	return Artifacts{
		Sources: []ArtifactSource{{
			Name: "example", Format: ArtifactFormatDeb, URL: "https://packages.example.com/debian",
			Suites: []string{"noble", "noble-updates"}, Components: []string{"main"}, Architectures: []string{"amd64"},
			SigningKey: testSigningKey(),
		}},
		Files: []FileArtifact{{
			Path: "tool/v1.2.3+build.1/tool-1.2.3~24.04_amd64.tar.gz", URL: "https://downloads.example.com/tool/v1.2.3/tool.tar.gz", SHA256: testDigest,
		}},
		Images: []string{
			"registry.example.com/team/app:1.2.3",
			"registry.example.com/library/base@sha256:" + testDigest,
			"registry.example.com/team/app:1.2.4@sha256:" + testDigest,
			"localhost:5000/app:dev",
			"mirror:5000/team/app_v2__x:v1-rc.1",
		},
		Python: []string{"Flask==3.1.3", "python-dateutil==2.9.0.post0", "pytz==2026.5"},
	}
}

func TestArtifactsValidateAcceptsWellFormedDeclarations(t *testing.T) {
	if err := validArtifacts().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	if err := (Artifacts{}).Validate(); err != nil {
		t.Errorf("empty Artifacts.Validate() = %v, want nil", err)
	}
	for _, source := range []ArtifactSource{
		{Name: "rpms", Format: ArtifactFormatRPM, URL: "http://packages.example.com/el9", Architectures: []string{"x86_64"}, SigningKey: testSigningKey()},
		{Name: "images", Format: ArtifactFormatOCI, URL: "https://registry.example.com"},
		{Name: "index", Format: ArtifactFormatPython, URL: "https://pypi.example.com/"},
		{Name: "downloads", Format: ArtifactFormatFile, URL: "https://downloads.example.com/"},
	} {
		if err := (Artifacts{Sources: []ArtifactSource{source}}).Validate(); err != nil {
			t.Errorf("Validate(%s source) = %v, want nil", source.Format, err)
		}
	}
}

func TestArtifactsValidateRefusesMalformedDeclarations(t *testing.T) {
	cases := map[string]func(*Artifacts){
		"source name not kebab-case":    func(a *Artifacts) { a.Sources[0].Name = "Example_Repo" },
		"unknown source format":         func(a *Artifacts) { a.Sources[0].Format = "tarball" },
		"relative source URL":           func(a *Artifacts) { a.Sources[0].URL = "packages.example.com/debian" },
		"source URL not http":           func(a *Artifacts) { a.Sources[0].URL = "ftp://packages.example.com/debian" },
		"source URL without host":       func(a *Artifacts) { a.Sources[0].URL = "https:///debian" },
		"source URL with user info":     func(a *Artifacts) { a.Sources[0].URL = "https://user:secret@packages.example.com/debian" },
		"source URL with query":         func(a *Artifacts) { a.Sources[0].URL = "https://packages.example.com/debian?token=x" },
		"source URL with empty query":   func(a *Artifacts) { a.Sources[0].URL = "https://packages.example.com/debian?" },
		"source URL with fragment":      func(a *Artifacts) { a.Sources[0].URL = "https://packages.example.com/debian#main" },
		"source URL with whitespace":    func(a *Artifacts) { a.Sources[0].URL = "https://packages.example.com/de bian" },
		"deb source without suites":     func(a *Artifacts) { a.Sources[0].Suites = nil },
		"deb source without components": func(a *Artifacts) { a.Sources[0].Components = nil },
		"suite with whitespace":         func(a *Artifacts) { a.Sources[0].Suites = []string{"noble updates"} },
		"duplicate component":           func(a *Artifacts) { a.Sources[0].Components = []string{"main", "main"} },
		"rpm source with suites": func(a *Artifacts) {
			a.Sources[0].Format = ArtifactFormatRPM
		},
		"oci source with architectures": func(a *Artifacts) {
			a.Sources[0] = ArtifactSource{Name: "images", Format: ArtifactFormatOCI, URL: "https://registry.example.com", Architectures: []string{"amd64"}}
		},
		"deb source without signing key": func(a *Artifacts) { a.Sources[0].SigningKey = nil },
		"rpm source without signing key": func(a *Artifacts) {
			a.Sources[0] = ArtifactSource{Name: "rpms", Format: ArtifactFormatRPM, URL: "https://packages.example.com/el9"}
		},
		"oci source with signing key": func(a *Artifacts) {
			a.Sources[0] = ArtifactSource{Name: "images", Format: ArtifactFormatOCI, URL: "https://registry.example.com", SigningKey: testSigningKey()}
		},
		"signing key over http":      func(a *Artifacts) { a.Sources[0].SigningKey.URL = "http://packages.example.com/debian/gpg" },
		"signing key without URL":    func(a *Artifacts) { a.Sources[0].SigningKey.URL = "" },
		"signing key URL with token": func(a *Artifacts) { a.Sources[0].SigningKey.URL = "https://packages.example.com/gpg?token=x" },
		"signing key URL with user info": func(a *Artifacts) {
			a.Sources[0].SigningKey.URL = "https://user:secret@packages.example.com/gpg"
		},
		"fingerprint too short":  func(a *Artifacts) { a.Sources[0].SigningKey.Fingerprint = testFingerprint[:39] },
		"fingerprint not hex":    func(a *Artifacts) { a.Sources[0].SigningKey.Fingerprint = "G" + testFingerprint[1:] },
		"fingerprint with space": func(a *Artifacts) { a.Sources[0].SigningKey.Fingerprint = "9DC8 5822" + testFingerprint[8:] },
		"no fingerprint":         func(a *Artifacts) { a.Sources[0].SigningKey.Fingerprint = "" },
		"duplicate source name":  func(a *Artifacts) { a.Sources = append(a.Sources, a.Sources[0]) },
		"absolute file path":     func(a *Artifacts) { a.Files[0].Path = "/tool/tool.tar.gz" },
		"file path escapes":      func(a *Artifacts) { a.Files[0].Path = "tool/../../etc/passwd" },
		"file path dot element":  func(a *Artifacts) { a.Files[0].Path = "tool/./tool.tar.gz" },
		"file path empty element": func(a *Artifacts) {
			a.Files[0].Path = "tool//tool.tar.gz"
		},
		"file path trailing slash": func(a *Artifacts) { a.Files[0].Path = "tool/" },
		"file path backslash":      func(a *Artifacts) { a.Files[0].Path = `tool\tool.tar.gz` },
		"empty file path":          func(a *Artifacts) { a.Files[0].Path = "" },
		"file without URL":         func(a *Artifacts) { a.Files[0].URL = "" },
		"file URL with user info":  func(a *Artifacts) { a.Files[0].URL = "https://token@downloads.example.com/tool.tar.gz" },
		"file without sha256":      func(a *Artifacts) { a.Files[0].SHA256 = "" },
		"file sha256 too short":    func(a *Artifacts) { a.Files[0].SHA256 = testDigest[:63] },
		"file sha256 uppercase":    func(a *Artifacts) { a.Files[0].SHA256 = "0123456789ABCDEF" + testDigest[16:] },
		"file sha256 with prefix":  func(a *Artifacts) { a.Files[0].SHA256 = "sha256:" + testDigest },
		"duplicate file path":      func(a *Artifacts) { a.Files = append(a.Files, a.Files[0]) },
		"image without registry":   func(a *Artifacts) { a.Images = []string{"team/app:1.2.3"} },
		"image short name":         func(a *Artifacts) { a.Images = []string{"busybox:1.36"} },
		"image without tag or digest": func(a *Artifacts) {
			a.Images = []string{"registry.example.com/team/app"}
		},
		"image with short digest":     func(a *Artifacts) { a.Images = []string{"registry.example.com/team/app@sha256:abc"} },
		"image with uppercase name":   func(a *Artifacts) { a.Images = []string{"registry.example.com/Team/app:1"} },
		"image with scheme":           func(a *Artifacts) { a.Images = []string{"https://registry.example.com/team/app:1"} },
		"duplicate image":             func(a *Artifacts) { a.Images = append(a.Images, a.Images[0]) },
		"unpinned Python requirement": func(a *Artifacts) { a.Python = []string{"numpy"} },
		"Python version range":        func(a *Artifacts) { a.Python = []string{"numpy>=2.5"} },
		"Python compatible release":   func(a *Artifacts) { a.Python = []string{"numpy~=2.5.4"} },
		"Python requirement extras":   func(a *Artifacts) { a.Python = []string{"requests[socks]==2.34.2"} },
		"Python requirement marker": func(a *Artifacts) {
			a.Python = []string{`numpy==2.5.4; python_version >= "3.12"`}
		},
		"Python project pinned twice": func(a *Artifacts) {
			a.Python = []string{"python-dateutil==2.9.0", "Python_DateUtil==2.9.0.post0"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			artifacts := validArtifacts()
			mutate(&artifacts)
			if err := artifacts.Validate(); !errors.Is(err, ErrInvalidArtifact) {
				t.Errorf("Validate() = %v, want ErrInvalidArtifact", err)
			}
		})
	}
}

// A manifest's artifacts are part of its contract: a malformed declaration stops the process like
// any other manifest error, and a misspelled key inside them is refused rather than ignored.
func TestManifestRefusesInvalidArtifacts(t *testing.T) {
	manifest := validManifest(KindSoftware, "nfs")
	manifest.Artifacts = Artifacts{Images: []string{"busybox:1.36"}}
	err := manifest.Validate()
	if !errors.Is(err, ErrInvalidManifest) || !errors.Is(err, ErrInvalidArtifact) {
		t.Errorf("Validate() = %v, want ErrInvalidManifest and ErrInvalidArtifact", err)
	}
	if _, err := ParseManifest([]byte(`{"contractVersion":1,"kind":"software","id":"nfs","version":"0.1.0","artifacts":{"image":["registry.example.com/app:1"]}}`)); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("misspelled artifacts key: error = %v, want ErrInvalidManifest", err)
	}
	if _, err := ParseManifest([]byte(`{"contractVersion":1,"kind":"software","id":"nfs","version":"0.1.0",
		"artifacts":{"sources":[{"name":"example","format":"deb","url":"https://packages.example.com/debian","suites":["noble"],"components":["main"],
		"signingKey":{"url":"https://packages.example.com/gpg","fingerprint":"` + testFingerprint + `","keyring":"x"}}]}}`)); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("unknown signingKey key: error = %v, want ErrInvalidManifest", err)
	}
	parsed, err := ParseManifest([]byte(`{"contractVersion":1,"kind":"software","id":"nfs","version":"0.1.0",
		"artifacts":{"sources":[{"name":"example","format":"deb","url":"https://packages.example.com/debian","suites":["noble"],"components":["main"],"architectures":["amd64"],
		"signingKey":{"url":"https://packages.example.com/debian/gpg","fingerprint":"` + strings.ToLower(testFingerprint) + `"}}],
		"files":[{"path":"tool/tool.tar.gz","url":"https://downloads.example.com/tool.tar.gz","sha256":"` + testDigest + `"}],
		"images":["registry.example.com/team/app:1.2.3"],"python":["numpy==2.5.4"]}}`))
	if err != nil {
		t.Fatalf("ParseManifest(valid artifacts) error = %v", err)
	}
	want := ArtifactSource{Name: "example", Format: "deb", URL: "https://packages.example.com/debian",
		Suites: []string{"noble"}, Components: []string{"main"}, Architectures: []string{"amd64"}, SigningKey: testSigningKey()}
	if got := parsed.Artifacts.Sources; len(got) != 1 || got[0].Name != want.Name || got[0].URL != want.URL ||
		!slices.Equal(got[0].Suites, want.Suites) || !slices.Equal(got[0].Components, want.Components) ||
		!slices.Equal(got[0].Architectures, want.Architectures) {
		t.Errorf("ParseManifest() sources = %+v, want [%+v]", got, want)
	}
	// A fingerprint is stored the way gpg prints it, whatever case the manifest wrote it in.
	if got := parsed.Artifacts.Sources[0].SigningKey; got == nil || *got != *want.SigningKey {
		t.Errorf("ParseManifest() signingKey = %+v, want %+v", got, want.SigningKey)
	}
	if got := parsed.Artifacts.Files; len(got) != 1 || got[0] != (FileArtifact{Path: "tool/tool.tar.gz", URL: "https://downloads.example.com/tool.tar.gz", SHA256: testDigest}) {
		t.Errorf("ParseManifest() files = %+v", got)
	}
	if !slices.Equal(parsed.Artifacts.Images, []string{"registry.example.com/team/app:1.2.3"}) || !slices.Equal(parsed.Artifacts.Python, []string{"numpy==2.5.4"}) {
		t.Errorf("ParseManifest() images = %v, python = %v", parsed.Artifacts.Images, parsed.Artifacts.Python)
	}
}

// The Local Repository keeps one copy of each source and file, so modules that declare the same one
// must mean the same content; what a deb source mirrors may differ, because the consumer merges it.
func TestNewRegistryChecksSharedArtifacts(t *testing.T) {
	source := ArtifactSource{Name: "shared", Format: ArtifactFormatDeb, URL: "https://packages.example.com/debian",
		Suites: []string{"noble"}, Components: []string{"main"}, SigningKey: testSigningKey()}
	file := FileArtifact{Path: "tool/tool.tar.gz", URL: "https://downloads.example.com/tool.tar.gz", SHA256: testDigest}
	conflicts := map[string]func(b *Artifacts){
		"source with another format": func(b *Artifacts) {
			b.Sources[0] = ArtifactSource{Name: "shared", Format: ArtifactFormatRPM, URL: source.URL, SigningKey: testSigningKey()}
		},
		"source with another URL": func(b *Artifacts) { b.Sources[0].URL = "https://mirror.example.com/debian" },
		"source with another signing key URL": func(b *Artifacts) {
			b.Sources[0].SigningKey.URL = "https://mirror.example.com/debian/gpg"
		},
		"source with another fingerprint": func(b *Artifacts) {
			b.Sources[0].SigningKey.Fingerprint = "0" + testFingerprint[1:]
		},
		"file with another URL":    func(b *Artifacts) { b.Files[0].URL = "https://mirror.example.com/tool.tar.gz" },
		"file with another digest": func(b *Artifacts) { b.Files[0].SHA256 = "f" + testDigest[1:] },
	}
	for name, mutate := range conflicts {
		t.Run(name, func(t *testing.T) {
			a, b := validManifest(KindSoftware, "nfs"), validManifest(KindSoftware, "podman")
			a.Artifacts = Artifacts{Sources: []ArtifactSource{source}, Files: []FileArtifact{file}}
			b.Artifacts = a.Artifacts.clone()
			mutate(&b.Artifacts)
			if _, err := NewRegistry(Module{Manifest: a}, Module{Manifest: b}); !errors.Is(err, ErrModuleConflict) {
				t.Errorf("NewRegistry() = %v, want ErrModuleConflict", err)
			}
		})
	}

	a, b := validManifest(KindSoftware, "nfs"), validManifest(KindSoftware, "podman")
	a.Artifacts = Artifacts{Sources: []ArtifactSource{source}, Files: []FileArtifact{file}}
	b.Artifacts = Artifacts{
		Sources: []ArtifactSource{{Name: "shared", Format: ArtifactFormatDeb, URL: source.URL,
			Suites: []string{"noble-updates"}, Components: []string{"universe"}, Architectures: []string{"arm64"},
			SigningKey: &ArtifactSigningKey{URL: source.SigningKey.URL, Fingerprint: strings.ToLower(testFingerprint)}}},
		Files: []FileArtifact{file},
	}
	if _, err := NewRegistry(Module{Manifest: a}, Module{Manifest: b}); err != nil {
		t.Errorf("NewRegistry(agreeing declarations with different suites) = %v, want nil", err)
	}
}

func TestRegistryArtifactsAreOrderedByModuleIDAndCopied(t *testing.T) {
	zeta, alpha, silent := validManifest(KindPlatform, "zeta"), validManifest(KindSoftware, "alpha"), validManifest(KindAccelerator, "silent")
	zeta.Artifacts = Artifacts{Images: []string{"registry.example.com/zeta:1"}}
	alpha.Artifacts = validArtifacts()
	registry, err := NewRegistry(Module{Manifest: zeta}, Module{Manifest: silent}, Module{Manifest: alpha})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	declared := registry.Artifacts()
	var ids []string
	for _, entry := range declared {
		ids = append(ids, entry.ModuleID)
	}
	if want := []string{"alpha", "zeta"}; !slices.Equal(ids, want) {
		t.Fatalf("Artifacts() module ids = %v, want %v (a module without declarations is left out)", ids, want)
	}
	declared[0].Artifacts.Sources[0].Suites[0] = "changed"
	declared[0].Artifacts.Sources[0].SigningKey.URL = "changed"
	declared[0].Artifacts.Images[0] = "changed"
	again := registry.Artifacts()
	if again[0].Artifacts.Sources[0].Suites[0] != "noble" || again[0].Artifacts.Images[0] != validArtifacts().Images[0] ||
		*again[0].Artifacts.Sources[0].SigningKey != *testSigningKey() {
		t.Errorf("Artifacts() after the caller changed its copy = %+v, want the registered declaration", again[0].Artifacts)
	}
}
