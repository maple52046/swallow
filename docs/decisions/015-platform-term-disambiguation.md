# 015. Platform names the managed runtime aggregate; swallow names the system

- Status: Accepted
- Date: 2026-09-03
- 現行規格：glossary [Platform](../development/glossaries/terms/platform.md)、[swallow](../development/glossaries/terms/swallow.md)

## Context

swallow originally named every Kubernetes and Slurm runtime it managed a Cluster. The name implied a
multi-node topology although k0s supports a useful single-node installation, and it would force
future runtime types into Kubernetes-shaped language. The term ran through API routes, stored
references, Workflows, monitoring, and the Dashboard, so a presentation-only rename would have left
two competing models.

Once the aggregate became Platform, the word "platform" carried three meanings: swallow itself
("the platform" in the agent guide and the architecture documents), the managed runtime aggregate,
and a generic adjective ("platform-wide", "platform component"). Even a task such as "reorganize the
platform definition" was ambiguous.

## Decision

- **Platform is the canonical aggregate** across the Platform Management context, persistence, the
  published API, and the Dashboard: a Kubernetes or Slurm runtime that swallow deployed
  ([decision 032](032-self-deployed-platform-management.md) (self-deployed platform management)),
  with a type and one or many Servers. Membership is observed from the runtime's own API.
- **"cluster" stays only where an external technology owns the word**: the Kubernetes cluster API, a
  k0s cluster credential, Slurm's `clusterName`, or an Ansible variable that technology consumes.
  Workflow kinds such as `deploy-kubernetes` and `uninstall-kubernetes` are therefore precise.
- **The aggregate has one name.** Storage, the API, and new code use `platforms` and `platformId`
  only; the old Cluster names are no longer accepted
  ([decision 074](074-retire-schema-v2-and-deprecated-aliases.md) (retire schema-v2 and deprecated
  aliases)).
- **`swallow` is the only name for the system** this repository builds: the system, the product, and
  the codebase. It is written in lower case in documentation, API contracts, and code comments, also
  at the start of a sentence; text the Dashboard shows and messages the API returns keep their own
  capitalization.
- **The common noun "platform" (and「平台」) never denotes swallow.** Say "swallow-wide" for something
  that applies across the whole system, and "component" for one of swallow's build and test units.
  "monorepo" and "component" describe how swallow is organized; they are not alternative names for it.
- The glossary terms `swallow` and `Platform` each list the other under Disallowed meaning, so the
  boundary is explicit.

## Alternatives considered

- **Rename only Dashboard labels:** rejected; the backend contracts and later Workflows would keep
  teaching two meanings for the same aggregate.
- **Treat standalone Kubernetes as something other than the aggregate:** rejected; its lifecycle,
  membership, and policy model is otherwise identical.
- **Support both names permanently:** rejected; every feature would double its contract and testing
  surface.
- **Rename the aggregate away from Platform to free the word:** rejected; it had just been renamed
  from Cluster, and a second rename would be churn that reopens a settled decision.
- **Keep calling swallow "the platform" and disambiguate by context:** rejected; relying on context
  is what made the definitions ambiguous.
- **Coin a product name such as "swallow control plane":** rejected; it adds a synonym for something
  that already has a name.

## Consequences

- The rename took a schema migration and, for one release, compatibility aliases, but orchestration
  code has one stable vocabulary, and Kubernetes-specific details keep their technical names.
- Ending the aliases was breaking for any client of the old routes and fields; the API contracts
  carry the compatibility notes.
- Documents that called swallow "the platform" or used "platform component" are corrected, and
  glossary labels meaning "system-wide" read "swallow-wide".

## Revision history

- 2026-09-03: decision 015 renamed the Cluster aggregate to Platform across storage, the API, monitoring, and the Dashboard, keeping the old names as aliases for one release.
- 2026-09-05: decision 015 reserved `swallow` as the only name of the system and capital-P Platform for the managed runtime aggregate.
- 2026-10-31: decision 074 ended the one-release Cluster aliases: the `/clusters` routes, `clusterId`, `kind=cluster`, and the `cluster` monitoring label.
- 2026-11-02: merged decision 015 into this ADR; the body states the current decision.
