package pulp

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// seedSite ensures what one Site serves: two deb sources, two registries, the python index, and
// one file, and returns the Repository.
func seedSite(t *testing.T, f *fakePulp) *Repository {
	t.Helper()
	f.seedSigningService()
	seedUbuntuUpstream(f)
	f.upstream["https://download.docker.com/linux/ubuntu"] = []object{
		{"package": "docker-ce", "version": "5:29.1.0", "architecture": "amd64", "relative_path": "pool/stable/d/docker-ce_29.1.0_amd64.deb"},
	}
	f.downloads[k0sURL] = fakeDownload{sha256: k0sFile().SHA256, size: 262 << 20}
	repo := f.newRepository(t)
	sources := []localrepodomain.Source{
		ubuntuSource(),
		{Name: "docker-ce", Format: localrepodomain.FormatDeb, URL: "https://download.docker.com/linux/ubuntu", Suites: []string{"noble"}, Components: []string{"stable"}},
		{Name: "docker.io", Format: localrepodomain.FormatOCI, URL: "https://docker.io"},
		{Name: "quay.io", Format: localrepodomain.FormatOCI},
		{Name: "pypi", Format: localrepodomain.FormatPython},
	}
	ctx := context.Background()
	for range 2 {
		for _, source := range sources {
			if _, err := repo.EnsureSource(ctx, source); err != nil {
				t.Fatalf("EnsureSource(%s) error = %v", source.Name, err)
			}
		}
		if _, err := repo.EnsureFile(ctx, k0sFile()); err != nil {
			t.Fatalf("EnsureFile(k0s) error = %v", err)
		}
	}
	return repo
}

func TestEndpointsReportsWhereHostsReadContent(t *testing.T) {
	f := newFakePulp(t)
	repo := seedSite(t, f)
	origin := f.server.URL

	got, err := repo.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	want := localrepodomain.HostEndpoints{
		Deb: []localrepodomain.DebSource{
			{Name: "docker-ce", URI: origin + "/pulp/content/deb/docker-ce/", Suites: []string{"noble"}, Components: []string{"stable"}},
			{Name: "ubuntu", URI: origin + "/pulp/content/deb/ubuntu/", Suites: []string{"noble", "noble-updates", "noble-security"}, Components: []string{"main", "universe"}},
		},
		DebSigningKey: "-----BEGIN PGP PUBLIC KEY BLOCK-----\nfake\n-----END PGP PUBLIC KEY BLOCK-----",
		Registry:      strings.TrimPrefix(origin, "http://"),
		RegistryMirrors: map[string]string{
			"docker.io": origin + "/v2/docker.io",
			"quay.io":   origin + "/v2/quay.io",
		},
		PythonIndex: origin + "/pypi/pypi/simple/",
		Files:       origin + "/pulp/content/files/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Endpoints() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEndpointsOfAnEmptyPulpAreEmpty(t *testing.T) {
	f := newFakePulp(t)
	got, err := f.newRepository(t).Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if !reflect.DeepEqual(got, localrepodomain.HostEndpoints{}) {
		t.Errorf("Endpoints() = %+v, want nothing served", got)
	}
}

func TestBrowsePagesAcrossSources(t *testing.T) {
	f := newFakePulp(t)
	repo := seedSite(t, f)
	ctx := context.Background()

	// Packages sort by source name: docker-ce (1 package), then ubuntu (2).
	page, err := repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatDeb, Offset: 0, Limit: 2})
	if err != nil {
		t.Fatalf("Browse(deb, 0, 2) error = %v", err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].Name != "docker-ce" || page.Items[1].Name != "chrony" ||
		page.Items[1].Source != "ubuntu" || page.Items[1].Path != "pool/main/c/chrony/chrony_4.5-1_amd64.deb" ||
		page.Items[1].Version != "4.5-1" || page.Items[1].Architecture != "amd64" {
		t.Errorf("Browse(deb, 0, 2) = %+v, want docker-ce then chrony of 3", page)
	}
	page, err = repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatDeb, Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("Browse(deb, 2, 2) error = %v", err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].Name != "nfs-common" {
		t.Errorf("Browse(deb, 2, 2) = %+v, want nfs-common, the last of 3", page)
	}

	page, err = repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatDeb, Source: "ubuntu", Search: "NFS", Limit: 10})
	if err != nil {
		t.Fatalf("Browse(deb ubuntu NFS) error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Name != "nfs-common" {
		t.Errorf("Browse(deb ubuntu NFS) = %+v, want nfs-common", page)
	}
	if searches := f.requestsTo("GET", api(debPackages)); searches[len(searches)-1].query.Get("package__icontains") != "NFS" {
		t.Errorf("deb package search query = %v, want package__icontains", searches[len(searches)-1].query)
	}
}

