// Package domain defines the Operation kernel: user intent, durable workflow state,
// executor progress, and the invariants swallow enforces before accepting mutating work.
// The context is named workflow, but its types keep their Operation names, and its records keep
// the collection and field names they were first stored under (renaming those is a data
// migration).
package domain

import (
	"errors"
	"fmt"
	"sort"
)

// WorkflowKind is what a Workflow is for. It selects the plan a launcher builds; that plan's
// Tasks may run playbooks, provider calls, or swallow's own steps, so a kind names no playbook
// (inspect-hardware runs none).
type WorkflowKind string

const (
	WorkflowKindInstallGPUDriver WorkflowKind = "install-gpu-driver"
	WorkflowKindDeployOS         WorkflowKind = "deploy-os"
	WorkflowKindReleaseOS        WorkflowKind = "release-os"
	// WorkflowKindRecoverServer returns a Server whose provisioning axis is not usable
	// (failed, broken, or in rescue) to the provider's ready pool. Its provider Step
	// chooses the primitive by observed state per the recovery policy (decision 033).
	WorkflowKindRecoverServer WorkflowKind = "recover-server"
	// WorkflowKindVerifyOSImage proves an OS Image deploys for a deploy target by running a real
	// deploy on a user-chosen ready Server, recording the verification on success, then
	// auto-releasing the Server (decision 035).
	WorkflowKindVerifyOSImage WorkflowKind = "verify-os-image"
	// WorkflowKindInspectHardware inspects one Server's hardware through its provisioner: it waits
	// for the provider's own enrollment to finish, applies Boot Media, and makes bounded inspection
	// attempts (decision 053). Launched by Inspect and by the automatic sweep, never by playbook.
	WorkflowKindInspectHardware WorkflowKind = "inspect-hardware"
	// WorkflowKindEnrollVirtualMachines registers libvirt domains of a Hypervisor with a
	// provisioner, one parallel Task per domain, without commissioning them (decision 055).
	// Launched by virtual-machine enrollment, never by playbook.
	WorkflowKindEnrollVirtualMachines WorkflowKind = "enroll-virtual-machines"
	WorkflowKindDeployKubernetes      WorkflowKind = "deploy-kubernetes"
	// WorkflowKindUninstallKubernetes removes the k0s installation created by a
	// deploy-kubernetes operation while preserving the host operating system.
	WorkflowKindUninstallKubernetes WorkflowKind = "uninstall-kubernetes"

	// WorkflowKindConfigureGPUOperator installs, changes, or uninstalls the swallow-managed AMD
	// GPU Operator of a Kubernetes Platform (decision 062). It is both the kind of the ansible
	// Task inside a deploy-kubernetes Workflow that installs the operator and the kind of the
	// standalone Workflow that changes it later; launched by the platform context, never by a
	// site playbook mapping.
	WorkflowKindConfigureGPUOperator WorkflowKind = "configure-gpu-operator"

	WorkflowKindConfigureSlurm WorkflowKind = "configure-slurm"
	// WorkflowKindUninstallSlurm removes the Slurm configuration and daemons swallow
	// deployed while preserving the host operating system and the image-supplied packages.
	WorkflowKindUninstallSlurm WorkflowKind = "uninstall-slurm"
	// WorkflowKindInstallExporters installs a host's Prometheus exporters as
	// containers: node-exporter on every target and the RDC exporter on AMD GPU
	// targets. It is the Ansible half of exporter ownership and is what OS deployment
	// auto-triggers.
	WorkflowKindInstallExporters WorkflowKind = "install-exporters"
	// WorkflowKindUninstallExporters removes the Ansible-installed exporters, so a
	// host can be handed over to a Kubernetes DaemonSet owner or cleaned up on retire.
	// It requires no particular provisioning state so a machine leaving service can
	// still be cleaned.
	WorkflowKindUninstallExporters WorkflowKind = "uninstall-exporters"
	// WorkflowKindDeployK8sExporters applies the exporter DaemonSets (node-exporter and
	// the RDC exporter) plus the AMD GPU device-plugin to a Kubernetes platform, run on a
	// control-plane target. The DaemonSets use hostNetwork on the same fixed ports, so
	// swallow's http_sd scrape and server_id join are unchanged.
	WorkflowKindDeployK8sExporters WorkflowKind = "deploy-k8s-exporters"
	// WorkflowKindRemoveK8sExporters deletes those DaemonSets, freeing the fixed ports so
	// the host can return to an Ansible-installed exporter.
	WorkflowKindRemoveK8sExporters WorkflowKind = "remove-k8s-exporters"
	// WorkflowKindCustom runs a named playbook with no swallow-side expectations
	// about what it does, which is the escape hatch for anything not yet modelled.
	WorkflowKindCustom WorkflowKind = "custom"

	// WorkflowKindConfigureOmnistat labels the ansible Task of a Slurm deploy that installs the
	// Omnistat exporter on the Slurm Platform (decision 063). It is never a standalone Workflow
	// and never resolved through a site playbook mapping.
	WorkflowKindConfigureOmnistat WorkflowKind = "configure-omnistat"
	// WorkflowKindConfigureNTP labels the ansible Task that points a freshly provisioned Server at
	// the installation's NTP server after SSH readiness (decision 065): one after every provision-os
	// Task, in a Deploy OS Workflow and in a Platform deployment's ensure-os Job alike. It is never
	// a standalone Workflow and never resolved through a site playbook mapping; the launchers name
	// the configure-ntp playbook.
	WorkflowKindConfigureNTP WorkflowKind = "configure-ntp"
)

