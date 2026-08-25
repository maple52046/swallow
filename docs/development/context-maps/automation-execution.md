# Automation execution context map

`operation` owns intent, execution status, leases, logs, and the site Automation
Configuration. It consumes server identity and policy through application ports.

```text
site identity ──> operation automation configuration
server projection ──> discovery inventory ──> operation runner
cluster policy ──> operation guard
operation dispatcher ──> ansible-runner ──> managed hosts
```

Ansible is an execution mechanism, not a bounded context or network service. MAAS and
Prometheus remain external site integrations. No component may call AWX or expose an AWX
webhook.
