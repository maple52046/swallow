// Package provisioning is the MongoDB store of the OS provisioning context: Boot ISOs, Deployment
// Templates, OS Image overlays and verifications, Server Tag overlays, and provisioning tasks.
//
// Overlays hold swallow-owned facts layered over what the provisioner reports (decisions 025 and
// 031), so reading one back is authoritative for what it holds. Provider access belongs to the
// osprovisioning adapters and provisioning rules to the core provisioning context.
package provisioning
