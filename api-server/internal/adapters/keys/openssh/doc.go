// Package openssh implements SSH key material with golang.org/x/crypto/ssh: generating a key pair,
// deriving the public key of a supplied private key, and parsing and fingerprinting the OpenSSH
// public keys operators register.
//
// It is the only place that knows SSH key encodings; none of its types leak into the core sshkey
// context. It stores nothing: sealing and persisting keys belong to the store adapter.
package openssh
