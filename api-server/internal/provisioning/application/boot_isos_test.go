package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

type bootISORepoFake struct {
	isos      map[string]*provisioningdomain.BootISO
	createErr error
}

func (r *bootISORepoFake) Create(_ context.Context, iso *provisioningdomain.BootISO) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.isos[iso.ID] = iso
	return nil
}

func (r *bootISORepoFake) FindByID(_ context.Context, id string) (*provisioningdomain.BootISO, error) {
	if iso, ok := r.isos[id]; ok {
		return iso, nil
	}
	return nil, provisioningdomain.ErrBootISONotFound
}

func (r *bootISORepoFake) List(_ context.Context, filter provisioningdomain.BootISOFilter) ([]*provisioningdomain.BootISO, error) {
	var out []*provisioningdomain.BootISO
	for _, iso := range r.isos {
		if filter.IntegrationID != "" && iso.IntegrationID != filter.IntegrationID {
			continue
		}
		if len(filter.IntegrationIDs) > 0 && !containsString(filter.IntegrationIDs, iso.IntegrationID) {
			continue
		}
		out = append(out, iso)
	}
	return out, nil
}

func (r *bootISORepoFake) Delete(_ context.Context, id string) error {
	if _, ok := r.isos[id]; !ok {
		return provisioningdomain.ErrBootISONotFound
	}
	delete(r.isos, id)
	return nil
}

type bootISOIntegrationsFake map[string]string

func (f bootISOIntegrationsFake) Find(_ context.Context, id string) (*provisioningdomain.ProvisionerIntegration, error) {
	if site, ok := f[id]; ok {
		return &provisioningdomain.ProvisionerIntegration{ID: id, SiteID: site}, nil
	}
	return nil, provisioningdomain.ErrIntegrationNotProvisioner
}

func (f bootISOIntegrationsFake) ListBySite(_ context.Context, siteID string) ([]provisioningdomain.ProvisionerIntegration, error) {
	var out []provisioningdomain.ProvisionerIntegration
	for id, site := range f {
		if site == siteID {
			out = append(out, provisioningdomain.ProvisionerIntegration{ID: id, SiteID: site})
		}
	}
	return out, nil
}

type bootISOBuilderFake struct {
	unavailable error
	buildErr    error
	scripts     map[string]string
	removed     []string
}

func (b *bootISOBuilderFake) Available() error { return b.unavailable }

func (b *bootISOBuilderFake) Build(_ context.Context, id, script string) (provisioningdomain.BootISOArtifact, error) {
	if b.buildErr != nil {
		return provisioningdomain.BootISOArtifact{}, b.buildErr
	}
	b.scripts[id] = script
	return provisioningdomain.BootISOArtifact{SizeBytes: 1024, SHA256: "abc", IPXEVersion: "v2.0.0"}, nil
}

func (b *bootISOBuilderFake) Remove(id string) error {
	b.removed = append(b.removed, id)
	return nil
}

func (b *bootISOBuilderFake) URL(id string) string {
	return "http://192.0.2.1/boot-media/ipxe/" + id + "/swallow-ipxe.iso"
}

type bootISOUsageFake map[string]int

