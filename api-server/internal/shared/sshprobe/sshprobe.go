// Package sshprobe resolves the working SSH login user for a host by trying candidate users with
// the automation key (the installation's Deployment Key, decision 041).
//
// It exists because different OS images use different default login users (ubuntu images use
// "ubuntu", swallow's custom images use "cloud-user"), while a Site has a single configured SSH
// user. swallow's dynamic inventory is already per-host, so the connection user is resolved per
// host rather than forced to one Site value. Since decision 039 a host whose deployed OS Image has
// a known default user uses exactly that user; the probe over candidates remains the fallback for
// hosts without one. The probe only sends a signature (never the private key) and does not verify
// the host key — the authoritative host-key check is the Ansible run's job (bounded
// trust-on-first-use over scanned keys); this probe answers only "which user logs in". It is
// shared by the operation runner and the app-layer wait-for-ssh step and must stay free of
// feature-specific rules beyond the candidate order.
package sshprobe

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Outcome classifies one authenticated SSH attempt.
type Outcome int

const (
	// Ready means the user authenticated: the host is reachable and can be logged into.
	Ready Outcome = iota
	// Unreachable means the TCP connect or pre-authentication handshake failed (transient at boot).
	Unreachable
	// AuthFailed means the host was reached and rejected authentication (publickey) for this user.
	AuthFailed
)

// builtinCandidates are the default login users tried after the Site-configured user. They cover
// swallow's known image conventions. The Site user is always tried first for the fast path and so
// existing single-user setups behave identically.
var builtinCandidates = []string{"cloud-user", "ubuntu"}

// Candidates returns the ordered, de-duplicated login users to try on one host. When the host's
// deployed OS Image has a known default user, it is the only candidate: a known user is
// deterministic, so a wrong value fails with a clear message instead of silently logging in as
// another account. Otherwise the Site user comes first (when set), then the built-ins. Empty
// entries are dropped.
func Candidates(imageDefaultUser, siteUser string) []string {
	if user := strings.TrimSpace(imageDefaultUser); user != "" {
		return []string{user}
	}
	ordered := append([]string{strings.TrimSpace(siteUser)}, builtinCandidates...)
	out := make([]string, 0, len(ordered))
	seen := make(map[string]bool, len(ordered))
	for _, user := range ordered {
		if user == "" || seen[user] {
			continue
		}
		seen[user] = true
		out = append(out, user)
	}
	return out
}

// Prober performs one authenticated SSH attempt against a host as a given user. It is an interface
// so callers can be tested without a live SSH server; production uses DefaultProber.
type Prober interface {
	Probe(ctx context.Context, address string, port int, user string, signer ssh.Signer) Outcome
}

// DefaultProber authenticates with x/crypto/ssh using the automation key.
type DefaultProber struct{}

// Probe dials the host and attempts a publickey handshake as user. It returns AuthFailed only when
// the host was reached and the handshake reached (and was rejected at) authentication; connection
// resets, EOFs, and timeouts during boot are Unreachable.
func (DefaultProber) Probe(ctx context.Context, address string, port int, user string, signer ssh.Signer) Outcome {
	dialer := net.Dialer{Timeout: 4 * time.Second}
	connection, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, strconv.Itoa(portOrDefault(port))))
	if dialErr != nil {
		return Unreachable
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(8 * time.Second))
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         6 * time.Second,
	}
	clientConn, _, _, handshakeErr := ssh.NewClientConn(connection, address, config)
	if handshakeErr != nil {
		if isAuthError(handshakeErr) {
			return AuthFailed
		}
		return Unreachable
	}
	_ = clientConn.Close()
	return Ready
}

// isAuthError reports whether an SSH handshake error is an authentication rejection rather than a
// pre-authentication transport failure. x/crypto/ssh reports a rejected publickey as an
// "unable to authenticate" handshake error.
func isAuthError(err error) bool {
	return strings.Contains(err.Error(), "unable to authenticate")
}

// Resolve tries candidates in order and returns the first user that authenticates. When none
// authenticate it returns AuthFailed if the host was reachable but rejected every candidate, or
// Unreachable if the host could not be reached at all.
func Resolve(ctx context.Context, prober Prober, address string, port int, signer ssh.Signer, candidates []string) (string, Outcome) {
	reachedAuth := false
	for _, user := range candidates {
		switch prober.Probe(ctx, address, port, user, signer) {
		case Ready:
			return user, Ready
		case AuthFailed:
			reachedAuth = true
		}
	}
	if reachedAuth {
		return "", AuthFailed
	}
	return "", Unreachable
}

// ParseSigner parses a PEM-encoded private key into an ssh.Signer for use with the probes.
func ParseSigner(privateKeyPEM string) (ssh.Signer, error) {
	return ssh.ParsePrivateKey([]byte(privateKeyPEM))
}

func portOrDefault(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}
