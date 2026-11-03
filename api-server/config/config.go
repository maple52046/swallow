// Package config defines the runtime configuration model for swallow.
// All environment variable parsing, config file loading, and validation are
// centralized here; application code receives a fully assembled Config object.
//
// Configuration covers only how this process runs. What it talks to — provisioners,
// automation controllers, metrics stores, clusters — is registered at runtime as
// integration records, because a fleet has many of each and they change without a
// redeploy. See docs/decisions/001-system-ownership-boundaries.md.
package config

import (
	"strings"
	"time"
)

// Config is the top-level runtime configuration.
// Populated by merging: defaults → config file → CLI flags → env vars.
type Config struct {
	API APIConfig `yaml:"api"`
}

// APIConfig holds all configuration required to run the API server.
type APIConfig struct {
	// ReleaseVersion is the promoted SemVer presented by health and metrics.
	ReleaseVersion string `yaml:"releaseVersion"`
	// Addr is the HTTP listen address, e.g. ":30051".
	Addr string `yaml:"addr"`
	// MongoURI is the full MongoDB connection string.
	MongoURI string `yaml:"mongoUri"`
	// MongoDB is the MongoDB database name.
	MongoDB string `yaml:"mongoDb"`
	// JWTSecret is the HMAC secret used to sign JWT tokens. Never log this.
	JWTSecret string `yaml:"jwtSecret"`
	// JWTExpiryHours is deprecated and ignored: it set the lifetime of the single 24-hour token
	// that Sessions replaced (decision 042). It is still parsed so existing config files keep
	// loading, and the API logs a warning when it is set. Use AccessTokenTTL.
	JWTExpiryHours int `yaml:"jwtExpiryHours"`
	// AccessTokenTTL is how long a Session access token is accepted. Keep it short (minutes):
	// logout cannot revoke an access token already issued, so this bounds the exposure.
	AccessTokenTTL time.Duration `yaml:"accessTokenTTL"`
	// RefreshTokenTTL is a Session's idle limit: a Session not refreshed for this long ends.
	// Every refresh restarts it, up to SessionMaxAge.
	RefreshTokenTTL time.Duration `yaml:"refreshTokenTTL"`
	// SessionMaxAge is the absolute lifetime of a Session from login; after it the user signs in
	// again however active they were.
	SessionMaxAge time.Duration `yaml:"sessionMaxAge"`
	// BootstrapAdminUsername is the username seeded on first startup.
	BootstrapAdminUsername string `yaml:"bootstrapAdminUsername"`
	// BootstrapAdminPassword is the password for the bootstrap admin. Never log this.
	BootstrapAdminPassword string `yaml:"bootstrapAdminPassword"`
	// CredentialKey is a base64-encoded 32-byte key used to seal integration
	// credentials at rest. Required: an integration cannot be registered without
	// it, and starting without it would defer that failure to the first user
	// who tries. Never log this value.
	CredentialKey string `yaml:"credentialKey"`
	// ReconcileInterval is how often the worker polls each enabled provisioner integration to
	// refresh its server projections, and reads every Platform's membership.
	ReconcileInterval time.Duration `yaml:"reconcileInterval"`
	// InventoryInterval is how often the attached-hardware inventory (GPUs) is
	// refreshed. Much longer than ReconcileInterval because attached hardware costs a
	// call per machine and changes only at commissioning, so polling it as often as
	// lifecycle state would multiply request count for near-static data.
	InventoryInterval time.Duration `yaml:"inventoryInterval"`
	// ControllerResyncInterval is how often each desired-state controller in the worker
	// reconciles every resource it owns. A spec change converges at once through the change feed;
	// this resync is when drift of an auto-correct field is corrected (decision 071), so it bounds
	// how long a change made directly in a provider survives, and how often controllers write to
	// providers when nothing changed in swallow.
	ControllerResyncInterval time.Duration `yaml:"controllerResyncInterval"`
	// ChangePlanTTL is how long a Change Plan stays valid after it is computed (decision 076): an
	// unfinished plan older than this expires instead of applying intent its observations no
	// longer support.
	ChangePlanTTL time.Duration `yaml:"changePlanTTL"`
	// WorkflowResyncInterval is how often the workflow-dispatch controller resyncs: it confirms
	// every unfinished Workflow still has a run and resumes one that lost it (decision 072).
	WorkflowResyncInterval time.Duration `yaml:"workflowResyncInterval"`
	// NodeExporterEnsure runs the worker's Node Exporter controller (software-deployment.md, Node
	// Exporter): it starts configure-node-exporter Workflows on its own, on every deployed,
	// unlocked Server of a Site with automation enabled that lacks a working Node Exporter. On by
	// default, because a production installation's monitoring depends on it. A stack that shares
	// machines it was not asked to manage (the development stack on the lab) turns it off; off,
	// the worker does not register the controller, and only OS deployments and Platform
	// uninstalls install the Node Exporter. Env: SWALLOW_WORKER_NODE_EXPORTER_ENSURE, whose
	// value must parse as a boolean, so a typo never leaves the controller on by accident.
	NodeExporterEnsure bool `yaml:"nodeExporterEnsure"`
	// OperationMaxParallelism bounds how many Steps a durable Operation runs at once,
	// protecting the provider, PXE, image mirror, and Temporal history from unbounded
	// fan-out. It is a per-Operation ceiling, not a global worker limit.
	OperationMaxParallelism int `yaml:"operationMaxParallelism"`
	// TemporalAddress and Namespace locate the durable workflow service shared by API and worker.
	TemporalAddress   string `yaml:"temporalAddress"`
	TemporalNamespace string `yaml:"temporalNamespace"`
	TemporalTaskQueue string `yaml:"temporalTaskQueue"`
	// AnsibleTaskQueue is the task queue on which the Ansible executor runs Ansible Tasks; only
	// that process registers it, because only it owns ansible-runner (decision 072).
	AnsibleTaskQueue string `yaml:"ansibleTaskQueue"`
	// AnsibleRunnerCommand is the pinned runner binary from the Execution Environment.
	AnsibleRunnerCommand string `yaml:"ansibleRunnerCommand"`
	// PlaybookManifest and PlaybookDir identify the immutable release bundle. PlaybookDir is
	// the modules root, which is the ansible-runner project dir: manifest paths are relative to
	// it, and each module's roles are found under it. The key keeps its historical name.
	PlaybookManifest string `yaml:"playbookManifest"`
	PlaybookDir      string `yaml:"playbookDir"`
	// JobRuntimeDir holds mode-0600 ephemeral credentials.
	JobRuntimeDir string `yaml:"jobRuntimeDir"`
	// JobArtifactDir retains logs and runner artifacts.
	JobArtifactDir string `yaml:"jobArtifactDir"`
	// JobArtifactRetention controls automatic removal of local runner artifacts.
	JobArtifactRetention time.Duration `yaml:"jobArtifactRetention"`
	// AllowedOrigins is a comma-separated development exception for a dashboard served from
	// another origin. Listed origins may send credentials (the refresh cookie), so each must be
	// explicit; "*" is rejected by validation.
	AllowedOrigins string `yaml:"allowedOrigins"`
	// ImageUploadMaxBytes caps the request body the server accepts, sized for OS image
	// uploads (POST /provisioning/images streams a potentially multi-gigabyte artifact).
	// It becomes the Fiber body limit; because the body is streamed, this bounds a single
	// upload rather than being buffered. Must be positive.
	ImageUploadMaxBytes int64 `yaml:"imageUploadMaxBytes"`
	// MachineToken is a static bearer token accepted by the endpoints whose callers
	// are other systems, such as Prometheus scraping discovery. Those hold one
	// credential in their configuration and cannot
	// refresh a JWT. Optional: without it those endpoints require an admin JWT.
	// Never log this value.
	MachineToken string `yaml:"machineToken"`
	// BootMedia configures where Boot ISOs are built and stored and the URL BMCs mount them at
	// (decision 047). Optional: without BaseURL no Boot ISO can be built or mounted, and
	// every build or enable request explains what is missing.
	BootMedia BootMediaConfig `yaml:"bootMedia"`
	// CLIBinary is the path of the linux-amd64 swallow CLI this process serves, without
	// authentication, at /downloads/swallow for Server Enrollment (decision 053): a host that keeps
	// its OS downloads it from the installation instead of the internet. The production bundle
	// ships it at bin/swallow. Optional: empty or missing makes the download, and so enrollment from
	// the generated command, fail with 404 and a reason. The file is public; it holds no secret.
	CLIBinary string `yaml:"cliBinary"`
	// RedfishProbeInterval is how often the worker looks for Servers whose Redfish capability
	// is missing (a newly enrolled Server) or older than RedfishProbeMaxAge, and probes them.
	RedfishProbeInterval time.Duration `yaml:"redfishProbeInterval"`
	// RedfishProbeMaxAge is how long a Redfish capability probe stays current before the sweep
	// re-probes it; BMC firmware updates change what a BMC supports.
	RedfishProbeMaxAge time.Duration `yaml:"redfishProbeMaxAge"`
	// NTPServer is the address (IP or DNS name) of the installation's Control-Plane NTP Server, as
	// the Servers swallow deploys reach it (decision 065). Every OS provisioning points the new
	// Server's time client (systemd-timesyncd, chrony, or ntpd, whichever the OS has) at it, and
	// Platform deployments measure node clocks against it. The
	// production installation sets it to SWALLOW_ADDRESS. Optional for the process to start, but
	// without it every OS deployment is refused at acceptance with the setting to fix.
	NTPServer string `yaml:"ntpServer"`
	// PublicURL is the absolute http(s) URL, without path, at which the Servers swallow deploys
	// reach this API through the installation's edge (for example http://10.170.168.20). A
	// Platform's own Grafana reads the Platform metrics gateway at
	// <PublicURL>/api/v1/platform-metrics/<platformId>/prometheus, so the processes that plan
	// Platform deployments (the API and the worker) need it. Optional for the process to start, but
	// without it a deployment that installs a Platform's Grafana is refused when it is planned.
	PublicURL string `yaml:"publicURL"`
	// MonitoringScrapeAddresses is the comma-separated list of IP addresses the Site Prometheus
	// scrapes exporters from, as the deployed Servers see them (the production installation sets
	// SWALLOW_ADDRESS). An exporter that admits only listed addresses, such as Omnistat's
	// allowed_ips, is configured with them. Optional for the process to start, but without it a
	// deployment that installs such an exporter is refused when it is planned.
	MonitoringScrapeAddresses string `yaml:"monitoringScrapeAddresses"`
}