func (u bootISOUsageFake) CountEnabledUsing(_ context.Context, id string) (int, error) {
	return u[id], nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type bootISOFixture struct {
	service *BootISOService
	repo    *bootISORepoFake
	builder *bootISOBuilderFake
	usage   bootISOUsageFake
}

func newBootISOFixture() *bootISOFixture {
	f := &bootISOFixture{
		repo:    &bootISORepoFake{isos: map[string]*provisioningdomain.BootISO{}},
		builder: &bootISOBuilderFake{scripts: map[string]string{}},
		usage:   bootISOUsageFake{},
	}
	f.service = NewBootISOService(f.repo, bootISOIntegrationsFake{"maas-tainan": "site-tainan", "maas-taipei": "site-taipei"}, f.builder, f.usage)
	ids := []string{"iso-1", "iso-2", "iso-3"}
	f.service.newID = func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
	return f
}

// A build renders the rack template, stores the record only after the file exists, and reports
// the Integration's Site and the served URL.
func TestCreateBootISORendersRackTemplate(t *testing.T) {
	f := newBootISOFixture()
	item, err := f.service.Create(context.Background(), CreateBootISOInput{
		Name: " tainan-rack ", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5", CreatedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if item.ID != "iso-1" || item.Name != "tainan-rack" || item.SiteID != "site-tainan" || item.ChainURL != "http://10.1.0.5:5248/ipxe.cfg" {
		t.Errorf("item = %+v", item)
	}
	if item.URL != "http://192.0.2.1/boot-media/ipxe/iso-1/swallow-ipxe.iso" || item.IPXEVersion != "v2.0.0" || item.CreatedBy != "admin" {
		t.Errorf("item = %+v, want the served URL, iPXE version and creator", item)
	}
	if script := f.builder.scripts["iso-1"]; !strings.Contains(script, "set maas_rack 10.1.0.5\n") || !strings.Contains(script, ":5248/ipxe.cfg") || script != item.Script {
		t.Errorf("built script = %q, want the stored script chaining to the rack", script)
	}
	if _, ok := f.repo.isos["iso-1"]; !ok {
		t.Error("the Boot ISO was not stored")
	}
}

// Nothing is built for invalid input, an unknown provisioner, an unavailable builder, or a taken
// name; a failed build stores nothing.
func TestCreateBootISORejections(t *testing.T) {
	cases := []struct {
		name  string
		input CreateBootISOInput
		setup func(*bootISOFixture)
		want  error
	}{
		{name: "blank name", input: CreateBootISOInput{Name: " ", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"}, want: provisioningdomain.ErrInvalidBootISO},
		{name: "rack URL instead of address", input: CreateBootISOInput{Name: "a", IntegrationID: "maas-tainan", RackAddress: "http://10.1.0.5:5240/MAAS"}, want: provisioningdomain.ErrInvalidBootISO},
		{name: "no provisioner", input: CreateBootISOInput{Name: "a", RackAddress: "10.1.0.5"}, want: provisioningdomain.ErrInvalidBootISO},
		{name: "not a provisioner", input: CreateBootISOInput{Name: "a", IntegrationID: "netbox", RackAddress: "10.1.0.5"}, want: provisioningdomain.ErrIntegrationNotProvisioner},
		{
			name: "builder unavailable", input: CreateBootISOInput{Name: "a", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"},
			setup: func(f *bootISOFixture) { f.builder.unavailable = provisioningdomain.ErrBootISOBuilderUnavailable },
			want:  provisioningdomain.ErrBootISOBuilderUnavailable,
		},
		{
			name: "name taken in the provisioner", input: CreateBootISOInput{Name: "Tainan-Rack", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"},
			setup: func(f *bootISOFixture) {
				f.repo.isos["old"] = &provisioningdomain.BootISO{ID: "old", Name: "tainan-rack", IntegrationID: "maas-tainan"}
			},
			want: provisioningdomain.ErrBootISONameTaken,
		},
		{
			name: "build failed", input: CreateBootISOInput{Name: "a", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"},
			setup: func(f *bootISOFixture) { f.builder.buildErr = provisioningdomain.ErrBootISOBuildFailed },
			want:  provisioningdomain.ErrBootISOBuildFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBootISOFixture()
			if tc.setup != nil {
				tc.setup(f)
			}
			before := len(f.repo.isos)
			if _, err := f.service.Create(context.Background(), tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("Create error = %v, want %v", err, tc.want)
			}
			if len(f.builder.scripts) != 0 || len(f.repo.isos) != before {
				t.Errorf("built %v, stored %d records; want nothing built or stored", f.builder.scripts, len(f.repo.isos)-before)
			}
		})
	}
}

// The same name in another provisioner is allowed; a record that cannot be stored (a race on the
// unique index) removes the built file.
func TestCreateBootISONameScopeAndStoreFailure(t *testing.T) {
	f := newBootISOFixture()
	f.repo.isos["old"] = &provisioningdomain.BootISO{ID: "old", Name: "rack", IntegrationID: "maas-taipei"}
	if _, err := f.service.Create(context.Background(), CreateBootISOInput{Name: "rack", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"}); err != nil {
		t.Fatalf("Create in another provisioner error = %v", err)
	}

	f.repo.createErr = provisioningdomain.ErrBootISONameTaken
	if _, err := f.service.Create(context.Background(), CreateBootISOInput{Name: "racer", IntegrationID: "maas-tainan", RackAddress: "10.1.0.5"}); !errors.Is(err, provisioningdomain.ErrBootISONameTaken) {
		t.Fatalf("Create error = %v, want ErrBootISONameTaken", err)
	}
	if want := []string{"iso-2"}; !equalStringList(f.builder.removed, want) {
		t.Errorf("removed = %v, want the unrecorded build %v", f.builder.removed, want)
	}
}

// The list reports the builder's state even when nothing can be built, narrows by Site and
// provisioner, and counts the Servers using each ISO.
func TestListBootISOs(t *testing.T) {
	f := newBootISOFixture()
	f.repo.isos["a"] = &provisioningdomain.BootISO{ID: "a", Name: "a", IntegrationID: "maas-tainan"}
	f.repo.isos["b"] = &provisioningdomain.BootISO{ID: "b", Name: "b", IntegrationID: "maas-taipei"}
	f.usage["a"] = 2
	f.builder.unavailable = errors.New("boot ISO builder unavailable: xorriso is not installed")

	list, err := f.service.List(context.Background(), "site-tainan", "")
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if list.Builder.Available || !strings.Contains(list.Builder.Reason, "xorriso") {
		t.Errorf("builder = %+v, want unavailable with the reason", list.Builder)
	}
	if len(list.Items) != 1 || list.Items[0].ID != "a" || list.Items[0].SiteID != "site-tainan" || list.Items[0].InUseBy != 2 {
		t.Errorf("items = %+v, want only a, in the Tainan Site, used by 2", list.Items)
	}

	list, err = f.service.List(context.Background(), "site-tainan", "maas-taipei")
	if err != nil || len(list.Items) != 0 {
		t.Errorf("List(Tainan Site, Taipei provisioner) = %+v, %v; want none", list, err)
	}
	list, err = f.service.List(context.Background(), "", "")
	if err != nil || len(list.Items) != 2 {
		t.Errorf("List() = %+v, %v; want both", list, err)
	}
}

// Deleting an ISO that enabled Boot Media uses is refused and keeps the file.
func TestDeleteBootISO(t *testing.T) {
	f := newBootISOFixture()
	f.repo.isos["a"] = &provisioningdomain.BootISO{ID: "a", Name: "a", IntegrationID: "maas-tainan"}
	f.usage["a"] = 1
	if err := f.service.Delete(context.Background(), "a"); !errors.Is(err, provisioningdomain.ErrBootISOInUse) {
		t.Fatalf("Delete in use error = %v, want ErrBootISOInUse", err)
	}
	if _, ok := f.repo.isos["a"]; !ok || len(f.builder.removed) != 0 {
		t.Fatal("an in-use Boot ISO was deleted")
	}

	f.usage["a"] = 0
	if err := f.service.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if _, ok := f.repo.isos["a"]; ok || !equalStringList(f.builder.removed, []string{"a"}) {
		t.Errorf("record kept = %v, removed = %v; want both gone", ok, f.builder.removed)
	}
	if err := f.service.Delete(context.Background(), "a"); !errors.Is(err, provisioningdomain.ErrBootISONotFound) {
		t.Errorf("Delete again error = %v, want ErrBootISONotFound", err)
	}
}

func equalStringList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
