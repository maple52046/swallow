package domain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GPUAllocationMode is how a Swallow-managed GPU Operator exposes a node's GPUs to workloads.
// Exactly one mode is active at a time; the two are alternatives, not layers.
type GPUAllocationMode string

const (
	// GPUAllocationDevicePlugin registers GPUs as an extended resource (amd.com/gpu) a Pod
	// requests in resources.limits.
	GPUAllocationDevicePlugin GPUAllocationMode = "device-plugin"
	// GPUAllocationDRA publishes GPUs as Dynamic Resource Allocation devices that a Pod claims
	// through a ResourceClaim. It needs Kubernetes resource.k8s.io/v1.
	GPUAllocationDRA GPUAllocationMode = "dra"
)

// Valid reports whether the mode is one of the defined values.
func (m GPUAllocationMode) Valid() bool {
	return m == GPUAllocationDevicePlugin || m == GPUAllocationDRA
}

// GPUResourceNaming is the device plugin's resource_naming_strategy: single always names the
// resource amd.com/gpu, mixed names partitioned GPUs per partition type.
type GPUResourceNaming string

const (
	GPUResourceNamingSingle GPUResourceNaming = "single"
	GPUResourceNamingMixed  GPUResourceNaming = "mixed"
)

// Valid reports whether the strategy is one the operator accepts.
func (n GPUResourceNaming) Valid() bool {
	return n == GPUResourceNamingSingle || n == GPUResourceNamingMixed
}

// GPUOperatorState is where the latest Swallow change to a Platform's GPU Operator stands. It is
// projected from the ansible Task that applies the change, never from a live cluster read.
type GPUOperatorState string

const (
	GPUOperatorInstalling   GPUOperatorState = "installing"
	GPUOperatorReady        GPUOperatorState = "ready"
	GPUOperatorFailed       GPUOperatorState = "failed"
	GPUOperatorUninstalling GPUOperatorState = "uninstalling"
	GPUOperatorAbsent       GPUOperatorState = "absent"
)

// GPUOperatorSettings is the desired configuration of a Swallow-managed GPU Operator. Enabled
// false is the uninstall intent; the other fields are then kept as the last requested settings
// so a later install can start from them. DRADriverVersion is empty unless AllocationMode is dra.
type GPUOperatorSettings struct {
	Enabled          bool
	Version          string
	AllocationMode   GPUAllocationMode
	DRADriverVersion string
	ResourceNaming   GPUResourceNaming
	NodeLabeller     bool
	MetricsExporter  bool
}

// GPUOperator is the Swallow-managed GPU Operator recorded on a Kubernetes Platform: the settings
// Swallow was last asked to apply and the state of the Workflow applying them. LastWorkflowID
// fences state writes: only that Workflow may move State, so an older run finishing late cannot
// overwrite a newer request.
type GPUOperator struct {
	Settings       GPUOperatorSettings
	State          GPUOperatorState
	LastWorkflowID string
	UpdatedAt      time.Time
}

// Managed reports whether Swallow currently manages an operator on the Platform, which is every
// recorded state except a completed uninstall. A nil record is never managed.
func (g *GPUOperator) Managed() bool {
	return g != nil && g.State != GPUOperatorAbsent
}

// GPUOperatorImages are the operand images a release pins. They are passed to the playbook as
// trusted variables so the cluster never resolves a floating tag such as latest.
type GPUOperatorImages struct {
	DevicePlugin    string
	NodeLabeller    string
	Utils           string
	MetricsExporter string
}

// KubernetesMinorVersion is a Kubernetes major.minor version, which is the granularity the
// operator's support matrix and resource.k8s.io/v1 availability are stated in.
type KubernetesMinorVersion struct {
	Major int
	Minor int
}

// AtLeast reports whether v is the same as or newer than minimum.
func (v KubernetesMinorVersion) AtLeast(minimum KubernetesMinorVersion) bool {
	if v.Major != minimum.Major {
		return v.Major > minimum.Major
	}
	return v.Minor >= minimum.Minor
}

