// Package sshaccess is swallow's direct SSH access to hosts, outside an Ansible run: the logins
// that set a Server Default User and the Deployment Key commands other adapters run on a host
// (the libvirt adapter on a Hypervisor).
//
// Host keys are not verified here (the Ansible run keeps the authoritative check), so it is meant
// only for the internal networks swallow targets. Passwords and private keys are held for one call
// and never logged, wrapped into errors, or returned. Host automation that changes a host's
// configuration belongs to the Ansible executor, not here.
package sshaccess
