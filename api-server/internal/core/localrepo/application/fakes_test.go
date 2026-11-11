package application

import (
	"context"
	"sync"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// fakeSites knows the Sites in ids.
type fakeSites struct {
	sitedomain.SiteRepository
	ids map[string]bool
}

func (s fakeSites) FindByID(_ context.Context, id string) (*sitedomain.Site, error) {
	if !s.ids[id] {
		return nil, sitedomain.ErrSiteNotFound
	}
	return &sitedomain.Site{ID: id, Name: id}, nil
}

// fakeIntegrations holds Integrations by id and filters them as the repository does.
type fakeIntegrations struct {
	sitedomain.IntegrationRepository
	byID map[string]*sitedomain.Integration
}

func newFakeIntegrations(integrations ...*sitedomain.Integration) *fakeIntegrations {
	f := &fakeIntegrations{byID: map[string]*sitedomain.Integration{}}
	for _, integration := range integrations {
		f.byID[integration.ID] = integration
	}
	return f
}

func (f *fakeIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	integration, ok := f.byID[id]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return integration, nil
}

func (f *fakeIntegrations) List(_ context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	var out []*sitedomain.Integration
	for _, integration := range f.byID {
		if (filter.SiteID == "" || integration.SiteID == filter.SiteID) && (filter.Kind == "" || integration.Kind == filter.Kind) &&
			(!filter.EnabledOnly || integration.Enabled) {
			out = append(out, integration)
		}
	}
	return out, nil
}

func repositoryIntegrationOf(siteID string) *sitedomain.Integration {
	return &sitedomain.Integration{
		ID: "repo-" + siteID, SiteID: siteID, Kind: sitedomain.IntegrationKindRepository, ProviderKind: "test",
		Name: "local", Enabled: true, Settings: map[string]string{localrepodomain.SettingContentOrigin: "http://10.0.0.5:8080"},
	}
}

// fakeRepository answers Ensure* with the state set per name, path, or reference (ready by
// default), fails the calls named in errs, and records every call. A source serves when
// sourceServing says so, and otherwise exactly while it is ready.
type fakeRepository struct {
	mu            sync.Mutex
	sourceState   map[string]localrepodomain.SyncState
	sourceServing map[string]bool
	fileState     map[string]localrepodomain.SyncState
	imageState    map[string]localrepodomain.SyncState
	errs          map[string]error
	endpoints     localrepodomain.HostEndpoints
	page          localrepodomain.ContentPage
	ensured       []string
	removed       []string
	browsed       []localrepodomain.ContentQuery
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		sourceState: map[string]localrepodomain.SyncState{}, sourceServing: map[string]bool{},
		fileState: map[string]localrepodomain.SyncState{}, imageState: map[string]localrepodomain.SyncState{},
		errs: map[string]error{},
	}
}

func (r *fakeRepository) EnsureSource(_ context.Context, source localrepodomain.Source) (localrepodomain.SourceStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, "source "+source.Name)
	if err := r.errs["source "+source.Name]; err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	state, ok := r.sourceState[source.Name]
	if !ok {
		state = localrepodomain.SyncReady
	}
	serving, ok := r.sourceServing[source.Name]
	if !ok {
		serving = state == localrepodomain.SyncReady
	}
	return localrepodomain.SourceStatus{Name: source.Name, Format: source.Format, State: state, Serving: serving, ContentCount: 3}, nil
}

func (r *fakeRepository) EnsureFile(_ context.Context, file localrepodomain.File) (localrepodomain.FileStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, "file "+file.Path)
	if err := r.errs["file "+file.Path]; err != nil {
		return localrepodomain.FileStatus{}, err
	}
	state, ok := r.fileState[file.Path]
	if !ok {
		state = localrepodomain.SyncReady
	}
	return localrepodomain.FileStatus{Path: file.Path, State: state, SizeBytes: 10}, nil
}

// EnsureImage reports a ready image with a size, so a test sees the size travel into the status.
func (r *fakeRepository) EnsureImage(_ context.Context, reference string) (localrepodomain.ImageStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensured = append(r.ensured, "image "+reference)
	if err := r.errs["image "+reference]; err != nil {
		return localrepodomain.ImageStatus{}, err
	}
	state, ok := r.imageState[reference]
	if !ok {
		state = localrepodomain.SyncReady
	}
	status := localrepodomain.ImageStatus{Reference: reference, State: state}
	if state == localrepodomain.SyncReady {
		status.SizeBytes = 2048
	}
	return status, nil
}

func (r *fakeRepository) RemoveSource(_ context.Context, name string, format localrepodomain.Format) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, string(format)+"/"+name)
	return r.errs["remove "+name]
}

func (r *fakeRepository) Endpoints(context.Context) (localrepodomain.HostEndpoints, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.errs["endpoints"]; err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	return r.endpoints, nil
}

func (r *fakeRepository) Browse(_ context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.browsed = append(r.browsed, query)
	if err := r.errs["browse"]; err != nil {
		return localrepodomain.ContentPage{}, err
	}
	return r.page, nil
}

// fakeRepositories serves one Repository for every Integration it knows, or fails with *err when
// it is set.
type fakeRepositories struct {
	repository *fakeRepository
	known      map[string]bool
	err        *error
}

func (f fakeRepositories) For(_ context.Context, integrationID string) (localrepodomain.Repository, error) {
	if f.err != nil && *f.err != nil {
		return nil, *f.err
	}
	if !f.known[integrationID] {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return f.repository, nil
}

// fakeRequester records the controllers asked to converge.
type fakeRequester struct {
	requested []string
}

func (f *fakeRequester) Request(_ context.Context, controller string) error {
	f.requested = append(f.requested, controller)
	return nil
}

func unavailable(detail string) error {
	return &localrepodomain.ProviderError{Kind: localrepodomain.ProviderErrorUnavailable, Detail: detail, Operation: "test"}
}

func refused(detail string) error {
	return &localrepodomain.ProviderError{Kind: localrepodomain.ProviderErrorRejected, Detail: detail, Operation: "test"}
}
