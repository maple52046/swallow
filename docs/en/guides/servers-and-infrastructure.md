# Servers and infrastructure

[繁體中文](../../zh-TW/guides/servers-and-infrastructure.md) · [Documentation home](../README.md)

Servers are reconciled projections of provisioner machines. Users inspect,
organize, protect, and act on them; they do not create Server records.

## Add servers

A machine becomes a Server once it is in the provisioner's inventory. **Servers →
Add servers** (also on an empty Server list) asks one question at a time and ends
on the one thing to do, then waits and lists each Server as it appears. A Site
without a provisioner shows **Connect a provisioner** instead.

| Answers | What you do | Result |
| --- | --- | --- |
| Boot from PXE, provisioner DHCP | Boot the server from PXE (reboot or power it on yourself). | **New**, then inspected automatically to **Ready**. |
| Boot from PXE, external DHCP | PXE goes through the iPXE Boot ISO: mount the shown Boot ISO URL from the BMC console (Virtual Media, boot once from the virtual CD) or run the generated Redfish commands; when the Server appears, enable its [Boot Media](os-provisioning.md#boot-media-for-networks-without-provisioner-dhcp). | Same as above. |
| Keep its OS | Run the shown command on the server. | **Deployed**, OS untouched, no reboot. |

You never press *Commission* in MAAS: swallow waits until enlistment powers the
machine off, then inspects it (see [Inspect hardware](#inspect-hardware)).

A libvirt virtual machine is enrolled by name instead, from its hypervisor: see
[Libvirt virtual machines](#libvirt-virtual-machines). One that you boot into
enlistment yourself (from the network or an iPXE boot medium) gets no power
driver from enlistment, so MAAS cannot report its power-off; its inspection
stops with **Set power configuration**. Give it a `virsh`
[Power configuration](#power-configuration) and retry.

The Redfish tab takes the BMC address and user, then prints `curl` commands that
insert the virtual media, set a one-time boot from it, and power the system on;
`curl` asks for the BMC password. The commands assume system `1` and virtual
media `CD1` and show how to list the real IDs.

The keep-OS command needs nothing installed beforehand:

```bash
curl -fsSL 'https://swallow.example.com/downloads/swallow-enroll.sh' \
  | sudo sh -s -- --provisioner=maas --endpoint '<MAAS URL>' --token '<MAAS API key>'
```

The script downloads the swallow CLI from your installation and runs
`swallow servers enroll`, which wraps MAAS `maas-run-scripts register-machine` and
`report-results`. The host needs `curl`, `python3`, and HTTP access to
swallow and MAAS (Ubuntu, x86_64). It is never followed by automatic inspection,
which would reboot the host. swallow installs the Node Exporter on it once it is deployed,
unlocked, and its Site's automation can log in, so its usage charts appear without an extra
step ([Monitoring](monitoring.md#the-node-exporter-is-kept-installed)). The
command contains the provisioner's API key, which
MAAS requires; run it only on trusted hosts and rotate the key in MAAS otherwise.

The Server's address in swallow is the one MAAS records on the host's network
interfaces, and MAAS can accept the first hardware report without recording it.
The command therefore checks that MAAS recorded the address the host reaches MAAS
from and reports once more if it did not; if the address is still missing it
prints a warning naming the MAAS machine to look at. A host enrolled before
without an address gets one by running the same command again with
`--system-id <Provider machine ID>`: it reports the hardware again instead of
registering.

MAAS registers a kept-OS host without a power driver, so its power button reads
**NO DRIVER** and swallow cannot switch it. Either set its
[Power configuration](#power-configuration) afterwards (the power button on the
Server list opens it), or check **Also set up its BMC over IPMI** in **Add
servers**, which appends `--configure-bmc` to the command. With it, the host
creates or reuses an IPMI user on its own BMC through its local IPMI interface,
records the BMC as the server's `ipmi` power driver in MAAS, and checks that MAAS
reads the power; when MAAS cannot, the previous driver is restored. It needs
`ipmitool` and `/dev/ipmi0` on the host, installs nothing, never switches power,
and leaves the BMC's network alone; a BMC without an IP address is refused. The
password goes from the host to MAAS only. For a host enrolled before, run the
command with `--configure-bmc --system-id <Provider machine ID>`.

## Libvirt virtual machines

Virtual machines on a libvirt hypervisor are enrolled by their domain names; you
never type a MAC address. The hypervisor must itself be a **Deployed** Server of
the Site. swallow logs in to it over SSH with the Deployment Key, as its Server
Default User or an account you name, and that account must be allowed to run
`virsh -c qemu:///system` (on Ubuntu, the `libvirt` group).

```bash
swallow servers virtual-machines list hv1                       # domains, state, MACs, existing Server
swallow servers virtual-machines enroll hv1 lab-vm-1 lab-vm-2 \
  --integration int1 [--boot-iso iso1] [--account ubuntu] [--power-off-running]
```

In development builds the Dashboard offers the same under **Add servers → It is
a libvirt virtual machine** (an experimental feature): pick the hypervisor, list
its virtual machines, tick the ones to enroll, and optionally choose a Boot ISO.

Enrolling starts one `enroll-virtual-machines` Workflow with a Task per virtual
machine, run in parallel. Each Task checks the domain is shut off (with
`--power-off-running`, swallow stops a running one), lets the provisioner's SSH
key log in to the hypervisor account, and — with a Boot ISO, for a network the
provisioner's DHCP does not serve — puts the ISO on the domain's CD-ROM and boots
it first. It then registers the domain with MAAS with its MAC addresses and the
`virsh` power driver, **without** commissioning it, and waits for the Server to
appear. Automatic inspection then starts the virtual machine through MAAS and
takes it to **Ready**. A domain that already is a Server is reused, not
duplicated.

| Task stopped with | Fix, then retry the Task |
| --- | --- |
| `domain_running` | Shut the virtual machine down, or enroll it again with `--power-off-running`. |
| `provisioner_cannot_reach_hypervisor` | Let MAAS's rack SSH key log in to the hypervisor account (the installation records it on the provisioner Integration as `virshSshPublicKey`, and the Task authorizes it). |
| `hypervisor_unreachable` | Check the hypervisor answers on SSH, accepts the Deployment Key, and lets the account use libvirt. |
| `machine_registration_refused` | Read MAAS's reason, for example a hostname already in use. |
| `server_not_projected` | Check the provisioner Integration is enabled and syncing. |

A domain that does not exist or has an architecture other than `x86_64` or
`aarch64` fails without a retry.

## Inventory and details

The Servers list supports Site scope, pagination, search, status and capability
filters, shareable URL-owned views, live row updates, and bulk selection. A Server detail
workspace separates summary, activity, monitoring, network, storage, and PCI
observations. A Server where swallow installed Docker CE also gets a
**Containers** tab for its Docker images, containers, volumes, and networks (see
[Managed Software](managed-software.md#docker-engine-api-and-the-containers-tab)).

Always use the opaque Server ID in links and automation. Duplicate hostnames and
addresses are valid across Sites.

### Filter views and display preferences

The chips above the inventory are built-in operational views: **All**,
**Deployable**, **Active deployments**, and **Needs attention**. **Filters**
adds provisioning, health, membership, GPU, hardware, placement, tag, lock,
provisioner sync, and absent-Server facets. Choosing an explicit provisioning
state leaves the built-in view so the two do not silently conflict.

A Server whose provisioner does not yet match what swallow wants shows its
**Provisioner sync** state under its name, for example **Drift detected** or
**Needs attention**; focus or hover it for the explanation. In-sync Servers show
nothing. The **Provisioner sync** filter narrows the list to Servers that are out
of sync, need attention, drifted, are still being applied, or are held.

Search, view, filters, grouping, sort, direction, Site scope, and page are
encoded in the URL. Copy or bookmark that URL to reapply the same view; there
is no separate named saved-view record. **Display** density and page size are
browser-local preferences, so they do not travel with a shared URL. Selection
is cleared when the filter set changes and is never stored in the link.

### GPU inventory

The Server Summary's **Capacity** card separates **Compute GPUs** from
**Display GPUs**. Compute GPUs are workload accelerators and contribute to
fleet capacity; this includes recognized physical accelerators and passed-through
virtual functions. Display GPUs are local-console, BMC, or virtual-machine
console controllers such as ASPEED, Matrox G200, and virtio-gpu. They remain
visible as hardware but do not inflate accelerator capacity or make a Server
GPU-eligible.

In the inventory, the **Hardware** column shows the CPU, RAM, and compute GPUs,
each as an icon with a short fact: the brand and core count, the memory, and the
GPU brand and count. Hover or focus an entry for the provider's full CPU model or
GPU models. The **GPU** filter and the GPU vendor and model facets count compute
GPUs only, so a Server with only a display controller is not GPU-equipped.

The classification preserves the provisioner's complete observation while
preventing a console controller from being counted as a workload device. An
unknown provider-reported GPU defaults to compute so a real accelerator is not
silently discarded. See the canonical
[GPU Inventory definition](../../development/glossaries/terms/gpu-inventory.md).

## Read the Deployment column

The **Deployment** column shows the Server's OS deployment state. The same
presentation appears on mobile Server cards and in the Server detail header. It
combines the latest swallow deployment result with the provider-neutral
[`provisioning.state`](../../development/glossaries/terms/os-provisioning-state.md)
in this order:

1. Running provider work that is not an OS installation: **Releasing**,
   **Inspecting**, or **Testing**.
2. A swallow deployment that is running or did not succeed: **Deploying**,
   **Verifying**, **Failed**, **Attention**, or **Canceled**.
3. When an OS is installed, its image name as plain text.
4. Otherwise, the OS provisioning state by its own name.

The Server detail header names the outcome instead of the image: **Deployed**
when swallow deployed and verified the installed OS, or **Unknown** when the OS
was installed without a swallow deployment result. The Summary card shows the
installed image.

| Display | User meaning |
| --- | --- |
| Inspecting / Testing | The provider is inventorying hardware or running hardware tests. |
| Releasing | The provider is returning the Server to its available pool, including any requested disk erasure. |
| Deploying / Verifying | The provider is installing an OS, or swallow is verifying the installation. |
| Ready | The Server is in the provider's available pool and can accept an OS deployment. This replaces the former “Not deployed” label. |
| Allocated / New / Retired | The Server is reserved but not deployed, not yet hardware-inspected, or withdrawn from service. |
| Failed / Broken / Rescue | The last lifecycle action failed, the provider marked the machine unusable, or it is in a diagnostic environment. |
| Unknown | There is no current provider observation, or the adapter does not recognize the provider state. It does not mean Ready. |

An in-progress label has a small spinner after it. The text remains the state;
the spinner is only a visual cue. The whole row or mobile card also has a light
blue tint with a soft band sweeping across it. Hover the state for details such
as the provider's failure reason.

When the start is known, a timer icon and ticking running time appear under the
state in list rows and mobile cards, and beside it in the Server detail header.
For a swallow OS deployment, the time starts when the deployment starts and
continues without resetting from **Deploying** into **Verifying**. For provider
work—**Releasing**, **Inspecting**, **Testing**, or **Deploying** not started by
swallow—it starts when swallow first observed that state. Hover the running time
to see which start it uses.

A provider-state start is swallow's observation, not the provider's exact
transition time, so it is only as precise as inventory reconcile or a targeted
Server refresh. It is close to real time after a user action such as
Release because the list follows that Server. If no start is known, no running
time is shown.

After Release is accepted, stay on the list: the row changes to **Releasing**
while the provider works and then to **Ready** when the Server returns to the
pool. The success notification of a Release or Recover offers **View workflow**
for the Workflow that carries it out.

A Server that a running Workflow still holds cannot be released. The Release
dialog lists that work; check **Cancel running operations before releasing** to
have swallow cancel it. The Release is accepted at once and starts by itself
when the canceled work has ended, which can take a few minutes. A canceled OS
deployment may already return the Server to **Ready** by then. **Cancel blocking
Operations and Recover** on a failed Server works the same way.

On the Server detail page, a **Releasing** Server shows a banner that links to
the Workflow following the release (a Release, a Recover, or a Platform
uninstall that releases its Servers). If that Workflow needs attention or has
ended while the provider is still releasing, the banner says so and also links
to **Provider events**. If no Workflow covers the release—it was started in the
provisioner—the banner links to **Provider events** only. swallow cannot change
the state of a Server the provider is releasing: Release, Recover, Lock, and
Deploy OS stay unavailable until the provider returns it to **Ready** or reports
a failure.

### Next-step icon and Actions menu

When the state calls for a next step, a small icon follows it in the Deployment
column. The icon has no text: hover or focus it to read what it does and, for
Activity links, which section of the Server's **Activity** tab it opens. The
first matching row applies.

| Icon | When | Opens |
| --- | --- | --- |
| Warning — **Review activity** | The Server is absent from provider inventory. | **Activity → Provider events** |
| Rocket — **Deploy OS** | The Server is unlocked and **Ready**. | An in-page Deploy OS dialog with the Server fixed; it checks live readiness and networking before the OS image, Installation, and Networking steps |
| Arrow — **View workflow** | A swallow deployment is running. | That deployment's Workflow |
| Eye — **View activity** | Provider-only work is running. | **Activity → Related Operations** for Releasing and Inspecting, **Provider events** otherwise |
| Arrow — **View workflow** | The last swallow deployment failed or needs attention. | That deployment's Workflow |
| Warning — **Review activity** | The provider reports **Failed**, **Broken**, or **Rescue**. | **Activity → Provider events** |

An idle Server has no icon; open it from its name. Provider actions—power,
hardware checks, lock, recovery, release, and delete—are in the row's
**Actions** menu, which is the same button on mobile cards.

Bulk **Deploy OS** fixes the current selection in the same dialog. A successful
single or bulk deployment leaves the list/detail page open, follows each target's
projection, clears a bulk selection, and offers **View workflow** in the success
notification.

## Quick views and fleet signals

- **All** shows the current inventory.
- **Deployable** shows present, unlocked Servers in `ready`.
- **Active deployments** includes provider work in `inspecting`, `deploying`,
  `releasing`, or `testing`, plus swallow deployments that are deploying or
  verifying.
- **Needs attention** includes provider `failed`, `broken`, and `rescue`
  states, plus swallow deployments that failed or require attention.

The fleet overview's **OS deployment → Active / Attention** counts, default
operational sort, and row highlights use the same rules. Running work sorts
first, followed by conditions that need attention.

## Inspect hardware

Hardware inspection runs as an `inspect-hardware` Workflow. It starts by itself
for a Server that network-boot enrollment just brought in, and on request with
**Take action → Hardware checks → Inspect hardware**. MAAS calls this action
*Commission*. The Deployment state is **Inspecting** while it runs. The Server
detail Summary card's **Inspection** field continues to show the provider's last
result label, such as *Passed*.

The Workflow first waits until enrollment ends (for MAAS, the machine powers
off), applies the Server's Boot Media if it is enabled, and then makes up to
three inspection attempts. An attempt that records no provider progress for 15
minutes is aborted, which returns a never-inspected MAAS machine to **New**. When
the attempts run out, the Workflow asks for attention: the Server list and the
Server detail page show **Hardware inspection needs attention** with a link to
the Workflow, which holds the reason. swallow does not mark the Server failed.

What to do depends on why it stopped:

| Reason | Fix, then retry |
| --- | --- |
| Enrollment ended but the Server has no power driver MAAS can read (typical for a VM) | Set its [Power configuration](#power-configuration); check **Power state**; power it off if it is on. |
| Enrollment did not end within 20 minutes | Check the host finished enlisting and that MAAS can read its power. |
| The Server never network-booted into an inspection | Enable its Boot Media if its network is not served by the provisioner's DHCP; for a VM, put its NIC or iPXE boot medium first in the hypervisor's boot order. |
| The provider reported the inspection failed | Read the inspection results in MAAS. |

Retry the Workflow's Task, or choose **Inspect hardware** again; either re-runs
the whole inspection. A retry waits for enrollment again, so it never
commissions a machine that is still enlisting; once MAAS reads the machine as
powered off, the wait passes within about 30 seconds. **Inspect hardware** on a
Server without a waiting Workflow starts one that skips the wait, because you
assert the Server may boot now.

Automatic inspection applies to Servers swallow first saw within the last 24
hours that were never inspected by swallow. Turn it off per provisioner with
**Inspect newly enrolled Servers automatically** on the Connection tab of the
Integration's settings page;
Inspect hardware stays available. Inspect is refused while a Server is deployed,
allocated, in rescue, or busy with other provider work or another Workflow.

## Power configuration

A Server's power configuration is the power driver MAAS uses to switch and read
its power, and that driver's settings. MAAS owns it; swallow reads it live and
saves your changes to MAAS. It is on the Server detail **Summary**: in the
**Management controller** card for a Server with a BMC, and in the **Power
control** card for a Server without one. Choose **Edit power configuration** (or
**Set power configuration**).

The power button on the Server list uses it too. A Server without a power driver
shows **NO DRIVER** (one switched by hand, **MANUAL**) instead of its power state,
and its button opens straight to the same form; a successful save closes it and the
button updates. A Server whose driver MAAS cannot read (power state **Unknown** or
**Error**) gets a warning above the power actions with **Edit power configuration**.

| Driver | For | Settings |
| --- | --- | --- |
| IPMI, Redfish | A physical Server's BMC | BMC address, account, password |
| virsh | A libvirt virtual machine | Hypervisor URI `qemu+ssh://user@host/system`, domain name or UUID, optional password |

A BMC driver gives a Server Redfish Boot Media, probed from the BMC whichever of
the two drivers MAAS uses. A `virsh` driver whose URI names a swallow Server
gives libvirt Boot Media through that hypervisor (see
[Boot Media](os-provisioning.md#boot-media-for-networks-without-provisioner-dhcp)).
The password is write-only: leave it empty to keep the stored one (changing the
driver removes it unless you enter a new one). The eye button in the password
field shows what you typed, so you can check it before saving.

For a `virsh` driver, MAAS — not swallow — connects to the hypervisor, so its
rack controller needs SSH access to the hypervisor account before the driver
works. The production installation gives the co-located MAAS rack an SSH key
that accepts a new hypervisor's host key on first connection, and
[enrolling virtual machines](#libvirt-virtual-machines) authorizes that key on
the hypervisor. Otherwise, with the MAAS snap, put the key, `known_hosts`, and
any SSH options in `/var/snap/maas/current/root/.ssh`, and check from inside the
snap: `sudo snap run --shell maas -c 'virsh -c qemu+ssh://user@host/system list --all'`.
Keep the URI clean: MAAS refuses query parameters such as `?keyfile=`. Several
VMs on one hypervisor share its URI and differ by domain.

Saving checks the settings first: MAAS reads the Server's power through them,
which can take up to 20 seconds. Only when MAAS reads it as on or off is the
configuration saved, and the confirmation shows what it read. Otherwise (a wrong
address, account, or password, or an address MAAS cannot reach; some BMCs do not
answer a wrong password at all, so it takes the full 20 seconds) nothing changes:
the dialog stays open with MAAS's reason and what you typed, so you can correct
it. Saving never switches power or resumes an inspection; retry the inspection
afterwards. A virtual machine that belongs to a MAAS VM host takes its power from
the VM host and is read-only here.

## Provider and observation failures

A provider error does not erase the last projection. The UI surfaces provider
failure and observation age separately from health. Refresh when a single
Server needs an immediate provider-backed update; reconcile remains the fleet
mechanism.

## Tags

Tags can be edited for one Server or as a tri-state bulk change. swallow owns a
Server's manual tags and applies them to MAAS; a tag changed directly in MAAS is
put back at the next periodic check. A provisioner without tag support reports
it on the Server instead of swallow keeping a separate copy.

Automatic tags are the ones MAAS applies itself, and the editor and tag manager
mark them **Automatic**. They include tags with a definition, such as one that
may back `amd-gpu`, and the `virtual` tag MAAS commissioning keeps on every
machine that is not physical. swallow shows them but never adds them, and when
MAAS adds one again swallow does not treat it as drift. You can still take
`virtual` off Servers by unchecking it in **Edit tags**: MAAS removes it right
away, but may add it again the next time the machine is inspected. A tag with a
definition cannot be removed from single Servers, because MAAS would apply it
again; you can only delete it.

**Manage tags** on the Servers page lists every tag the Site knows, how many
Servers carry it, and whether it is automatic:

- **Rename** a manual tag to change it on every Server of the Site and in MAAS.
  Renaming to a tag that already exists merges the two, after a confirmation.
- **Delete** a tag to remove it from every Server of the Site and from MAAS. The
  confirmation says how many Servers carry it. Deleting a tag with a definition
  deletes the definition too. Tags can select monitoring and automation targets,
  so check which Servers use one before you delete or rename it.
- To delete several tags at once, select them (or **Select all tags**) and choose
  **Delete N tags**. The confirmation lists each tag with what deleting it does.
  The tags are deleted one after another; if one cannot be deleted, the rest still
  go ahead, and it stays selected with the reason so you can try again.

## Zones and Pools

Zones and Pools are swallow-owned grouping resources managed under
**Infrastructure**. Assign a Server through its placement action. A capable
provisioner realizes the grouping; a provisioner without grouping support
reports it on the Server instead of swallow keeping a separate copy.

A Pool is also who may use its Servers. Its members see them (and the Platforms,
Workflows, and Validation Runs on them) and log in to them with their SSH keys; its
owners operate them. Manage a Pool's members and sudo defaults under **Pools**, or
with the **Members** action on the Pool's row under **Infrastructure → Pools**. Only
an admin moves Servers between Pools, and all the Servers of a Platform stay in one
Pool: a Server a Platform claims can only move into that Platform's Pool or out of
every Pool (which leaves the Platform to admins until its Servers are in one Pool
again). Deleting a Pool also deletes its memberships.

## Locks and destructive actions

Lock a Server to exclude it from automatic or destructive management. Unlock it
only after confirming no Workflow or external user depends on the
protection. Power, release, delete, rescue/recovery, and placement actions are
also gated by provider capability and current state.

Deleting a Server is provider-backed: it removes the backing machine before the
projection, preventing reconcile from immediately recreating it.

A Server that a Platform was deployed onto belongs to that Platform until the
Platform is uninstalled or its record is deleted. While it does, **Take action**
says which Platform it belongs to and disables **Release** and **Delete only**;
release its Servers by uninstalling the Platform with **Release all servers**.

To delete several Servers, select them and choose **Delete only** from
**Take action**. The dialog lists them and names any that belong to a Platform or
that a running Workflow holds; deleting does not uninstall the Platform or cancel
that work. Type the number of Servers to confirm. If the provisioner refuses
some deletions, the others still go ahead and the result details show each
refusal.

## CLI examples

```bash
swallow servers list --site-id site1 --provisioning-state deployed
swallow servers list --site-id site1 --provisioning-state inspecting
swallow servers get server1
swallow servers inspect server1
swallow servers power-configuration get server1
swallow servers power-configuration set server1 --driver virsh \
  --address qemu+ssh://maas@hypervisor.lab/system --power-id vm-01
swallow servers power-state server1
swallow servers virtual-machines list hv1        # libvirt domains of a deployed hypervisor
swallow servers virtual-machines enroll hv1 vm-01 vm-02 --integration int1
swallow integrations enroll-bundle int1          # existing-OS command (contains the MAAS API key)
swallow provisioning tags edit --server server1 --add rack-a
swallow provisioning tags delete legacy --site-id site1
swallow provisioning tags rename rack_a rack-a --site-id site1
swallow infrastructure zones list --site-id site1
```

Use `swallow servers --help`, `swallow provisioning tags --help`, and
`swallow infrastructure --help` for exact flags.
