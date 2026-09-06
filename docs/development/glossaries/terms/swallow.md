# swallow

- Bounded context: swallow-wide.
- Definition: The system this repository builds — the data-center management system that owns intent, policy, and identity mapping across Sites and integrates external systems for everything it does not own. `swallow` is the only name for this system.
- Allowed meaning: The whole system and product named `swallow`, delivered as one monorepo containing the `api-server` and `dashboard` components; the Go module `github.com/maple52046/swallow` and its `swallow` binary; the thing an Installation installs, starts, upgrades, or uninstalls. Refer to the system simply as `swallow`.
- Disallowed meaning: Not "the platform" — that word and「平台」are reserved for a `Platform` (a managed Kubernetes or Slurm runtime aggregate). Not a single component (`api-server` or `dashboard` alone), not a Site, an Integration, or a Platform. "monorepo" and "component" describe how swallow is structured; they are not alternative names for swallow.
- Synonyms: None. Do not coin "swallow platform", "control plane", or "product" as names for the system.
- Deprecated terms: `the platform` / `平台`, when used to mean swallow itself.
- Examples: "swallow reconciles provisioner inventory into Server projections." / "A Platform is deployed by swallow; swallow is not a Platform."
- Related terms: Platform, Installation, Site, Integration, Server.
- Change note: Added 2026-09-05 to resolve the platform/Platform overload ([decision 015](../../../decisions/015-platform-term-disambiguation.md)). swallow names the system; Platform names the managed runtime aggregate.