func TestBrowseImagesPythonPackagesAndFiles(t *testing.T) {
	f := newFakePulp(t)
	repo := seedSite(t, f)
	ctx := context.Background()

	f.mu.Lock()
	image := f.store(containerRepositories, map[string]any{"name": "docker.io/grafana/grafana"})
	tags := []string{
		f.store(containerTags, map[string]any{"name": "12.4.12"}),
		f.store(containerTags, map[string]any{"name": "latest"}),
	}
	f.addVersion(image, tags)
	pkg := f.store(pythonPackages, map[string]any{"name": "numpy", "version": "2.5.4", "filename": "numpy-2.5.4-cp312-manylinux_x86_64.whl", "size": 16.0e6})
	f.addVersion(f.order[pythonRepositories][0], []string{pkg})
	f.mu.Unlock()

	images, err := repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatOCI, Limit: 10})
	if err != nil {
		t.Fatalf("Browse(oci) error = %v", err)
	}
	wantImage := localrepodomain.ContentItem{Name: "grafana/grafana", Version: "12.4.12, latest", Source: "docker.io", Path: "docker.io/grafana/grafana"}
	if images.Total != 1 || len(images.Items) != 1 || images.Items[0] != wantImage {
		t.Errorf("Browse(oci) = %+v, want %+v", images, wantImage)
	}
	if quay, err := repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatOCI, Source: "quay.io", Limit: 10}); err != nil || quay.Total != 0 {
		t.Errorf("Browse(oci quay.io) = %+v, %v; want nothing pulled through quay.io", quay, err)
	}

	python, err := repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatPython, Search: "num", Limit: 10})
	if err != nil {
		t.Fatalf("Browse(python) error = %v", err)
	}
	wantPackage := localrepodomain.ContentItem{Name: "numpy", Version: "2.5.4", Source: "pypi", Path: "numpy-2.5.4-cp312-manylinux_x86_64.whl", SizeBytes: 16e6}
	if python.Total != 1 || len(python.Items) != 1 || python.Items[0] != wantPackage {
		t.Errorf("Browse(python) = %+v, want %+v", python, wantPackage)
	}

	files, err := repo.Browse(ctx, localrepodomain.ContentQuery{Format: localrepodomain.FormatFile, Search: "K0S", Limit: 10})
	if err != nil {
		t.Fatalf("Browse(file) error = %v", err)
	}
	wantFile := localrepodomain.ContentItem{Name: "k0s-v1.36.3+k0s.2-amd64", Path: k0sFile().Path, SizeBytes: 262 << 20}
	if files.Total != 1 || len(files.Items) != 1 || files.Items[0] != wantFile {
		t.Errorf("Browse(file) = %+v, want %+v", files, wantFile)
	}
}

func TestBrowseRefusesWhatItCannotList(t *testing.T) {
	repo := newFakePulp(t).newRepository(t)
	for name, query := range map[string]localrepodomain.ContentQuery{
		"rpm":          {Format: localrepodomain.FormatRPM, Limit: 10},
		"no limit":     {Format: localrepodomain.FormatDeb},
		"unknown kind": {Format: "iso", Limit: 10},
	} {
		_, err := repo.Browse(context.Background(), query)
		var providerErr *localrepodomain.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorRejected {
			t.Errorf("Browse(%s) error = %v, want a rejected ProviderError", name, err)
		}
	}
}