// String renders the version as "major.minor", the form the release catalog publishes.
func (v KubernetesMinorVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// ParseKubernetesMinorVersion reads the Kubernetes major.minor from a k0s version such as
// "v1.36.3+k0s.2" (k0s versions are the Kubernetes version plus a +k0s suffix). It returns false
// for anything it cannot read, so callers can refuse rather than guess.
func ParseKubernetesMinorVersion(k0sVersion string) (KubernetesMinorVersion, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(k0sVersion), "v")
	value, _, _ = strings.Cut(value, "+")
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return KubernetesMinorVersion{}, false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil || major < 1 {
		return KubernetesMinorVersion{}, false
	}
	return KubernetesMinorVersion{Major: major, Minor: minor}, true
}

// GPUOperatorRelease is one AMD GPU Operator release Swallow ships and has verified: the chart
// version (which names the chart archive bundled with the playbook), its Kubernetes support
// floor, the DRA driver releases verified with it, and the operand images it pins.
type GPUOperatorRelease struct {
	Version                 string
	Default                 bool
	MinKubernetesVersion    KubernetesMinorVersion
	DRAMinKubernetesVersion KubernetesMinorVersion
	DRADriverVersions       []string
	DefaultDRADriverVersion string
	Images                  GPUOperatorImages
	// DRADriverImages maps each DRA driver version to its pinned image.
	DRADriverImages map[string]string
}

// gpuOperatorReleases is the release catalog, newest first. Adding an entry requires the matching
// chart archive under the amd_gpu_operator role's files and a lab verification of both modes.
var gpuOperatorReleases = []GPUOperatorRelease{
	{
		Version:                 "v1.5.1",
		Default:                 true,
		MinKubernetesVersion:    KubernetesMinorVersion{Major: 1, Minor: 29},
		DRAMinKubernetesVersion: KubernetesMinorVersion{Major: 1, Minor: 34},
		DRADriverVersions:       []string{"v1.0.1"},
		DefaultDRADriverVersion: "v1.0.1",
		Images: GPUOperatorImages{
			DevicePlugin:    "docker.io/rocm/k8s-device-plugin:1.31.0.11",
			NodeLabeller:    "docker.io/rocm/k8s-device-plugin:labeller-1.31.0.11",
			Utils:           "docker.io/rocm/gpu-operator-utils:v1.5.1",
			MetricsExporter: "docker.io/rocm/device-metrics-exporter:v1.5.1",
		},
		DRADriverImages: map[string]string{"v1.0.1": "docker.io/rocm/k8s-gpu-dra-driver:v1.0.1"},
	},
}

// GPUOperatorReleases returns a copy of the release catalog, newest first.
func GPUOperatorReleases() []GPUOperatorRelease {
	releases := make([]GPUOperatorRelease, len(gpuOperatorReleases))
	copy(releases, gpuOperatorReleases)
	return releases
}

// LookupGPUOperatorRelease returns the catalog entry for version. An empty version selects the
// default release.
func LookupGPUOperatorRelease(version string) (GPUOperatorRelease, bool) {
	version = strings.TrimSpace(version)
	for _, release := range gpuOperatorReleases {
		if release.Version == version || version == "" && release.Default {
			return release, true
		}
	}
	return GPUOperatorRelease{}, false
}

// GPUOperatorLaunch is a validated request to start the Workflow that changes a Platform's
// GPU Operator after deployment. WorkflowID is preassigned by the use case, which records it on
// the Platform before launching so the Task observer's fence matches from the first Task.
// TargetServerIDs are the Platform's deployment targets; TrustedVars are the playbook's
// swallow_amd_gpu_operator_* variables. Uninstall only changes the Workflow's operator-facing
// summary; the playbook reads the intent from the trusted vars.
type GPUOperatorLaunch struct {
	WorkflowID         string
	Platform           *Platform
	TargetServerIDs    []string
	TrustedVars        map[string]any
	Uninstall          bool
	RequestedBy        string
	RequestCorrelation string
}

