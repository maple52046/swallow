package domain

// Setting keys a platform Integration may carry. An operator sets them on an Integration they
// register, and swallow sets them on the Integration it records for a Platform it deployed; the
// platform API clients read them when they connect. The keys are stored on Integrations, so
// renaming one silently drops the setting from every existing Integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
	// SettingSlurmAPIVersion selects the slurmrestd endpoint version, which tracks
	// the Slurm release.
	SettingSlurmAPIVersion = "slurmApiVersion"
	// SettingControllerLeaseDiscovery makes the Kubernetes reader also report dedicated
	// k0s controllers from their kube-node-lease leases. Off by default because the lease
	// naming is a k0s implementation detail; a platform swallow itself deploys turns it on.
	SettingControllerLeaseDiscovery = "controllerLeaseDiscovery"
)
