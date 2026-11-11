# OS provisioning

[繁體中文](../../zh-TW/guides/os-provisioning.md) · [Documentation home](../README.md)

OS provisioning changes provider-owned machine state through a durable
Workflow. A deployment request records intent first, then the provisioner and
automation stages converge the Server toward the requested result.

## Prepare an image

Under **OS Images** (above **Infrastructure** in the sidebar):

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
Deleting an image removes it from its owning provider and also removes the
metadata swallow keeps for it.

## Deployment templates

A Deployment Template records reusable user choices, not executable
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
- A Platform deployment reuses the same image, deploy-mode, cloud-init, addressing,
  and network-assignment controls only when selected Servers still need an OS.
  Kubernetes presents this as **Operating system** before its runtime networking;
  Slurm presents it as **Server preparation** without Kubernetes network fields.

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
Provider release is omitted because it is not a reliable user-facing label. The picker shares
the page or dialog scroll surface instead of adding a nested scrollbar. The catalog loads when
the step opens and refreshes only when you choose **Refresh**; existing results, family, search
text, and the selected image remain visible while that request is in progress.
Deploy mode and addressing choices explain their effects in full-width cards; the memory-backed
choice is labelled **RAM deploy**. When an image supports exactly one deploy mode, that mode is
selected by default. Images that support both modes—or neither mode—default to RAM. Choosing a
different mode remains an explicit user override. An unavailable mode shows a warning, but
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
**Activity → Workflows**; do not infer completion from the request returning successfully.
While a Server deploys, its detail page shows the provisioner's current
installation stage (for example *Configuring OS*). If that stage stops advancing
for 25 minutes, the Workflow step asks for attention instead of waiting out the
two-hour limit; the provider deployment is left running for you to inspect,
retry, or release.

## Time synchronization

The swallow host serves NTP on UDP 123, and every Server swallow provisions uses it.
Each OS deployment gets one **Configure time synchronization** step per Server after
SSH comes up; so does each Server a Platform deployment provisions. The step points
the time client the OS already has at the swallow host: `systemd-timesyncd` if present,
otherwise `chrony`, otherwise `ntpd`. It installs chrony only when the OS has none, and
stops any other client. Its output names the client it used. It fails when the client
gets no reply within two minutes. If it fails, check that UDP 123 reaches the swallow
host, then retry the step. The step runs with Ansible, so Site automation
must be enabled; otherwise the deployment is refused.

The swallow host follows `ntp.ubuntu.com` by default. Your administrator can change
that upstream on the host. Servers keep following the swallow host either way.

