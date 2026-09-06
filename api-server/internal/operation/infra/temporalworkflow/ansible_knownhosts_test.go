package temporalworkflow

import (
	"context"
	"strings"
	"testing"
)

type fakeHostKeyScanner struct {
	out       string
	gotAddrs  []string
	gotPort   int
	callCount int
}

func (f *fakeHostKeyScanner) Scan(_ context.Context, addresses []string, port int) (string, error) {
	f.callCount++
	f.gotAddrs = addresses
	f.gotPort = port
	return f.out, nil
}

// The Ansible run must trust exactly the hosts it targets, using freshly scanned keys plus
// any operator-configured static entries — so a provision-OS deploy (unknowable host keys)
// configures without a hand-maintained known_hosts.
func TestResolveKnownHostsScansTargetsAndMergesStatic(t *testing.T) {
	inventory := map[string]any{
		"_meta": map[string]any{"hostvars": map[string]any{
			"s1": map[string]any{"ansible_host": "10.0.0.1"},
			"s2": map[string]any{"ansible_host": "10.0.0.2"},
			"s3": map[string]any{"ansible_host": "10.0.0.3"}, // present but not a target
		}},
	}
	scanner := &fakeHostKeyScanner{out: "10.0.0.1 ssh-ed25519 AAAA\n10.0.0.2 ssh-ed25519 BBBB"}
	executor := &AnsibleStepExecutor{hostKeys: scanner}

	knownHosts, err := executor.resolveKnownHosts(
		context.Background(), inventory, []string{"s1", "s2"}, 2222, "203.0.113.9 ssh-ed25519 STATIC")
	if err != nil {
		t.Fatalf("resolveKnownHosts: %v", err)
	}
	if scanner.gotPort != 2222 {
		t.Fatalf("scanned port = %d, want the configured 2222", scanner.gotPort)
	}
	if len(scanner.gotAddrs) != 2 || scanner.gotAddrs[0] != "10.0.0.1" || scanner.gotAddrs[1] != "10.0.0.2" {
		t.Fatalf("scanned addresses = %v, want only the two targets", scanner.gotAddrs)
	}
	for _, want := range []string{
		"10.0.0.1 ssh-ed25519 AAAA",
		"10.0.0.2 ssh-ed25519 BBBB",
		"203.0.113.9 ssh-ed25519 STATIC",
	} {
		if !strings.Contains(knownHosts, want) {
			t.Fatalf("known_hosts missing %q:\n%s", want, knownHosts)
		}
	}
}

// A run with targets whose keys cannot be learned and with no static fallback must fail
// clearly rather than proceed unable to verify any host.
func TestResolveKnownHostsFailsWhenNothingLearnedAndNoStatic(t *testing.T) {
	inventory := map[string]any{"_meta": map[string]any{"hostvars": map[string]any{
		"s1": map[string]any{"ansible_host": "10.0.0.1"},
	}}}
	executor := &AnsibleStepExecutor{hostKeys: &fakeHostKeyScanner{out: ""}}

	if _, err := executor.resolveKnownHosts(context.Background(), inventory, []string{"s1"}, 22, ""); err == nil {
		t.Fatal("expected an error when no host keys can be learned and no static known_hosts is set")
	}
}
