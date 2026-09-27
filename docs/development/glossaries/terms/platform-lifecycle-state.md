# Platform Lifecycle State

- Bounded context: Platform Management.
- Definition: The current lifecycle of a Platform, derived by the backend from durable deployment and uninstall Operations rather than inferred from membership or connectivity.
- Allowed meaning: The value set is `registered`, `deploying`, `deploy_failed`, `active`, `uninstalling`, `uninstall_failed`, or `uninstalled`. `registered` means there is no Swallow deployment provenance; it is a one-release compatibility state for pre-existing records only — the public API no longer produces a new `registered` Platform ([decision 032](../../../decisions/032-self-deployed-platform-management.md)). `active` means the latest deployment succeeded and no newer uninstall determines the state. `uninstalled` means the latest uninstall succeeded while the Platform record remains.
- Disallowed meaning: Lifecycle State must not be derived from a nullable Integration, membership count, sync freshness, or host reachability. It must not describe the execution status of one Operation.
- Synonyms: None.
- Deprecated terms: Waiting for deployment, when used for every Platform without an Integration.
- Examples: A failed initial deployment is `deploy_failed` even if some hosts contain k0s state. A retrying uninstall is `uninstalling`. A successfully cleaned Platform remains addressable with state `uninstalled` until Delete.
- Related terms: Platform, Workflow, Membership, Integration.
- Change note: Added on 2026-08-29 to make Platform lifecycle a provider-owned aggregate read model. On 2026-09-19, `registered` was narrowed to a one-release compatibility state after the public register-existing path was removed ([decision 032](../../../decisions/032-self-deployed-platform-management.md)).
