# Servers and infrastructure

[繁體中文](../../zh-TW/guides/servers-and-infrastructure.md) · [Documentation home](../README.md)

Servers are reconciled projections of provisioner machines. Operators inspect,
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

A lab virtual machine enlists the same way (boot it from the network or from an
iPXE boot medium), but enlistment cannot give it a power driver, so MAAS cannot
report its power-off. Its inspection stops with **Set power configuration**; give
it a `virsh` [Power configuration](#power-configuration) and retry.

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
which would reboot the host. The command contains the provisioner's API key, which
MAAS requires; run it only on trusted hosts and rotate the key in MAAS otherwise.

## Inventory and details

The Servers list supports Site scope, pagination, search, status and capability
filters, saved views, live row updates, and bulk selection. A Server detail
workspace separates summary, activity, monitoring, network, storage, and PCI
observations. A Server where swallow installed Docker CE also gets a
**Containers** tab for its Docker images, containers, volumes, and networks (see
[Managed Software](managed-software.md#docker-engine-api-and-the-containers-tab)).

Always use the opaque Server ID in links and automation. Duplicate hostnames and
addresses are valid across Sites.

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

| Display | Operator meaning |
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
Server refresh. It is close to real time after an operator action such as
Release because the list follows that Server. If no start is known, no running
time is shown.

After Release is accepted, stay on the list: the row changes to **Releasing**
while the provider works and then to **Ready** when the Server returns to the
pool.

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
| Eye — **View activity** | Provider-only work is running. | **Activity → Provisioning tasks** for Releasing, **Related Operations** for Inspecting, **Provider events** otherwise |
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
**Inspect newly enrolled Servers automatically** in the Integration dialog;
Inspect hardware stays available. Inspect is refused while a Server is deployed,
allocated, in rescue, or busy with other provider work or another Workflow.

## Power configuration

A Server's power configuration is the power driver MAAS uses to switch and read
its power, and that driver's settings. MAAS owns it; swallow reads it live and
saves your changes to MAAS. It is on the Server detail **Summary**: in the
**Management controller** card for a Server with a BMC, and in the **Power
control** card for a Server without one. Choose **Edit power configuration** (or
**Set power configuration**).

| Driver | For | Settings |
| --- | --- | --- |
| IPMI, Redfish | A physical Server's BMC | BMC address, account, password |
| virsh | A libvirt virtual machine | Hypervisor URI `qemu+ssh://user@host/system`, domain name or UUID, optional password |

Only a BMC driver gives a Server Boot Media; Redfish Boot Media is probed from
the BMC whichever of the two drivers MAAS uses. The password is write-only:
leave it empty to keep the stored one (changing the driver removes it unless you
enter a new one).

For a `virsh` driver, MAAS — not swallow — connects to the hypervisor, so its
rack controller needs SSH access to the hypervisor account before the driver
works. With the MAAS snap, put the key, `known_hosts`, and any SSH options in
`/var/snap/maas/current/root/.ssh`, and check from inside the snap:
`sudo snap run --shell maas -c 'virsh -c qemu+ssh://user@host/system list --all'`.
Keep the URI clean: MAAS refuses query parameters such as `?keyfile=`. Several
VMs on one hypervisor share its URI and differ by domain.

Saving does not switch power or resume an inspection. Read **Power state** to
confirm MAAS can reach the driver, then retry the inspection. A virtual machine
that belongs to a MAAS VM host takes its power from the VM host and is read-only
here.

## Provider and observation failures

A provider error does not erase the last projection. The UI surfaces provider
failure and observation age separately from health. Refresh when a single
Server needs an immediate provider-backed update; reconcile remains the fleet
mechanism.

## Tags

Tags can be edited for one Server or as a tri-state bulk change. When MAAS
supports tags, MAAS is authoritative and reconciliation mirrors the result.
Otherwise swallow stores a same-shaped fallback. Consumers see one effective
tag set.

## Zones and Pools

Zones and Pools are swallow-owned grouping resources managed under
**Infrastructure**. Assign a Server through its placement action. A capable
provisioner realizes the grouping; otherwise the swallow-owned value remains
the effective placement.

## Locks and destructive actions

Lock a Server to exclude it from automatic or destructive management. Unlock it
only after confirming no Workflow or external operator depends on the
protection. Power, release, delete, rescue/recovery, and placement actions are
also gated by provider capability and current state.

Deleting a Server is provider-backed: it removes the backing machine before the
projection, preventing reconcile from immediately recreating it.

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
swallow integrations enroll-bundle int1          # existing-OS command (contains the MAAS API key)
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

Use `swallow servers --help`, `swallow provisioning tags --help`, and
`swallow infrastructure --help` for exact flags.
