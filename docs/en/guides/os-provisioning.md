# OS provisioning

[繁體中文](../../zh-TW/guides/os-provisioning.md) · [Documentation home](../README.md)

OS provisioning changes provider-owned machine state through a durable
Workflow. A deployment request records intent first, then the provisioner and
automation stages converge the Server toward the requested result.

## Prepare an image

Under **Provisioning → Images**:

1. Select the Site/provisioner catalog.
2. Use an existing provider image or upload a supported custom image.
3. Set the image's default user if it is a custom image (the account its
   cloud-init creates; synced Ubuntu, CentOS, and RHEL images have a built-in
   one). swallow logs in to deployed Servers as this user — see
   [SSH keys and image login users](ssh-keys.md).
4. Verify the image before deployment.
5. Review verification failures and target compatibility.

Uploading an image from the Dashboard is in development, and release builds
hide the Upload action; see [Features in development](dashboard.md#features-in-development).

Upload limits, content types, and provider requirements are defined by the
[active provisioning contract](../../../api-server/docs/development/api-contracts/api-server/provisioning.md).
Deleting an image acts on its owning provider or overlay according to that
contract.

## Deployment templates

A Deployment Template records reusable operator choices, not executable
automation. It can preselect an image and settings while the deployment request
still validates each current target. Templates never bypass current eligibility.

Templates in the Dashboard are in development; release builds hide them and
deploy with a custom configuration. The API and CLI are unaffected.

## Deploy an operating system

Choose the entry point that already knows the most about your intent:

- **Deploy OS** on a Server detail page or Server row opens a dialog with that
  Server fixed. A bulk action fixes the selected Servers; add or remove targets
  on the list before opening it.
- **Deploy** on an OS image opens a dialog with the image fixed and starts by
  choosing Servers.
- A Platform deployment uses the same **OS image**, **Installation**, and
  **OS networking** steps inside its outer wizard, then submits through the
  Platform workflow.

Contextual dialogs stay on their source page. Fixed-Server dialogs run live
deployment preflight and network inspection before showing the first configuration
step. A failure lists the affected Server and links to Networking; correct it and
choose **Check again**. Fixed-Server configuration is split into **OS image**,
**Installation**, and **Networking**, followed by **Review**. A fixed-image launch instead uses
**Targets**, **Installation**, **Networking**, and **Review**: because the source action already
fixes the catalog image, the dialog omits the OS-image selection step entirely and confirms that
image only in the dialog context and Review. The target selector shows only ready, unlocked Servers
from the image's provisioner as searchable responsive cards; ineligible Servers are omitted instead
of disabled. Contextual dialogs keep Site, provisioner, and target-count facts out of configuration
and present them together in Review. The fixed-Server dialog and Platform wizard use one
shared **OS image** surface: **Refresh** stays on the same row as the heading, with
space between them, followed directly by the explanation, OS families, and images. An optional
Configuration source appears only where templates are supported; it does not wrap or relabel the
picker. **Installation** and **Networking** follow
the same hierarchy: each description follows the title divider directly, with its controls below
and no intermediate section card. Choose existing templates from the Platform flow; a contextual deployment can still be saved
as a new template in Review.
After submission, Server pages keep following the affected rows, while every contextual success
notification offers **View workflow**.
Image selection first narrows the catalog by OS family, then searches and compares images
inside that family. Image cards show the name, architecture, size, a distinct provider ID,
sorted tags, and icon-only Disk/RAM availability. A check means the mode can deploy; an X means
it cannot. Custom-image failures and modes that have not been tested intentionally use the same
unavailable icon because that distinction does not change the selection. A spinner means a
verification was active when the picker opened. This activity is read once and is not polled.
Provider release is omitted because it is not a reliable operator-facing label. The picker shares
the page or dialog scroll surface instead of adding a nested scrollbar. The catalog loads when
the step opens and refreshes only when you choose **Refresh**; existing results, family, search
text, and the selected image remain visible while that request is in progress.
Deploy mode and addressing choices explain their effects in full-width cards; the memory-backed
choice is labelled **RAM deploy**. When an image supports exactly one deploy mode, that mode is
selected by default. Images that support both modes—or neither mode—default to RAM. Choosing a
different mode remains an explicit operator override. An unavailable mode shows a warning, but
does not block the next step or submission in this Dashboard release. Per-Server network
assignments use a comparison table on wide screens and labelled cards on mobile.
Each Site must have exactly one enabled provisioner Integration. Deployment is
blocked when it has none, more than one, a disabled provisioner, or when a
fixed Server/image belongs to another Integration. Use **Manage integrations**
from the message to correct the Site; no image catalog is read and no
deployment is submitted while the configuration is invalid.

Dashboard provisioning flows validate:

- selected Servers belong to the intended Site and are deployable;
- the image is available and verified for the target;
- the requested disk or RAM deployment target is supported;
- network settings and release options are valid;
- no lock or active conflicting Workflow blocks the Server.

Submission creates one durable Workflow for the selected targets. Follow it in
**Workflows**; do not infer completion from the request returning successfully.
While a Server deploys, its detail page shows the provisioner's current
installation stage (for example *Configuring OS*). If that stage stops advancing
for 25 minutes, the Workflow step asks for attention instead of waiting out the
two-hour limit; the provider deployment is left running for you to inspect,
retry, or release.

## Boot Media for networks without provisioner DHCP

Some Servers sit on a network whose DHCP is run by the site, not by the
provisioner, so they cannot PXE-boot into it. For those, the Server's BMC mounts
a swallow-built [Boot ISO](../../development/glossaries/terms/boot-iso.md) as
virtual media and boots it first. The ISO takes an address from the site's DHCP
and chains to that Server's provisioner.

- **Build one per provisioner rack:** open **Provisioning → Boot ISOs → Build
  ISO**. Choose a provisioner Integration in the current Site, give the ISO a
  1–63-character name unique for that provisioner (case-insensitive), and enter
  the MAAS rack address (hostname or IPv4 address, optionally with a port;
  default `5248`). The dialog suggests
  `<integration-name>-ipxe`, pre-fills the Integration endpoint's host, and
  previews `http://<rack>:<port>/ipxe.cfg`. On the single-VM installation that host
  is the installation address (`install.sh --address`), which is the rack. Confirm
  the rack address: swallow neither resolves nor contacts it.
- **Fixed boot flow:** swallow renders a verified script rather than accepting
  script edits. It gets DHCP from the site network, sets the rack as
  `next-server`, and chains directly to its `ipxe.cfg` without using the DHCP
  boot filename. DHCP retries after failure, Ctrl-B opens the iPXE shell, and
  UEFI returns to the next firmware boot device if the chain returns. The
  synchronous build normally takes seconds.
- **Boot compatibility:** each ISO contains iPXE v2.0.0 and boots BIOS and UEFI
  x86_64. iPXE is unsigned, so turn off Secure Boot on Servers that use it.
- **Manage ISOs:** the Boot ISOs table shows the provisioner and Site, chain
  URL, size, usage count, and build details. **View script** also shows the rack,
  iPXE version, ISO URL, and SHA-256; **Download** uses the same URL BMCs mount.
  **Delete** is disabled or refused while any enabled Server uses the ISO.
  After deletion, its file is removed and its URL stops working.
- **Choose per Server:** on **Server → Summary → Management controller → Boot
  media**, **Enable Boot Media** requires a Boot ISO built for that Server's own
  provisioner. If none exists, follow the link to **Boot ISOs** and build one.
  Enabling runs the Redfish preflight and saves the setting only after the BMC
  mounts the ISO and accepts the boot override. The **Boot ISO** row shows its
  name and the URL mounted by the BMC.
- **Follow the preflight:** **Enable Boot Media**, **Change ISO**, and
  **Re-apply** usually take about five minutes, mostly a deliberate three-minute
  wait after a fresh mount while the BMC settles it. The dialog shows a progress
  bar, elapsed time, time left during that wait, and five steps: check the BMC
  (including ejecting the previous ISO when switching), mount the ISO as a
  virtual CD, let the mount settle, direct the next boots to the virtual CD, and
  read both settings back. Each step is done, in progress, or not started; an
  unnecessary step, such as mounting an ISO the BMC already holds, is shown as
  done.
- **Continue in background:** closing the dialog this way does not stop the
  preflight. The Boot media block shows **Applying**, the same progress, and no
  available actions until it ends. Its success or failure arrives as a
  notification while the page stays open; a reload or another browser tab can
  recover the progress from the API.
- **Operate and deploy:** an enabled Server offers **Change ISO** (the preflight
  ejects the current ISO first), **Re-apply**, **Disable**, **Re-detect
  Redfish**, and **Check BMC**. Disabling keeps the chosen Boot ISO for next
  time. Every OS deployment re-applies that Server's chosen ISO first and
  freezes the choice for the deployment; a BMC failure stops at the Boot Media
  Task until it is fixed and retried.
- **Enroll and inspect:** a machine on such a network is not a Server yet, so its
  first, enlisting boot needs the ISO mounted by hand: mount the ISO URL as
  virtual media in its BMC and boot it once. Enable Boot Media as soon as the
  Server appears. Hardware inspection applies Boot Media before every run and
  reads the setting when it runs, so a retry after enabling it boots the ISO (see
  [Servers and infrastructure](servers-and-infrastructure.md#add-servers)).

For API and CLI users, `GET /api/v1/servers/{id}/boot-media` returns `apply` as
`null` while idle. During a preflight, `apply` contains `isoId`, the current
`probing`, `ejecting`, `mounting`, `settling`, `directing`, or `verifying`
`phase`, plus `startedAt` and `phaseStartedAt`; `phaseEndsAt` is non-null only
for the settle wait. `swallow servers boot-media get <server>` shows the same
progress while `enable` waits. Only one preflight can run per Server, so another
enable or a disable during it is refused with HTTP 409.

After upgrading from the former installation-supplied ISO, an enabled Server
may warn **Choose a Boot ISO**. Its OS deployment stops with
`boot_media_not_configured` until **Change ISO** or **Re-apply** selects one.
If the chosen ISO was deleted or is no longer served, the panel warns **The
Boot ISO cannot be mounted** and deployments stop the same way.
The old hand-made `swallow-ipxe.iso` and its fixed URL are no longer served.

## Hardware inspection and tests

From a Server's **Take action → Hardware checks**, **Inspect hardware** asks the
provisioner to inventory the machine's hardware again. MAAS calls this action
*Commission*. The Server reads **Inspecting** while it runs; **Test** similarly
reads **Testing** while the provider runs hardware tests. The Summary card's
**Inspection** field is the provider's last inspection result, such as *Passed*.

Inspection is an `inspect-hardware` Workflow with one Job, `ensure-inspected`:
wait for the provider's enrollment to finish, apply Boot Media, then up to three
inspection attempts. It starts automatically for newly enrolled Servers. When it
asks for attention, enable Boot Media if the Server's network is not served by
the provisioner's DHCP, then retry the Task or choose **Inspect hardware** again.
See [Inspect hardware](servers-and-infrastructure.md#inspect-hardware) for the
details and for turning automatic inspection off.

## Release and recovery

Release returns a provider machine to the available pool without deleting its
Server. It is available from deployed, allocated, failed, broken, or rescue
state and may include explicit erase options. After the action is accepted, the
Servers list shows **Releasing** while the provider works and then **Ready**
when the machine is back in the pool; the list follows this transition without
requiring you to leave the page.

Recovery handles allocated, failed, broken, or rescue states according to the
configured provider recovery policy. Power-off warnings must be reviewed when
memory-backed state or active work could be lost.

## Network inspection

Network configuration reads and writes provider-owned interfaces and links.
Automatic addressing requests provider auto-assignment; it does not invent a
separate swallow address allocator.

## CLI

```bash
swallow provisioning images list --integration int1
swallow provisioning templates list --site-id site1
swallow provisioning boot-isos list --site-id site1
swallow provisioning boot-isos create --integration int1 --name rack-ipxe --rack 10.0.0.2
swallow provisioning boot-isos get iso1
swallow provisioning boot-isos delete iso1
swallow provisioning deploy --file deploy.yaml
swallow provisioning release --server server1 --erase
swallow servers inspect server1
swallow servers redfish-probe server1
swallow servers boot-media enable server1 --iso iso1
swallow servers boot-media disable server1
swallow servers boot-media get server1 --live
```

Use JSON/YAML request files for structured payloads so fields remain aligned
with the provider-owned contract.
