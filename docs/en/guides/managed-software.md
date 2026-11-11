# Managed Software

[繁體中文](../../zh-TW/guides/managed-software.md) · [Documentation home](../README.md)

Managed Software installs or removes one host-level software product on already
deployed Servers. It is intentionally separate from Platform deployment.

## Supported software

- **Docker CE:** installs the Docker runtime from the configured package source.
  The **Enable the Docker Engine API** option (on by default) lets swallow manage
  the host's Docker from the Server's **Containers** tab; see below.
- **Podman:** installs the Podman variant selected by the request.
- **NFS:** installs a server or client role with the required storage settings.
- **Prometheus** and **Grafana:** install one pinned instance per Server, as a
  container on the Server's Docker CE or Podman or natively from the vendor's
  official release or package.
- **AMD Device Metrics Exporter:** installs AMD's per-GPU metrics exporter
  natively on AMD GPU Servers; see below.
- **Node Exporter:** installs the Prometheus host exporter from the OS package.
  swallow installs it automatically and keeps it installed on every deployed Server it
  automates; see below.
- **VictoriaMetrics:** installs one pinned single-node VictoriaMetrics natively, a
  metrics store that Prometheus-compatible clients write to and Grafana reads; see below.
- **libvirt (QEMU/KVM):** turns a Server into a KVM hypervisor whose virtual machines
  swallow can list and enroll; see below.

The shipped automation manifest is the allowlist. If a software kind or variant
does not appear in the active API contract and manifest, it is not supported.

## Eligibility

Before installation:

- each target Server is in a deployed, manageable state — the dashboard offers
  only Servers with an installed OS and no swallow OS deployment that is
  running, failed, or needs attention (a Server whose OS was installed outside
  swallow qualifies);
- the Server is not locked;
- no conflicting active Workflow owns the target;
- Site automation can resolve the SSH user and credential for every host;
- required software-specific settings are present.

One request may target multiple eligible Servers. The API applies the same gates
again at submission time.

## Software Assignments

A Software Assignment is keyed by Server and software kind. It records desired
state, last-applied state, related Workflow, and failure information. It is not
proof that every external package fact is current; use the associated Workflow
and host diagnostics when the result is unclear.

## Install and uninstall

Use **Software** as the catalog of software swallow can manage. Open a software
card to see its capabilities and current deployments, then choose **Install
software** to select one or more eligible Servers before configuring that
software. Servers that already have the software are managed from the same
detail page with **Reconfigure**, **Retry install**, or **Uninstall**.

A Server's **Take action** → **Install software** is the fixed-Server entry
point. It first presents the grouped software catalog and then the selected
software's configuration. An existing or failed assignment is prefilled for
reconfigure or retry; an in-progress assignment links to its Workflow instead.
The action explains why when the Server cannot take an install. Submission
creates a Workflow using the registered playbook mapping. Uninstall requests
are explicit and produce a separate Workflow; deleting a Server or Platform
does not silently imply every software uninstall.

NFS server/client role changes deserve extra review because they can affect
mounted storage and workload availability.

## AMD Device Metrics Exporter

The AMD Device Metrics Exporter serves per-GPU Prometheus metrics on the
Server's address at port 5001. It needs a Server with AMD compute GPUs running
Ubuntu 24.04, and it has no settings. Installing it also installs swallow's
amdgpu driver loader when the Server has none.

It installs only the exporter. swallow's Site Prometheus collects it once it is
installed; to graph the metrics, install **Grafana** with the Device Metrics
Exporter dashboards and a datasource that reads that Prometheus.

A Slurm Platform deployed with the Device Metrics Exporter installs the same
software on its GPU nodes, with Slurm job attribution. While a Platform owns a
Server, the exporter there is managed through the Platform: the Software page
does not install or uninstall it on that Server, and uninstalling the Platform
removes it. A Kubernetes Platform's GPU Operator runs its own exporter, so this
software is not offered on Kubernetes nodes.

A Slurm Platform's own Grafana is **Grafana** on its primary controller, listed here
like any other installation, with the container runtime it runs on. Both stay when
the Platform is uninstalled, until you uninstall them here. A Slurm Platform deployed
before the Site Prometheus collected its exporters also has a **Prometheus** there;
swallow no longer uses it and never removes it, so uninstall it here when you no
longer need its history.

## Node Exporter

The Node Exporter serves host metrics (CPU, memory, disk, network) on port 9100,
where swallow's Prometheus discovery scrapes every Server. It installs the OS
distribution's package — `prometheus-node-exporter` on Debian and Ubuntu,
`node_exporter` on the RHEL family, where it comes from EPEL — and needs no
container runtime. It has no settings.

