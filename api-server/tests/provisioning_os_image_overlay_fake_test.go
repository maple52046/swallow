package tests

import (
	"context"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// fakeOSImageOverlayRepo is an in-memory OSImageOverlayRepository for HTTP tests. It mirrors
// the natural-key behavior of the Mongo repository: overlays are keyed by
// (integrationId, imageId, architecture), a missing overlay is a normal absence, and delete of
// an absent overlay succeeds.
type fakeOSImageOverlayRepo struct {
	mu       sync.Mutex
	overlays map[string]*provisioningdomain.OSImageOverlay
}

func newFakeOSImageOverlayRepo() *fakeOSImageOverlayRepo {
	return &fakeOSImageOverlayRepo{overlays: make(map[string]*provisioningdomain.OSImageOverlay)}
}

func fakeOSImageOverlayKey(integrationID, imageID, architecture string) string {
	return integrationID + "\x00" + imageID + "\x00" + architecture
}

func (r *fakeOSImageOverlayRepo) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageOverlay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*provisioningdomain.OSImageOverlay
	for _, overlay := range r.overlays {
		if overlay.IntegrationID == integrationID {
			clone := *overlay
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *fakeOSImageOverlayRepo) Upsert(_ context.Context, overlay *provisioningdomain.OSImageOverlay) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *overlay
	r.overlays[fakeOSImageOverlayKey(overlay.IntegrationID, overlay.ImageID, overlay.Architecture)] = &clone
	return nil
}

func (r *fakeOSImageOverlayRepo) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overlays, fakeOSImageOverlayKey(integrationID, imageID, architecture))
	return nil
}
