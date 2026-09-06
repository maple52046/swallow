# Site

- Bounded context: swallow-wide.
- Definition: A physical or logical location that owns its own infrastructure — a datacenter, a colocation cage, or a lab — and is the frame every other managed fact hangs off. swallow defines Sites; no external system owns the set of Sites.
- Allowed meaning: A swallow-owned record identified by an opaque, stable `siteId`, carrying an operator-facing `name` and optional `description`. A Server belongs to one Site through its source; an Integration serves exactly one Site; `site` is a required label in the metrics contract. Sites are deliberately thin: an identifier and a name, not a model of a building.
- Disallowed meaning: Not a Platform, an Integration, a provisioner, an arbitrary Server group, or a physical-placement hierarchy (datacenter/room/rack). Anything more specific about location is an attribute of the things inside the Site, not of the Site itself.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "A Server belongs to a Site through its source." / "A MAAS spanning two locations is modelled as two Integrations, one per Site."
- Related terms: Integration, Server, Staleness, Operation.
- Change note: Migrated 2026-09-05 from the narrative `docs/glossaries/site.md` into the canonical structured tree (reorg D2). Bounded context relabelled from "Platform-wide" to "swallow-wide" per [decision 015](../../../decisions/015-platform-term-disambiguation.md).
