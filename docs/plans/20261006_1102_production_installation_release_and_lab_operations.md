# Production Installation, Release, Ephemeral k0s, and Server Inventory

## Purpose

Record the durable plan and implementation status for four related operational areas:
the single-VM production installation in the lab, the release artifacts and bootstrap
path used to install it, disposable k0s Platforms on RAM-backed MAAS Servers, and the
Server inventory's Zone, Pool, and network-address presentation.

This plan preserves the settled host, network, release, installation, Platform, and
dashboard decisions while distinguishing completed work from remaining publication,
validation, cleanup, and inventory work.

## Source Scope

This plan consolidates five AI manuscripts from `docs/plans/manuscripts/`:

- `20260914-ephemeral-k0s-maas-image.md` — minimal Ubuntu image and accepted
  seven-Server ephemeral k0s Platform.
- `20260930-server-inventory-fields.md` — independent Zone and Pool columns and
  compact network-address copy control.
- 20261005 production swallow VM manuscript — the production VM's host, storage,
  networking, forwarding, and validation design.
- 20261005 production swallow installation manuscript — the initial installation
  prerequisites and lab-reachable MAAS and Boot Media addresses.
- `20261005-swallow-installation.md` — release publication, artifact reduction,
  image ownership, the `0.1.0-rc.3` release, and the next one-command
  installation and upgrade path.

The later installation manuscript is authoritative where status changed: the VM now
exists, `0.1.0-rc.3` has been published and installed there, and the earlier notes
that the VM or installation had not yet run are superseded. The one-command
installation work is implemented only in local commit `5748deb`; it has not been
pushed, released, or exercised against the production VM.

## Consolidated Background

The production installation is a single Ubuntu 24.04 VM on the lab's front-end host.
It needs Internet egress for release artifacts, Snap, Docker repositories, GHCR,
Docker Hub, and MAAS images, while also needing direct reachability to the isolated
lab Server and BMC network. The host already owns DHCP, PXE, Apache, NFS, and fixed
lab addresses, so the VM must coexist without bridging or reconfiguring those
services.

The resulting VM is dual-homed. Its first interface uses libvirt's existing default
NAT network for the default route and operator access. Its second interface is a
macvtap attachment to the lab NIC with a static address, no gateway, and no DNS.
Host-specific destination NAT exposes the VM's dashboard on the management address
without taking the lab-side iPXE URL away from Apache.

The public installation contract depends on released, digest-pinned artifacts rather
than a repository checkout. Early publication work was blocked because the release
images and bundle did not exist and the publishing host provided a Docker-compatible
nerdctl wrapper but no `docker buildx`. The release path was changed to native
amd64 `docker build` and `docker push`, then simplified so GHCR contains only images
built by swallow and official runtime images remain at their upstream registry by
digest. Release `0.1.0-rc.3` is published and anonymously readable, and it is running
on the production VM.

The next installation slice removes the remaining manual download, checksum,
extraction, and `.env` setup. A release-specific `install.sh` will bootstrap or
upgrade the Compose installation while leaving `swallowctl` as the sole lifecycle
tool. That slice is locally implemented and rehearsed, but publication and real-VM
validation remain pending.

Separately, the lab proved that the existing
`machinePreparation.settings.ephemeral` intent can deploy a disposable k0s Platform
onto a minimal, RAM-backed Ubuntu image. The image owns only kernel and host
prerequisites; swallow continues to own the requested k0s version, topology, and
configuration. A completed seven-Server R4 run remains available for inspection.

The Server inventory refinement is intentionally smaller and independent: Zone and
Pool stay distinct, Pool is the default visible placement field, and the address
copy affordance should match the compact hostname treatment. Its source records the
implementation plan but no completion outcome.

## Confirmed Decisions

- **Production VM topology.** The VM is named `swallow`, uses 8 vCPU, 32 GiB RAM,
  host-passthrough CPU, UEFI, virtio disk and NICs, and an Ubuntu 24.04 cloud image.
  Its root disk is the raw 1 TiB partition `/dev/nvme2n1p1`; the host neither formats
  nor mounts it.
