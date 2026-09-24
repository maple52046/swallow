package infra

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
)

// perHostFakeProber authenticates a specific (address, user) pair and rejects others, standing in
// for a live SSH handshake so the per-host resolution can be tested without a network.
type perHostFakeProber struct{ ready map[string]string } // address -> the one user that authenticates

func (p perHostFakeProber) Probe(_ context.Context, address string, _ int, user string, _ ssh.Signer) sshprobe.Outcome {
	if p.ready[address] == user {
		return sshprobe.Ready
	}
	return sshprobe.AuthFailed
}

func testKeyPEM(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(block))
}

func inventoryWith(hosts map[string]string) map[string]any {
	hostvars := map[string]any{}
	for id, addr := range hosts {
		hostvars[id] = map[string]any{"ansible_host": addr}
	}
	return map[string]any{"_meta": map[string]any{"hostvars": hostvars}}
}

func userOf(inventory map[string]any, id string) any {
	return inventory["_meta"].(map[string]any)["hostvars"].(map[string]any)[id].(map[string]any)["ansible_user"]
}

func TestSetConnectionVarsResolvesUserPerHost(t *testing.T) {
	runner := NewLocalRunner("runner", "/root", "/run", "/artifacts")
	// host-a authenticates as cloud-user; host-b authenticates as ubuntu. One Site user cannot serve
	// both, which is exactly what per-host resolution fixes.
	runner.AttachUserProber(perHostFakeProber{ready: map[string]string{
		"10.0.0.1": "cloud-user",
		"10.0.0.2": "ubuntu",
	}})
	inventory := inventoryWith(map[string]string{"host-a": "10.0.0.1", "host-b": "10.0.0.2"})

	runner.setConnectionVars(context.Background(), inventory,
		&operationdomain.AutomationConfiguration{SSHUser: "cloud-user", SSHPort: 22},
		operationdomain.AutomationCredential{SSHPrivateKey: testKeyPEM(t)})

	if got := userOf(inventory, "host-a"); got != "cloud-user" {
		t.Errorf("host-a ansible_user = %v, want cloud-user", got)
	}
	if got := userOf(inventory, "host-b"); got != "ubuntu" {
		t.Errorf("host-b ansible_user = %v, want ubuntu", got)
	}
}

func TestSetConnectionVarsFallsBackToSiteUserWithoutProber(t *testing.T) {
	// No prober attached: behaviour is unchanged (every host gets the Site user), which the other
	// LocalRunner tests and production's single-user sites rely on.
	runner := NewLocalRunner("runner", "/root", "/run", "/artifacts")
	inventory := inventoryWith(map[string]string{"host-a": "10.0.0.1"})

	runner.setConnectionVars(context.Background(), inventory,
		&operationdomain.AutomationConfiguration{SSHUser: "cloud-user", SSHPort: 22},
		operationdomain.AutomationCredential{SSHPrivateKey: testKeyPEM(t)})

	if got := userOf(inventory, "host-a"); got != "cloud-user" {
		t.Errorf("host-a ansible_user = %v, want cloud-user (site fallback)", got)
	}
}
