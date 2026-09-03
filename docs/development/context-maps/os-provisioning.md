# OS provisioning context map

The OS Provisioning bounded context owns operator intent, policy, validation,
Deployment Templates, and durable Provisioning Tasks. External provisioners own
machine inventory, OS Images, subnets, IP allocation, and the execution of
deploy/release/network operations.

```text
dashboard --published HTTP contract--> OS Provisioning application
server projection --identity/status--> OS Provisioning policy
OS Provisioning intent --provider port--> anticorruption adapter --> MAAS
MAAS machine/link state --translation--> Network Configuration observation
```

The adapter is not a conformist boundary. Provider terms such as MAAS `AUTO`,
`LINK_UP`, `link_subnet`, and `unlink_subnet` do not enter the Swallow deployment
model. `AUTO` is translated to the read-only `provider_managed` observation;
`LINK_UP` is translated to the NIC configuration state `link_only`.

Swallow applies DHCP or static intent when the operator deploys an OS or changes a
Ready Server's Network. Release cleanup is coordinated through a durable
Provisioning Task. No continuous network reconciliation runs on deployed hosts.