- **External VM network.** The first NIC uses libvirt `default` NAT, with a reserved
  address of `192.168.122.10`. It is the only default route and is the SSH path from
  the production host.
- **Lab VM network.** The second NIC uses macvtap bridge mode on
  `enp193s0f1np1`, with static `192.168.254.200/24`, MTU 1500, no gateway, and no
  DNS. This avoids unreliable host-local DHCP through macvtap, avoids the dynamic
  `.201`-`.254` pool, matches Server-facing MTU, and prevents a non-functional
  default route through `192.168.254.1`.
- **Dashboard forwarding.** Traffic specifically addressed to
  `10.99.254.133:80` is forwarded to `192.168.122.10:80` by a persistent nftables
  systemd unit. `192.168.254.1:80` remains on the host's Apache and continues to
  serve `/ipxe/menu.ipxe`.
- **Existing infrastructure is preserved.** The host's dnsmasq, PXE, NFS, netplan,
  udev naming, Apache, and physical interface layout are not changed. Neither
  `eno1np0` nor `enp193s0f1np1` is placed in a Linux bridge.
- **Production installation addresses.** MAAS must be reachable by lab Servers at
  `http://192.168.254.200:5240/MAAS`; Boot Media is served at
  `http://192.168.254.200` because the BMC path requires port 80. MAAS DHCP remains
  disabled on `192.168.254.0/24`.
- **Native amd64 publication.** On an x86_64 publishing host, `publish.sh` uses
  `docker build` followed by `docker push`; it does not require buildx, emulation,
  `--platform`, or build-time push. `BUILDX_NO_DEFAULT_ATTESTATIONS=1` suppresses
  Docker's default provenance index so swallow images remain single manifests.
  Registry digests are read back with local `crane`.
- **Portable release assembly.** The release assembler extracts the CLI from the
  pushed image with `crane export`; it does not depend on copying from a stopped
  rootless container or on Docker image archives.
- **Release content is Compose-focused.** Native bundles and offline OCI archives
  are not released until those installation modes are complete. Temporal UI is an
  optional operator setting and is not mirrored or managed by the release manifest.
  `offline-media-manifest.json` remains an artifact even though complete offline
  installation is not yet supported.
- **Image ownership.** GHCR carries only `api`, `dashboard`, and `cli` images built
  by swallow. MongoDB, PostgreSQL, and Temporal Server are pulled from their official
  registries by immutable upstream index digest after publication verifies an
  amd64 manifest. Future offline support must not be implemented by copying every
  upstream image into GHCR.
- **Neutral PostgreSQL naming.** The release manifest key is `postgres`, the
  environment variable is `SWALLOW_POSTGRES_IMAGE`, and the Compose service and
  volume are `postgres` and `postgres-data`. Existing database account and secret
  names remain unchanged.
- **Release state.** `0.1.0-rc.3` is the current published prerelease. Its five
  assets are the Compose bundle, amd64 CLI, release manifest, offline-media
  manifest, and checksums. The manifest's six image references are anonymously
  readable; the production VM is installed and running with this release.
- **Single bootstrap entry.** The next release adds `install.sh`. It downloads and
  checks the Compose bundle, extracts it into the installation directory, and calls
  `swallowctl install` or `swallowctl upgrade`. `swallowctl` remains the only
  lifecycle implementation.
- **Installation completion marker.** `state/installed`, written only after
  `doctor` passes and removed by uninstall, is authoritative for distinguishing a
  completed installation from an interrupted first install. Presence of a release
  version in `.env` is not sufficient.
- **One address input.** `--address` or `SWALLOW_ADDRESS` supplies the installation
  address. When omitted on a single-NIC first install, it comes from the default
  route. On upgrade it can be recovered from the persisted MAAS marker. The address
  derives the MAAS URL, Boot Media base URL, and dashboard summary URL.
