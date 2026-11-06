package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/localrepo/application"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/core/localrepo/domain/localrepotest"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

type handlerSites struct {
	sitedomain.SiteRepository
}

func (handlerSites) FindByID(_ context.Context, id string) (*sitedomain.Site, error) {
	if id != "site-1" && id != "site-2" {
		return nil, sitedomain.ErrSiteNotFound
	}
	return &sitedomain.Site{ID: id}, nil
}

// handlerIntegrations holds site-1's repository Integration only.
type handlerIntegrations struct {
	sitedomain.IntegrationRepository
}

func (handlerIntegrations) List(_ context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	if filter.SiteID != "site-1" || filter.Kind != sitedomain.IntegrationKindRepository {
		return nil, nil
	}
	return []*sitedomain.Integration{{
		ID: "repo-1", SiteID: "site-1", Kind: sitedomain.IntegrationKindRepository, Name: "local", Enabled: true,
		Settings: map[string]string{localrepodomain.SettingContentOrigin: "http://10.0.0.5:8080"},
	}}, nil
}

// handlerRepository serves one page of content, or err.
type handlerRepository struct {
	localrepodomain.Repository
	err error
}

func (r handlerRepository) Browse(context.Context, localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	if r.err != nil {
		return localrepodomain.ContentPage{}, r.err
	}
	return localrepodomain.ContentPage{Items: []localrepodomain.ContentItem{
		{Name: "docker-ce", Version: "5:29.0.0", Architecture: "amd64", Source: "docker-ce", SizeBytes: 1024},
		{Name: "k0s/k0s-v1", Path: "k0s/k0s-v1"},
	}, Total: 2}, nil
}

type handlerRepositories struct {
	repository handlerRepository
}

func (f handlerRepositories) For(context.Context, string) (localrepodomain.Repository, error) {
	return f.repository, nil
}

type handlerRequester struct{}

func (handlerRequester) Request(context.Context, string) error { return nil }

var handlerModules = []localrepodomain.ModuleDeclaration{{
	ModuleID: "docker-ce",
	Sources: []localrepodomain.Source{{
		Name: "docker-ce", Format: localrepodomain.FormatDeb, URL: "https://download.example/ubuntu",
		Suites: []string{"noble"}, Components: []string{"stable"},
	}},
	Files:  []localrepodomain.File{{Path: "k0s/k0s-v1", URL: "https://files.example/k0s-v1", SHA256: "aa"}},
	Images: []string{"quay.io/k0sproject/coredns:1.14.1", "quay.io/k0sproject/pause:3.10"},
	Python: []string{"numpy==2.5.4"},
}}

// newHandlerApp mounts the routes at their production paths behind the contract validator, over a
// store holding an observed record for site-1.
func newHandlerApp(t *testing.T, browseErr error) *fiber.App {
	t.Helper()
	records := localrepotest.NewStore()
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	source := handlerModules[0].Sources[0]
	records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Status: localrepodomain.Status{
		IntegrationID: "repo-1", ObservedAt: &now,
		Sources: []localrepodomain.ObservedSource{{Source: source, Status: localrepodomain.SourceStatus{
			Name: source.Name, Format: source.Format, State: localrepodomain.SyncReady, LastSyncedAt: &now, ContentCount: 12,
		}}},
		Files: []localrepodomain.ObservedFile{{File: handlerModules[0].Files[0], Status: localrepodomain.FileStatus{
			Path: "k0s/k0s-v1", State: localrepodomain.SyncFailed, Message: "checksum mismatch",
		}}},
		Images: []localrepodomain.ObservedImage{{Reference: "quay.io/k0sproject/pause:3.10", Status: localrepodomain.ImageStatus{
			Reference: "quay.io/k0sproject/pause:3.10", State: localrepodomain.SyncReady, SizeBytes: 321_000,
		}}},
		Endpoints: &localrepodomain.HostEndpoints{Registry: "10.0.0.5:8080"},
		Conditions: []convergence.Condition{
			{Type: convergence.ConditionRealized, Status: convergence.ConditionFalse, Reason: localrepodomain.ReasonFileFailed,
				Message: "file k0s/k0s-v1 failed: checksum mismatch", LastTransitionTime: now},
			{Type: localrepodomain.ConditionReady, Status: convergence.ConditionFalse, Reason: localrepodomain.ReasonFileFailed,
				Message: "file k0s/k0s-v1 failed: checksum mismatch", LastTransitionTime: now},
		},
	}})
	service := application.NewService(handlerSites{}, handlerIntegrations{}, records,
		handlerRepositories{repository: handlerRepository{err: browseErr}}, handlerRequester{}, handlerModules)
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	NewHandler(service).RegisterRoutes(app.Group("/api/v1/sites"))
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]any
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return resp, decoded
}

