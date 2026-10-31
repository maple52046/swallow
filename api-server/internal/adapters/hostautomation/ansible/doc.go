// Package ansible is the host automation adapter: it runs the pinned ansible-runner against
// swallow's shipped playbooks for the Ansible executor.
//
// It owns the playbook manifest (the immutable allowlist of playbook names and their paths under
// the content root), the runner invocation (private runtime directory, inventory, SSH options, and
// the login-user probe), the parsing of runner events into Task progress, and artifact retention.
// SSH keys, become passwords, and Workflow secret vars exist only inside the private runtime
// directory of one run and are deleted with it; they must never reach an artifact or a log.
//
// Workflow, Job, and Task semantics belong to the core workflow context, and the playbook content
// belongs to the modules; neither may be placed here.
package ansible
