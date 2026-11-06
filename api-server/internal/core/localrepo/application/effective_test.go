package application

import (
	"reflect"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func debSource(name, url string, suites, components []string) localrepodomain.Source {
	return localrepodomain.Source{Name: name, Format: localrepodomain.FormatDeb, URL: url, Suites: suites, Components: components}
}

// Declarations of one source merge into one with the union of their suites and components, in order
// of appearance, and every declarer.
func TestEffectiveMergesSourcesOfOneName(t *testing.T) {
	modules := []localrepodomain.ModuleDeclaration{
		{ModuleID: "docker-ce", Sources: []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"})}},
		{ModuleID: "slurm", Sources: []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu/", []string{"noble", "jammy"}, []string{"test"})}},
	}
	spec := Effective(modules, nil)
	want := []EffectiveSource{{
		Source:     debSource("docker-ce", "https://download.example/ubuntu", []string{"noble", "jammy"}, []string{"stable", "test"}),
		DeclaredBy: []string{"module:docker-ce", "module:slurm"},
	}}
	if !reflect.DeepEqual(spec.Sources, want) || len(spec.Conflicts) > 0 {
		t.Errorf("Effective sources = %+v, conflicts %+v; want %+v and none", spec.Sources, spec.Conflicts, want)
	}
}

// A module's signing key travels with its source, also when the user's spec adds to the source or a
// later module without the key is merged into it; a source no module declares has none.
func TestEffectiveKeepsTheModulesSigningKey(t *testing.T) {
	key := localrepodomain.SigningKey{URL: "https://download.example/ubuntu/gpg", Fingerprint: "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"}
	modules := []localrepodomain.ModuleDeclaration{
		{ModuleID: "amd", Sources: []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"})}},
		{ModuleID: "docker-ce", Sources: []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"})},
			SigningKeys: map[string]localrepodomain.SigningKey{"docker-ce": key}},
	}
	user := []localrepodomain.Source{
		debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"test"}),
		debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"}),
	}
	spec := Effective(modules, user)
	if len(spec.Sources) != 2 || spec.Sources[0].SigningKey != key || spec.Sources[1].SigningKey != (localrepodomain.SigningKey{}) {
		t.Errorf("Effective sources = %+v; want docker-ce with the module's key and ubuntu with none", spec.Sources)
	}
}

// A later declaration with another format or URL is left out as a conflict naming both sides; one
// from the user's spec is a conflict the spec write refuses.
func TestEffectiveLeavesOutContradictingDeclarations(t *testing.T) {
	modules := []localrepodomain.ModuleDeclaration{{ModuleID: "docker-ce", Sources: []localrepodomain.Source{
		debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"}),
	}}}
	user := []localrepodomain.Source{
		debSource("docker-ce", "https://mirror.example/ubuntu", []string{"noble"}, []string{"stable"}),
		debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"}),
	}
	spec := Effective(modules, user)
	if len(spec.Sources) != 2 || spec.Sources[0].URL != "https://download.example/ubuntu" || spec.Sources[1].Name != "ubuntu" {
		t.Fatalf("Effective sources = %+v; want the module's docker-ce and the user's ubuntu", spec.Sources)
	}
	if len(spec.Conflicts) != 1 || !spec.Conflicts[0].InvolvesUser() {
		t.Fatalf("Effective conflicts = %+v; want one involving the user", spec.Conflicts)
	}
	want := "source docker-ce of user has URL https://mirror.example/ubuntu, which contradicts its declaration by module:docker-ce"
	if got := spec.Conflicts[0].Message(); got != want {
		t.Errorf("Conflict.Message() = %q, want %q", got, want)
	}
}

// Every registry host an image names gets an oci pull-through source declared by the images'
// declarers, any Python requirement adds the python source pypi, and a reference without a host
// gets none.
func TestEffectiveAddsPullThroughSources(t *testing.T) {
	modules := []localrepodomain.ModuleDeclaration{
		{ModuleID: "kubernetes", Images: []string{"quay.io/k0sproject/pause:3.10", "registry.k8s.io/coredns:1.12"}},
		{ModuleID: "amd", Images: []string{"quay.io/vendor/operator:1.0", "unqualified/image:1"}},
		{ModuleID: "slurm", Python: []string{"numpy==2.5.4"}},
	}
	spec := Effective(modules, nil)
	want := []EffectiveSource{
		{Source: localrepodomain.Source{Name: "pypi", Format: localrepodomain.FormatPython, URL: "https://pypi.org/"}, DeclaredBy: []string{"module:slurm"}},
		{Source: localrepodomain.Source{Name: "quay.io", Format: localrepodomain.FormatOCI, URL: "https://quay.io"}, DeclaredBy: []string{"module:kubernetes", "module:amd"}},
		{Source: localrepodomain.Source{Name: "registry.k8s.io", Format: localrepodomain.FormatOCI, URL: "https://registry.k8s.io"}, DeclaredBy: []string{"module:kubernetes"}},
	}
	if !reflect.DeepEqual(spec.Sources, want) {
		t.Errorf("Effective sources = %+v, want %+v", spec.Sources, want)
	}
	if len(spec.Images) != 4 || spec.Images[0].Reference != "quay.io/k0sproject/pause:3.10" {
		t.Errorf("Effective images = %+v; want all four, ordered by reference", spec.Images)
	}
	if !reflect.DeepEqual(spec.Python, []EffectivePython{{Requirement: "numpy==2.5.4", DeclaredBy: []string{"module:slurm"}}}) {
		t.Errorf("Effective python = %+v", spec.Python)
	}
}

// A declared source of a pull-through source's name and format keeps its URL (a mirror); one of
// another format keeps the name and the pull-through source is a conflict involving the user.
func TestEffectiveDefersPullThroughSourcesToDeclaredOnes(t *testing.T) {
	modules := []localrepodomain.ModuleDeclaration{{ModuleID: "grafana", Images: []string{"docker.io/grafana/grafana:12.4"}, Python: []string{"six==1.0"}}}
	mirror := []localrepodomain.Source{{Name: "docker.io", Format: localrepodomain.FormatOCI, URL: "https://mirror.example"}}
	spec := Effective(modules, mirror)
	if len(spec.Sources) != 2 || spec.Sources[0].URL != "https://mirror.example" ||
		!reflect.DeepEqual(spec.Sources[0].DeclaredBy, []string{"user", "module:grafana"}) || len(spec.Conflicts) > 0 {
		t.Errorf("mirror: sources %+v, conflicts %+v; want docker.io at the mirror declared by both", spec.Sources, spec.Conflicts)
	}

	clash := []localrepodomain.Source{debSource("pypi", "http://debs.example/", []string{"noble"}, []string{"main"})}
	spec = Effective(modules, clash)
	if len(spec.Conflicts) != 1 || !spec.Conflicts[0].InvolvesUser() {
		t.Errorf("clash: conflicts %+v; want the pull-through pypi left out, involving the user", spec.Conflicts)
	}
}

// One file path declared twice is stored once when the declarations agree and is a conflict
// otherwise.
func TestEffectiveMergesFilesByPath(t *testing.T) {
	file := localrepodomain.File{Path: "k0s/k0s-v1", URL: "https://files.example/k0s-v1", SHA256: "aa"}
	other := localrepodomain.File{Path: "k0s/k0s-v1", URL: "https://files.example/k0s-v1", SHA256: "bb"}
	spec := Effective([]localrepodomain.ModuleDeclaration{
		{ModuleID: "kubernetes", Files: []localrepodomain.File{file}},
		{ModuleID: "slurm", Files: []localrepodomain.File{file}},
		{ModuleID: "amd", Files: []localrepodomain.File{other}},
	}, nil)
	want := []EffectiveFile{{File: file, DeclaredBy: []string{"module:kubernetes", "module:slurm"}}}
	if !reflect.DeepEqual(spec.Files, want) || len(spec.Conflicts) != 1 || spec.Conflicts[0].InvolvesUser() {
		t.Errorf("Effective files = %+v, conflicts %+v; want %+v and one module conflict", spec.Files, spec.Conflicts, want)
	}
}

func TestRegistryHost(t *testing.T) {
	for reference, want := range map[string]string{
		"quay.io/k0sproject/pause:3.10": "quay.io",
		"localhost/app:1":               "localhost",
		"registry:5000/app:1":           "registry:5000",
		"grafana/grafana:12":            "",
		"nginx":                         "",
	} {
		host, ok := RegistryHost(reference)
		if host != want || ok != (want != "") {
			t.Errorf("RegistryHost(%q) = %q, %v; want %q", reference, host, ok, want)
		}
	}
}
