# 048. swallow-defined OS provisioning states

- Status: Accepted
- Date: 2026-10-04

## Context

On 2026-10-04 two Servers were releasing, and the dashboard Server list showed no
"Releasing" anywhere. The list's Deployment cell passed the provisioning axis to its badge
only when the state was `deployed`, documented as presenting the Swallow deployment
"without leaking provider lifecycle labels"; its tests asserted that a releasing row reads
"Not deployed", and that failed or broken rows show neither state.

That treated the whole provisioning axis as MAAS vocabulary. Most of it is not: every OS
provisioner can release a machine, install an OS, or mark one broken. The one value that
really was MAAS vocabulary, `commissioning`, flowed unchanged through the domain, the API,
and the UI. Ironic, for example, calls the same step introspection. Meanwhile the list
showed "Not deployed" for what the provisioner calls Ready, which made two names for one
state.

## Decision

- swallow owns the `provisioning.state` vocabulary (glossary term OS Provisioning State):
  `new | inspecting | ready | allocated | deploying | deployed | releasing | testing |
  rescue | broken | failed | retired | unknown`. Each provider adapter maps its own
  lifecycle onto these values and keeps its own label only as the display-only
  `providerState`. A new provider step is mapped to an existing value; it never adds a
  provider word to the set.
- `commissioning` is renamed to `inspecting` on the wire. The operator action follows:
  `POST /api/v1/servers/{id}/commission` becomes `POST /api/v1/servers/{id}/inspect`
  (the MAAS adapter still calls MAAS `commission`), and the CLI verb becomes
  `swallow servers inspect`. `commissioningStatus`, the provider's label for its last
  inspection result, keeps its name because it is a provider label, not a state.
- Projections stored before the rename are read as `inspecting`, and a
  `provisioningState=inspecting` filter also matches them, until reconcile rewrites the
  stored value. No schema migration is needed.
- Clients present these states as swallow states. The dashboard Deployment cell shows the
  Swallow deployment outcome while one is in progress or needs attention, the installed
  OS image when deployed, and otherwise the OS Provisioning State by its own name:
  Releasing, Inspecting, Testing, Deploying, Ready, Allocated, Failed, Broken, and so on.
  "Not deployed" is removed; the idle state is Ready. In-progress states show a loading
  spinner after the label; the label still carries the state.

## Alternatives considered

- **Keep hiding provider lifecycle in the list and show only the Swallow deployment
  result.** Rejected: Release, deploy, and failure states are generic. Hiding them left
  operators unable to see that a Server was releasing, and it produced a second name
  ("Not deployed") for `ready`.
- **Keep `commissioning` on the API and relabel it only in the UI.** Rejected by the
  user: the API is the published language shared by the dashboard, the CLI, and
  integrators. A MAAS word there makes every consumer depend on MAAS vocabulary.
- **A schema migration that rewrites stored `commissioning` values.** Rejected: the
  value is a mirrored observation that reconcile rewrites within one inventory pass, so
  reading the old value as `inspecting` is enough. A schema version bump would block
  serving until an operator runs `swallow-api migrate`.

## Consequences

- Breaking API change: clients that send or branch on `commissioning` or call
  `/commission` must move to `inspecting` and `/inspect`. The dashboard and CLI move in
  the same change.
- An Ironic or other adapter has a defined target vocabulary.
- The Server list now shows provider failures (Failed, Broken, Rescue) and in-progress
  provider work (Releasing, Inspecting, Testing). The list's changing and attention
  signals count them too, so the column and the row signals agree.

## Current status

Implemented: glossary, `api-server` domain, MAAS mapping, storage read compatibility,
route and contract; `cli` verb and usage; `dashboard` state and action vocabulary, the
Deployment cell, and in-progress spinners.
