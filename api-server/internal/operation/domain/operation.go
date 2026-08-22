// Package domain defines Operation: an operator's intent, executed by an external
// automation controller.
//
// swallow stores what someone wanted and a reference to the job doing it. It stores no
// logs, implements no retry, and enforces no idempotency — those belong to Ansible and
// AWX, which solved them. See docs/decisions/004-automation-via-awx.md.
package domain

import (
	"errors"
	"time"
)

// OperationKind is what an operation is for. Each kind maps to a job template
// configured in the automation controller.
type OperationKind string

const (
	OperationKindInstallGPUDriver OperationKind = "install-gpu-driver"
	OperationKindDeployKubernetes OperationKind = "deploy-kubernetes"
	OperationKindConfigureSlurm   OperationKind = "configure-slurm"
	// OperationKindCustom runs a named job template with no swallow-side expectations
	// about what it does, which is the escape hatch for anything not yet modelled.
	OperationKindCustom OperationKind = "custom"
)

var ValidOperationKinds = []OperationKind{
	OperationKindInstallGPUDriver,
	OperationKindDeployKubernetes,
	OperationKindConfigureSlurm,
	OperationKindCustom,
}

func (k OperationKind) Valid() bool {
	for _, valid := range ValidOperationKinds {
		if k == valid {
			return true
		}
	}
	return false
}

// RequiredProvisioningState is the provisioning state targets must be in, or "" when
// the kind does not care.
//
// Running post-install automation against a machine that is mid-deployment fails slowly
// and confusingly; refusing up front is faster and says why.
func (k OperationKind) RequiredProvisioningState() string {
	switch k {
	case OperationKindInstallGPUDriver, OperationKindDeployKubernetes, OperationKindConfigureSlurm:
		return "deployed"
	default:
		return ""
	}
}

// Status mirrors the automation controller's job state.
type Status string

const (
	// StatusPending covers every pre-run controller state: queued, waiting for a
	// slot, and so on. swallow has no use for the distinctions.
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	// StatusSucceeded and the failure states are terminal.
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusError     Status = "error"
	// StatusIndeterminate means the controller no longer has the job. Absence is not
	// an outcome, so this is deliberately not "failed".
	StatusIndeterminate Status = "indeterminate"
)

// Terminal reports whether a status will no longer change, which is what stops the
// poller from following an operation forever.
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusError, StatusIndeterminate:
		return true
	default:
		return false
	}
}

// AutomationRef is the reference to, and mirror of, the controller's job.
//
// Status is a mirror and says when it was last observed. swallow never infers that a job
// finished because time passed.
type AutomationRef struct {
	IntegrationID string
	JobTemplateID string
	// JobName is what an operator configured, kept so that a template renamed or
	// deleted in the controller can be reported by the name swallow was asked for.
	JobName string
	// JobID is empty until the controller has accepted the launch.
	JobID      string
	Status     Status
	StartedAt  *time.Time
	FinishedAt *time.Time
	ObservedAt time.Time
}

// Operation is an operator's intent plus the job executing it.
type Operation struct {
	ID   string
	Kind OperationKind
	// Intent is the operator's own description of why. The controller records that a
	// job ran; only this records what it was for.
	Intent string
	SiteID string
	// ClusterID is set when the operation is about a cluster.
	ClusterID string
	// TargetServerIDs is frozen at creation. Re-resolving targets from a live query
	// would silently change scope between request and execution.
	TargetServerIDs []string

	Automation AutomationRef

	RequestedBy string
	RequestedAt time.Time
	UpdatedAt   time.Time
}

var (
	ErrOperationNotFound = errors.New("operation not found")
	// ErrTargetsBusy means a target is already in another operation that has not
	// finished. The controller would happily run both, because it sees two unrelated
	// jobs.
	ErrTargetsBusy = errors.New("a target is already in an unfinished operation")
	// ErrTargetStateInvalid means a target is not in the provisioning state this kind
	// of operation requires.
	ErrTargetStateInvalid = errors.New("a target is not in the required provisioning state")
	// ErrPolicyConflict means the operation contradicts a cluster policy, e.g.
	// installing GPU drivers on nodes whose cluster delegates that to the GPU operator.
	ErrPolicyConflict = errors.New("operation conflicts with cluster policy")
	// ErrNoAutomationIntegration means the site has no automation controller
	// registered, so there is nothing to execute with.
	ErrNoAutomationIntegration = errors.New("site has no automation integration")
	// ErrJobTemplateNotFound means the controller has no template by that name.
	// Reported at creation rather than at execution.
	ErrJobTemplateNotFound = errors.New("job template not found")
	// ErrNotLaunched means an operation has no job yet, so there is nothing to read
	// logs from.
	ErrNotLaunched = errors.New("operation has not been launched")
)
