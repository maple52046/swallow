# Fleet Operations, Access, Installation, and Boot Media

## Purpose

Record the long-term plan and the decisions made from 2026-09-27 to 2026-10-05 for how
swallow operates a GPU datacenter fleet: the Server list as a fleet console, generic OS
provisioning states, OS deployment progress and stall detection, Redfish Boot Media with
swallow-built iPXE Boot ISOs, machine and human access (Deployment Key, Access Keys, API keys,
sessions, Server Default User), Docker host management, single-VM production installation,
and hiding unfinished dashboard features in release builds. It is the reference for
extending these areas without reopening settled decisions.

## Source Scope

This plan consolidates thirteen AI manuscripts from `docs/plans/manuscripts/`:

- `20260927-server-list-ui-refactor.md` — Server list fleet console.
- `20261001-dashboard-experimental-features.md` — hiding in-development features in release builds.
- `20261001-installation-l2-release.md` — single-VM production installation (L2) and publication.
- `20261001-ssh-key-management.md` — Deployment Key, Access Keys, OS Image default user.
- `20261002-api-keys-and-token-refresh.md` — API keys, access/refresh tokens, sessions.
- `20261002-deployment-key-only-automation.md` — automation uses only the Deployment Key.
- `20261002-docker-host-management.md` — Docker Engine explorer and registry credentials.
- `20261002-server-default-user.md` — Server Default User and the docker group.
- `20261003-os-deploy-apt-upstream-resilience.md` — deploy stall detection; package-origin preflight removed.
- `20261003-redfish-boot-media.md` — Redfish capability and Boot Media.
- `20261004-os-provisioning-generic-states.md` — generic OS provisioning states, list cues, elapsed clock.
- `20261005-ipxe-boot-iso-builder.md` — Boot ISOs built in swallow.
- `20261005-boot-media-apply-progress.md` — Boot Media preflight progress.

Related decision records: ADR 039 (SSH keys), 040 (single-VM MAAS co-location), 041
(Deployment-Key-only automation), 042 (API keys and sessions), 043 (Docker host management),
044 (Docker registry credentials), 045 (Server Default User), 046 (OS deployment progress and
stall), 047 (Redfish Boot Media), 048 (generic OS provisioning states), 049 (Boot ISO builder).

## Consolidated Background

swallow drives bare-metal GPU Servers through an OS provisioning provider (MAAS today) and
runs automation on them over SSH with Ansible; there is no node agent. Several operator
problems shaped this period:

- **Visibility.** The Server list had to serve both discovery (GPU inventory, tags,
  placement) and triage (deployment, power, health), and it hid real provisioning work: two
  Servers were releasing yet no "Releasing" appeared, because the whole provisioning axis had
  been treated as MAAS vocabulary. The real provider leak was `commissioning` (Ironic calls the
  step introspection).
- **Silent deploy hangs.** An OS deploy sat in MAAS "Configuring OS" for 1h28m because the lab's
  egress to `archive.ubuntu.com` failed; swallow showed only "deploying" until a two-hour
  timeout. A package-origin preflight was built and then removed because it could not probe the
  repository the installation really uses.
- **Networks without provisioner DHCP.** Servers such as `tainan-ci` sit on networks whose DHCP
  is external, so MAAS cannot PXE them. The manual recipe (mount an iPXE ISO through the BMC,
  boot it first, chain to the MAAS rack) did not scale. Redfish Boot Media automated it, first
  with one installation-supplied ISO and then with Boot ISOs built per provisioner in swallow.
  The enable preflight takes about five minutes (mostly a deliberate settle wait), which looked
  like a hang until it reported progress.
- **Access.** Automation needed one machine identity (the Deployment Key) instead of per-site
  private keys; humans needed Access Keys synced to the provisioner; the CLI needed API keys;
  the dashboard needed silent token renewal; existing hosts needed a known login user.
- **Installation.** "Production" means swallow's installation contract (digest-pinned images,
  generated secrets, lifecycle tooling) on one VM with a co-located MAAS region and rack.

## Confirmed Decisions

### Server list and status presentation

