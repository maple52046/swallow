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
	// OperationKindUninstallKubernetes removes the k0s installation created by a
	// deploy-kubernetes operation while preserving the host operating system.
	OperationKindUninstallKubernetes OperationKind = "uninstall-kubernetes"

	OperationKindConfigureSlurm OperationKind = "configure-slurm"
	// OperationKindInstallExporters installs a host's Prometheus exporters as
	// containers: node-exporter on every target and the RDC exporter on AMD GPU
	// targets. It is the Ansible half of exporter ownership and is what OS deployment
	// auto-triggers.
	OperationKindInstallExporters OperationKind = "install-exporters"
	// OperationKindUninstallExporters removes the Ansible-installed exporters, so a
	// host can be handed over to a Kubernetes DaemonSet owner or cleaned up on retire.
	// It requires no particular provisioning state so a machine leaving service can
	// still be cleaned.
	OperationKindUninstallExporters OperationKind = "uninstall-exporters"
	// OperationKindDeployK8sExporters applies the exporter DaemonSets (node-exporter and
	// the RDC exporter) plus the AMD GPU device-plugin to a Kubernetes cluster, run on a
	// control-plane target. The DaemonSets use hostNetwork on the same fixed ports, so
	// swallow's http_sd scrape and server_id join are unchanged.
	OperationKindDeployK8sExporters OperationKind = "deploy-k8s-exporters"
	// OperationKindRemoveK8sExporters deletes those DaemonSets, freeing the fixed ports so
	// the host can return to an Ansible-installed exporter.
	OperationKindRemoveK8sExporters OperationKind = "remove-k8s-exporters"
	// OperationKindCustom runs a named playbook with no swallow-side expectations
	// about what it does, which is the escape hatch for anything not yet modelled.
	OperationKindCustom OperationKind = "custom"
)

var ValidOperationKinds = []OperationKind{
	OperationKindInstallGPUDriver,
	OperationKindDeployKubernetes,
	OperationKindUninstallKubernetes,
	OperationKindConfigureSlurm,
	OperationKindInstallExporters,
	OperationKindUninstallExporters,
	OperationKindDeployK8sExporters,
	OperationKindRemoveK8sExporters,
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
	case OperationKindInstallGPUDriver, OperationKindDeployKubernetes, OperationKindConfigureSlurm,
		OperationKindInstallExporters, OperationKindDeployK8sExporters:
		return "deployed"
	default:
		// Uninstalling exporters intentionally has no state requirement: a machine
		// being retired or handed to a k8s owner must still be cleanable.
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

// RefusedWhenLocked reports whether an Operation changes a target host. Every valid
// Operation executes automation against the host and is therefore refused while the
// provider-owned Server lock is active.
func (k OperationKind) RefusedWhenLocked() bool {
	return k.Valid()
}

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
	// ErrTargetLocked means a target machine is locked and this kind of operation must
	// not change it. It protects machines an operator has deliberately taken off-limits.
	ErrTargetLocked = errors.New("a target is locked and must not be changed")
	// ErrPolicyConflict means the operation contradicts a cluster policy, e.g.
	// installing GPU drivers on nodes whose cluster delegates that to the GPU operator.
	ErrPolicyConflict = errors.New("operation conflicts with cluster policy")
)