// ScrapeAddresses returns MonitoringScrapeAddresses as a list, empty when none is set. Validate
// has already checked and trimmed each entry.
func (c APIConfig) ScrapeAddresses() []string {
	var addresses []string
	for _, address := range strings.Split(c.MonitoringScrapeAddresses, ",") {
		if address = strings.TrimSpace(address); address != "" {
			addresses = append(addresses, address)
		}
	}
	return addresses
}

// BootMediaConfig locates the Boot ISOs the API process builds and serves (decision 047) and the
// URL BMCs mount them at.
//
// A Boot ISO's URL is derived, never typed per Server: BaseURL plus
// /boot-media/ipxe/<isoId>/swallow-ipxe.iso. BaseURL must be reachable from the BMC network; many
// BMCs (AMI MegaRAC among them) mount only plain http:// on port 80, so the production reverse
// proxy publishes /boot-media/ on port 80.
type BootMediaConfig struct {
	// ISOPath is retired (decision 047): the installation no longer supplies an ISO. A value is
	// ignored with a startup warning so an upgraded configuration still loads.
	ISOPath string `yaml:"isoPath"`
	// BaseURL is the absolute http(s) URL, without path, at which BMCs reach this API process.
	BaseURL string `yaml:"baseURL"`
	// Dir is where the API process writes Boot ISOs, one subdirectory per ISO. It must be
	// writable by the API process and persistent across restarts. The worker reads it (read-only)
	// when hardware inspection resolves a Server's Boot ISO.
	Dir string `yaml:"dir"`
	// IPXEDir holds the prebuilt iPXE assets a build packages: ipxe.lkrn, ipxe.efi, iPXE's
	// genfsimg script, and a VERSION file. The api-server image installs them; a missing file
	// only makes the builder unavailable with a reason.
	IPXEDir string `yaml:"ipxeDir"`
}
