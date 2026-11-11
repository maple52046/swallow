package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/core/localrepo/domain/localrepotest"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

type serviceFixture struct {
	service    *Service
	records    *localrepotest.Store
	repository *fakeRepository
	requester  *fakeRequester
}

// newServiceFixture serves site-1 (with a repository Integration) and site-2 (without one).
func newServiceFixture() serviceFixture {
	f := serviceFixture{records: localrepotest.NewStore(), repository: newFakeRepository(), requester: &fakeRequester{}}
	f.service = NewService(fakeSites{ids: map[string]bool{"site-1": true, "site-2": true}},
		newFakeIntegrations(repositoryIntegrationOf("site-1")), f.records,
		fakeRepositories{repository: f.repository, known: map[string]bool{"repo-site-1": true}}, f.requester, testModules)
	return f
}

// A Site exists with a Local Repository only while it has a repository Integration.
func TestServiceRequiresARepositoryIntegration(t *testing.T) {
	f := newServiceFixture()
	ctx := context.Background()
	if _, err := f.service.Get(ctx, "site-2"); !errors.Is(err, localrepodomain.ErrNoRepositoryIntegration) {
		t.Errorf("Get(site-2) = %v, want ErrNoRepositoryIntegration", err)
	}
	if _, err := f.service.Get(ctx, "site-9"); !errors.Is(err, sitedomain.ErrSiteNotFound) {
		t.Errorf("Get(site-9) = %v, want ErrSiteNotFound", err)
	}
	if err := f.service.RequestSync(ctx, "site-2"); !errors.Is(err, localrepodomain.ErrNoRepositoryIntegration) || len(f.requester.requested) > 0 {
		t.Errorf("RequestSync(site-2) = %v with requests %v, want ErrNoRepositoryIntegration and none", err, f.requester.requested)
	}
	if err := f.service.RequestSync(ctx, "site-1"); err != nil || !reflect.DeepEqual(f.requester.requested, []string{ControllerName}) {
		t.Errorf("RequestSync(site-1) = %v with requests %v, want the controller requested", err, f.requester.requested)
	}
}

