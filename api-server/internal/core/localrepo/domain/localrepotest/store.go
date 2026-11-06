// Package localrepotest provides an in-memory localrepodomain.Store for tests of the Local
// Repository use cases, handlers, and controller, and of the code that composes them. It follows
// the store contract, including the conditional spec write, but is not durable; production code
// uses the MongoDB store.
package localrepotest

import (
	"context"
	"sort"
	"sync"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// Store is an in-memory localrepodomain.Store. StatusWrites counts SetStatus calls. Records are
// returned as shallow copies, so a test must not change the slices of a record it read.
type Store struct {
	mu           sync.Mutex
	records      map[string]localrepodomain.LocalRepository
	StatusWrites int
}

var _ localrepodomain.Store = (*Store)(nil)

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{records: map[string]localrepodomain.LocalRepository{}}
}

// Put stores record as is, for a test's setup.
func (s *Store) Put(record localrepodomain.LocalRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.SiteID] = record
}

// Get implements localrepodomain.Store.
func (s *Store) Get(_ context.Context, siteID string) (*localrepodomain.LocalRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[siteID]
	if !ok {
		return nil, localrepodomain.ErrLocalRepositoryNotFound
	}
	return &record, nil
}

// List implements localrepodomain.Store, ordered by Site id so tests compare a stable order.
func (s *Store) List(context.Context) ([]*localrepodomain.LocalRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*localrepodomain.LocalRepository, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, &record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SiteID < out[j].SiteID })
	return out, nil
}

// PutSpec implements localrepodomain.Store: a missing record counts as generation 0.
func (s *Store) PutSpec(_ context.Context, siteID string, expectedGeneration int64, spec localrepodomain.Spec) (*localrepodomain.LocalRepository, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[siteID]
	if record.Generation != expectedGeneration {
		return nil, localrepodomain.ErrGenerationConflict
	}
	record.SiteID, record.Spec, record.Generation = siteID, spec, record.Generation+1
	s.records[siteID] = record
	return &record, nil
}

// SetStatus implements localrepodomain.Store and counts the write in StatusWrites.
func (s *Store) SetStatus(_ context.Context, siteID string, status localrepodomain.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.records[siteID]
	record.SiteID, record.Status = siteID, status
	s.records[siteID] = record
	s.StatusWrites++
	return nil
}

// Delete implements localrepodomain.Store.
func (s *Store) Delete(_ context.Context, siteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, siteID)
	return nil
}
