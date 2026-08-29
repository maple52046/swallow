# Cluster Lifecycle State

- Bounded context: Cluster Management.
- Definition: The current lifecycle of a Cluster, derived by the backend from durable deployment and uninstall Operations rather than inferred from membership or connectivity.
- Allowed meaning: The value set is `registered`, `deploying`, `deploy_failed`, `active`, `uninstalling`, `uninstall_failed`, or `uninstalled`. `registered` means there is no Swallow deployment provenance. `active` means the latest deployment succeeded and no newer uninstall determines the state. `uninstalled` means the latest uninstall succeeded while the Cluster record remains.
- Disallowed meaning: Lifecycle State must not be derived from a nullable Integration, membership count, sync freshness, or host reachability. It must not describe the execution status of one Operation.
- Synonyms: None.
- Deprecated terms: Waiting for deployment, when used for every Cluster without an Integration.
- Examples: A failed initial deployment is `deploy_failed` even if some hosts contain k0s state. A retrying uninstall is `uninstalling`. A successfully cleaned Cluster remains addressable with state `uninstalled` until Delete.
- Related terms: Cluster, Operation, Membership, Integration.
- Change note: Added on 2026-08-29 to make Cluster lifecycle a provider-owned aggregate read model.