- **Persist operator settings.** Install and upgrade write explicitly supplied
  settings back to mode-0600 `.env`; derived values fill only missing settings.
  The new MAAS Integration endpoint uses the actual installation address rather
  than `host.docker.internal`, with no dashboard or api-server feature change.
- **Ephemeral k0s contract.** Reuse
  `machinePreparation.settings.ephemeral`; do not add a request or response field.
  Operator-facing documentation must warn that OS, etcd, container runtime, and
  workload state disappear on reboot.
- **Ephemeral image boundary.** The Noble image contains the complete modules for
  the observed MAAS `ga-24.04` PXE kernel ABI but does not embed k0s. Swallow
  installs the pinned k0s binary and owns the Platform configuration.
- **Fail-closed host validation.** Before any k0s node starts, automation verifies
  cgroup v2, required commands and kernel modules, and `k0s sysinfo`, retaining
  stdout as operator evidence.
- **RAM-root compatibility.** k0s uses containerd's native snapshotter and
  `use_local_image_pull=true`; this avoids nested-overlay extraction failures and
  works with both an overlay root and a direct tmpfs root.
- **Lab acceptance topology.** Dedicated controllers are `lab-control-1..3`,
  workers are `lab-compute-1..4`, and the reserved API VIP is
  `192.168.100.200/24`.
- **Server inventory fields.** Zone and Pool remain independent optional columns.
  Pool is visible by default and Zone is hidden by default. Existing saved choices
  win, and legacy `placement` visibility is migrated without restoring a composed
  Placement column.
- **Compact copy action.** A network address uses the shared `CopyButton` inside a
  shrink-wrapped inline container matching the hostname control.

## Architecture and Design Principles

- Keep **Installation** distinct from an OS Deployment. The production contract
  installs and manages the swallow control plane and co-located MAAS; MAAS deploying
  an OS onto a Server is a separate provider-backed action.
- Treat production as an installation contract: immutable image digests, generated
  secrets, lifecycle tooling, state markers, and recoverable operator settings. It
  does not imply a multi-host control-plane topology.
- Preserve network ownership. The production host continues to own lab DHCP, PXE,
  Apache, NFS, and its interface addresses; the VM receives only narrowly scoped NAT,
  macvtap, and destination-forwarding integration.
- Keep image ownership explicit. Swallow releases what it builds and consumes
  official third-party images from their owners by digest.
- Separate bootstrap from lifecycle. `install.sh` obtains and verifies a release;
  `swallowctl` owns install, upgrade, doctor, backup, restore, and uninstall.
- Preserve settings across release replacement. Program files may be overwritten,
  while `.env`, `secrets/`, `backups/`, and `state/` remain installation-owned data.
- Keep Platform orchestration and image responsibilities separate. The image supplies
  OS prerequisites; the swallow Workflow and idempotent Ansible content supply the
  k0s version and desired topology.
- Treat a RAM Deploy Target as intentionally volatile. Successful deployment does
  not imply persistence across reboot, and acceptance must verify that no physical
  disk is used as root.
- Fail before mutation when compatibility cannot be proved. Image gates precede
  upload, and k0s host gates precede starting controllers or workers.
- Keep dashboard presentation changes in the dashboard. Zone/Pool defaults, saved
  preference migration, and copy-control layout do not alter provider semantics,
  filtering, grouping, API contracts, or domain models.

## Functional Scope

- Maintain the dual-homed production VM and persistent management-address dashboard
  forwarding without disturbing the host's lab services.
- Publish amd64 Compose releases with digest-pinned swallow and official runtime
  images, release manifests, checksums, the CLI, and installation metadata.
- Bootstrap a fresh Ubuntu 24.04 host or upgrade an existing completed installation
  through a release-specific `install.sh`.
- Install Docker Engine, co-located MAAS, the swallow services, and bootstrap data
  through `swallowctl`; synchronize the official `ubuntu/noble` image through the
  existing installation flow.
- Persist the operator's installation address and derive lab-reachable MAAS and Boot
  Media URLs.
