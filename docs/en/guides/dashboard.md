# Dashboard guide

[繁體中文](../../zh-TW/guides/dashboard.md) · [Documentation home](../README.md)

The Dashboard is the browser interface for swallow's active user workflows.
It talks only to the published HTTP API and stores only UI preferences and the
current session in the browser.

## Navigation

The sidebar groups its areas into four sections separated by lines: Overview;
Servers, Platforms, and Software; Monitoring and Activity; and OS Images,
Deployment Templates, Infrastructure, Pools, and Users. It lists only the areas you
can use: an admin sees all of them, and other users see Servers, Platforms, Software,
Activity, Validation, OS Images, and Pools (see [Sessions and permissions](#sessions-and-permissions)). Collapse the sidebar to icons with the button at the top of the
page (this browser remembers the choice); hovering or focusing an icon names it.
On a phone, the menu button opens the same sidebar as a drawer.

| Area | What it is for |
| --- | --- |
| Overview | Site-scoped fleet health, attention items, integrations, Platforms, and recent Workflows |
| Servers | Inventory, Add servers (enrollment guidance), shareable filter views, bulk actions, tags and Manage tags (rename and delete across a Site), locks, and Server details including Boot Media |
| Platforms | Kubernetes/Slurm deployment, lifecycle, settings, and runtime views |
| Software | Grouped catalog (Docker CE, Podman, libvirt (QEMU/KVM), NFS, Prometheus, Grafana, AMD Device Metrics Exporter, Node Exporter, VictoriaMetrics, and any software of this release without its own card under Other software), per-software deployments, and multi-Server installation |
| Monitoring | Fleet health, usage charts for selected Servers, Pools, Zones, and Platforms, current metrics, optional Grafana links, and (in development) alerts and silences |
| Activity | Two tabs. **Workflows**: durable execution state, Jobs, Tasks, events, logs, cancel, rerun, and retry. **Change Plans**: plans for destructive Platform changes, the ones waiting for approval or rejection, the record of approved ones, and the safety switch |
| OS Images | The OS images Servers are deployed with: the catalog, which modes each image supports, and Deploy. An admin also uploads, edits, tests, and deletes images; other users see every image, refresh the catalog, and deploy an image to the Servers of the Pools they own |
| Deployment Templates (admins) | Reusable deployment settings for an OS image: Disk or RAM, network, and optional cloud-init |
| Infrastructure | Sites, Integrations, Zones, credentials, automation settings, the Site's local repository (where deployments fetch artifacts, its sources, sync state, and content; see [Initial setup](initial-setup.md#4-register-the-local-repository)), and [Boot ISOs](#boot-isos-and-server-boot-media) |
| Pools | The Pools you belong to (an admin sees every Pool of the installation and creates, renames, and deletes them); each Pool's members, their Pool Role, and who has sudo on its Servers |
| Users (admins) | Accounts, the access settings, the Directory's status, and recent access changes |
| SSH keys (account menu) | The deployment key, your access keys, and (for admins) their provisioner sync status — see [SSH keys](ssh-keys.md) |
| API keys (account menu) | Keys that let scripts, CI, and the CLI call swallow as you without a password — see [API keys](api-keys.md) |
| Change password (account menu) | Your own swallow password, which you sign in with and which a password-protected sudo rule asks for on Servers |

The tabs of an area keep their own addresses, so links and bookmarks still work:
Activity is `/workflows` and `/change-plans`. OS Images is `/provisioning/images`
and Deployment Templates `/provisioning/templates`; neither page has tabs. Routes under `/clusters`
and `/operations` are compatibility redirects, and `/provisioning/boot-isos`
redirects to `/infrastructure/boot-isos`, keeping its Site scope.

## Use Overview

Overview is a read-only, point-in-time summary of facts owned elsewhere. Choose
a Site to scope it, or clear the Site selection for the swallow-wide view. The
**Updated** time says when the aggregate was generated.

- **Fleet health**, **Capacity**, and **Workflows** summarize Server reachability,
  compute GPU and deployment counts, and active and recently failed work. Fleet
  health counts every Server once as Up, Down, Unknown (swallow monitors it but
  has no answer), or N/A (swallow does not monitor it, absent Servers included);
  see [Server health](monitoring.md#server-health). With
  Alerts enabled in a development build, **Alerts** also summarizes the optional
  Alertmanager source. A missing Alertmanager reads **Not configured**; an
  unreadable configured source reads **Unavailable**. Release builds keep the
  Alerts card in place but mark it unavailable.
- **Needs attention** shows up to ten failed Integration syncs, unreachable
  Platforms, unmatched members, down Servers, and Workflows that failed in the
  last 24 hours. With Alerts enabled it also includes current alerts.
  Follow an item to the owning page to act on it; resolving it there removes it
  from a later Overview read.
- **Recent workflows** links to at most the eight most recently requested
  Workflows. **Integrations** shows each Integration's latest successful sync or
  failure and links to its management page.

Overview does not store a second status or execute repairs. Use it to find the
owning Server, Integration, Platform, Workflow, or alert view.

## Features in development

Four Dashboard features are still in development. Release builds of the
Dashboard hide them; the API and the `swallow` CLI are not affected.

- **Alerts**: the Monitoring page has no alert list, alert counts, or
  Acknowledge action, and Overview's Alerts card reads "Not available in this
  release" and raises no alert items. Basic monitoring is not affected: health,
  current metrics, and usage charts are shown in every build. Use
  `swallow monitoring alerts` instead.
- **OS image upload**: OS Images has no Upload action. Verifying an existing
  custom image is still available.
- **Deployment Templates**: the Deployment Templates page and its sidebar entry,
  Create deployment template on an OS image, and choosing or saving a template while
  deploying are absent; deployments use a custom configuration.
- **Virtual machines**: Add servers has no libvirt virtual machine path, and a
  virtual machine's Summary shows no Boot Media through its hypervisor. Use
  `swallow servers virtual-machines` and `swallow servers boot-media` instead.

Development builds show these features and offer **Account menu →
Experimental features** to switch each one off.

## Site scope

Choose a Site before interpreting inventory or monitoring results. Site scope is
preserved in the URL so links can be shared. A missing Site or a Site with no
configured integration naturally produces empty states.

## Boot ISOs and Server Boot Media

Most Sites do not need a Boot ISO. Use **Infrastructure → Boot ISOs** only when
a Server receives external DHCP and cannot use its provisioner's managed
network-boot path. The page shows the optional
**Server BMC → Swallow Boot ISO → Site DHCP → MAAS rack** path and one card per
configured ISO. **Build Boot ISO** suggests the MAAS rack address from the
selected Integration endpoint; the generated name is under **Advanced
settings**. Cards separate served state from Server usage, keep **Download**
visible, and place the iPXE script, delete action, URLs, hash, and build facts
under **More** and **Technical details**.

On **Server → Summary → Management controller → Boot media**, enabling Boot
Media requires a Boot ISO built for that Server's provisioner. With none
available, the panel links to the **Boot ISOs** tab. An enabled Server can change
or re-apply its ISO, or disable Boot Media while retaining the choice.
**Enable Boot Media**, **Change ISO**, and **Re-apply** show the BMC
preflight's five-step progress, elapsed time, and the remaining time in its
three-minute mount-settle wait. Choose **Continue in background** to close the
dialog without stopping it; the block stays **Applying**, shows the same
progress after a reload or in another tab, and disables its actions until the
result arrives as a notification.

## Local repository

**Infrastructure → Local repository** shows the scoped Site's local repository,
its own copy of the packages, images, and files its deployments install, and
where those deployments fetch them from. Choose a Site first; an installation
with one Site opens it directly.

- **Artifact source for deployments** chooses how the Site's deployments use
  this repository: **Upstream sources** (each artifact from the upstream source
  its module declares; Servers keep their own OS package sources), **Cache**
  (what this repository already serves comes from it and the rest from the
  upstream sources; deployments never wait for it to synchronize), or
  **Offline** (every artifact from this repository only; deployments are
  accepted once it serves everything). Until someone changes it, deployments use
  the upstream sources. Choosing another option asks you to confirm first;
  deployments already accepted keep the source they were accepted with, and you
  can switch back at any time. If someone else changed the repository after the
  page read it, nothing is saved and the page shows the current choice. A source
  whose resync runs reads **Still serving its last sync**, because Servers keep
  getting its previous synchronization meanwhile.
- While deployments use this repository, the status block says whether
  deployments that fetch from it are accepted and, when they are not, what is in
  the way, in the API's words. When swallow cannot use the Integration's
  credential or content origin, an alert links to the Integration settings tab
  where you fix it. While deployments use the upstream sources, the status block
  says so and that the repository is not synchronized; what it already holds is
  kept. Either way it shows the content origin Servers read it at (with a copy
  button), the Site's `repository` Integration and whether it is paused, and when
  swallow last observed the repository.
- **Repository sources** lists every source: the ones swallow's modules declare
  (named by module), the ones you added (**User**), and the pull-through caches
  for container registries and PyPI. Each shows its state (**Pending**,
  **Syncing**, **Ready**, or **Failed**) with the provider's message, its last
  successful sync, and how many items it serves. **Files** lists the files the
  modules declare with a shortened sha256 you can copy in full, and their size.
  **Declared images** lists the container images the modules declare, each with
  its state, the provider's message when a fetch failed, and its size once every
  layer is stored. swallow fetches each image in advance once its registry's
  cache is ready, and the repository stays not ready, so deployments wait, until
  every image is stored; an interrupted or failed fetch is started again. The
  declared Python packages follow. While anything is still syncing, the page
  refreshes every 5 seconds. While deployments use the upstream sources, each
  source, file, and image keeps the state swallow last observed: **Ready**,
  **Not fetched**, or **Last sync failed** with the provider's message.
- **Sync now** asks swallow to converge the repository right away instead of at
  its next periodic check; the states update as swallow observes them. It is
  offered while deployments use this repository.
- **User sources** shows the sources you asked for. **Manage user sources** adds
  a Debian or Ubuntu archive (name, URL, suites, components, and optional
  architectures) or removes one of yours, then saves the whole list; where
  deployments fetch artifacts stays as it is. Sources a module declares are
  listed as not editable. If someone else changed the repository after you
  opened the dialog, nothing is saved: the dialog shows the current list, and
  you make your change again.
- **Content** browses what the repository serves by format (OS packages,
  container images, Python packages, files), one page at a time, optionally
  narrowed to one source or a name. The format, source, search, and page stay in
  the URL, so you can share the view.

A Site without a local repository explains that its deployments fetch each
artifact from the upstream source its module declares.
**Register a repository** opens Create integration on the Integrations page with
the Repository role and the Pulp provider: the endpoint is where swallow reaches
Pulp's API, **Content origin** is the address Servers read it at
(`http://host:port`), and the credential is the Pulp admin password.

## Reading status

- Never expect one combined Server status. Provisioning, membership, and health
  are independent.
- On **Servers**, the **Deployment** column combines swallow's deployment result
  with the provider-neutral OS provisioning state. **Ready** is a known,
  deployable provider state; **Unknown** is not. A spinner marks work in
  progress, and hovering the state shows details. See
  [Servers and infrastructure](servers-and-infrastructure.md#read-the-deployment-column).
- Unknown values remain unknown; the UI does not render them as failure.
- Health reads **Up**, **Down** (a failed scrape, shown as a failure), **Unknown**
  (a warning: swallow expects to monitor the Server but has no answer), or **N/A**
  (neutral: swallow does not monitor the Server). See
  [Server health](monitoring.md#server-health).
- Check integration sync errors and observation timestamps before trusting
  cached values.
- Destructive actions show state and lock gates before submission.

## Sessions and permissions

Signing in starts a session. The Dashboard keeps its short-lived access token in
memory only and renews it automatically through a refresh cookie the page cannot
read, so you are not sent back to the login page while you work. Each tab resumes
the session on load. A session ends when you sign out, after 7 days without use,
or 30 days after sign-in (server defaults); the Dashboard then returns to the
login page, says the session ended, and brings you back to the same page after
you sign in again.

For scripts and the CLI, create an API key under **Account menu → API keys**.
The secret is shown once; delete the key to revoke it. See
[API keys](api-keys.md) for expiry, storage, and CLI guidance.

The Dashboard reads the current caller from `/auth/me` and does not decode token
claims. Role-gated controls are presentation guidance; the API remains
authoritative for authorization.

Who can do what:

- An **admin** administers swallow: Sites, Integrations, OS Images, Zones and Pools,
  Server placement, users, and every Server and Platform. Admins can log in to every
  Server and use sudo there.
- Every other **user** gets access only through Pool Memberships. A **member** of a
  Pool sees its Servers, Platforms, Workflows, and Validation Runs and logs in to its
  Servers with their SSH keys; an **owner** also operates them (power, OS deployment,
  software, Platforms, Validation) and manages the Pool's members and sudo defaults
  under **Pools**. A Pool and its memberships cover its Servers in every Site. A
  Server in no Pool belongs to admins only.
- Only admins create accounts (**Users → Create user**); an owner adds an existing
  account to their Pool with **Add member**, on the Pool's row under **Pools** or on the
  Pool's page, which its name opens. The dialog offers the accounts that are not members yet as you type
  part of the username. A user whose Pools hold no Servers sees **No servers
  available** in the server list. A Pool's **Sudo defaults** decide whether
  members have sudo and whether sudo asks for a password; each member can override
  either one. Owners always have sudo. A Platform may narrow what members do on its
  nodes: on a Slurm cluster they log in only to login nodes, without sudo.
- Actions you cannot take are disabled or hidden: for example a member sees a
  Server's **Actions** button disabled with the reason, and its power state and tags
  without their controls. A member also sees its Pool's Platforms, Change Plans,
  Workflows, Validation Runs, and software deployments without an owner's controls
  (uninstall, sync, approve, cancel, retry, run a suite, install), and a Kubernetes
  Platform without its live cluster tabs, which only owners and admins open. A page
  only an admin can use shows **Access denied**.
- Settings only an admin changes stay out of other users' screens: a Server's Power
  Configuration, Boot Media, and placement; the Change Plan approval policy; Validation
  Schedules and custom script suites; software settings such as Docker registry
  credentials; and the Deployment Key. An owner who deploys an OS or a Platform deploys
  through the provisioner each Server records, without choosing one. A Server's live
  hardware detail (Storage, PCI devices) is shown to the Pool's owners and admins.

## Errors and diagnostics

Action failures show the API error message and request ID when available. Copy
the request ID when correlating a UI failure with API logs. For long-running
work, open the resulting Workflow rather than waiting on the original dialog.

The complete route implementation is listed in the
[dashboard component README](../../../dashboard/README.md).
