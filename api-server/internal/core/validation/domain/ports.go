package domain

import (
	"context"
	"time"
)

// SuiteRepository stores the user-defined suites (module suites come from the build). Production
// implementations are durable; Get returns ErrSuiteNotFound for an unknown id, retired suites
// included in every read.
type SuiteRepository interface {
	Create(ctx context.Context, suite Suite) error
	// Replace writes the whole suite; ErrSuiteNotFound when it does not exist.
	Replace(ctx context.Context, suite Suite) error
	Get(ctx context.Context, id string) (Suite, error)
	List(ctx context.Context, includeRetired bool) ([]Suite, error)
}

// RunFilter selects runs; every field is optional and exact-match. ServerID matches a run whose
// target or member Servers include it.
type RunFilter struct {
	SuiteID    string
	ServerID   string
	PlatformID string
	ScheduleID string
	Status     Status
	Verdict    Verdict
	Trigger    TriggerKind
	ActiveOnly bool
	Offset     int
	Limit      int
}

// RunRepository stores Validation Runs. Create and Save are called inside the transaction of the
// Workflow transition or acceptance they belong to; Get returns ErrRunNotFound for an unknown id
// (including one past its retention). The store deletes a run after its ExpiresAt.
type RunRepository interface {
	Create(ctx context.Context, run Run) error
	// Save replaces the run; ErrRunNotFound when it does not exist.
	Save(ctx context.Context, run Run) error
	Get(ctx context.Context, id string) (Run, error)
	// List returns one page newest first and the total matching.
	List(ctx context.Context, filter RunFilter) ([]Run, int, error)
}

// ScheduleRepository stores Validation Schedules. WriteSpec is the API's write; WriteStatus the
// controller's; neither changes the other's fields.
type ScheduleRepository interface {
	Create(ctx context.Context, schedule Schedule) error
	// WriteSpec replaces the spec at expectedGeneration (0 accepts any) and increments the
	// generation, returning it: ErrScheduleNotFound or ErrGenerationConflict otherwise.
	WriteSpec(ctx context.Context, id string, expectedGeneration int64, spec ScheduleSpec, siteID string, now time.Time) (int64, error)
	WriteStatus(ctx context.Context, id string, status ScheduleStatus) error
	Get(ctx context.Context, id string) (Schedule, error)
	List(ctx context.Context) ([]Schedule, error)
	Delete(ctx context.Context, id string) error
	// UsesSuite reports whether a schedule names the suite.
	UsesSuite(ctx context.Context, suiteID string) (bool, error)
}
