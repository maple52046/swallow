package domain

import (
	"context"
	"time"
)

// LaunchRequest describes a job to start.
type LaunchRequest struct {
	JobTemplateID string
	// Limit restricts the job to these inventory hosts. gdcm's dynamic inventory
	// keys hosts by server ID, so these are server IDs and need no translation.
	Limit []string
	// ExtraVars are passed to the playbook. They carry gdcm's identifiers so that a
	// playbook can report back in terms gdcm understands.
	ExtraVars map[string]any
}

// JobState is a controller job's observable state.
type JobState struct {
	Status     Status
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// AutomationController is the port an automation system must implement.
//
// Implementations translate controller vocabulary into this package's types and map
// transport failures onto *ControllerError.
type AutomationController interface {
	// Name is the stable controller kind, e.g. "awx".
	Name() string

	// FindJobTemplate resolves a template name to its controller-side ID, so that
	// operators configure names and gdcm stores whatever the controller uses.
	FindJobTemplate(ctx context.Context, name string) (string, error)

	// Launch starts a job and returns its identifier and initial state.
	Launch(ctx context.Context, req LaunchRequest) (jobID string, state JobState, err error)

	// JobState reads a job's current state. Implementations return
	// ErrJobNotFound when the controller no longer has it.
	JobState(ctx context.Context, jobID string) (JobState, error)

	// JobLogs returns the job's output. Logs are proxied on demand and never stored
	// by gdcm, so this may be called repeatedly while a job runs.
	JobLogs(ctx context.Context, jobID string) (string, error)
}

// ControllerFactory resolves a registered integration into a usable controller.
type ControllerFactory interface {
	For(ctx context.Context, integrationID string) (AutomationController, error)
}

// ControllerErrorKind classifies a controller failure for the delivery layer.
type ControllerErrorKind string

const (
	ControllerErrorUnavailable ControllerErrorKind = "unavailable"
	ControllerErrorAuth        ControllerErrorKind = "auth"
	ControllerErrorRejected    ControllerErrorKind = "rejected"
)

// ControllerError is a failure reported by, or while reaching, an automation controller.
type ControllerError struct {
	Kind ControllerErrorKind
	// Detail is surfaced to API clients. Adapters keep credentials out of it.
	Detail string
	Err    error
}

func (e *ControllerError) Error() string {
	if e.Detail != "" {
		return string(e.Kind) + ": " + e.Detail
	}
	return string(e.Kind)
}

func (e *ControllerError) Unwrap() error { return e.Err }