// GPUOperatorLauncher builds the configure-gpu-operator Workflow. It is a port so the platform
// context does not depend on the operation context. Like DeploymentLauncher it is split so the
// change is accepted in one store transaction without external calls (decision 069):
// PrepareGPUOperator does the reads (lock check, playbook and target resolution) and writes
// nothing; PersistGPUOperator writes the prepared Workflow and runs inside the caller's
// transaction, which also writes the GPU Operator record. Implementations return the operation
// context's busy, lock, and validation errors unchanged, from whichever step detects them.
type GPUOperatorLauncher interface {
	PrepareGPUOperator(ctx context.Context, launch GPUOperatorLaunch) (PreparedWorkflow, error)
	PersistGPUOperator(ctx context.Context, prepared PreparedWorkflow) (workflowID string, err error)
}

var (
	// ErrInvalidGPUOperator covers settings that cannot be applied as given; the delivery layer
	// maps it to 400 validation_error.
	ErrInvalidGPUOperator = errors.New("invalid GPU operator settings")
	// ErrGPUOperatorConflict means the request is valid but the Platform's current state does not
	// allow it (not an active Swallow-deployed Kubernetes Platform, nothing to uninstall, or a
	// policy change that would contradict a managed operator); it maps to 409 conflict.
	ErrGPUOperatorConflict = errors.New("GPU operator change conflicts with the platform state")
)

// NormalizeGPUOperatorSettings applies defaults and checks an enable request against the release
// catalog and the cluster's Kubernetes version. hasAMDGPUWorkload reports whether any workload
// Server of the Platform has an AMD compute GPU; it gates the feature only, because the nodes the
// operator manages are decided in the cluster by its Node Feature Discovery (decision 062). A
// disable request needs no validation and is returned with Enabled false and its other fields
// untouched. Every failure wraps ErrInvalidGPUOperator.
func NormalizeGPUOperatorSettings(
	settings GPUOperatorSettings,
	kubernetes KubernetesMinorVersion,
	hasAMDGPUWorkload bool,
) (GPUOperatorSettings, error) {
	if !settings.Enabled {
		return settings, nil
	}
	release, ok := LookupGPUOperatorRelease(settings.Version)
	if !ok {
		return settings, fmt.Errorf("%w: version %q is not a supported GPU Operator release", ErrInvalidGPUOperator, settings.Version)
	}
	settings.Version = release.Version
	if !kubernetes.AtLeast(release.MinKubernetesVersion) {
		return settings, fmt.Errorf("%w: GPU Operator %s needs Kubernetes %s or later, the Platform runs %s",
			ErrInvalidGPUOperator, release.Version, release.MinKubernetesVersion, kubernetes)
	}

	if settings.AllocationMode == "" {
		settings.AllocationMode = GPUAllocationDevicePlugin
	}
	if !settings.AllocationMode.Valid() {
		return settings, fmt.Errorf("%w: allocationMode must be device-plugin or dra", ErrInvalidGPUOperator)
	}
	if settings.AllocationMode == GPUAllocationDRA {
		if !kubernetes.AtLeast(release.DRAMinKubernetesVersion) {
			return settings, fmt.Errorf("%w: DRA needs Kubernetes %s or later, the Platform runs %s",
				ErrInvalidGPUOperator, release.DRAMinKubernetesVersion, kubernetes)
		}
		settings.DRADriverVersion = strings.TrimSpace(settings.DRADriverVersion)
		if settings.DRADriverVersion == "" {
			settings.DRADriverVersion = release.DefaultDRADriverVersion
		}
		if _, ok := release.DRADriverImages[settings.DRADriverVersion]; !ok {
			return settings, fmt.Errorf("%w: DRA driver %q is not verified with GPU Operator %s",
				ErrInvalidGPUOperator, settings.DRADriverVersion, release.Version)
		}
	} else {
		settings.DRADriverVersion = ""
	}

	if settings.ResourceNaming == "" {
		settings.ResourceNaming = GPUResourceNamingSingle
	}
	if !settings.ResourceNaming.Valid() {
		return settings, fmt.Errorf("%w: resourceNaming must be single or mixed", ErrInvalidGPUOperator)
	}

	if !hasAMDGPUWorkload {
		return settings, fmt.Errorf("%w: no workload Server of this Platform has an AMD compute GPU", ErrInvalidGPUOperator)
	}
	return settings, nil
}
