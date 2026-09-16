package tests

import (
	"context"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// fakeServerTagOverlayRepo is an in-memory ServerTagOverlayRepository for HTTP tests. It mirrors
// the Mongo repository: overlays are keyed by ServerID, a missing overlay is a normal absence
// (ErrServerTagOverlayNotFound from Get), and delete of an absent overlay succeeds.
type fakeServerTagOverlayRepo struct {
	mu       sync.Mutex
	overlays map[string]*provisioningdomain.ServerTagOverlay
}

func newFakeServerTagOverlayRepo() *fakeServerTagOverlayRepo {
	return &fakeServerTagOverlayRepo{overlays: make(map[string]*provisioningdomain.ServerTagOverlay)}
}

func (r *fakeServerTagOverlayRepo) Get(
	_ context.Context, serverID string,
) (*provisioningdomain.ServerTagOverlay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	overlay, ok := r.overlays[serverID]
	if !ok {
		return nil, provisioningdomain.ErrServerTagOverlayNotFound
	}
	clone := *overlay
	return &clone, nil
}

func (r *fakeServerTagOverlayRepo) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.ServerTagOverlay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*provisioningdomain.ServerTagOverlay
	for _, overlay := range r.overlays {
		if overlay.IntegrationID == integrationID {
			clone := *overlay
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *fakeServerTagOverlayRepo) Upsert(_ context.Context, overlay *provisioningdomain.ServerTagOverlay) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *overlay
	r.overlays[overlay.ServerID] = &clone
	return nil
}

func (r *fakeServerTagOverlayRepo) Delete(_ context.Context, serverID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overlays, serverID)
	return nil
}