- Deploy and validate a three-controller, four-worker ephemeral k0s Platform from a
  minimal ABI-matched Ubuntu image.
- Display Pool by default, retain Zone as an opt-in Server inventory column, migrate
  legacy visibility preferences, and compact the observed-address copy control.

## Constraints and Rules

- Run the production VM as a system libvirt domain. The guest's NAT NIC must remain
  first, the lab NIC second, and disk must be the only boot device so lab PXE cannot
  capture the VM.
- The lab address must remain outside dnsmasq's dynamic pool and must be checked for
  use before assignment. The lab NIC has no default route.
- macvtap prevents direct guest-to-host communication on the lab NIC by design;
  guest-to-host management uses NAT.
- The host must not format or mount `/dev/nvme2n1p1`, alter its existing NFS mounts,
  or repartition the disk after the VM has been established.
- The destination NAT rule must match only `10.99.254.133:80`; lab-side
  `192.168.254.1:80` must continue to terminate on the host.
- The publishing host must be x86_64 Linux with a native amd64-capable Docker Engine
  or running BuildKit-backed nerdctl, authenticated for GHCR publication, and have
  local `crane` v0.22.1 available. Buildx and Python/pip are not prerequisites.
- A private GHCR package would require deploy-host authentication; the current
  package is public and all current manifest references must remain anonymously
  retrievable.
- `install.sh` accepts only root execution on Ubuntu 24.04 x86_64. It must verify the
  exact Compose bundle checksum before extraction and must not classify an
  interrupted install as an upgrade.
- The installation's MAAS URL must be reachable from managed lab Servers. Inferring
  the NAT address as MAAS's public endpoint is invalid for this topology.
- BMC Boot Media uses HTTP port 80; the configured base URL must not silently choose
  a different port.
- Do not enable MAAS DHCP on the existing lab subnet.
- Official runtime images stay at their original registry. Docker Hub anonymous pull
  limits are an operational dependency for online installation.
- A release manifest change from `temporalPostgres` to `postgres`, along with the
  Compose service and volume rename, is breaking. No rc.2 data migration exists
  because no real rc.2 installation was established.
- The ephemeral image must match the PXE kernel ABI observed immediately before its
  build and include the complete matching `linux-modules-extra` package.
- Ephemeral deployment must remain fail-closed on cgroup, command, kernel-module, and
  `k0s sysinfo` compatibility.
- Failed image gates prohibit upload. Failed Platform attempts retain diagnostics;
  their Servers are uninstalled and released through swallow before reuse.
- Server inventory changes must preserve existing saved column preferences and must
  not reintroduce the deprecated Placement column.

## Data Model and Format Notes

- VM network facts:
  - NAT address `192.168.122.10`, MAC `52:54:00:b8:05:01`, gateway
    `192.168.122.1`.
  - Lab address `192.168.254.200/24`, MAC `52:54:00:b8:05:02`, parent
    `enp193s0f1np1`, MTU 1500, no gateway or DNS.
  - Management forward `10.99.254.133:80` to `192.168.122.10:80`.
- VM storage is a GPT disk with a 1 TiB `/dev/nvme2n1p1` raw guest disk; cloud-init
  grows the imported cloud-image root filesystem to the available partition.
- Installation-owned persistent paths are `.env`, `secrets/`, `backups/`, and
  `state/`. The completed-install marker is `state/installed`.
- The release manifest remains schema version 1. Its six image references are:
  swallow-owned `api`, `dashboard`, and `cli` in GHCR, plus upstream `mongo`,
  `postgres`, and `temporalServer` references in Docker Hub. Removed fields are
  `temporalUI`, `artifacts.ociArchive`, and `artifacts.nativeBundle`.
- `SWALLOW_TEMPORAL_UI_IMAGE` is operator-owned and, when supplied, must still be a
  digest reference.
- The existing `machinePreparation.settings.ephemeral` wire field carries the RAM
  Deploy Target intent. No parallel field is introduced by this work.
