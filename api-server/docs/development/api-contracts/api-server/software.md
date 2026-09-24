# Software Deployment

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client)

## Purpose

Install and uninstall a single piece of host software (Managed Software) on one or more
already-deployed Servers, and read the swallow-owned Software Assignment records that track
what is installed where. This is deliberately distinct from platform deployment: the deploy
target is one software and its variants, not a multi-component runtime (see
[decision 038](../../../../../docs/decisions/038-software-deployment.md)).

Every install/uninstall creates a durable Workflow; the response returns its `operationId`,
and progress is observed through the [workflows](workflows.md) surface.

## Related Glossary Terms

- [Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
- [Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
- [Workflow](../../../../../docs/development/glossaries/terms/workflow.md)
- [Server Status](../../../../../docs/development/glossaries/terms/server-status.md)

## Endpoints

```text
GET  /api/v1/software/catalog
GET  /api/v1/software/assignments
POST /api/v1/software/assignments
POST /api/v1/software/uninstall
```

All endpoints require an admin JWT according to [conventions](conventions.md).

## Software Catalog

`GET /api/v1/software/catalog` returns the fixed set of installable software kinds and their
rules. The result is a small bounded array, not paginated.

```json
{
  "items": [
    {
      "kind": "docker-ce",
      "label": "Docker CE",
      "roles": [],
      "mutuallyExclusiveWith": ["podman"],
      "refusedForKubernetesMembers": true,
      "specFields": ["version"]
    },
    {
      "kind": "podman",
      "label": "Podman",
      "roles": [],
      "mutuallyExclusiveWith": ["docker-ce"],
      "refusedForKubernetesMembers": true,
      "specFields": ["version"]
    },
    {
      "kind": "nfs",
      "label": "NFS",
      "roles": ["server", "client"],
      "mutuallyExclusiveWith": [],
      "refusedForKubernetesMembers": false,
      "specFields": ["exportPath", "exportOptions", "source", "mountPath", "mountOptions"]
    }
  ]
}
```

`kind`, `roles`, and `mutuallyExclusiveWith` values are domain language from the
[Managed Software](../../../../../docs/development/glossaries/terms/managed-software.md)
glossary term; adding a value is a model change, not just an API change.

## Software Assignment

A Software Assignment is the swallow-owned record of one Managed Software kind on one Server,
keyed by `(serverId, kind)`.

```json
{
  "serverId": "srv-abc",
  "kind": "nfs",
  "roles": ["client"],
  "spec": {
    "source": "10.0.0.9:/export/data",
    "mountPath": "/shared",
    "mountOptions": "rw,_netdev"
  },
  "state": "installed",
  "lastWorkflowId": "op-123",
  "lastAppliedAt": "2026-09-24T06:00:00Z",
  "createdAt": "2026-09-24T05:59:00Z",
  "updatedAt": "2026-09-24T06:00:00Z"
}
```

`state` is one of `pending | installed | failed | uninstalling | absent`. These are domain
values from the
[Software Assignment](../../../../../docs/development/glossaries/terms/software-assignment.md)
term. `absent` means Swallow no longer considers the software present (for example the Server
left `deployed`).

### List Assignments

`GET /api/v1/software/assignments` returns a plain array of Software Assignments (small,
bounded per fleet). Optional query filters:

| Param | Type | Meaning |
| --- | --- | --- |
| `serverId` | string | Only assignments on this Server. |
| `kind` | string | Only assignments of this software kind. |

By default `absent` assignments are omitted; pass `includeAbsent=true` to include them.

```json
{ "items": [ /* Software Assignment objects */ ] }
```

## Install Software

`POST /api/v1/software/assignments` installs one software kind on one or more deployed
Servers, creating a durable Workflow and a `pending` assignment per Server.

```json
{
  "kind": "nfs",
  "assignments": [
    { "serverId": "srv-a", "roles": ["server"] },
    { "serverId": "srv-b", "roles": ["client"] }
  ],
  "spec": {
    "exportPath": "/export/data",
    "exportOptions": "rw,sync,no_subtree_check,root_squash",
    "source": "srv-a:/export/data",
    "mountPath": "/shared",
    "mountOptions": "rw,_netdev"
  }
}
```

- `kind` is required and must be a catalog kind.
- `assignments[]` lists the target Servers. Each `serverId` is required; `roles` is required
  for a kind that declares roles (NFS) and must be a subset of that kind's roles, and must be
  empty/omitted for a role-less kind (Docker CE, Podman).
- `spec` is kind-specific and optional per field. For NFS, at least one `server` role in the
  batch requires `exportPath`, and a `client` role requires `source` and `mountPath`.
- Every target Server must be in provisioning state `deployed`.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-123" }
```

### Install Errors

In addition to the shared codes in [conventions](conventions.md):

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | Unknown kind, missing/invalid roles for the kind, or missing required spec field. |
| `conflict` | 409 | A target already has an active durable Workflow, a mutually exclusive software kind is already installed on a target, or a target is already a Kubernetes Platform member for a kind that refuses members. |
| `not_found` | 404 | A target Server does not exist. |
| `conflict` | 409 | A target Server is not in the required `deployed` provisioning state, or is locked. |

## Uninstall Software

`POST /api/v1/software/uninstall` removes one software kind from one or more Servers on which
it is installed, creating a durable Workflow that runs the `uninstall-<kind>` playbook and
marks the assignments `absent`.

```json
{
  "kind": "nfs",
  "serverIds": ["srv-a", "srv-b"]
}
```

- `kind` is required and must be a catalog kind.
- `serverIds[]` lists the target Servers; each must have an existing, non-`absent` assignment
  of that kind.

On success the response is `202 Accepted`:

```json
{ "operationId": "op-124" }
```

### Uninstall Errors

| `error.code` | HTTP Status | Meaning |
| --- | ---: | --- |
| `validation_error` | 400 | Unknown kind or empty `serverIds`. |
| `not_found` | 404 | A target Server or its assignment does not exist. |
| `conflict` | 409 | A target has active durable Workflow, is not `deployed`, or is locked. |

## Compatibility Notes

- Software kinds, roles, and assignment states are glossary-owned enums; adding a value is a
  model change per [conventions](conventions.md).
- The install/uninstall responses intentionally return only `operationId`; clients read
  progress and the per-Task Job grouping through the [workflows](workflows.md) surface.

## Future (slice 2, not yet implemented)

- Platform deploys will compose a software kind as a Job on the same Workflow (for example a Slurm
  or Kubernetes deploy pulling in `nfs` or `docker-ce`), sharing these playbooks and roles. That
  composition is a launcher change, not a new endpoint, so this contract is unaffected.
- Slurm's currently embedded NFS (HA state and workload storage) is not the generic `nfs` software
  above; extracting it into the shared `configure-nfs` Job is deferred. Until then, generic NFS and
  Slurm-managed state NFS are distinct.
