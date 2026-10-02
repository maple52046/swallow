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
records. The login user is the Server's effective Server Default User (decision 045): the
value an operator set on the Server, else the projection's mirrored OS Image default user
(provisioning). It reaches the runner as the `default_user` inventory hostvar (the image value
stays available as `image_default_user`); the site `sshUser` and built-in candidates are the
fallback. Setting a Server Default User is the one place outside a run where api-server logs in
to a host itself: a short SSH session that may install the Deployment Key with a one-time
password, then proves a key login.

Ansible is an execution mechanism, not a bounded context or network service. MAAS and
Prometheus remain external site integrations. Execution is embedded: no component may
delegate it to an external automation controller or expose a controller webhook.