- The accepted image is MAAS resource 21,
  `custom/ubuntu-24.04-k0s-ephemeral-ga-6.8.0-139-20260914`, built for
  `6.8.0-139-generic`, size 559,855,862 bytes, SHA-256
  `cad7ff961f63fa649a02e7fa7476c58a1d209eb8afd069eb5729b0363f68f61d`.
- Five accepted 32 GiB Servers expose an overlay root with
  `overlayroot=tmpfs:size=24G`; two 16 GiB Servers expose `/` directly as tmpfs.
  Both are valid memory-backed roots and passed the same k0s acceptance.
- The retained accepted Platform is
  `ce499003-fc65-4cec-9a7c-1abf4c0d1cc9`; its completed Workflow is
  `d9cc1acb-5789-410d-8733-7eca5c1b42b0`.
- Server inventory visibility stores independent Zone and Pool column choices.
  Existing preferences override defaults; a legacy `placement` preference is input
  only to migration and does not recreate that column.

## CLI / API / Config Notes

- No API request or response change is required for ephemeral k0s. The backend accepts
  the existing normalized ephemeral intent and must pass it to the Platform deployment
  launcher while preflighting only Ready targets.
- No API, provider-placement, filter, grouping, or Server domain-model change is
  required for the inventory refinement.
- Current manual installation uses:
  `swallowctl install --profile production --release-manifest ../release-manifest.json`
  after persisting lab-reachable MAAS and Boot Media URLs.
- The next preferred entry point is:
  `curl -fsSL https://github.com/maple52046/swallow/releases/download/v<version>/install.sh | sudo bash -s -- --address 192.168.254.200`.
- `install.sh` accepts `--address` / `SWALLOW_ADDRESS`, `--version` /
  `SWALLOW_VERSION`, and `--dir` / `SWALLOW_INSTALL_DIR`; the default installation
  directory is `/opt/swallow`.
- Address derivation produces:
  - `SWALLOW_MAAS_URL=http://<address>:5240/MAAS`
  - `SWALLOW_BOOT_MEDIA_BASE_URL=http://<address>` when the HTTP port is 80, with
    the configured port included otherwise.
- The production VM's effective values are
  `SWALLOW_MAAS_URL=http://192.168.254.200:5240/MAAS` and
  `SWALLOW_BOOT_MEDIA_BASE_URL=http://192.168.254.200`.
- New installation bootstrap registers MAAS through its real address. Boot ISO
  creation should prefill rack address `192.168.254.200`.
- Publication uses `deploy/release/publish.sh <version> --github-release`; prerelease
  status currently requires an explicit follow-up edit.
- Release assembly adds a version-pinned `install.sh` to `SHA256SUMS` and the GitHub
  Release, increasing the next release from five to six assets.

## Implementation Plan

1. **Production VM — complete.** The `swallow` VM exists with its NAT and macvtap
   interfaces, 1 TiB root disk, management-address port forward, Internet access,
   lab address, and connectivity to `p01-r01-n08`. The earlier unexecuted-script note
   is historical and must not be treated as an instruction to recreate the VM.
2. **Ephemeral k0s — complete and accepted.** The ABI-matched image was built and
   uploaded. R1-R3 diagnostics were retained and their Servers were uninstalled and
   released. R4 passed three-controller, four-worker, three-etcd-member, VIP,
   kube-system, DNS, Service routing, authenticated API, and cross-worker traffic
   checks. The successful Platform remains running for inspection.
3. **Server inventory refinement — pending status confirmation.** Implement Pool
   visible by default, Zone hidden by default, legacy preference migration, compact
   address copy layout, and focused Playwright coverage; then run dashboard lint,
   build, and the focused scenario. The source records no completed result.
4. **Initial publication — complete.** Commits `72aed84`, `e3f9125`, and `12f7553`
   established native-build publication, portable assembly, release documentation,
   and the first successful `0.1.0-rc.2` release. That release is superseded.
5. **Release reduction — complete.** Commit `0db0905` removed Temporal UI mirroring,
   offline OCI and native bundles, and the crane container shim. Its rehearsal
   produced only the Compose-focused artifacts.