// builtInWorkflowKinds are the kinds the workflow context accepts without any module, each with
// the provisioning state it requires of its targets ("" when it does not care). The Platform kinds
// are listed here until their modules declare them.
//
// Running post-install automation against a machine that is mid-deployment fails slowly and
// confusingly; refusing up front is faster and says why.
var builtInWorkflowKinds = map[WorkflowKind]string{
	WorkflowKindDeployOS:              "",
	WorkflowKindReleaseOS:             "",
	WorkflowKindRecoverServer:         "",
	WorkflowKindVerifyOSImage:         "",
	WorkflowKindInspectHardware:       "",
	WorkflowKindEnrollVirtualMachines: "",
	WorkflowKindInstallGPUDriver:      "deployed",
	WorkflowKindDeployKubernetes:      "deployed",
	WorkflowKindUninstallKubernetes:   "",
	WorkflowKindConfigureGPUOperator:  "deployed",
	WorkflowKindConfigureSlurm:        "deployed",
	WorkflowKindUninstallSlurm:        "",
	WorkflowKindInstallExporters:      "deployed",
	// Uninstalling exporters intentionally has no state requirement: a machine being retired or
	// handed to a k8s owner must still be cleanable.
	WorkflowKindUninstallExporters: "",
	WorkflowKindDeployK8sExporters: "deployed",
	WorkflowKindRemoveK8sExporters: "",
	WorkflowKindConfigureOmnistat:  "deployed",
	// configure-ntp reaches the host over SSH; the preceding provision-os Task satisfies this
	// (PrepareAnsibleStep provisionFirst).
	WorkflowKindConfigureNTP: "deployed",
	WorkflowKindCustom:       "",
}

// BuiltInWorkflowKinds returns the kinds the workflow context accepts without any module, sorted.
func BuiltInWorkflowKinds() []WorkflowKind {
	return sortedKinds(builtInWorkflowKinds)
}

// ErrWorkflowKindConflict means a module declares a Workflow kind that is already accepted.
var ErrWorkflowKindConflict = errors.New("workflow kind is already declared")

// WorkflowKinds are the Workflow kinds a build accepts and the provisioning state each requires of
// its targets. The zero value accepts the built-in kinds; the composition root adds each module's
// kinds from the module registry (decision 075), so api and ansible-executor accept the same set.
type WorkflowKinds struct {
	modules map[WorkflowKind]string
}

// With returns a copy that also accepts kinds, each requiring required of its targets ("" when it
// does not care). A kind that is already accepted is ErrWorkflowKindConflict.
func (k WorkflowKinds) With(required string, kinds ...WorkflowKind) (WorkflowKinds, error) {
	out := WorkflowKinds{modules: make(map[WorkflowKind]string, len(k.modules)+len(kinds))}
	for kind, state := range k.modules {
		out.modules[kind] = state
	}
	for _, kind := range kinds {
		if out.Valid(kind) {
			return WorkflowKinds{}, fmt.Errorf("%w: %s", ErrWorkflowKindConflict, kind)
		}
		out.modules[kind] = required
	}
	return out, nil
}

// Valid reports whether the build accepts kind.
func (k WorkflowKinds) Valid(kind WorkflowKind) bool {
	if _, ok := builtInWorkflowKinds[kind]; ok {
		return true
	}
	_, ok := k.modules[kind]
	return ok
}

// RequiredProvisioningState is the provisioning state targets of kind must be in, or "" when the
// kind does not care or is not accepted.
func (k WorkflowKinds) RequiredProvisioningState(kind WorkflowKind) string {
	if state, ok := builtInWorkflowKinds[kind]; ok {
		return state
	}
	return k.modules[kind]
}

// RefusedWhenLocked reports whether a Workflow of kind changes a target host. Every accepted kind
// executes automation against the host and is therefore refused while the provider-owned Server
// lock is active.
func (k WorkflowKinds) RefusedWhenLocked(kind WorkflowKind) bool {
	return k.Valid(kind)
}

// List returns every accepted kind, sorted.
func (k WorkflowKinds) List() []WorkflowKind {
	all := make(map[WorkflowKind]string, len(builtInWorkflowKinds)+len(k.modules))
	for kind, state := range builtInWorkflowKinds {
		all[kind] = state
	}
	for kind, state := range k.modules {
		all[kind] = state
	}
	return sortedKinds(all)
}

func sortedKinds(kinds map[WorkflowKind]string) []WorkflowKind {
	out := make([]WorkflowKind, 0, len(kinds))
	for kind := range kinds {
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Status is the state of an operation's run.
type Status string

const (
	// StatusPending is the persisted-but-not-yet-dispatched state. Every accepted
	// operation is pending before its compatible executor claims it.
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
	ErrWorkflowNotFound = errors.New("operation not found")
	// ErrTargetsBusy means a target is already in another operation that has not
	// finished. Two runs against one host would interleave.
	ErrTargetsBusy = errors.New("a target is already in an unfinished operation")
	// ErrTargetStateInvalid means a target is not in the provisioning state this kind
	// of operation requires.
	ErrTargetStateInvalid = errors.New("a target is not in the required provisioning state")
	// ErrTargetLocked means a target machine is locked and this kind of operation must
	// not change it. It protects machines a user has deliberately taken off-limits.
	ErrTargetLocked = errors.New("a target is locked and must not be changed")
	// ErrPolicyConflict means the operation contradicts a platform policy, e.g.
	// installing GPU drivers on nodes whose platform delegates that to the GPU operator.
	ErrPolicyConflict = errors.New("operation conflicts with platform policy")
)
