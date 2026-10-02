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

The shipped automation manifest is the allowlist. If a software kind or variant
does not appear in the active API contract and manifest, it is not supported.

## Eligibility

Before installation:

- each target Server is in a deployed, manageable state;
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

Use **Software** to select a kind, variant/settings, and targets. Submission
creates a Workflow using the registered playbook mapping. Uninstall requests are
explicit and produce a separate Workflow; deleting a Server or Platform does
not silently imply every software uninstall.

NFS server/client role changes deserve extra review because they can affect
mounted storage and workload availability.

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

To pull private images, add a **Registry credential** under **Software ›
Settings › Docker CE** (settings are grouped by software kind there): the
registry host (for example `harbor.example.com` or `docker.io` for Docker
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

Use the Dashboard **Software** page for operator workflows. The `swallow` CLI
does not yet expose a `software` command group; automation clients should use
the active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
for payload fields, authentication, and errors, and the
[Docker Host Explorer contract](../../../api-server/docs/development/api-contracts/api-server/servers-docker.md)
for the Containers tab's API.