- The Server list loads every projection in the Site scope (`includeAbsent=true`) and applies
  search, facets, grouping, sorting, and pagination locally; no API fan-out. The fleet overview
  ignores list filters and never synthesizes a combined health value.
- Filters, view, group, sort, and page are URL-owned; density and page size are browser
  preferences. Dashboard `q` searches identity, tags, and GPU vendor/model; other hardware
  facts use structured facets.
- The default order is operational priority: changing work first, then provisioning issues,
  Ready, Deployed, other states, and absent projections.
- Row navigation uses real links; mutating work stays in the guarded action surfaces. SSE
  connection state (Live / Reconnecting / unavailable) is exposed through an application port.
- swallow owns the generic OS provisioning states; each provider adapter maps its lifecycle onto
  them: `new | inspecting | ready | allocated | deploying | deployed | releasing | testing |
  rescue | broken | failed | retired | unknown`. In progress: `inspecting`, `deploying`,
  `releasing`, `testing`. `commissioning` became `inspecting` on the API, including the action
  (`POST /servers/{id}/inspect`; `/commission` removed). The idle state is shown as **Ready**;
  "Not deployed" is gone. Other states keep their own names (New, Retired, Unknown) so an
  uninspected or retired machine never reads as Ready.
- This supersedes the 2026-09-28 list refinement that hid provider lifecycle from the list: the
  list now shows the generic provider states, while the Swallow deployment axis stays separate
  and "View workflow" / "Monitor workflow" still require a running or failed Swallow deployment.
- In-progress states show a trailing spinner inside the badge (no stripes), and the whole row or
  card carries a sweeping light band. Both keep moving under `prefers-reduced-motion` by product
  request.
- In-progress states show a running clock: provider states from `provisioning.stateSince` (when
  swallow first observed the current state; reset when it changes), Swallow deployments from
  `deployment.startedAt` as one continuous clock across Deploying and Verifying.
- Server identity shows tags beneath the hostname, a lock icon before it when locked, and a RAM
  deployment as an amber memory glyph with an explanatory tooltip; an installed OS image name is
  neutral text, lifecycle outcomes are badges.

### Release builds and experimental features

- Availability is decided at build time by `import.meta.env.DEV`, read only in
  `src/di/container.ts`. Release builds hide monitoring, OS image upload, and Deployment
  Templates with no switch; dev builds offer a per-feature "Experimental features" dialog (all on
  by default, stored in `swallow.dev.experimentalFeatures`). Shared-page health keeps its place
  with "Not available in this release". API, CLI, and backend are unaffected.

### OS deployment progress and stall

- While MAAS reports Deploying, the newest machine event since the attempt start is projected as
  a non-terminal stage (for example `configuring_os`). With no newer event for 25 minutes the
  Step asks for attention with `deployment_provider_stage_stall`, measured from MAAS event
  timestamps; MAAS is never aborted.
- The package-origin preflight and the OS Image check URL were removed (user, 2026-10-03): a
  preflight is worth having only if swallow can read each image's real repository configuration
  and probe exactly that.

### Access and automation identity

- One installation-wide, system-owned **Deployment Key** (ed25519, private key sealed with the
  credential key, never returned) and per-user **Access Keys** (public key only; "generate"
  returns the private key once). Keys sync to capable provisioners (MAAS `/account/prefs/sshkeys/`)
  capability-first; only keys swallow recorded are removed from the provider.
- The Deployment Key is created at installation by `swallow-api deployment-key ensure` (run by
  `swallowctl install|upgrade` after `migrate`, and by dev/testing seeds), not at API start. OS
  and Platform deployments are refused with `409` without it. This supersedes the
  "generate on first API start" part of ADR 039.
- Every automation SSH login uses the Deployment Key (ADR 041); a Site `sshPrivateKey` is
  rejected with `400`. `CheckRunnable()` (enabled and Deployment Key present) gates execution.
- The login user is the effective **Server Default User**: the value set on the Server, else the
  deployed OS Image's default user (overlay value, else per OS family), else the fallback probe.
  It is cleared when swallow starts a new OS deployment or the Server is observed without an OS.
  Setting it may take the account's password once to install the Deployment Key; the password is
  never stored. Docker CE adds that user to the `docker` group unless it is root.
