package tests

import (
	"context"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// fakeOSImageVerificationRepo is an in-memory OSImageVerificationRepository for HTTP tests. It
// mirrors the Mongo repository: verifications are keyed by (integrationId, imageId, architecture),
// per-target evidence is merged so verifying one target never clears another, a missing record is a
// normal absence, and delete of an absent record succeeds.
type fakeOSImageVerificationRepo struct {
	mu            sync.Mutex
	verifications map[string]*provisioningdomain.OSImageVerification
}

func newFakeOSImageVerificationRepo() *fakeOSImageVerificationRepo {
	return &fakeOSImageVerificationRepo{verifications: make(map[string]*provisioningdomain.OSImageVerification)}
}

func (r *fakeOSImageVerificationRepo) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*provisioningdomain.OSImageVerification
	for _, verification := range r.verifications {
		if verification.IntegrationID == integrationID {
			out = append(out, cloneVerification(verification))
		}
	}
	return out, nil
}

func (r *fakeOSImageVerificationRepo) Find(
	_ context.Context, integrationID, imageID, architecture string,
) (*provisioningdomain.OSImageVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	verification, ok := r.verifications[fakeOSImageOverlayKey(integrationID, imageID, architecture)]
	if !ok {
		return nil, nil
	}
	return cloneVerification(verification), nil
}

func (r *fakeOSImageVerificationRepo) RecordTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget,
	evidence provisioningdomain.OSImageVerificationEvidence,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fakeOSImageOverlayKey(integrationID, imageID, architecture)
	verification, ok := r.verifications[key]
	if !ok {
		verification = &provisioningdomain.OSImageVerification{
			IntegrationID: integrationID, ImageID: imageID, Architecture: architecture,
			Targets: map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence{},
		}
		r.verifications[key] = verification
	}
	verification.Targets[target] = evidence
	verification.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeOSImageVerificationRepo) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.verifications, fakeOSImageOverlayKey(integrationID, imageID, architecture))
	return nil
}

func (r *fakeOSImageVerificationRepo) DeleteByIntegration(_ context.Context, integrationID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, verification := range r.verifications {
		if verification.IntegrationID == integrationID {
			delete(r.verifications, key)
		}
	}
	return nil
}

func cloneVerification(v *provisioningdomain.OSImageVerification) *provisioningdomain.OSImageVerification {
	targets := make(map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence, len(v.Targets))
	for target, evidence := range v.Targets {
		targets[target] = evidence
	}
	return &provisioningdomain.OSImageVerification{
		IntegrationID: v.IntegrationID, ImageID: v.ImageID, Architecture: v.Architecture,
		Targets: targets, UpdatedAt: v.UpdatedAt,
	}
}
