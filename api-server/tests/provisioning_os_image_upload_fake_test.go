package tests

import (
	"context"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// fakeOSImageUploadRepo is an in-memory OSImageUploadRepository for HTTP tests. It mirrors the
// Mongo repository's claim: only a pending, unexpired upload can be claimed, a sending one is
// in progress, and anything else is not found. Expired records are not discarded, which only
// matters to tests that look at what is left.
type fakeOSImageUploadRepo struct {
	mu      sync.Mutex
	uploads map[string]*provisioningdomain.OSImageUpload
}

func newFakeOSImageUploadRepo() *fakeOSImageUploadRepo {
	return &fakeOSImageUploadRepo{uploads: make(map[string]*provisioningdomain.OSImageUpload)}
}

func (r *fakeOSImageUploadRepo) Create(_ context.Context, upload *provisioningdomain.OSImageUpload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *upload
	r.uploads[upload.ID] = &stored
	return nil
}

func (r *fakeOSImageUploadRepo) Claim(_ context.Context, id string, now time.Time) (*provisioningdomain.OSImageUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[id]
	switch {
	case !ok:
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	case upload.State == provisioningdomain.OSImageUploadSending:
		return nil, provisioningdomain.ErrOSImageUploadInProgress
	case !upload.ExpiresAt.After(now):
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	}
	upload.State = provisioningdomain.OSImageUploadSending
	claimed := *upload
	return &claimed, nil
}

func (r *fakeOSImageUploadRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.uploads, id)
	return nil
}

// count reports how many upload records remain, so a test can confirm a finished content
// request removed its upload.
func (r *fakeOSImageUploadRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.uploads)
}
