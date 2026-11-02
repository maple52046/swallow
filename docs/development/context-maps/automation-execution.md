# Automation execution context map

The `workflow` context owns intent, execution status, resource ownership, logs, and the site Automation
Configuration; its store is the only record of execution state ([decision 072](../../decisions/072-store-driven-workflow-execution.md) (store driven workflow execution)). It consumes server identity and policy through application ports.

```text
site identity ──> workflow automation configuration
sshkey Deployment Key ──DeploymentKeySource port──> workflow effective credential
server projection (image default user) ──> discovery inventory ──> workflow runner
Platform policy ──> workflow guard
worker workflow-dispatch ──> executor (Temporal) ──> ansible executor activity ──> ansible-runner ──> managed hosts
```

The `sshkey` feature (Access context, [decision 039](../../decisions/039-ssh-key-management-and-default-user.md) (ssh key management and default user)) is upstream of automation for key
material only: `workflow` reads the Deployment Key's private key through its own
`DeploymentKeySource` port (an `internal/app` adapter) and uses it for every Site; the site
credential carries only the become password (decision 039). `workflow` never sees SSH Key
records. The login user is the Server's effective Server Default User (decision 039): the
value an operator set on the Server, else the projection's mirrored OS Image default user
(provisioning). It reaches the runner as the `default_user` inventory hostvar (the image value
stays available as `image_default_user`); the site `sshUser` and built-in candidates are the
fallback. Setting a Server Default User is the one place outside a run where api-server logs in
to a host itself: a short SSH session that may install the Deployment Key with a one-time
password, then proves a key login.

Ansible is an execution mechanism, not a bounded context or network service. MAAS and
Prometheus remain external site integrations. Execution is embedded: no component may
delegate it to an external automation controller or expose a controller webhook.
