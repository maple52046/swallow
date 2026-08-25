# Swallow Dev / Testing / Production installation

- Status: Implemented baseline
- Date: 2026-08-25
- Supported platform: Ubuntu 24.04 amd64
- Initial topology: single control-plane node

## Terminology

Installing, running, upgrading, restoring, or uninstalling the Swallow control plane is
an **installation**. **Deployment** is reserved for the MAAS-owned server/OS action.
Development, testing, and production are environments; container and native are
installation methods. The existing `deploy/` repository path remains an internal
compatibility name and does not define the product term.

## Environment contract

| Environment | Runtime | Data | Promotion identity |
| --- | --- | --- | --- |
| Dev | Source bind mounts, Go hot reload, Vite HMR, MongoDB, pinned Ansible venv | Local disposable data | Working tree |
| Testing | No build and no source mount; production images only | Isolated test data and integrations | Candidate OCI digest |
| Production | Compose or native systemd bundle | Persistent customer data | Promoted SemVer reference to the tested digest |

`deploy/dev` is the only development golden path. Testing has a shared project and a
unique-project-name CI form. Branch names never identify an environment.

## Runtime architecture

Nginx is the only public edge. It serves the dashboard and proxies same-origin API and
health requests. API and MongoDB remain internal. The API exposes separate liveness and
Mongo/schema readiness checks plus machine-authenticated metrics. External MAAS and
Prometheus failures do not change readiness.

Operations are persisted pending before the embedded dispatcher claims them. Mongo site
leases enforce one run per site and permit cross-site concurrency. The runner accepts
only release-manifest playbooks, verifies SSH host keys, materializes encrypted
credentials under `/run/swallow/jobs` with restrictive permissions, deletes temporary
material, and persists logs under `/var/lib/swallow/jobs`. Interrupted leases become
indeterminate without automatic replay.

## Release and air gap

Main produces a candidate once. CI publishes signed OCI digests, SBOMs, native binary,
dashboard dist, playbooks, offline Python wheelhouse, and checksums. A SemVer tag downloads
that exact candidate and adds references without rebuilding. The release has independent
core and third-party offline media manifests.

Production image references must be OCI digests. Secrets use Compose secrets or root-only
files and every sensitive runtime value supports `*_FILE`. The credential encryption
key is backed up with MongoDB and job artifacts.

## Lifecycle

Both production paths provide preflight, install, upgrade, backup, restore, doctor, and
uninstall. Upgrade refuses active operations unless force is explicit, backs up first,
runs only supported schema migrations, and smoke-tests readiness. Uninstall preserves
data unless `--purge-data` is explicit. Destructive downgrade is unsupported.

MongoDB backups run daily, retaining seven daily and four weekly recovery points. Restore
drills must validate login, integrations, history, artifacts, and encrypted credentials.

## Third-party boundary

Docker CE is a host prerequisite, MongoDB 8 is core-bundle managed, MAAS 3.6 runs on a
dedicated Ubuntu 24.04 host/VM with production PostgreSQL, Ansible is embedded, and each
site owns an independent 30-day Prometheus. DNS, TLS CA, MAAS networking and target
machines are installation inputs. Central TSDB, Alertmanager, Grafana, API HA, and MongoDB
replica sets remain later milestones.

## Acceptance gates

The release gate covers fresh dev, digest parity, Compose/native install and restart,
fully disconnected installation, MAAS-to-Ansible-to-log-to-Prometheus E2E, failure
injection, non-root/container isolation, backup/restore, active-operation upgrade refusal,
and liveness/readiness semantics.
