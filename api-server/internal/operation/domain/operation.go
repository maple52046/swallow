// Package domain defines the operation kernel: what an operation is for (its kind), the
// state of the run carrying it out (its status), and the invariants swallow enforces
// before accepting one.
//
// swallow owns operation execution itself through an embedded Ansible runner; the durable
// intent and the locally owned run live in execution.go. See
// docs/decisions/006-embedded-ansible-execution.md.
package domain

import (
	"errors"
)

// OperationKind is what an operation is for. Each kind maps to a release-manifest playbook,
// resolved through the site's automation configuration.
type OperationKind string

const (
	OperationKindInstallGPUDriver OperationKind = "install-gpu-driver"
	OperationKindDeployKubernetes OperationKind = "deploy-kubernetes"
	OperationKindConfigureSlurm   OperationKind = "configure-slurm"
	// OperationKindCustom runs a named playbook with no swallow-side expectations
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

// Status is the state of an operation's run.
type Status string

const (
	// StatusPending is the persisted-but-not-yet-dispatched state. Every accepted
	// operation is pending before the embedded dispatcher claims it.
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	// StatusSucceeded and the failure states are terminal.
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	// StatusIndeterminate means the API or host stopped while a run held a lease, so its
	// outcome cannot be proven. Absence of evidence is not an outcome, so this is
	// deliberately not "failed", and it is never retried automatically.
	StatusIndeterminate Status = "indeterminate"
)

// Terminal reports whether a status will no longer change on its own, which is what stops
// the dispatcher and any watcher from following an operation forever.
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusIndeterminate:
		return true
	default:
		return false
	}
}

var (
	ErrOperationNotFound = errors.New("operation not found")
	// ErrTargetsBusy means a target is already in another operation that has not
	// finished. Two runs against one host would interleave.
	ErrTargetsBusy = errors.New("a target is already in an unfinished operation")
	// ErrTargetStateInvalid means a target is not in the provisioning state this kind
	// of operation requires.
	ErrTargetStateInvalid = errors.New("a target is not in the required provisioning state")
	// ErrPolicyConflict means the operation contradicts a cluster policy, e.g.
	// installing GPU drivers on nodes whose cluster delegates that to the GPU operator.
	ErrPolicyConflict = errors.New("operation conflicts with cluster policy")
)
