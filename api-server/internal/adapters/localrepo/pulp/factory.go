package pulp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// ProviderKind is the provider kind of a `repository` Integration served by Pulp 3. It is persisted
// in Integrations, so it stays stable.
const ProviderKind = "pulp"

// Timeouts of the default HTTP client. Every Pulp API call is short (lists, creates, task
// dispatches); the slow work runs in Pulp's tasks.
const (
	apiTimeout            = 60 * time.Second
	responseHeaderTimeout = 60 * time.Second
)

// RepositoryFactory builds the Pulp Repository of a `repository` Integration
// (localrepodomain.RepositoryFactory).
//
// Repositories are cached per Integration and rebuilt when the Integration's UpdatedAt changes,
// which it does whenever its endpoint, settings, or credential change, so a polling controller
// reuses one HTTP connection pool instead of building one per call. A cached Repository holds the
// admin password in memory for as long as the Integration is unchanged; it is never logged, and is
// sent only as Basic authentication to the Integration's endpoint. The factory is safe for
// concurrent use.
//
// One process uses one factory for its controller: the image downloads it tracks are the process's,
// and a second factory would start the same downloads again.
type RepositoryFactory struct {
	integrations sitedomain.IntegrationRepository
	client       *http.Client
	registry     *http.Client
	// downloads is shared by every Repository the factory builds, so the bound on concurrent image
	// downloads, and which images are downloading or recently failed, hold across Integration
	// changes.
	downloads *imageDownloads
	now       func() time.Time

	mu     sync.Mutex
	cached map[string]cachedRepository
}

type cachedRepository struct {
	repository *Repository
	updatedAt  time.Time
}

var _ localrepodomain.RepositoryFactory = (*RepositoryFactory)(nil)

// NewRepositoryFactory returns a factory reading Integrations from integrations. client carries
// every Pulp API call; nil means a client with a 60-second timeout per call. Image downloads use a
// copy of client without its overall timeout (a layer may take long on a slow uplink); the
// transport's own timeouts still apply, and each download bounds itself.
func NewRepositoryFactory(integrations sitedomain.IntegrationRepository, client *http.Client) *RepositoryFactory {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		client = &http.Client{Timeout: apiTimeout, Transport: transport}
	}
	registry := *client
	registry.Timeout = 0
	return &RepositoryFactory{
		integrations: integrations,
		client:       client,
		registry:     &registry,
		downloads:    newImageDownloads(),
		now:          time.Now,
		cached:       make(map[string]cachedRepository),
	}
}

// For returns the Repository of the Integration integrationID. It reads the Integration and its
// credential but does not contact Pulp. It returns the repository's errors as they are
// (sitedomain.ErrIntegrationNotFound, sitedomain.ErrCredentialNotSet), and a rejected
// *localrepodomain.ProviderError for an Integration that is not a Pulp `repository` one, or whose
// endpoint or contentOrigin setting is not an http or https URL.
func (f *RepositoryFactory) For(ctx context.Context, integrationID string) (localrepodomain.Repository, error) {
	integration, err := f.integrations.FindByID(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindRepository || integration.ProviderKind != ProviderKind {
		return nil, rejected("open repository", fmt.Sprintf(
			"Integration %q is a %s Integration of provider kind %q; the Pulp adapter serves only %s Integrations of provider kind %q.",
			integration.Name, integration.Kind, integration.ProviderKind, sitedomain.IntegrationKindRepository, ProviderKind))
	}

	f.mu.Lock()
	if hit, ok := f.cached[integrationID]; ok && hit.updatedAt.Equal(integration.UpdatedAt) {
		f.mu.Unlock()
		return hit.repository, nil
	}
	f.mu.Unlock()

	password, err := f.integrations.Credential(ctx, integration.ID)
	if err != nil {
		return nil, err
	}
	repository, err := f.build(integration, password)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.cached[integrationID] = cachedRepository{repository: repository, updatedAt: integration.UpdatedAt}
	f.mu.Unlock()
	return repository, nil
}

// build validates the Integration's addresses and assembles its Repository.
func (f *RepositoryFactory) build(integration *sitedomain.Integration, password string) (*Repository, error) {
	api, err := newClient(integration.Endpoint, password, f.client)
	if err != nil {
		return nil, rejected("open repository", fmt.Sprintf("Integration %q: %v.", integration.Name, err))
	}
	origin, err := normalizeOrigin(integration.Setting(localrepodomain.SettingContentOrigin, ""), localrepodomain.SettingContentOrigin+" setting")
	if err == nil && strings.Count(origin, "/") > 2 {
		// The registry API must be at the root of the origin, so a path cannot be honored.
		err = fmt.Errorf("the Pulp %s setting must be an origin without a path", localrepodomain.SettingContentOrigin)
	}
	if err != nil {
		return nil, rejected("open repository", fmt.Sprintf(
			"Integration %q: %v; set it to the origin hosts read content from, for example http://10.0.0.5:8080.", integration.Name, err))
	}
	return &Repository{
		api:           api,
		contentOrigin: origin,
		registry:      f.registry,
		downloads:     f.downloads,
		now:           f.now,
	}, nil
}
