// Package config defines the runtime configuration model for swallow.
// All environment variable parsing, config file loading, and validation are
// centralized here; application code receives a fully assembled Config object.
//
// Configuration covers only how this process runs. What it talks to — provisioners,
// automation controllers, metrics stores, clusters — is registered at runtime as
// integration records, because a fleet has many of each and they change without a
// redeploy. See docs/decisions/001-system-ownership-boundaries.md.
package config

import "time"

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
	// it, and starting without it would defer that failure to the first operator
	// who tries. Never log this value.
	CredentialKey string `yaml:"credentialKey"`
	// ReconcileInterval is how often each enabled provisioner integration is
	// polled to refresh its server projections.
	ReconcileInterval time.Duration `yaml:"reconcileInterval"`
	// InventoryInterval is how often the attached-hardware inventory (GPUs) is
	// refreshed. Much longer than ReconcileInterval because attached hardware costs a
	// call per machine and changes only at commissioning, so polling it as often as
	// lifecycle state would multiply request count for near-static data.
	InventoryInterval time.Duration `yaml:"inventoryInterval"`
	// SSHKeySyncInterval is how often the API process re-realizes every SSH Key in each
	// key-capable provisioner (decision 039). Key changes and deployments already trigger a
	// sync, so this periodic pass only repairs drift (a key an operator deleted in MAAS, a
	// provisioner that was unreachable) and can be much longer than ReconcileInterval.
	SSHKeySyncInterval time.Duration `yaml:"sshKeySyncInterval"`
	// OperationDispatchInterval controls how quickly pending embedded runs are claimed.
	OperationDispatchInterval time.Duration `yaml:"operationDispatchInterval"`
	// OperationLeaseDuration is renewed while ansible-runner is alive.
	OperationLeaseDuration time.Duration `yaml:"operationLeaseDuration"`
	// OperationMaxParallelism bounds how many Steps a durable Operation runs at once,
	// protecting the provider, PXE, image mirror, and Temporal history from unbounded
	// fan-out. It is a per-Operation ceiling, not a global worker limit.
	OperationMaxParallelism int `yaml:"operationMaxParallelism"`
	// TemporalAddress and Namespace locate the durable workflow service shared by API and worker.
	TemporalAddress       string        `yaml:"temporalAddress"`
	TemporalNamespace     string        `yaml:"temporalNamespace"`
	TemporalTaskQueue     string        `yaml:"temporalTaskQueue"`
	TemporalStartInterval time.Duration `yaml:"temporalStartInterval"`
	// AnsibleRunnerCommand is the pinned runner binary from the Execution Environment.
	AnsibleRunnerCommand string `yaml:"ansibleRunnerCommand"`
	// PlaybookManifest and PlaybookDir identify the immutable release bundle.
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
	// BootMedia configures the installation's Boot Media ISO (decision 047). Optional: with either
	// field empty Boot Media is unavailable and every enable request explains what is missing.
	BootMedia BootMediaConfig `yaml:"bootMedia"`
	// RedfishProbeInterval is how often the API process looks for Servers whose Redfish capability
	// is missing (a newly enrolled Server) or older than RedfishProbeMaxAge, and probes them.
	RedfishProbeInterval time.Duration `yaml:"redfishProbeInterval"`
	// RedfishProbeMaxAge is how long a Redfish capability probe stays current before the sweep
	// re-probes it; BMC firmware updates change what a BMC supports.
	RedfishProbeMaxAge time.Duration `yaml:"redfishProbeMaxAge"`
}

// BootMediaConfig locates the Boot Media ISO the API process serves and the URL BMCs mount it at.
//
// The ISO URL is fixed by the installation, never per Server: BaseURL plus the fixed path
// /boot-media/ipxe/swallow-ipxe.iso. BaseURL must be reachable from the BMC network; many BMCs
// (AMI MegaRAC among them) mount only plain http:// on port 80, so the production reverse proxy
// publishes the path on port 80.
type BootMediaConfig struct {
	// ISOPath is the iPXE ISO file on the API host (or in its container).
	ISOPath string `yaml:"isoPath"`
	// BaseURL is the absolute http(s) URL, without path, at which BMCs reach this API process.
	BaseURL string `yaml:"baseURL"`
}
