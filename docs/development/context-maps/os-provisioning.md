# OS provisioning context map

The OS Provisioning bounded context owns operator intent, policy, validation,
Deployment Templates, and durable Provisioning Tasks. External provisioners own
machine inventory, OS Images, subnets, IP allocation, and the execution of
deploy/release/network operations.

```text
dashboard/cli --published HTTP contract--> OS Provisioning application
server projection --identity/status--> OS Provisioning policy
OS Provisioning intent --provider port--> anticorruption adapter --> MAAS
MAAS machine/link state --translation--> Network Configuration observation
```

The adapter is not a conformist boundary. Provider terms such as MAAS `AUTO`,
`LINK_UP`, `link_subnet`, and `unlink_subnet` do not enter the Swallow deployment
model. `AUTO` is translated to the read-only `provider_managed` observation;
`LINK_UP` is translated to the NIC configuration state `link_only`.

Swallow applies `automatic`, `dhcp`, or `static` intent when the operator deploys
an OS or changes a Ready Server's Network. `automatic` asks the provider to
allocate and record an address; `dhcp` leaves address, gateway, and DNS to the
network's DHCP server; `static` carries the operator-selected address. Release
cleanup is coordinated through a durable Provisioning Task. No continuous
network reconciliation runs on deployed hosts.

## Enrollment and boot path (decisions 047, 049, 053, 055, 057)

```text
network boot/BMC --Boot ISO or provider PXE--> provisioner enrollment
dashboard/cli --published enrollment contract--> durable enrollment Workflow --> provisioner adapter
host-side CLI --provisioner credential--> existing-OS enrollment
provisioner inventory --reconciliation--> Server projection
Boot ISO builder --owned file--> Boot Media --BMC or hypervisor--> machine firmware --> provisioner rack
```

Server Enrollment puts a physical machine or lab virtual machine into the
provisioner's inventory; it never creates a Server record directly.
Reconciliation remains the only source of the Server projection. The existing-OS
path intentionally calls the provisioner from the host and keeps its OS, while
network-boot and virtual-machine paths end in provider inventory and use the
same automatic hardware-inspection flow.

Swallow owns each per-provisioner Boot ISO and a Server's Boot Media intent.
The BMC or hypervisor owns the media attachment and firmware boot, while MAAS
owns the downstream PXE/GRUB boot resources and resulting machine inventory.
The Boot ISO translates the site's DHCP boot into the MAAS rack's BIOS iPXE or
UEFI GRUB path; it is not an OS Image.

## Disk-layout inspection guard (decision 057)

```text
MAAS storage/firmware observation --adapter translation--> Server provisioning.inspectionRequired
Server projection --published HTTP contract--> dashboard/cli
Server projection --preflight gate--> disk deployment and explicit deployment Retry
```

The provisioner owns the observed storage layout. The adapter derives
`inspectionRequired` when a UEFI machine's boot disk lacks the EFI layout a disk
deployment needs. OS Provisioning owns the safety policy: a disk deployment or
its explicit Retry stops before provider writes or recovery, while a RAM
deployment remains allowed. Re-inspection updates the provider layout and the
next reconciliation updates the guard; clients do not clear it directly.

## SSH Keys realized in the provisioner (decisions 039 and 041)

```text
sshkey Service --ProvisionerKeys port--> sshkey infra adapter --ProviderFactory--> SSHKeyRegistrar --> MAAS account sshkeys
OS Provisioning deploy --SSHKeyRegistration port--> sshkey EnsureRegistered (before any provider write)
```

The Access context owns SSH Key records; the provisioner's key list is only a
realization target. The optional `SSHKeyRegistrar` capability (advertised as
`SSHKeyRegistration`) keeps MAAS vocabulary (`/account/prefs/sshkeys/`, numeric key ids)
inside the MAAS adapter. swallow removes only provider keys it registered or adopted, so
keys an operator added in MAAS are untouched. A non-capable provisioner is recorded as
`unsupported`; there is no cloud-init fallback yet.

The installation Deployment Key is the only SSH credential used for readiness,
login-user probes, and Ansible execution. Site Automation may store a become
password but cannot override that key.

The OS Image default user is swallow-owned data on the OS Image Provider Data Overlay
(`defaultUser`, with a built-in per provider OS family). Reconcile mirrors the effective
value onto each deployed Server as `deployedImageDefaultUser`, alongside
`deployedImageName`, so automation reads it from the projection without a catalog call.

## Provisioner HTTP Proxy written through (decision 056)

```text
dashboard --provisioner-http-proxy.md--> HTTPProxyUseCase --ProviderFactory--> HTTPProxyReader/Writer --> MAAS get_config/set_config
```

The provisioner owns the proxy setting; swallow keeps no copy. The optional
`HTTPProxyReader`/`HTTPProxyWriter` capability (advertised as `HTTPProxy`) translates the
swallow mode set (`none | builtin | external | peer`) to and from MAAS's
`enable_http_proxy`, `use_peer_proxy`, and `http_proxy`; those names stay inside the MAAS
adapter.

## Subnet DNS written through (decision 058)

```text
dashboard --provisioner-subnet-dns.md--> SubnetDNSUseCase --ProviderFactory--> SubnetDNSReader/Writer --> MAAS subnets
```

The provisioner owns its subnets and DNS server lists; swallow keeps no copy.
The optional Subnet DNS capability reads live and writes through by provider
subnet id. `automatic` and `static` deployment intent consume this observation
(falling back to the provisioner's DNS when the list is empty), while `dhcp`
intentionally takes DNS from the network DHCP server. Provider subnet ids and
MAAS `dns_servers` remain outside the Swallow deployment model.

## Time synchronization after SSH (decision 065)

```text
installation (SWALLOW_API_NTP_SERVER = SWALLOW_ADDRESS) --> api-server config
Deploy OS / ensure-os launcher --ntpStepPlanner--> configure-ntp-<serverId> ansible Task --> the OS's own time client
platform launcher --ntpStepPlanner--> install-platform trusted vars --> time_sync_check role (reads only)
swallowctl ntp upstream (local-ntp.sh) --> MAAS ntp_servers --> MAAS chronyd on the swallow host (UDP 123)
```

Time sync crosses the provisioner boundary without a write-through. Every Server swallow
provisions is pointed at the installation's NTP server by the `configure-ntp-<serverId>` Ansible
Task right after SSH readiness; the target is an api-server setting, not a provider fact or a
Site record. Platform deployments do not set time: their install playbooks only check it first.
The co-located MAAS's chronyd is the installation's Control-Plane NTP Server, so the
installation installs none of its own; the installation tooling (not the API) sets its upstream
through MAAS's `ntp_servers`, which MAAS also uses to hand its rack to the machines it deploys
through cloud-init. swallow composes no cloud-init for time (ADR 058 still holds).
