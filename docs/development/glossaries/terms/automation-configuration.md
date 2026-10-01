# Automation Configuration

An **Automation Configuration** is the single site-scoped configuration that enables
Swallow's embedded Ansible executor. It owns the SSH user and port, verified known-hosts,
and the mapping from each operation kind to a playbook name registered in the release
manifest.

Automation always logs in with the installation's Deployment Key; a Site cannot carry its
own SSH private key. The Site's only secret is the optional write-only become password,
encrypted at rest and represented to API consumers only by `hasCredential`. The effective
key source is reported as `credentialSource` (`deploymentKey`, or `none` before the
installation step has created the Deployment Key). Ansible may run for a Site when
automation is enabled and the Deployment Key exists.

The login user is resolved per host: when the Server's deployed OS Image has an effective
default user, that user is used exclusively; otherwise the site SSH user is tried first,
then swallow's built-in candidates (`cloud-user`, `ubuntu`). The site SSH user is therefore
a fallback, not a requirement.

Automation is not an external integration: Swallow owns the dispatcher, execution state,
logs, artifacts, and retention policy. Automation still respects provider-owned Server
Lock: target protection is checked at acceptance and immediately before runner startup,
because embedded Ansible can mutate a host without going through its provisioner.

See also: [Operation](operation.md), [Server Lock](server-lock.md),
[Deployment Key](deployment-key.md), [OS Image](os-image.md).

Change note: Updated 2026-10-01 so the site private key is an optional override of the
Deployment Key and the login user prefers the OS Image default user, per
[decision 039](../../../decisions/039-ssh-key-management-and-default-user.md). Updated
2026-10-02 to remove the site private-key override: automation uses only the Deployment
Key and a Site stores only its become password, per
[decision 041](../../../decisions/041-deployment-key-only-automation.md).
