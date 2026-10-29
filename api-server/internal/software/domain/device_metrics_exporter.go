package domain

// The AMD Device Metrics Exporter as Managed Software (decision 068): AMD's native per-GPU
// exporter package, installed alone on a single Server or composed by a Slurm deployment onto its
// GPU compute nodes. The kind has one built-in install path and no operator spec fields; the
// values below are the published language the playbook, the composing platform, and the
// assignment record share.
const (
	// DeviceMetricsExporterVersion is the exporter release Swallow installs. The role pins that
	// release's native package and its SHA-256, so a new version is a role and catalog change, and
	// the recorded assignment spec carries it.
	DeviceMetricsExporterVersion = "1.5.3"
	// DeviceMetricsExporterPort is where the exporter listens on the Server's primary address. AMD's
	// default, 5000, is the RDC exporter port Swallow scrapes on a host (decision 003), so it must
	// never be used here. A Slurm Platform scrapes the same port.
	DeviceMetricsExporterPort = 5001
)

// DeviceMetricsExporterJobAttribution is which scheduler the exporter attributes GPU metrics to,
// the playbook's swallow_device_metrics_exporter_job_attribution. A standalone install always
// sends JobAttributionNone; only a composing Slurm deployment sends slurm, together with the job
// hook it installs itself.
type DeviceMetricsExporterJobAttribution string

const (
	// JobAttributionNone runs the exporter without scheduler attribution.
	JobAttributionNone DeviceMetricsExporterJobAttribution = "none"
	// JobAttributionSlurm attributes each GPU to the Slurm job the job hook recorded on it.
	JobAttributionSlurm DeviceMetricsExporterJobAttribution = "slurm"
)
