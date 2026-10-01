# Automation execution context map

`operation` owns intent, execution status, leases, logs, and the site Automation
Configuration. It consumes server identity and policy through application ports.

```text
site identity ──> operation automation configuration
sshkey Deployment Key ──DeploymentKeySource port──> operation effective credential
server projection (image default user) ──> discovery inventory ──> operation runner
Platform policy ──> operation guard
operation dispatcher ──> ansible-runner ──> managed hosts
```

The `sshkey` feature (Access context, decision 039) is upstream of automation for key
material only: `operation` reads the Deployment Key's private key through its own
`DeploymentKeySource` port (an `internal/app` adapter) and uses it for every Site; the site
credential carries only the become password (decision 041). `operation` never sees SSH Key
records. The login user
comes from the Server projection's mirrored OS Image default user (provisioning), exposed
to the runner as the `image_default_user` inventory hostvar; the site `sshUser` and
built-in candidates are the fallback.

Ansible is an execution mechanism, not a bounded context or network service. MAAS and
Prometheus remain external site integrations. Execution is embedded: no component may
delegate it to an external automation controller or expose a controller webhook.
