# Glossary Index

This directory defines the **ubiquitous language** of the gdcm platform: the terms
every sub-project must use with the same meaning.

Use this file as an index. When working on a specific concept, read the detail
document for that concept rather than every document here.

## Documents

| Document | Concepts defined |
|----------|------------------|
| [`site.md`](site.md) | `Site`, `Integration`, `Staleness` — the only part of the world gdcm defines rather than observes |
| [`server.md`](server.md) | `Server`, its three identity layers, and the three status axes |
| [`provisioning.md`](provisioning.md) | `OS Provisioning Provider`, `Machine`, `MachineStatus`, `OS Image`, `Deployment`, `Release` |
| [`cluster.md`](cluster.md) | `Cluster`, `GPU Stack Owner`, `Membership` |
| [`operation.md`](operation.md) | `Operation`, the automation mirror, and what gdcm refuses |

Monitoring vocabulary — the metrics label contract, `gpuStackOwner`'s effect on exporters,
and why alerts are read rather than stored — lives in
[decision 003](../decisions/003-metrics-label-contract.md) rather than here, because it is
a set of decisions about topology more than a set of terms.

## Conventions

- A term defined here means the same thing in every sub-project, in code, in API
  payloads, and in the UI.
- Where two concepts are easy to conflate, the document says so explicitly and
  explains why they are kept apart. `Machine` and `Server` are the current example.
- Enum values are written exactly as they appear on the wire (lower case, no spaces).
  The UI may relabel them for display; the domain value is what these documents define.
- A concept that only exists inside one sub-project does not belong here. It belongs
  in that sub-project's own documentation.

## Related

- [`docs/decisions/`](../decisions) — **read first.** The decisions constrain what these
  concepts are allowed to mean; where a glossary document disagrees with a decision, the
  decision wins and the glossary is stale.
- [`docs/api-contracts/README.md`](../api-contracts/README.md) — how these concepts are
  shaped on the wire.
- [`docs/architecture.md`](../architecture.md) — the repository structure contract.
