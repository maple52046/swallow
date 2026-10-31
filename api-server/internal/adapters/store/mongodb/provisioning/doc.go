// Package provisioning is the MongoDB store of the OS provisioning context: Boot ISOs, Deployment
// Templates, OS Image overlays and verifications, provisioning tasks, and the provisioner settings
// swallow owns as desired state (`provisioner_http_proxies`, `provisioner_subnet_dns`).
//
// OS Image overlays hold swallow-owned facts layered over what the provisioner reports (decision
// 025), so reading one back is authoritative for what it holds. Provider access belongs to the
// osprovisioning adapters and provisioning rules to the core provisioning context.
package provisioning
