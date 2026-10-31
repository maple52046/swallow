package tests

import (
	"context"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// fakeOSImageMetadataRepo is an in-memory OSImageMetadataRepository for HTTP tests. It mirrors
// the natural-key behavior of the Mongo repository: metadata records are keyed by
// (integrationId, imageId, architecture), a missing metadata is a normal absence, and delete of
// an absent metadata succeeds.
type fakeOSImageMetadataRepo struct {
	mu              sync.Mutex
	metadataRecords map[string]*provisioningdomain.OSImageMetadata
}

func newFakeOSImageMetadataRepo() *fakeOSImageMetadataRepo {
	return &fakeOSImageMetadataRepo{metadataRecords: make(map[string]*provisioningdomain.OSImageMetadata)}
}

func fakeOSImageMetadataKey(integrationID, imageID, architecture string) string {
	return integrationID + "\x00" + imageID + "\x00" + architecture
}

func (r *fakeOSImageMetadataRepo) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*provisioningdomain.OSImageMetadata
	for _, metadata := range r.metadataRecords {
		if metadata.IntegrationID == integrationID {
			clone := *metadata
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *fakeOSImageMetadataRepo) Upsert(_ context.Context, metadata *provisioningdomain.OSImageMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *metadata
	r.metadataRecords[fakeOSImageMetadataKey(metadata.IntegrationID, metadata.ImageID, metadata.Architecture)] = &clone
	return nil
}

func (r *fakeOSImageMetadataRepo) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.metadataRecords, fakeOSImageMetadataKey(integrationID, imageID, architecture))
	return nil
}
