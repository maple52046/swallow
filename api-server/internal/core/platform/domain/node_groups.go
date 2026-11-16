package domain

import (
	"context"
	"time"
)

// NodeGroup is a group of a Platform's members that scales together (glossary Node Group): the
// members share one role and are installed from one Deployment Template. Groups are a Platform
// module's settings; the core reads them through NodeGroupModule only to resolve each member's
// template, to create the templates a Platform generates, and to list where templates are used.
type NodeGroup struct {
	Name string
	// Role is the type's role name the group's members carry (for example "compute").
	Role string
	// TemplateID is the Deployment Template the group's new members are installed from; empty
	// before the Platform generates or is given one.
	TemplateID string
	// ExpectedGPUsPerNode is the GPU count every member should have; 0 expects nothing.
	ExpectedGPUsPerNode int
	Members             []string
}

// NodeGroupModule is the optional part of a PlatformModule whose spec groups its members. Every
// method is pure: the core does the reads and writes the groups imply.
type NodeGroupModule interface {
	// NodeGroups returns the spec's groups in spec order; a spec written before groups existed is
	// read with the groups UpgradeSpec would give it.
	NodeGroups(spec PlatformSpec) []NodeGroup
	// WithGroupTemplates returns spec with the named groups' TemplateID set; groups not named keep
	// theirs.
	WithGroupTemplates(spec PlatformSpec, templates map[string]string) PlatformSpec
	// UpgradeSpec returns spec in the module's current form and whether that changed anything,
	// without changing what realizing it does (a spec from before groups gains the groups its
	// members imply). The platform controller stores the upgraded form of the spec and the applied
	// spec together.
	UpgradeSpec(spec PlatformSpec) (PlatformSpec, bool)
}

// TopologyTarget is what a Platform module may read about one deployment target to plan its
// topology: the Accelerator's compute GPUs on it and their model names, and its CPU and memory.
// Name is how people know the Server (its display name), for warnings.
type TopologyTarget struct {
	ServerID    string
	Name        string
	ComputeGPUs int
	GPUModels   []string
	CPUCores    int
	MemoryMiB   int64
}

// DeploymentTopology is the module-owned part of a deployment: trusted variables its install reads
// beside the core's, and warnings the spec write and the deployment report.
type DeploymentTopology struct {
	Vars     map[string]any
	Warnings []string
}

// TopologyModule is the optional part of a PlatformModule that plans the module-owned part of a
// deployment (its node groups and scheduling, for example). It is pure; an invalid topology is
// ErrInvalidDeployment.
type TopologyModule interface {
	DeploymentTopology(spec PlatformSpec, targets []TopologyTarget) (DeploymentTopology, error)
}

// GeneratedTemplate is a Deployment Template a Platform creates from its machine preparation for a
// node group that names none (decision 083). The spec records it when the group is given its id; the
// template itself is created when the change that introduced it is realized, and stays after that.
type GeneratedTemplate struct {
	ID            string
	Name          string
	IntegrationID string
	Group         string
}

// TemplateMinimum is a Deployment Template's optional hardware floor as a Platform checks it; a zero
// field sets no floor for its dimension.
type TemplateMinimum struct {
	CPUCores  int
	MemoryMiB int64
	StorageGB int
	GPUModel  string
	GPUCount  int
}

// DeploymentTemplateIntent is the part of a Deployment Template a Platform deployment reads: whose
// Integration it belongs to, how it installs (Ephemeral, for the members' root), its hardware floor,
// and a digest of what it installs, recorded with each member installed from it.
type DeploymentTemplateIntent struct {
	ID            string
	IntegrationID string
	// PoolID is the Pool that owns the template, "" for none.
	PoolID    string
	Name      string
	Ephemeral bool
	Minimum   *TemplateMinimum
	Digest    string
}

// DeploymentTemplates is the Platform context's port to the OS Provisioning context's Deployment
// Templates; the composition root supplies the adapter.
type DeploymentTemplates interface {
	// Find returns the template, or nil without an error when it does not exist.
	Find(ctx context.Context, id string) (*DeploymentTemplateIntent, error)
	// NameTaken reports whether integrationID already has a template with name other than id in
	// poolID ("" for the templates without a Pool): names are unique within an Integration and a Pool.
	NameTaken(ctx context.Context, integrationID, poolID, name, id string) (bool, error)
	// EnsureGenerated creates each generated template that does not exist yet from preparation's
	// image, deploy mode, network intent, and user data, owned by owner's Pool and recording owner's
	// creator. It writes only the store, so it runs inside the transaction that accepts the realization.
	EnsureGenerated(ctx context.Context, generated []GeneratedTemplate, preparation MachinePreparation, owner TemplateOwner) error
	// AdoptGenerated gives the templates ids that belong to another Pool, or to none, poolID: a
	// Platform's generated templates belong to the Platform's Pool, and follow it to another one.
	AdoptGenerated(ctx context.Context, ids []string, poolID string) error
}

// TemplateOwner is who a template a Platform generates belongs to: the Platform's Pool ("" when the
// Platform has none, so only an admin reads it) and the User who carried out the change, as a record.
type TemplateOwner struct {
	PoolID    string
	CreatedBy string
}

// MemberRemoval is how one member a spec drops leaves the Platform (the scale-in of a member,
// decision 083). A member dropped without one leaves with the defaults: drained for
// DefaultDrainTimeoutSeconds, not forced, and its operating system kept.
type MemberRemoval struct {
	ServerID string
	// ReleaseServer returns the Server to the provisioner after it left, with ReleaseOptions.
	ReleaseServer  bool
	ReleaseOptions ServerReleaseOptions
	// DrainTimeoutSeconds bounds how long the member's running work is waited for; 0 is the default.
	DrainTimeoutSeconds int
	// Force ends the work still running when the drain times out instead of failing the removal.
	Force bool
}

// DefaultDrainTimeoutSeconds is how long a removed member's running work is waited for when its
// removal names no timeout.
const DefaultDrainTimeoutSeconds = 600

// MaxDrainTimeoutSeconds bounds a removal's drain, so a removal cannot hold its Servers forever.
const MaxDrainTimeoutSeconds = 86400

// MemberTemplateRecord records which Deployment Template a member was installed from and a digest
// of what the template installed then, so a reader can tell a member that differs from the current
// template.
type MemberTemplateRecord struct {
	ServerID   string
	TemplateID string
	Digest     string
	DeployedAt time.Time
}

// MemberTemplateRecorder records which template members were installed from. A record replaces
// the member's previous one, and a record without a TemplateID (a member installed from the
// machine preparation) only removes it; it runs inside the transaction that accepts the deployment.
type MemberTemplateRecorder interface {
	RecordMemberTemplates(ctx context.Context, platformID string, records []MemberTemplateRecord) error
}

// QueueReader is the optional part of a PlatformReader that counts the work queued or running in
// each of the platform's scheduling queues (a Slurm partition), so removing a queue that still has
// work is refused with the count.
type QueueReader interface {
	QueuedJobs(ctx context.Context) (map[string]int, error)
}
