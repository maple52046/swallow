# Dashboard guide

[繁體中文](../../zh-TW/guides/dashboard.md) · [Documentation home](../README.md)

The Dashboard is the browser interface for swallow's active operator workflows.
It talks only to the published HTTP API and stores only UI preferences and the
current session in the browser.

## Navigation

| Area | What it is for |
| --- | --- |
| Overview | Site-scoped fleet health, attention items, integrations, Platforms, and recent Workflows |
| Servers | Inventory, filters, saved views, bulk actions, tags, locks, and Server details including Boot Media |
| Provisioning | OS deployment, templates, images, Boot ISOs, upload, and verification |
| Platforms | Kubernetes/Slurm deployment, lifecycle, settings, and runtime views |
| Software | Docker CE, Podman, and NFS installation state and actions |
| Workflows | Durable execution state, Jobs, Tasks, events, logs, cancel, rerun, and retry |
| Monitoring | Alerts, silences, fixed metrics, fleet health, and Grafana links |
| Infrastructure | Sites, Integrations, Zones, Pools, credentials, and automation settings |
| SSH keys (account menu) | The deployment key, your access keys, and their provisioner sync status — see [SSH keys](ssh-keys.md) |
| API keys (account menu) | Keys that let scripts, CI, and the CLI call Swallow as you without a password |

Routes under `/clusters` and `/operations` are compatibility redirects.
Canonical navigation uses Platforms and Workflows.

## Features in development

Three Dashboard features are still in development. Release builds of the
Dashboard hide them; the API and the `swallow` CLI are not affected.

- **Monitoring**: the Monitoring page and the Server Monitoring tab are absent.
  Health keeps its place on Overview, the Server list, and Server details but
  reads "Not available in this release". You can still register a metrics
  Integration.
- **OS image upload**: OS images has no Upload action. Verifying an existing
  custom image is still available.
- **Deployment Templates**: the Templates workspace, Create template, and
  choosing or saving a template while deploying are absent; deployments use a
  custom configuration.

Development builds show these features and offer **Account menu →
Experimental features** to switch each one off.

## Site scope

Choose a Site before interpreting inventory or monitoring results. Site scope is
preserved in the URL so links can be shared. A missing Site or a Site with no
configured integration naturally produces empty states.

## Boot ISOs and Server Boot Media

Use **Provisioning → Boot ISOs** to build and manage the iPXE ISO for each
provisioner rack in the selected Site. **Build ISO** asks for the provisioner,
name, and MAAS rack address; the table provides script details, download,
usage, and delete actions.

On **Server → Summary → Management controller → Boot media**, enabling Boot
Media requires a Boot ISO built for that Server's provisioner. With none
available, the panel links back to **Boot ISOs**. An enabled Server can change
or re-apply its ISO, or disable Boot Media while retaining the choice.
**Enable Boot Media**, **Change ISO**, and **Re-apply** show the BMC
preflight's five-step progress, elapsed time, and the remaining time in its
three-minute mount-settle wait. Choose **Continue in background** to close the
dialog without stopping it; the block stays **Applying**, shows the same
progress after a reload or in another tab, and disables its actions until the
result arrives as a notification.

## Reading status

- Never expect one combined Server status. Provisioning, membership, and health
  are independent.
- On **Servers**, the **Deployment** column combines swallow's deployment result
  with the provider-neutral OS provisioning state. **Ready** is a known,
  deployable provider state; **Unknown** is not. A spinner marks work in
  progress, and hovering the state shows details. See
  [Servers and infrastructure](servers-and-infrastructure.md#read-the-deployment-column).
- Unknown values remain unknown; the UI does not render them as failure.
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

For scripts and the CLI, create an API key under **Account menu → API keys**. The
secret is shown once; delete the key to revoke it.

The Dashboard reads the current caller from `/auth/me` and does not decode token
claims. Role-gated controls are presentation guidance; the API remains
authoritative for authorization.

## Errors and diagnostics

Action failures show the API error message and request ID when available. Copy
the request ID when correlating a UI failure with API logs. For long-running
work, open the resulting Workflow rather than waiting on the original dialog.

The complete route implementation is listed in the
[dashboard component README](../../../dashboard/README.md).