// Before any observation, Get shows the effective spec pending and not ready.
func TestServiceGetBeforeTheFirstObservation(t *testing.T) {
	view, err := newServiceFixture().service.Get(context.Background(), "site-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Ready || view.Generation != 0 || len(view.Sources) != 2 || view.Sources[0].State != localrepodomain.SyncPending || len(view.Files) != 1 ||
		len(view.Images) != 1 || view.Images[0].State != localrepodomain.SyncPending {
		t.Errorf("Get = %+v; want not ready, generation 0, docker-ce and quay.io pending, one file, the image pending", view)
	}
}

// A spec write normalizes the sources, checks them against the module declarations and the stored
// generation, and stores them.
func TestServicePutSpec(t *testing.T) {
	ctx := context.Background()
	ubuntu := localrepodomain.Source{
		Name: " ubuntu ", Format: localrepodomain.FormatDeb, URL: " http://archive.example/ubuntu/ ",
		Suites: []string{"noble", " noble-updates "}, Components: []string{"main"}, Architectures: []string{"amd64"},
	}
	upstream := localrepodomain.ModeUpstream
	for _, tc := range []struct {
		name    string
		input   PutSpecInput
		wantErr error
	}{
		{"no mode", PutSpecInput{Sources: []localrepodomain.Source{ubuntu}}, localrepodomain.ErrInvalidSpec},
		{"unknown mode", PutSpecInput{Mode: "mirror", Sources: []localrepodomain.Source{ubuntu}}, localrepodomain.ErrInvalidSpec},
		{"bad name", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{{Name: "Ubuntu!", Format: "deb", URL: "http://x.example"}}}, localrepodomain.ErrInvalidSpec},
		{"unknown format", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{{Name: "x", Format: "zip", URL: "http://x.example"}}}, localrepodomain.ErrInvalidSpec},
		{"credentials in URL", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{{Name: "x", Format: "oci", URL: "https://user:pw@x.example"}}}, localrepodomain.ErrInvalidSpec},
		{"deb without suites", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{{Name: "x", Format: "deb", URL: "http://x.example", Components: []string{"main"}}}}, localrepodomain.ErrInvalidSpec},
		{"suites on oci", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{{Name: "x", Format: "oci", URL: "https://x.example", Suites: []string{"a"}}}}, localrepodomain.ErrInvalidSpec},
		{"listed twice", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{ubuntu, ubuntu}}, localrepodomain.ErrInvalidSpec},
		{"contradicts a module", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{debSource("docker-ce", "https://mirror.example/", []string{"noble"}, []string{"stable"})}}, localrepodomain.ErrInvalidSpec},
		{"stale generation", PutSpecInput{Generation: 4, Mode: upstream, Sources: []localrepodomain.Source{ubuntu}}, localrepodomain.ErrGenerationConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture()
			if _, err := f.service.PutSpec(ctx, "site-1", tc.input); !errors.Is(err, tc.wantErr) {
				t.Errorf("PutSpec error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	f := newServiceFixture()
	if _, err := f.service.PutSpec(ctx, "site-2", PutSpecInput{Mode: upstream, Sources: []localrepodomain.Source{ubuntu}}); !errors.Is(err, localrepodomain.ErrNoRepositoryIntegration) {
		t.Errorf("PutSpec(site-2) = %v, want ErrNoRepositoryIntegration", err)
	}
	view, err := f.service.PutSpec(ctx, "site-1", PutSpecInput{Generation: 0, Mode: localrepodomain.ModeOffline, Sources: []localrepodomain.Source{ubuntu}})
	if err != nil {
		t.Fatalf("PutSpec: %v", err)
	}
	want := debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble", "noble-updates"}, []string{"main"})
	want.Architectures = []string{"amd64"}
	if view.Generation != 1 || view.Spec.Mode != localrepodomain.ModeOffline || !reflect.DeepEqual(view.Spec.Sources, []localrepodomain.Source{want}) || len(view.Sources) != 3 {
		t.Errorf("PutSpec = generation %d spec %+v, %d sources; want generation 1 in offline mode, the trimmed source, three effective sources",
			view.Generation, view.Spec, len(view.Sources))
	}
	// Until the controller observes the new spec, the first thing in the way is that; switching to
	// upstream makes not_serving the first.
	if view.Ready || view.NotReady[0].Reason != localrepodomain.ReasonNotObserved {
		t.Errorf("PutSpec offline: ready %v, causes %+v; want not ready, not_observed first", view.Ready, view.NotReady)
	}
	view, err = f.service.PutSpec(ctx, "site-1", PutSpecInput{Generation: 1, Mode: localrepodomain.ModeUpstream, Sources: []localrepodomain.Source{ubuntu}})
	if err != nil || view.Spec.Mode != localrepodomain.ModeUpstream || view.Ready || view.NotReady[0].Reason != localrepodomain.ReasonNotServing {
		t.Errorf("PutSpec switching to upstream = %+v, %v; want upstream, not_serving first", view, err)
	}
	// Nothing converges in upstream mode, so the unobserved spec and its pending items are no cause.
	if len(view.NotReady) != 1 {
		t.Errorf("PutSpec switching to upstream: causes %+v, want only not_serving", view.NotReady)
	}
}

// Browse validates the query before asking the provider, and passes the provider's page through.
func TestServiceBrowse(t *testing.T) {
	f := newServiceFixture()
	ctx := context.Background()
	if _, err := f.service.Browse(ctx, "site-1", localrepodomain.ContentQuery{Format: "zip", Limit: 20}); !errors.Is(err, localrepodomain.ErrInvalidContentQuery) {
		t.Errorf("Browse of an unknown format = %v, want ErrInvalidContentQuery", err)
	}
	if _, err := f.service.Browse(ctx, "site-1", localrepodomain.ContentQuery{Format: "deb", Limit: MaxContentPageSize + 1}); !errors.Is(err, localrepodomain.ErrInvalidContentQuery) {
		t.Errorf("Browse of an oversized page = %v, want ErrInvalidContentQuery", err)
	}
	if len(f.repository.browsed) > 0 {
		t.Fatalf("provider browsed %v for invalid queries", f.repository.browsed)
	}
	f.repository.page = localrepodomain.ContentPage{Items: []localrepodomain.ContentItem{{Name: "docker-ce", Version: "1"}}, Total: 7}
	page, err := f.service.Browse(ctx, "site-1", localrepodomain.ContentQuery{Format: "deb", Source: " docker-ce ", Offset: 20, Limit: 20})
	if err != nil || page.Total != 7 || len(page.Items) != 1 {
		t.Fatalf("Browse = %+v, %v; want the provider's page", page, err)
	}
	if got := f.repository.browsed[0]; got.Source != "docker-ce" || got.Offset != 20 {
		t.Errorf("provider query = %+v, want the trimmed source and the offset", got)
	}
}