6. **Upstream runtime ownership — complete.** Commit `0613851` changed release
   manifests and Compose naming so GHCR holds only swallow-built images; commit
   `86567e1` updated decision status. Rehearsal passed, and `0.1.0-rc.3` was published
   with five assets and installed on the production VM.
7. **One-command installation — implemented locally, not delivered.** Local commit
   `5748deb` adds `install.sh`, the `state/installed` rule, address derivation,
   persisted settings, actual-address MAAS bootstrap, six-asset release assembly, and
   EN/ZH documentation. Shell checks, release validation, documentation checks,
   local release rehearsal, host rejection tests, Ubuntu container tests, checksum
   failure tests, and setting-persistence tests passed.
8. **One-command delivery — pending approval.** Push the local installation commit,
   publish the next prerelease, and use its one-command path to upgrade the production
   VM from rc.3. Confirm that the API container reaches MAAS through the actual
   address and that the new Integration endpoint and Boot ISO rack prefill are
   correct.
9. **Stable release — pending.** Publish `0.1.0` only after the current release path
   passes clean-VM installation or approved production-VM upgrade validation.

## Non-goals

- Rebuilding or replacing the existing production host's DHCP, PXE, Apache, NFS,
  netplan, udev, or physical network layout.
- Enabling MAAS DHCP or changing the current dnsmasq reservations and dynamic pool.
- Bridging either physical host interface, routing the VM's lab interface through the
  host, or moving `192.168.254.1:80` away from Apache.
- Installing swallow as part of VM creation; VM provisioning and product installation
  remain separate operations.
- Building from a git checkout on the installation host.
- Requiring buildx, cross-architecture emulation, or Python to publish the Compose
  release.
- Mirroring official runtime images into GHCR merely to anticipate future offline
  installation.
- Shipping native installation or claiming complete air-gap support before native
  packaging and offline MAAS media are complete.
- Building Boot ISOs in `swallowctl`, compiling iPXE at runtime, or changing the Boot
  Media base URL default as part of release publication.
- Adding a new ephemeral API field, embedding k0s in the OS image, or providing
  persistence guarantees for a RAM-backed Platform.
- Forcing the two smaller lab Servers to imitate the larger Servers' overlay mount
  presentation after both forms passed acceptance.
- Changing Zone, Pool, provider placement, Server filtering/grouping, or backend
  contracts as part of the inventory presentation refinement.
- Supporting post-install address changes or migrations across the rc.2-to-rc.3
  Compose service and volume rename.

## Open Questions

- Was the Server inventory field refinement implemented elsewhere, or should the
  recorded four-step dashboard implementation still be executed?
- Which prerelease version will carry local commit `5748deb`, and should prerelease
  marking become automatic in `publish.sh` before that publication?
- Will the real installation confirm that the API container can reach the co-located
  MAAS through `192.168.254.200`, including bootstrap of the Integration endpoint and
  Boot ISO rack-address prefill?
- When should native installation and complete offline installation resume, and what
  media contract will include MAAS images without reverting the decision to keep
  official online images at their source registries?

## Future Work

- Publish and validate the one-command installer, then promote a validated prerelease
  to `0.1.0`.
- Remove the orphaned `api-0.1.0-rc.1` package version when release cleanup is
  authorized; retain old mirrors still referenced by published rc.2 manifests.
- Optionally teach `publish.sh --github-release` to mark SemVer prereleases
  automatically.
- Complete the Server inventory implementation and focused dashboard verification if
  its outcome is still pending.
- Add a separately designed path for configuring MAAS DHCP/PXE if swallow is later
  expected to own that integration.
- Add a supported post-install address-change workflow rather than editing persisted
  endpoint settings ad hoc.
- Define and publish native installation and true offline media only when native
  packaging and offline MAAS image delivery are complete.
- Tear down the retained ephemeral k0s Platform through swallow uninstall and Server
  release when inspection is complete; do not treat a reboot as a supported cleanup
  or recovery mechanism.
