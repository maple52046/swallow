# Dashboard guide

[繁體中文](../../zh-TW/guides/dashboard.md) · [Documentation home](../README.md)

The Dashboard is the browser interface for swallow's active operator workflows.
It talks only to the published HTTP API and stores only UI preferences and the
current session in the browser.

## Navigation

| Area | What it is for |
| --- | --- |
| Overview | Site-scoped fleet health, attention items, integrations, Platforms, and recent Workflows |
| Servers | Inventory, filters, saved views, bulk actions, tags, locks, and Server details |
| Provisioning | OS deployment, templates, images, upload, and verification |
| Platforms | Kubernetes/Slurm deployment, lifecycle, settings, and runtime views |
| Software | Docker CE, Podman, and NFS installation state and actions |
| Workflows | Durable execution state, Jobs, Tasks, events, logs, cancel, rerun, and retry |
| Monitoring | Alerts, silences, fixed metrics, fleet health, and Grafana links |
| Infrastructure | Sites, Integrations, Zones, Pools, credentials, and automation settings |
| SSH keys (account menu) | The deployment key, your access keys, and their provisioner sync status — see [SSH keys](ssh-keys.md) |

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

## Reading status

- Never expect one combined Server status. Provisioning, membership, and health
  are independent.
- Unknown values remain unknown; the UI does not render them as failure.
- Check integration sync errors and observation timestamps before trusting
  cached values.
- Destructive actions show state and lock gates before submission.

## Sessions and permissions

The Dashboard obtains an opaque bearer token from the login endpoint and reads
the current caller from `/auth/me`. It does not decode JWT claims. An expired
or deleted session returns to login. Role-gated controls are presentation
guidance; the API remains authoritative for authorization.

## Errors and diagnostics

Action failures show the API error message and request ID when available. Copy
the request ID when correlating a UI failure with API logs. For long-running
work, open the resulting Workflow rather than waiting on the original dialog.

The complete route implementation is listed in the
[dashboard component README](../../../dashboard/README.md).
