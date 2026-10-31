// Package sshkey is the MongoDB store of SSH Keys (`ssh_keys`) and their provisioner sync records
// (`ssh_key_provider_syncs`). The Deployment Key's private key is sealed with shared/secret before
// it is written; every ordinary read projects it away, and only the explicit Deployment private key
// read opens it.
//
// Key generation and parsing belong to the keys adapter, and the sync policy to the core sshkey
// context.
package sshkey