// GET returns the documented shape: the effective sources, files, and images with their state, the
// Python requirements with their declarers, and not ready while a file failed.
func TestGetLocalRepository(t *testing.T) {
	app := newHandlerApp(t, nil)
	resp, body := doRequest(t, app, http.MethodGet, "/api/v1/sites/site-1/local-repository", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, body %v", resp.StatusCode, body)
	}
	if body["ready"] != false || body["integrationId"] != "repo-1" || body["contentOrigin"] != "http://10.0.0.5:8080" {
		t.Errorf("GET = %v; want not ready, repo-1, the content origin", body)
	}
	sources, _ := body["sources"].([]any)
	if len(sources) != 3 {
		t.Fatalf("sources = %v; want docker-ce, pypi, quay.io", sources)
	}
	docker, _ := sources[0].(map[string]any)
	if docker["name"] != "docker-ce" || docker["state"] != "ready" || docker["contentCount"] != float64(12) {
		t.Errorf("docker-ce = %v, want ready with 12 items", docker)
	}
	files, _ := body["files"].([]any)
	if file, _ := files[0].(map[string]any); file["state"] != "failed" || file["message"] != "checksum mismatch" {
		t.Errorf("files = %v, want the failed file with its message", files)
	}
	// An image the status holds reads as observed; one it does not hold reads pending.
	images, _ := body["images"].([]any)
	wantImages := []any{
		map[string]any{"reference": "quay.io/k0sproject/coredns:1.14.1", "declaredBy": []any{"module:docker-ce"}, "state": "pending", "message": nil, "sizeBytes": float64(0)},
		map[string]any{"reference": "quay.io/k0sproject/pause:3.10", "declaredBy": []any{"module:docker-ce"}, "state": "ready", "message": nil, "sizeBytes": float64(321_000)},
	}
	if !reflect.DeepEqual(images, wantImages) {
		t.Errorf("images = %v, want %v", images, wantImages)
	}
	conditions, _ := body["conditions"].([]any)
	ready, _ := conditions[1].(map[string]any)
	if len(conditions) != 2 || ready["type"] != "ready" || ready["status"] != "false" || ready["reason"] != "file_failed" ||
		body["observedGeneration"] != float64(0) {
		t.Errorf("conditions = %v, observedGeneration %v; want realized and ready false with file_failed at 0", conditions, body["observedGeneration"])
	}

	for path, want := range map[string]int{
		"/api/v1/sites/site-2/local-repository": http.StatusNotFound,
		"/api/v1/sites/site-9/local-repository": http.StatusNotFound,
	} {
		if resp, body := doRequest(t, app, http.MethodGet, path, ""); resp.StatusCode != want {
			t.Errorf("GET %s = %d %v, want %d", path, resp.StatusCode, body, want)
		}
	}
}

// PUT stores the sources at the generation read and refuses a stale generation, a missing one, and
// an invalid source.
func TestPutLocalRepositorySpec(t *testing.T) {
	app := newHandlerApp(t, nil)
	ubuntu := `{"name": "ubuntu", "format": "deb", "url": "http://archive.example/ubuntu/", "suites": ["noble"], "components": ["main"]}`
	resp, body := doRequest(t, app, http.MethodPut, "/api/v1/sites/site-1/local-repository/spec", `{"generation": 0, "sources": [`+ubuntu+`]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, body %v", resp.StatusCode, body)
	}
	if spec, _ := body["spec"].(map[string]any); spec["generation"] != float64(1) {
		t.Errorf("PUT spec = %v, want generation 1", body["spec"])
	}
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"stale generation", `{"generation": 0, "sources": []}`, http.StatusConflict},
		{"no generation", `{"sources": []}`, http.StatusBadRequest},
		{"invalid source", `{"generation": 1, "sources": [{"name": "x", "format": "deb", "url": "ftp://x"}]}`, http.StatusBadRequest},
		{"not JSON", `{`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if resp, body := doRequest(t, app, http.MethodPut, "/api/v1/sites/site-1/local-repository/spec", tc.body); resp.StatusCode != tc.want {
				t.Errorf("PUT %s = %d %v, want %d", tc.body, resp.StatusCode, body, tc.want)
			}
		})
	}
}

// The content route pages the provider's items, nulling the fields a format does not have, and maps
// query and provider failures.
func TestBrowseLocalRepositoryContent(t *testing.T) {
	app := newHandlerApp(t, nil)
	resp, body := doRequest(t, app, http.MethodGet, "/api/v1/sites/site-1/local-repository/content?format=deb&page=2&pageSize=10", "")
	if resp.StatusCode != http.StatusOK || body["total"] != float64(2) || body["page"] != float64(2) || body["pageSize"] != float64(10) {
		t.Fatalf("GET content = %d %v; want the page envelope", resp.StatusCode, body)
	}
	items, _ := body["items"].([]any)
	if file, _ := items[1].(map[string]any); file["version"] != nil || file["path"] != "k0s/k0s-v1" || file["sizeBytes"] != nil {
		t.Errorf("file item = %v, want version and sizeBytes null", file)
	}
	for _, query := range []string{"", "?format=zip", "?format=deb&pageSize=500", "?format=deb&page=0"} {
		if resp, body := doRequest(t, app, http.MethodGet, "/api/v1/sites/site-1/local-repository/content"+query, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET content%s = %d %v, want 400", query, resp.StatusCode, body)
		}
	}

	down := newHandlerApp(t, &localrepodomain.ProviderError{Kind: localrepodomain.ProviderErrorUnavailable, Detail: "The repository could not be reached."})
	if resp, body := doRequest(t, down, http.MethodGet, "/api/v1/sites/site-1/local-repository/content?format=deb", ""); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("GET content with the provider down = %d %v, want 503", resp.StatusCode, body)
	}
}

func TestSyncLocalRepository(t *testing.T) {
	app := newHandlerApp(t, nil)
	if resp, _ := doRequest(t, app, http.MethodPost, "/api/v1/sites/site-1/local-repository/sync", ""); resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST sync = %d, want 202", resp.StatusCode)
	}
	if resp, _ := doRequest(t, app, http.MethodPost, "/api/v1/sites/site-2/local-repository/sync", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST sync of a Site without a repository = %d, want 404", resp.StatusCode)
	}
}