- API keys (`swk_…`, SHA-256 stored, inherit the owner's role, optional expiry, revoke by
  delete) serve the CLI and scripts. The browser uses a short-lived in-memory access JWT and a
  rotating refresh token in an HttpOnly `SameSite=Strict` cookie, with a 30-second rotation
  grace and reuse detection. Defaults: access 15m, refresh idle 168h, session max 720h.

### Docker host management

- The Server Containers tab manages Docker through `dashboard → api-server → host Docker Engine
  API` (never browser-to-host). Docker CE's `spec.enableApi` (default `true`) opens an
  unauthenticated API through a systemd drop-in (`-H tcp://0.0.0.0:2375`), with a UI risk notice.
  swallow stores only the Software Assignment, never images, containers, volumes, or networks.
- Registry credentials are a Docker CE setting (`/api/v1/software/docker-ce/registry-credentials`),
  installation-wide, one per normalized registry host, password sealed and write-only; Docker Hub
  aliases fold into `docker.io`. Pulls are bounded at 55 minutes and the pull dialog can be closed
  while the Images section keeps the request.

### Redfish Boot Media and Boot ISOs

- swallow probes Redfish itself (MAAS knows only the power driver); an IPMI-driven BMC may offer
  Redfish at the same address. BMC credentials are read live from the provisioner and never
  stored. The host System is chosen by hardware UUID or as the only System with `Boot`.
- Both paths apply Boot Media: the Server detail **preflight** (probe, mount, direct the boot,
  verify, then save) and an `ensure-boot-media` Task before every `provision-os`, because BMCs
  lose settings. A deployment boot watch recovers a host that missed the ISO (mount at once in
  POST; re-apply and Redfish restart at most once when MAAS never reached PXE).
- Boot direction is per firmware: AMI Aptio fixed boot order "UEFI USB Device" first; else
  BootOrder plus `UefiBootNext`; else the DMTF `Cd` override (which hung an Aptio host). A fresh
  mount waits three minutes before the host may power on.
- **Boot ISOs** (ADR 049, superseding the installation-supplied ISO of ADR 047): the operator
  chooses a Site's provisioner and the MAAS rack address (pre-filled from the provisioner
  endpoint host); swallow renders its fixed, verified template (DHCP from the site network, set
  `next-server`, chain `http://<rack>:<port or 5248>/ipxe.cfg`; no script editing) and packages a
  BIOS and UEFI ISO synchronously. A Boot ISO belongs to one provisioner Integration; a Server
  may only use one of its own provisioner's. A Boot ISO in use by enabled Boot Media cannot be
  deleted. `api.bootMedia.isoPath` is retired. iPXE is unsigned: Secure Boot must be off.
- Server Boot Media chooses a Boot ISO (`isoId`), can switch (ejecting the previous one) or
  re-apply, and keeps the choice when disabled. A setting enabled before Boot ISOs names none and
  must choose one; until then its deployments fail `boot_media_not_configured`.
- The preflight reports real progress (user, 2026-10-05): phases `probing`, `ejecting`,
  `mounting`, `settling` (with a known end), `directing`, `verifying`; the dashboard shows a
  progress bar, steps, elapsed time, and the settle countdown; the dialog may be closed while the
  preflight continues; only one preflight runs per Server.

### Installation and publication

- Production is one Ubuntu 24.04 VM with a co-located MAAS 3.6 region+rack (snap, never
  `maas-test-db`) sharing the Compose PostgreSQL in a separate `maas` role and `maasdb`
  database. The Dashboard is served on HTTP port 80; TLS termination is follow-up work.
- Images: one GHCR package `ghcr.io/<owner>/swallow` tagged `<component>-<version>` (`api`,
  `dashboard`, `cli`, plus mirrored upstream runtime images), digests recorded in the manifest.
  There is no CI: releases are published from a workstation with `deploy/release/publish.sh`.
- Backend services join a non-internal `egress` network to reach MAAS and Servers.

### Working rules confirmed by the user