A Platform deployment does not change time settings. Before it installs the platform,
it checks every node's clock; see
[Clock check](platforms.md#clock-check). A Server deployed before this step
existed keeps its image's time setup until it is deployed again.

## Node Exporter

Each OS deployment also installs the [Node Exporter](managed-software.md#node-exporter)
on every Server it deploys, after time synchronization, so the Server's usage charts
work once it is up ([Monitoring](monitoring.md)); so does each Server a Platform
deployment provisions, unless Kubernetes owns that Platform's exporters. The Server's
Software list shows it as installed Managed Software. A failed install does not affect
the OS: the Server is deployed, but the Workflow needs attention until you retry the
**Install Node Exporter** step, for example after enabling EPEL on a RHEL-family image, or
cancel it, after which swallow retries the install on its own. From then on swallow keeps
the Node Exporter installed ([Monitoring](monitoring.md#the-node-exporter-is-kept-installed)).
Releasing or recovering a Server clears its Software Assignments, because the software
went with the OS.

## Boot Media for networks without provisioner DHCP

Some Servers sit on a network whose DHCP is run by the site, not by the
provisioner, so they cannot PXE-boot into it. For those, the Server's BMC mounts
a swallow-built [Boot ISO](../../development/glossaries/terms/boot-iso.md) as
virtual media and boots it first. The ISO takes an address from the site's DHCP
(or a static address typed at the Server's console) and chains to that Server's
provisioner.

A libvirt virtual machine has no BMC. When its `virsh` power configuration names
a hypervisor that is a swallow Server, its Boot Media goes through that
hypervisor instead: swallow uploads the Boot ISO to the hypervisor's `default`
storage pool (creating the pool when the hypervisor has none), puts it on the
domain's CD-ROM (adding one when needed), and makes the CD-ROM boot first in the
domain's definition. It needs no Boot Media base URL, takes seconds, and lasts
until disabled. The Server Summary shows it in the **Power control** card, with
**Re-detect hypervisor** and **Check hypervisor**; in the Dashboard this is an
experimental feature of development builds, while the CLI and API offer it in
every release. [Enrolling virtual machines](servers-and-infrastructure.md#libvirt-virtual-machines)
with a Boot ISO sets this up for you.

- **Build only when this path is needed:** open **Infrastructure → Boot ISOs →
  Build Boot ISO**. With one provisioner Integration, the dialog shows it as a
  read-only summary; with several, choose the Integration first. The **MAAS rack
  address** is suggested from the Integration endpoint by keeping only its
  hostname or IPv4 address and dropping the region API scheme, port, and path.
  You can replace the suggestion with the address reachable from the Site DHCP
  network. The dialog generates `<integration-name>-ipxe` under **Advanced
  settings** and previews `http://<rack>:<port>/ipxe.cfg` (default port `5248`),
  the BIOS chain; UEFI loads `grub/grub.cfg-<mac>` from the same rack and port.
  Confirm the rack address: swallow neither resolves nor contacts it.
- **Fixed boot flow:** swallow renders a fixed script rather than accepting
  script edits. It gets DHCP from the site network, sets the rack as
  `next-server`, and never uses the DHCP boot filename. The firmware decides
  which boot loader runs: BIOS runs iPXE, which chains the rack's `ipxe.cfg`;
  UEFI runs a GRUB built into the ISO, which takes a DHCP lease and loads the
  rack's `grub/grub.cfg-<mac>`, MAAS's own UEFI path, followed by MAAS's default
  configuration, which enlists a machine MAAS does not know yet; it returns to
  the next firmware boot device if MAAS does not boot it. That matters because MAAS gives
  a Machine an EFI system partition only when it saw a UEFI boot; without one, a
  UEFI disk deployment fails while installing GRUB. The synchronous build
  normally takes seconds.
- **Boot mode menu:** both boot loaders first show a text menu on the Server's
  console (BMC KVM or VM console): **DHCP (default)** or **Static IPv4
  address**; BIOS also offers the iPXE shell. With no key pressed, DHCP starts
  after 10 seconds, so unattended inspections and deployments wait only that
  long. When DHCP fails, BIOS shows why for a few seconds and returns to the
  menu, whose countdown retries. UEFI first lists the network cards GRUB can use
  (up to 64), asks every card for a DHCP lease at once, and then tries the cards
  that answered one at a time: it takes a DHCP lease on that card alone and
  checks that the rack is reachable through it, so a broken card or a card on
  another network shows its own result and the next card is still tried. When
  no card reaches the rack, every card is tried one at a time after 10 seconds,
  and Esc returns to the menu. A card missing from the list is one the
  firmware does not give to boot loaders (for example its UEFI network stack or
  UEFI PXE is off) or whose UEFI driver failed to start; enable it in the
  firmware settings.
- **Static IPv4 address:** choose the network card from the list (each shows its
  MAC), then enter the IPv4 address, subnet mask (for example `255.255.255.0`),
  and gateway (leave it empty when the rack is on the same subnet). On BIOS
  this is a form: move between fields with the arrow keys, edit with
  Backspace, and press Enter on the last field or Ctrl-X to continue. On UEFI
  each value is a row: select it, type the value, and press Enter; Backspace
  does not work there, so select the row again to fix a typo, then choose
  **Continue**. When the rack address is a hostname, UEFI also asks for a DNS
  server; BIOS needs the rack as an IPv4 address. A value that is not an
  address, or a rack that cannot be reached, brings the settings back with the
  reason. The values are typed at every boot and never stored. The static
  address only reaches the rack: MAAS still takes a DHCP lease when it enlists,
  commissions, deploys, or rescues the machine, so those still need DHCP on
  that network. Without any DHCP, a static address only lets a deployed Server
  boot its disk through the ISO.
- **Rebuild after a template change:** a card marked **UEFI needs rebuild** was
  built before the UEFI hand-off. Choose **Rebuild** (or **More → Rebuild Boot
  ISO**): swallow renders the current script for the same rack and replaces the
  file at the same URL, so no Boot Media setting changes. Then inspect each
  UEFI Server that uses it again before its next deployment, so MAAS records the
  UEFI boot and recreates its disk layout with an EFI system partition. A Boot
  ISO built before the boot mode menu only takes DHCP; rebuild it to get the
  menu and the static mode.
- **Boot compatibility:** each ISO contains iPXE v2.0.0 for BIOS and a GRUB for
  UEFI x86_64. Neither is signed, so turn off Secure Boot on Servers that use it.
- **Manage the small setup:** the page uses one card per configured ISO rather
  than an inventory table. Each card keeps **Served** or **Not served** separate
  from **Used by N Servers** or **Not in use**, links to its provisioner
  Integration, and shows the Site in all-sites scope. **Download** stays visible.
  **Technical details** discloses the ID, rack, BIOS and UEFI chain URLs, ISO
  URL, SHA-256, iPXE version, size, and build and rebuild facts. **More** opens
  the rendered boot scripts (iPXE for BIOS, GRUB for UEFI), Rebuild, or Delete. Delete is disabled or refused while any enabled Server uses
  the ISO; after deletion, its file is removed and its URL stops working.
- **Choose per Server:** on **Server → Summary → Management controller → Boot
  media**, **Enable Boot Media** requires a Boot ISO built for that Server's own
  provisioner. If none exists, follow the link to the **Boot ISOs** tab and build one.
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
  time: the Boot media block shows the Boot ISO as N/A, and enabling preselects
  the kept one. Every OS deployment re-applies that Server's chosen ISO first and
  freezes the choice for the deployment; a BMC failure stops at the Boot Media
  Task until it is fixed and retried. The failed Task's diagnostic says whether
  the BMC rejected the account, refused the request, or did not answer, and what
  to change before retrying.
- **Read the Redfish line:** next to the BMC vendor and firmware, the block shows
  how swallow recognized the BMC: **profile ami** or **profile supermicro** for a
  BMC family swallow has been tried on, or **unverified BMC (standard Redfish
  only)** for any other BMC, which gets only standard Redfish behavior. An
  unverified BMC may still work; the preflight tells. A BMC that refuses a
  request Boot Media needs (for example a feature it requires a license for)
  shows as without Boot Media support, with the BMC's own message; a rejected
  BMC account shows as unreachable.
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

For a failed OS deployment, retry the failed `provision-os` Task from its
Workflow. An explicit retry first observes the Server. If the requested image
is already installed and reachable, it completes without writing again. If the
provider reports `failed`, `allocated`, `broken`, or `rescue`, swallow returns
the Server to Ready, keeps static IP bindings, and redeploys the same frozen
image, network, and cloud-init intent. Activity retries caused by worker
turnover never perform this destructive recovery.

A disk deployment is refused when
`provisioning.inspectionRequired` says an earlier UEFI inspection left the boot
disk without the required EFI partition. Retry stops before recovery with
`inspection_required`; run **Inspect hardware**, then start a new deployment.
A RAM deployment is not blocked by the disk layout.

If unfinished Workflows still hold a Server, the Release dialog lists them.
Check **Cancel running operations before releasing** to request their
cancellation and queue Release. On a failed Server,
**Cancel blocking Operations and Recover** performs the same hand-off for
Recover. The new action waits for their resource leases instead of requiring
you to resubmit later. Review the canceled Workflow first when a completed
external effect may still matter. See [Workflows](workflows.md#cancellation).

## Network inspection

Network configuration reads and writes provider-owned interfaces and links. A
deployment chooses one of three addressing modes:

- **Automatic** requests provider auto-assignment: MAAS allocates and records a
  fixed address. It does not invent a separate swallow address allocator.
  Automatic is preselected for every deployment unless the boot NIC already has
  an explicit Static address.
- **DHCP** leaves the address, gateway, and DNS to the network's own DHCP
  server. Choose it on networks whose DHCP the provisioner does not serve, such
  as Servers that boot through a Boot ISO: there, Automatic may hand out an
  address the site DHCP server does not know about and may also lease to
  another machine. When a chosen subnet has no provisioner DHCP and Automatic
  is selected, the Deploy OS dialog and the Platform deploy wizard show a
  warning that says so; switching to DHCP is your choice.
- **Static** uses an address you choose for every Server.

Automatic and Static deployments take their DNS servers from the subnet's
**Subnet DNS**, or MAAS's own DNS when it has none. Set it per subnet on the
**Subnet DNS** tab of the MAAS Integration's settings page (Infrastructure →
Integrations → the Integration): each subnet's **Apply DNS** writes it to MAAS
directly, it is not saved with the Integration, and it affects only deployments
MAAS starts afterwards. The same list shows whether MAAS runs each subnet's DHCP.

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
