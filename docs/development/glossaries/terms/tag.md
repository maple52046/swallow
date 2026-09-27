# Tag

- Bounded context: swallow-wide. `api-server`, `dashboard`, and `cli` use this term with the same meaning.
- Definition: An operator-facing label attached to a Server. Ownership is capability-first with a swallow-owned fallback (see decision 031): when the Site's provisioner supports tagging (MAAS), the tag is provider-owned and swallow drives the provisioner to set it; when the provisioner does not, swallow owns the tag itself. A tag is a flat name and carries real operational meaning — the `amd-gpu` tag derives Server Type and selects the RDC exporter, and tags drive the Prometheus and Ansible discovery filters.
- Allowed meaning: Names mirrored onto a Server as `observed.tags`, editable through swallow one Server at a time or in batch. When the provisioner supports tagging, editing ensures a provider-side manual tag and assigns/unassigns it on the machines, and the result is mirrored back on reconcile; when it does not, swallow stores the tag set and merges it into the Server's effective tags. A provisioner's automatic tag — MAAS tags defined by an XPath `definition` — is provider-computed and read-only through swallow.
- Disallowed meaning: Not an OS Image tag (a separate swallow-owned label on images; see decision 025), not a Zone or Pool (those are groupings), not a Server status, and not a hardware probe (a machine that physically has a GPU but is untagged is still `cpu` for targeting).
- Synonyms: Label.
- Deprecated terms: None.
- Examples: "Adding the `amd-gpu` tag makes a Server an AMD GPU server, so it runs the RDC exporter and appears in that scrape job." / "A provisioner without tagging keeps a Server's tags in swallow, merged into its observed tags on reconcile."
- Related terms: Server, Server Type, OS Provisioning Provider, Exporter Ownership, OS Image.
- Change note: Added 2026-09-15 for Server tag editing (single and batch). States the capability-first, swallow-owned-fallback ownership recorded in decision 031, consistent with the OS Image name overlay (decision 025) and zone/pool (decision 029).
