package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// ScheduleTriggerKind is when a Validation Schedule starts a run.
type ScheduleTriggerKind string

// The schedule triggers.
const (
	// ScheduleCron starts a run each time a cron expression comes due.
	ScheduleCron ScheduleTriggerKind = "cron"
	// ScheduleAfterDeployment starts a run after each deployment of the target: a Platform's newly
	// applied spec generation, or a deploy-os Workflow that succeeded on a target Server.
	ScheduleAfterDeployment ScheduleTriggerKind = "after-deployment"
)

// ScheduleTrigger is a schedule's trigger.
type ScheduleTrigger struct {
	Kind     ScheduleTriggerKind
	Cron     string
	Timezone string
}

// ScheduleSpec is a Validation Schedule's desired state, written only by the API.
type ScheduleSpec struct {
	Name       string
	SuiteID    string
	ServerIDs  []string
	PlatformID string
	Parameters map[string]any
	Trigger    ScheduleTrigger
	Enabled    bool
}

// ScheduleStatus is what the schedule's controller observed and did; derived, written only by it.
type ScheduleStatus struct {
	ObservedGeneration int64
	// NextRunAt is the next cron time, nil for after-deployment and while disabled.
	NextRunAt *time.Time
	LastRunID string
	LastRunAt *time.Time
	// LastSkipReason says why the latest occurrence started no run; empty after one did.
	LastSkipReason string
	// LastDeploymentKey identifies the latest deployment an after-deployment schedule handled, so
	// one deployment starts one run.
	LastDeploymentKey string
	Conditions        []convergence.Condition
}

// Schedule is a Validation Schedule.
type Schedule struct {
	ID         string
	SiteID     string
	Generation int64
	Spec       ScheduleSpec
	Status     ScheduleStatus
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Normalize trims the spec and checks what it can on its own: a name, a suite, targets of one kind
// at most, and a trigger whose fields fit its kind (a cron trigger's expression and zone parse; an
// after-deployment trigger has neither). Whether the targets fit the suite is the service's check.
func (s ScheduleSpec) Normalize() (ScheduleSpec, error) {
	s.Name, s.SuiteID, s.PlatformID = strings.TrimSpace(s.Name), strings.TrimSpace(s.SuiteID), strings.TrimSpace(s.PlatformID)
	s.Trigger.Cron, s.Trigger.Timezone = strings.TrimSpace(s.Trigger.Cron), strings.TrimSpace(s.Trigger.Timezone)
	if s.Name == "" || s.SuiteID == "" {
		return ScheduleSpec{}, fmt.Errorf("%w: name and suiteId are required", ErrInvalidRequest)
	}
	switch s.Trigger.Kind {
	case ScheduleCron:
		if _, err := ParseCron(s.Trigger.Cron, s.Trigger.Timezone); err != nil {
			return ScheduleSpec{}, err
		}
		if s.Trigger.Timezone == "" {
			s.Trigger.Timezone = "UTC"
		}
	case ScheduleAfterDeployment:
		if s.Trigger.Cron != "" || s.Trigger.Timezone != "" {
			return ScheduleSpec{}, fmt.Errorf("%w: an after-deployment trigger takes no cron or timezone", ErrInvalidRequest)
		}
	default:
		return ScheduleSpec{}, fmt.Errorf("%w: trigger kind must be %q or %q", ErrInvalidRequest, ScheduleCron, ScheduleAfterDeployment)
	}
	if s.Parameters == nil {
		s.Parameters = map[string]any{}
	}
	return s, nil
}

// NextAfter returns the spec's next cron time after after, or nil for an after-deployment or a
// disabled schedule and for a cron expression with no time in reach.
func (s ScheduleSpec) NextAfter(after time.Time) *time.Time {
	if !s.Enabled || s.Trigger.Kind != ScheduleCron {
		return nil
	}
	cron, err := ParseCron(s.Trigger.Cron, s.Trigger.Timezone)
	if err != nil {
		return nil
	}
	next, ok := cron.Next(after)
	if !ok {
		return nil
	}
	return &next
}