Every OS deployment installs it on the Servers it deploys, and swallow keeps it installed on
every deployed, unlocked Server of a Site whose automation is enabled, including Servers it
did not deploy: it installs it where it is missing, retries a failed install, and installs it
again when the Site's Prometheus stops scraping it (see
[Monitoring](monitoring.md#the-node-exporter-is-kept-installed)). You rarely install it here;
an uninstall on such a Server is undone at the next check, so lock the Server to keep the
Node Exporter off it. A member of any Platform accepts it like any other deployed Server. Installing it first
removes swallow's older containerised node-exporter
(`swallow-node-exporter.service`) if the host still runs it; Docker and any RDC
exporter container are left alone.

## VictoriaMetrics

VictoriaMetrics installs VictoriaMetrics 1.155.0 (single-node) from its official
release binary, which swallow fetches from the Site's chosen source and verifies. It
needs no container runtime and runs on amd64 Servers only. It is verified on Ubuntu
24.04; the RHEL family is not verified yet. It serves VictoriaMetrics's
Prometheus-compatible write and query API on port 8428. It has two settings:

- **Answers on:** `loopback` (the default) answers only programs on the same Server,
  such as a Grafana there; `management` answers on the Server's management address.
  VictoriaMetrics has no login, so anyone who reaches that address can read and write
  every series. Choose `management` only on a trusted network.
- **Retention:** how long samples are kept, as a whole number with `d` (days), `w`
  (weeks), `M` (months), or `y` (years), for example `90d` or `1y`; at most 100 years.
  Leave it blank for VictoriaMetrics's default, `1M`. A bare number, `m`, or hours are
  refused, because VictoriaMetrics reads them differently or refuses less than a day.

To graph its data, install **Grafana** with its **Prometheus URL** set to the
VictoriaMetrics address, for example `http://127.0.0.1:8428` on the same Server.
swallow's Site Prometheus does not collect it, and no Platform installs it.
Reconfiguring it with another address or retention restarts it and keeps its data;
uninstalling it removes the service, the release, and **all of its data**.

## libvirt (QEMU/KVM)

libvirt installs QEMU, libvirt, and UEFI firmware from the OS packages, keeps the
libvirt daemon running, and adds the Server's default user — the account swallow logs
in as — to the `libvirt` and `kvm` groups. The Server can then act as a hypervisor:
its virtual machines can be listed and enrolled by name (see
[Libvirt virtual machines](servers-and-infrastructure.md#libvirt-virtual-machines)). It
does not create virtual machines, and it has no settings.

- It needs Ubuntu 24.04 on an x86_64 CPU with hardware virtualization (Intel VT-x or
  AMD-V) enabled in the BIOS. A Server that is itself a virtual machine also needs
  nested virtualization from its own hypervisor. The installation stops with the
  reason before installing anything when one of these is missing.
- On a RAM deployment it also installs the kernel modules KVM needs, which the RAM
  image leaves out. Everything it installs on a RAM-deployed Server is gone after a
  reboot, while swallow still shows libvirt as installed; install it again then.
- It is safe on a host that already runs virtual machines: it does not restart the
  libvirt daemon or change libvirt's networks, the host's network configuration, or
  existing virtual machines.
- The installation's Workflow reports how many IOMMU groups the host has. Zero means
  the IOMMU is off: enable it in the BIOS before passing PCI devices to virtual
  machines.
- Uninstalling is refused while the host still has virtual machines. It removes the
  packages but keeps disk images under `/var/lib/libvirt`.

## The docker group

Installing Docker CE adds the Server's default user — the account swallow logs in
as, shown on the Server's **Connection** card — to the `docker` group, so it can
run Docker without sudo after its next login. On a Server whose OS was installed
outside swallow, set that default user first (see SSH keys and image login
users). Re-applying Docker CE adds the account on Servers installed earlier.

## Docker Engine API and the Containers tab

When swallow installed Docker CE on a Server, the Server detail page shows a
**Containers** tab with four sections — **Containers** (create, start, stop,
restart, logs, remove; shown first), **Images** (pull, remove), **Volumes**, and
**Networks**. Everything is read live from the host's Docker Engine through the
swallow API; swallow stores none of these objects.

Management requires the Docker Engine API, which the Docker CE installation
enables by default:

- The Docker daemon also listens on TCP port `2375` of every interface,
  **without authentication or TLS**. Anyone who can reach that port controls the
  host as root. Use it only on internal networks, and turn it off for Servers
  reachable from the Internet.
- Without the API, the tab explains why and offers **Enable Docker API**. On a
  Server with the API, **Disable Docker API** turns it off. Both re-run the same
  Docker CE installation with the changed setting; there is no separate job.
- Changing the setting restarts the Docker daemon, so containers without a
  restart policy stop.
- Docker CE installed before this option existed is treated as having the API
  disabled. A Docker API you enabled yourself is not used.
- On a locked Server the tab stays readable, but every change is disabled until
  the Server is unlocked.
- A pull may take up to 55 minutes, enough for GPU images of tens of gigabytes.
  You can close the **Pull image** dialog while it runs: the Images section
  lists pulls still in progress and you are notified when each finishes. A pull
  that runs out of time keeps the layers it downloaded, so pulling again
  resumes from there.

### Private registries

To pull private images, add a **Registry credential** under **Software › Docker
CE › Settings**. Only software with its own settings exposes this action. Enter
the registry host (for example `harbor.example.com` or `docker.io` for Docker
Hub — `hub.docker.com` is saved as `docker.io` too), a username, and a password
or access token. The dialog shows the registry the credential is saved as. A
pull uses the credential of
the registry in its image reference — `nginx` and `team/app` mean Docker Hub —
and the **Pull image** dialog shows which one it will use; without a credential
the pull is anonymous.

Credentials are shared by every Server, stored encrypted, and the password is
never shown again (replace it to change it). Running `docker login` on a host
does not help: the Docker Engine API ignores credentials stored on the host. The
credential travels to the host over the same unencrypted API listener as other
Docker requests.

## Dashboard and API

Use the Dashboard **Software** page for user workflows. The `swallow` CLI
does not yet expose a `software` command group; automation clients should use
the active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
for payload fields, authentication, and errors, and the
[Docker Host Explorer contract](../../../api-server/docs/development/api-contracts/api-server/servers-docker.md)
for the Containers tab's API.
