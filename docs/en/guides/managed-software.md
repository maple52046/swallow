# Managed Software

[繁體中文](../../zh-TW/guides/managed-software.md) · [Documentation home](../README.md)

Managed Software installs or removes one host-level software product on already
deployed Servers. It is intentionally separate from Platform deployment.

## Supported software

- **Docker CE:** installs the Docker runtime from the configured package source.
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

## Dashboard and API

Use the Dashboard **Software** page for operator workflows. The `swallow` CLI
does not yet expose a `software` command group; automation clients should use
the active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
for payload fields, authentication, and errors.
