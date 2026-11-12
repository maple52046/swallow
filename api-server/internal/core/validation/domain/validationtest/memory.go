// Package validationtest gives the Validation context's tests in-memory stores. Only tests import
// it; production code uses the durable stores of the MongoDB adapter.
package validationtest

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// Runs is an in-memory RunRepository. List applies the status, activity, and Server filters and
// returns runs newest first; it is safe for concurrent use.
type Runs struct {
	mu   sync.Mutex
	runs map[string]validationdomain.Run
}

// NewRuns returns an empty run store.
func NewRuns() *Runs { return &Runs{runs: map[string]validationdomain.Run{}} }

// Create implements validationdomain.RunRepository.
func (m *Runs) Create(_ context.Context, run validationdomain.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[run.ID] = run
	return nil
}

// Save implements validationdomain.RunRepository.
func (m *Runs) Save(_ context.Context, run validationdomain.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[run.ID]; !ok {
		return validationdomain.ErrRunNotFound
	}
	m.runs[run.ID] = run
	return nil
}

// Get implements validationdomain.RunRepository.
func (m *Runs) Get(_ context.Context, id string) (validationdomain.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[id]
	if !ok {
		return validationdomain.Run{}, fmt.Errorf("%w: %s", validationdomain.ErrRunNotFound, id)
	}
	return run, nil
}

// List implements validationdomain.RunRepository.
func (m *Runs) List(_ context.Context, filter validationdomain.RunFilter) ([]validationdomain.Run, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var runs []validationdomain.Run
	for _, run := range m.runs {
		if filter.SiteID != "" && run.SiteID != filter.SiteID {
			continue
		}
		if filter.SuiteID != "" && run.Suite.ID != filter.SuiteID {
			continue
		}
		if filter.ServerID != "" && !slices.Contains(run.Target.ServerIDs, filter.ServerID) && !slices.Contains(run.Target.MemberIDs, filter.ServerID) {
			continue
		}
		if filter.PlatformID != "" && run.Target.PlatformID != filter.PlatformID {
			continue
		}
		if filter.ScheduleID != "" && run.Trigger.ScheduleID != filter.ScheduleID {
			continue
		}
		if filter.Status != "" && run.Status != filter.Status {
			continue
		}
		if filter.Verdict != "" && run.Verdict != filter.Verdict {
			continue
		}
		if filter.Trigger != "" && run.Trigger.Kind != filter.Trigger {
			continue
		}
		if filter.ActiveOnly && !run.Status.Active() {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].RequestedAt.After(runs[j].RequestedAt) })
	total := len(runs)
	start := min(max(filter.Offset, 0), total)
	end := total
	if filter.Limit > 0 {
		end = min(start+filter.Limit, total)
	}
	return runs[start:end], total, nil
}

// Suites is an in-memory SuiteRepository.
type Suites struct {
	suites map[string]validationdomain.Suite
}

// NewSuites returns an empty suite store.
func NewSuites() *Suites { return &Suites{suites: map[string]validationdomain.Suite{}} }

// Create implements validationdomain.SuiteRepository.
func (m *Suites) Create(_ context.Context, suite validationdomain.Suite) error {
	m.suites[suite.ID] = suite
	return nil
}

// Replace implements validationdomain.SuiteRepository.
func (m *Suites) Replace(_ context.Context, suite validationdomain.Suite) error {
	if _, ok := m.suites[suite.ID]; !ok {
		return validationdomain.ErrSuiteNotFound
	}
	m.suites[suite.ID] = suite
	return nil
}

// Get implements validationdomain.SuiteRepository.
func (m *Suites) Get(_ context.Context, id string) (validationdomain.Suite, error) {
	suite, ok := m.suites[id]
	if !ok {
		return validationdomain.Suite{}, validationdomain.ErrSuiteNotFound
	}
	return suite, nil
}

// List implements validationdomain.SuiteRepository.
func (m *Suites) List(_ context.Context, includeRetired bool) ([]validationdomain.Suite, error) {
	var suites []validationdomain.Suite
	for _, suite := range m.suites {
		if includeRetired || !suite.Retired {
			suites = append(suites, suite)
		}
	}
	return suites, nil
}

// Schedules is an in-memory ScheduleRepository.
type Schedules struct {
	mu        sync.Mutex
	schedules map[string]validationdomain.Schedule
}

// NewSchedules returns an empty schedule store.
func NewSchedules() *Schedules {
	return &Schedules{schedules: map[string]validationdomain.Schedule{}}
}

// Create implements validationdomain.ScheduleRepository.
func (m *Schedules) Create(_ context.Context, schedule validationdomain.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedules[schedule.ID] = schedule
	return nil
}

// WriteSpec implements validationdomain.ScheduleRepository.
func (m *Schedules) WriteSpec(_ context.Context, id string, expected int64, spec validationdomain.ScheduleSpec, siteID string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	schedule, ok := m.schedules[id]
	if !ok {
		return 0, validationdomain.ErrScheduleNotFound
	}
	if expected > 0 && expected != schedule.Generation {
		return 0, validationdomain.ErrGenerationConflict
	}
	schedule.Spec, schedule.SiteID, schedule.UpdatedAt = spec, siteID, now
	schedule.Generation++
	m.schedules[id] = schedule
	return schedule.Generation, nil
}

// WriteStatus implements validationdomain.ScheduleRepository.
func (m *Schedules) WriteStatus(_ context.Context, id string, status validationdomain.ScheduleStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	schedule, ok := m.schedules[id]
	if !ok {
		return validationdomain.ErrScheduleNotFound
	}
	schedule.Status = status
	m.schedules[id] = schedule
	return nil
}

// Get implements validationdomain.ScheduleRepository.
func (m *Schedules) Get(_ context.Context, id string) (validationdomain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	schedule, ok := m.schedules[id]
	if !ok {
		return validationdomain.Schedule{}, validationdomain.ErrScheduleNotFound
	}
	return schedule, nil
}

// List implements validationdomain.ScheduleRepository.
func (m *Schedules) List(_ context.Context, filter validationdomain.ScheduleFilter) ([]validationdomain.Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var schedules []validationdomain.Schedule
	for _, schedule := range m.schedules {
		if filter.SiteID == "" || schedule.SiteID == filter.SiteID {
			schedules = append(schedules, schedule)
		}
	}
	return schedules, nil
}

// Delete implements validationdomain.ScheduleRepository.
func (m *Schedules) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return validationdomain.ErrScheduleNotFound
	}
	delete(m.schedules, id)
	return nil
}

// UsesSuite implements validationdomain.ScheduleRepository.
func (m *Schedules) UsesSuite(_ context.Context, suiteID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, schedule := range m.schedules {
		if schedule.Spec.SuiteID == suiteID {
			return true, nil
		}
	}
	return false, nil
}
