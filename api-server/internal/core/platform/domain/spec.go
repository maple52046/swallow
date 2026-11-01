package domain

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// PlatformDesired is whether an operator wants a Platform to exist.
type PlatformDesired string

// The desired presences. Absent asks for the Platform to be uninstalled; its record stays until the
// uninstall finishes.
const (
	PlatformPresent PlatformDesired = "present"
	PlatformAbsent  PlatformDesired = "absent"
)

// PlatformMember is one Server of a Platform's spec and the roles it takes there. The roles are the
// Platform module's vocabulary (for Kubernetes `control-plane`, `worker`, and `workloads`; for
// Slurm `controller`, `compute`, and `login`).
type PlatformMember struct {
	ServerID string
	Roles    []string
}

// PlatformSpec is what an operator wants a Platform to be (desired-state-model §3). Members and
// Desired are the core's: the Platform Claims follow the members, and an absent Platform is
// uninstalled. Settings are the Platform module's, described by its spec JSON Schema and validated
// by its hook; the core does not read them.
type PlatformSpec struct {
	Desired PlatformDesired
	Members []PlatformMember
	// MachinePreparation is how a member that is not deployed gets its operating system.
	MachinePreparation MachinePreparation
	Settings           map[string]any
	// Removal is how an absent Platform is removed; nil keeps its members and their OS.
	Removal *PlatformRemoval
}

// MemberIDs returns the members' Server ids in spec order.
func (s PlatformSpec) MemberIDs() []string {
	ids := make([]string, len(s.Members))
	for i, member := range s.Members {
		ids[i] = member.ServerID
	}
	return ids
}

// Member returns the member for serverID, or false when the spec has none.
func (s PlatformSpec) Member(serverID string) (PlatformMember, bool) {
	for _, member := range s.Members {
		if member.ServerID == serverID {
			return member, true
		}
	}
	return PlatformMember{}, false
}

// Clone returns a copy that shares nothing mutable with s at the top level of members and
// settings.
func (s PlatformSpec) Clone() PlatformSpec {
	out := s
	out.Members = make([]PlatformMember, len(s.Members))
	for i, member := range s.Members {
		out.Members[i] = PlatformMember{ServerID: member.ServerID, Roles: slices.Clone(member.Roles)}
	}
	out.Settings = maps.Clone(s.Settings)
	out.MachinePreparation.Assignments = slices.Clone(s.MachinePreparation.Assignments)
	if s.Removal != nil {
		removal := *s.Removal
		out.Removal = &removal
	}
	return out
}

// PlatformRemoval is how an absent Platform's uninstall treats its members: whether it also releases
// them to the provisioner (with the erase and static-IP options), and whether it first cancels the
// unfinished Workflows holding them.
type PlatformRemoval struct {
	ReleaseServers        bool
	ReleaseOptions        ServerReleaseOptions
	CancelActiveWorkflows bool
}

// PlatformStatus is what swallow observed and realized for a Platform's spec. ObservedGeneration is
// the spec generation the conditions were judged for; Applied is the spec the last successful
// Workflow realized, which the next change is planned against (nil before the first deploy
// succeeded).
type PlatformStatus struct {
	ObservedGeneration int64
	Conditions         []convergence.Condition
	Applied            *PlatformSpec
	// Realizing is the Workflow carrying out a spec generation, until it succeeds.
	Realizing *PlatformRealizing
}

// PlatformRealizing is a Workflow swallow started to realize one spec generation. Spec is that
// generation's spec, which becomes the applied spec when the Workflow (or a rerun of it) succeeds,
// even if the spec changed again meanwhile.
type PlatformRealizing struct {
	Generation int64
	WorkflowID string
	Spec       PlatformSpec
	StartedAt  time.Time
}

// ErrGenerationConflict means the Platform's spec changed since the writer read it: the write named
// a generation that is no longer current.
var ErrGenerationConflict = errors.New("the platform spec changed since it was read")

// PlatformSpecStore writes a Platform's spec and status. WriteSpec replaces the spec when the
// stored generation is expectedGeneration and returns the new generation (expectedGeneration+1);
// another generation is ErrGenerationConflict and a missing Platform ErrPlatformNotFound.
type PlatformSpecStore interface {
	WriteSpec(ctx context.Context, platformID string, expectedGeneration int64, spec PlatformSpec) (int64, error)
	WriteStatus(ctx context.Context, platformID string, status PlatformStatus) error
}

// DeploymentRecord is the deployment a Platform's latest deploy Workflow carried out, read back from
// what that Workflow recorded.
type DeploymentRecord struct {
	// Request is the deploy request body the deployment stands for.
	Request map[string]any
	// MachinePreparation is how the deployment prepared members that were not deployed.
	MachinePreparation MachinePreparation
}

// DeploymentRecordReader reads the deployment of a Platform's latest deploy Workflow, from which a
// Platform deployed before specs existed is adopted; ok is false when the Platform has none.
type DeploymentRecordReader interface {
	LatestDeploymentRecord(ctx context.Context, platform *Platform) (record DeploymentRecord, ok bool, err error)
}
