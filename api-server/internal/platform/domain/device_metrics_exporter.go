package domain

import (
	"context"
	"errors"
	"time"
)

// The Device Metrics Exporter on a Swallow-deployed Slurm Platform (decision 066, glossary Device
// Metrics Exporter): AMD's per-GPU-node exporter plus the Slurm job attribution (a job hook of the
// Prolog/Epilog dispatcher). Like Omnistat it belongs to the Platform, and its Prometheus and
// Grafana are Managed Software the same deployment composes onto the observability Server (the
// primary controller). The two features are mutually exclusive on a Platform.
const (
	// DeviceMetricsExporterVersion is the exporter release Swallow installs. The playbook pins its
	// native package and that package's SHA-256 per version.
	DeviceMetricsExporterVersion = "1.5.3"
	// DeviceMetricsExporterPort is where every GPU node's exporter listens. AMD's default, 5000, is
	// the RDC exporter port Swallow scrapes on a host (decision 003), so it must never be used here.
	DeviceMetricsExporterPort = 5001
	// DeviceMetricsExporterGrafanaLinkName is the Monitoring Link a successful install registers and
	// an uninstall removes; other links stay operator-owned.
	DeviceMetricsExporterGrafanaLinkName = "Device Metrics Exporter Grafana"
	// DeviceMetricsExporterScrapeJob is the Prometheus job name of the exporters.
	DeviceMetricsExporterScrapeJob = "device-metrics-exporter"
	// DeviceMetricsExporterDashboardBundle is the Grafana dashboard bundle with the exporter's Slurm
	// dashboards (overview, GPU, node, job).
	DeviceMetricsExporterDashboardBundle = "device-metrics-exporter"
)

// DeviceMetricsExporterState is where the latest Swallow change to a Platform's Device Metrics
// Exporter stands, projected from the Workflow Task that installs it. Values are the glossary's
// Device Metrics Exporter State.
type DeviceMetricsExporterState string

const (
	DeviceMetricsExporterInstalling DeviceMetricsExporterState = "installing"
	DeviceMetricsExporterReady      DeviceMetricsExporterState = "ready"
	DeviceMetricsExporterFailed     DeviceMetricsExporterState = "failed"
	// DeviceMetricsExporterAbsent means the Platform was uninstalled, which removed the exporter.
	DeviceMetricsExporterAbsent DeviceMetricsExporterState = "absent"
)

// GPUIdentity is whether the exporters tell a Platform's GPUs apart, which per-GPU job attribution
// depends on: the job hook names a GPU by its Slurm index and the exporter joins that index to its
// gpu_id label. It is known only after a successful install.
type GPUIdentity string

const (
	// GPUIdentityUnknown is the value before an install reported one.
	GPUIdentityUnknown GPUIdentity = ""
	// GPUIdentityDistinct means every GPU node's exporter reports one gpu_id per GPU.
	GPUIdentityDistinct GPUIdentity = "distinct"
	// GPUIdentityCollapsed means at least one node's exporter reports fewer distinct gpu_ids than it
	// has GPUs (MI308X SR-IOV virtual functions all report gpu_id 0), so a job is attributed to the
	// wrong GPUs there. The metrics endpoint still works; only per-GPU attribution does not.
	GPUIdentityCollapsed GPUIdentity = "collapsed"
)

// ParseGPUIdentity reads an identity a configure-device-metrics-exporter run reported. Anything
// but distinct is collapsed: an identity Swallow cannot confirm is never reported as reliable.
func ParseGPUIdentity(value string) GPUIdentity {
	if GPUIdentity(value) == GPUIdentityDistinct {
		return GPUIdentityDistinct
	}
	return GPUIdentityCollapsed
}

// DeviceMetricsExporterSettings is the operator's Device Metrics Exporter intent in a Slurm deploy
// request. An empty InstallMethod is the container method.
type DeviceMetricsExporterSettings struct {
	Enabled       bool
	InstallMethod ObservabilityInstallMethod
}

// NormalizeDeviceMetricsExporterSettings defaults the install method and refuses an unknown one.
func NormalizeDeviceMetricsExporterSettings(settings DeviceMetricsExporterSettings) (DeviceMetricsExporterSettings, error) {
	method, err := normalizeObservabilityInstallMethod(settings.InstallMethod, ErrInvalidDeviceMetricsExporter)
	if err != nil {
		return DeviceMetricsExporterSettings{}, err
	}
	return DeviceMetricsExporterSettings{Enabled: settings.Enabled, InstallMethod: method}, nil
}

// DeviceMetricsExporter is the Platform's Swallow-managed Device Metrics Exporter record.
// ObservabilityServerID names the Server running Prometheus and Grafana, installed by InstallMethod.
// GPUIdentity is what the latest successful install found. LastWorkflowID is the fence: only that
// Workflow's Task may move the State. Written only through DeviceMetricsExporterRecorder, never by a
// Platform Update.
type DeviceMetricsExporter struct {
	Enabled               bool
	Version               string
	InstallMethod         ObservabilityInstallMethod
	ObservabilityServerID string
	State                 DeviceMetricsExporterState
	GPUIdentity           GPUIdentity
	LastWorkflowID        string
	UpdatedAt             time.Time
}

// Managed reports whether Swallow currently manages the exporter on the Platform. A nil record
// (never enabled) and an absent one (uninstalled) are both unmanaged.
func (e *DeviceMetricsExporter) Managed() bool {
	return e != nil && e.State != DeviceMetricsExporterAbsent
}

// DeviceMetricsExporterLaunch adds the Device Metrics Exporter to a Slurm deployment Workflow:
// Prometheus and Grafana composed onto ObservabilityServerID by InstallMethod (the container method
// also composes that Server's container runtime), then the exporter on every GPUComputeServerIDs
// node. TrustedVars are the configure-device-metrics-exporter playbook's
// swallow_device_metrics_exporter_* variables.
type DeviceMetricsExporterLaunch struct {
	ObservabilityServerID string
	GPUComputeServerIDs   []string
	InstallMethod         ObservabilityInstallMethod
	TrustedVars           map[string]any
}

// DeviceMetricsExporterRecorder persists a Platform's Device Metrics Exporter record. Like
// OmnistatRecorder it is separate from PlatformRepository so the record has only its writers: the
// deploy use case accepting it, the observer and the completion of the Task applying it, and the
// uninstall completion marking it absent.
type DeviceMetricsExporterRecorder interface {
	// SetDeviceMetricsExporter replaces the record; nil removes it. It returns ErrPlatformNotFound
	// for a missing Platform.
	SetDeviceMetricsExporter(ctx context.Context, platformID string, exporter *DeviceMetricsExporter) error
	// UpdateDeviceMetricsExporterState moves the state only while the record's LastWorkflowID equals
	// workflowID and reports whether it did. A missing Platform or a different latest Workflow is not
	// an error: the observer of a superseded or deleted Platform's Task has nothing to do.
	UpdateDeviceMetricsExporterState(ctx context.Context, platformID, workflowID string, state DeviceMetricsExporterState, at time.Time) (bool, error)
	// UpdateDeviceMetricsExporterGPUIdentity records what an install found, under the same fence.
	UpdateDeviceMetricsExporterGPUIdentity(ctx context.Context, platformID, workflowID string, identity GPUIdentity, at time.Time) (bool, error)
}

var (
	// ErrInvalidDeviceMetricsExporter means a Device Metrics Exporter request cannot apply, for
	// example on a Kubernetes Platform, together with Omnistat, without a GPU compute node, or when
	// the process cannot record it.
	ErrInvalidDeviceMetricsExporter = errors.New("invalid Device Metrics Exporter request")
)