- User documentation (docs/en and docs/zh-TW) is written by a GPT 5.6 Sol xhigh documentation
  subagent and reviewed against the code.
- Lab verification runs only on `lab-*` Servers or `tainan-ci`; touching a lab BMC or deploying
  on lab hardware needs the operator's go-ahead when not explicitly requested.
- Lab-network workarounds (package mirrors, cache-preserving layer splits) belong in a
  throwaway verification Dockerfile, never in the dev or production Dockerfiles. "Production"
  is a contract to be treated with production thinking, not a label.
- Operator-facing failure reasons are one plain sentence (for example "the Boot Media directory
  … is not writable"), without sentinel prefixes or raw OS errors.

## Architecture and Design Principles

- **Ownership is explicit.** swallow owns its settings and observations (Boot Media setting,
  Redfish capability, apply progress, Server Default User, `stateSince`, deployment stage); the
  provider owns machine lifecycle and BMC credentials; the BMC owns live media state. Each
  swallow-owned field on the Server document is written only by its own store method so the
  reconcile `Upsert` never erases it.
- **Capability-first providers** (ADR 031): optional provider capabilities (SSH key
  registration, BMC connection, hardware validation) with a recorded `unsupported` state rather
  than provider checks in callers.
- **Generic vocabulary at the boundary.** API values are swallow's generic terms; provider
  labels survive only as `providerState` or explicitly provider-scoped fields
  (`commissioningStatus`).
- **Truthful status.** Never synthesize a combined Server health; never claim completion before
  the API answers; status is always text, never colour or icon alone; an unknown value is shown
  safely rather than as a failure.
- **Clean Architecture per component.** Dashboard domain/application import no React, router, or
  HTTP; presentation never imports infrastructure; one shared composed component per feature
  (deployment badge, elapsed time, in-progress spinner, Boot Media progress).
- **Long BMC and provider work stays honest.** Synchronous requests are bounded; progress is
  recorded where any client can read it; closing a dialog never cancels server-side work.
- **No secret leaves its store.** Private keys, become passwords, registry passwords, and BMC
  passwords are sealed or read live, and never appear in responses, logs, or errors.

## Functional Scope

- Server list: discovery facets, operational ordering, contextual links, SSE state, responsive
  table and cards, in-progress spinner, row sweep, and elapsed clock.
- Server detail: provider stage while deploying; Connection card with the Server Default User;
  Management controller card with Redfish capability and Boot Media (choose, change, re-apply,
  disable, live check, re-probe, apply progress); Containers tab (images, containers, volumes,
  networks, logs) for Servers with Docker CE.
- Provisioning: Deploy OS, Templates (dev builds), OS Images (default user overlay, upload in dev
  builds), Boot ISOs (build, view script, download, delete).
- Software: Docker CE install with `enableApi`; Software settings with Docker CE registry
  credentials.
- Account: SSH Keys (Deployment Key, Access Keys, provisioner sync status) and API Keys.
- Installation: `swallowctl install|upgrade|doctor|backup|restore|uninstall` on one VM, local
  MAAS, bootstrap of Site, Integration, and automation defaults.

## Constraints and Rules

- BMCs: HTTP only on port 80, image under a directory, Range support, no credentials for HTTP
  mounts on AMI; the BMC streams the ISO at every boot, so the serving host must stay up. Many BMC
  networks have no Internet egress, so swallow serves ISOs itself.
- A Boot ISO's rack address is an IPv4 address or hostname with an optional port; swallow neither
  resolves nor contacts it. Names are unique per Integration, 1–63 characters.
- An apply record older than ten minutes is abandoned; a second Boot Media write during a running
  preflight is `409`.
- Automation never uses a Site private key; deployments require the Deployment Key.
- Docker Engine API is unauthenticated by design in this version; internal datacenters only.
- In dev, editing api-server Go code makes Air restart the worker, which fences a running
  deployment's lease; avoid it while a lab deployment is in flight.
- The lab cannot reach `archive.ubuntu.com`, `security.ubuntu.com`, `download.docker.com`,
  `deb.debian.org`, or PyPI at times (Fastly / Canonical CDN paths); that is a network matter, not
  swallow code, and lab acceptance deploys on `tainan-ci` are RAM deploys.
- Dashboard completion gate: lint, build, relevant Playwright specs, and a manual JSDoc,
  accessibility, state, and layering review; the full Playwright suite is compared with the HEAD
  baseline (stale visual baselines and some operator-console specs fail there already).

## Data Model and Format Notes

- Server document (swallow-owned top-level fields): `defaultUser`, `bootMedia {enabled, isoId,
  updatedAt, lastAppliedAt, lastAppliedBy (preflight|ensure), bootOverride, lastError,
  lastErrorAt}`, `redfish {support, reason, serviceRoot, vendor, product, redfishVersion,
  firmwareVersion, systemId, virtualMedia, bootOverrideModes, probedAt}`, `bootMediaApply {isoId,
  phase, startedAt, phaseStartedAt, phaseEndsAt}`; projection `provisioning.stateSince`,
  `provisioning.deployedImageDefaultUser`. Stored `commissioning` reads as `inspecting`.
- `boot_isos` collection: `{_id, integrationId, normalizedName (unique with integrationId), name,
  rackAddress, chainUrl, script, ipxeVersion, sizeBytes, sha256, createdAt, createdBy}`; files at
  `<api.bootMedia.dir>/<isoId>/swallow-ipxe.iso`.
- iPXE assets: pinned `v2.0.0` commit `12798ec29aa8a64d8675c4378b99f5fe28447afb`, `ipxe.lkrn`,
  `ipxe.efi`, `genfsimg`, `VERSION` in `api.bootMedia.ipxeDir`; each ISO is `genfsimg -s
  autoexec.ipxe ipxe.lkrn ipxe.efi` (script on the ESP root and as the BIOS initrd).
- SSH keys: `purpose` deployment|access, partial unique index on `purpose=deployment`,
  `providerSync` per Integration with `providerKeyId`. API keys: hash, display prefix, per-user
  unique name, at most 50, `lastUsedAt` at most once a minute. `auth_sessions`: refresh hash,
  previous hash, idle and absolute expiry, `revokedAt`, TTL index.
- Docker CE assignment spec: explicit `enableApi` boolean; legacy records without it read as
  disabled. Registry credential: normalized host, username, sealed password.

## CLI / API / Config Notes

- Servers: `POST /servers/{id}/inspect`; `GET|PUT /servers/{id}/boot-media` (PUT `{enabled,
  isoId}`; GET `apply`, `image`, `setting.isoId`; `?live=true`); `POST
  /servers/{id}/redfish/probe`; `PUT|DELETE /servers/{id}/default-user`; `/servers/{id}/docker/*`.
- Provisioning: `GET|POST /provisioning/boot-isos`, `GET|DELETE /provisioning/boot-isos/{id}`;
  unauthenticated `GET|HEAD /boot-media/ipxe/{isoId}/swallow-ipxe.iso` (Range).
- Access: `/ssh-keys` (list, create, generate, get, delete, deployment regenerate/replace, sync),
  `/api-keys`, `/auth/login|refresh|logout|me`.
- Software: `/software/assignments`, `/software/docker-ce/registry-credentials`.
- CLI: `swallow servers inspect|boot-media get|enable --iso|disable|redfish-probe`, `swallow
  provisioning boot-isos list|get|create|delete`, `swallow ssh-keys …`, `swallow api-keys
  list|create|revoke`, `login --api-key-stdin`, `SWALLOW_API_KEY` / `--api-key`.
- Config: `api.bootMedia.baseURL` (`SWALLOW_API_BOOT_MEDIA_BASE_URL`), `api.bootMedia.dir`
  (`SWALLOW_API_BOOT_MEDIA_DIR`, default `/var/lib/swallow/boot-media`, writable),
  `api.bootMedia.ipxeDir` (`SWALLOW_API_IPXE_DIR`, default `/usr/share/swallow/ipxe`),
  `isoPath` retired with a startup warning; `api.redfishProbeInterval` (10m) and
  `redfishProbeMaxAge` (24h); `accessTokenTTL`, `refreshTokenTTL`, `sessionMaxAge`
  (`jwtExpiryHours` deprecated). Production Compose mounts the named `boot-media` volume.

## Implementation Plan

All slices below are implemented. Remaining work per slice is listed under Open Questions and
Future Work.

1. Server list fleet console and its refinements (dashboard only).
2. Experimental features gate, dev dialog, release-build e2e.
3. Single-VM installation L2: Compose, `swallowctl`, local MAAS, bootstrap, doctor, release
   scripts; accepted on a clean libvirt VM.
4. SSH keys, Deployment Key at installation, OS Image default user; Deployment-Key-only
   automation.
5. API keys and access/refresh sessions across api-server, CLI, and dashboard.
6. Docker host management, registry credentials, long pulls.
7. Server Default User with one-time key installation and the docker group.
8. OS deployment stage projection and 25-minute stage stall.
9. Redfish Boot Media with preflight, ensure, and deployment boot watch; accepted with four
   Ubuntu 24.04 and four Rocky 8.10 RAM deploys on `tainan-ci`.
10. Generic OS provisioning states, spinner, row sweep, elapsed clock.
11. Boot ISO builder: iPXE in both api-server images, builder, per-ISO route, Boot Media ISO
    choice, dashboard tab, CLI; verified by a real build, El Torito and ESP checks, and QEMU
    OVMF and SeaBIOS boots to a stand-in rack.
12. Boot Media apply progress: recorded phases, `apply` field, progress UI, single preflight per
    Server.

## Non-goals

- Backend changes for list presentation; GPU telemetry, capacity scores, tag provenance.
- Multi-user management, API key scopes, and pushing keys to already-deployed Servers.
- A cloud-init key-injection fallback for providers without SSH key registration.
- Fixing corporate egress from inside swallow; replacing MAAS curtin or MAAS power control.
- Free-form iPXE script editing; compiling iPXE per ISO; keeping an installation-supplied ISO.
- An Ironic adapter; merging the Swallow deployment axis into the provisioning axis.
- Docker Swarm or fleet-wide Docker data; authenticating the Docker Engine API in this version.
- Multi-host MAAS, HTTPS enforcement, native Temporal packaging, Prometheus (L3), third-party OS
  images and S3 media in the L2 installation.

## Open Questions

- **Package repositories.** Provisioning-side mirror configuration (MAAS package repositories or a
  Site mirror) and apt settings for Ansible jobs are deferred; a deploy preflight returns only if
  swallow can read and probe each image's real repository configuration.
- **Aptio boot direction.** Whether to prefer BootOrder plus BootNext over the fixed-boot-order
  write on Aptio, since the BIOS re-sorted to "UEFI Hard Disk" first and every deploy relied on
  the boot check (about ten extra minutes).
- **Native installs and Boot ISOs.** The native package does not ship the iPXE assets, so its
  builder reports unavailable; how (or whether) native installs provide them is undecided.
- **Ensure versus preflight.** The deployment `ensure-boot-media` Task neither records progress
  nor is excluded from running alongside an operator preflight.
- **Unknown routes return 500.** The API's error handler maps unmatched routes (including the
  retired fixed ISO URL) to `500` instead of `404`.
- **Publication inputs.** A GHCR token with `write:packages`, package visibility, and review of
  the pinned upstream digests are needed before a real release.
- **Lab checks pending.** Building a Boot ISO for the `tainan-ci` Site, choosing it there, and
  running one deployment; rebuilding the dev images from the committed Dockerfiles once PyPI and
  `deb.debian.org` are reachable.
- **Flaky test.** `TestCheckDeploymentKeyLoginClassifiesSudo` fails intermittently.
- **Rocky 10.2** cloud-init never finished on the `tainan-ci` network; Rocky 8.10 needs its site
  DHCP address pinned with static mode.

## Future Work

- Commissioning-time capture of the Server Default User with Deployment Key import.
- Asynchronous Docker image pulls with progress.
- TLS termination in front of the Dashboard.
- A local package mirror sync job for restricted-egress Sites.
- Producing Boot ISOs for other provisioners than MAAS once a second provisioner exists.
- Removing experimental-feature gates as monitoring, OS image upload, and Deployment Templates
  are finished.
