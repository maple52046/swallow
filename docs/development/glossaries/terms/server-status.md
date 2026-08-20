# Server Status

- Bounded context: Platform-wide. The value set is authoritative for `api-server`, `agent`, and `dashboard`.
- Definition: The operational health and availability state of a Server, expressed as one closed set of domain values.
- Allowed meaning: Exactly one of the following six values. The domain value is the string on the left; a UI may render a different label but must not introduce a new state.

  | Value | Meaning |
  | --- | --- |
  | `live` | Server is reachable and operating normally. |
  | `warning` | Server is reachable but has non-critical issues. |
  | `error` | Server has a critical fault. |
  | `maintain` | Server is in maintenance mode — intentional downtime. |
  | `offline` | Server is confirmed unreachable or unavailable. |
  | `unknown` | Insufficient information to determine health or availability. Common causes: newly registered Server not yet probed, monitoring agent has not reported, or the data source is unavailable. |

  `unknown` is the correct default for a newly registered Server, before any monitoring data has been received. The three "not healthy" states are distinct and must not be collapsed: `unknown` means *we do not know*, `offline` means *confirmed unreachable*, and `maintain` means *intentionally out of service*.

  The dashboard renders `maintain` as "Maintenance". The domain value remains `maintain`, and the display label must not leak into API payloads, filters, or persistence.
- Disallowed meaning: A power state, a provisioning state, a workload state, an allocation state, or an alert severity. Server Status must not be used to express whether a Server is assigned to an owner, nor whether its hardware is powered on.
- Synonyms: None. Do not use "health" for this concept when the value set above is meant.
- Deprecated terms: None.
- Examples: "Filter the server list by `status=live` to show only normally operating servers." / "A server under planned firmware upgrade is `maintain`, not `offline`, so operators can distinguish intentional downtime from a fault."
- Related terms: Server（本值集所描述的實體）, Alert Severity（不同概念，不可互換）.
- Change note: Extracted from the previous single-file `docs/glossaries/server.md` "Related Enums" section into its own term document, because a closed value set is authoritative domain language and needs to be findable on its own.
