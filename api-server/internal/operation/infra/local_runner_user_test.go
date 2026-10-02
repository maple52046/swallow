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

// TestSetConnectionVarsUsesImageDefaultUserWithoutProbing guards decision 039: a host whose
// deployed OS Image has a known default user logs in as exactly that user. The prober would accept
// only "ubuntu" here, so probing would wrongly pick it; the image user must win without a probe.
func TestSetConnectionVarsUsesImageDefaultUserWithoutProbing(t *testing.T) {
	runner := NewLocalRunner("runner", "/root", "/run", "/artifacts")
	runner.AttachUserProber(perHostFakeProber{ready: map[string]string{"10.0.0.1": "ubuntu"}})
	inventory := inventoryWith(map[string]string{"host-a": "10.0.0.1"})
	inventory["_meta"].(map[string]any)["hostvars"].(map[string]any)["host-a"].(map[string]any)["image_default_user"] = "rocky"

	runner.setConnectionVars(context.Background(), inventory,
		&operationdomain.AutomationConfiguration{SSHUser: "ubuntu", SSHPort: 22},
		operationdomain.AutomationCredential{SSHPrivateKey: testKeyPEM(t)})

	if got := userOf(inventory, "host-a"); got != "rocky" {
		t.Errorf("host-a ansible_user = %v, want the image default user rocky", got)
	}
}

// TestSetConnectionVarsPrefersServerDefaultUser guards decision 045: default_user is the effective
// Server Default User (a value set on the Server overrides the image's), so it wins over
// image_default_user and over any probe.
func TestSetConnectionVarsPrefersServerDefaultUser(t *testing.T) {
	runner := NewLocalRunner("runner", "/root", "/run", "/artifacts")
	runner.AttachUserProber(perHostFakeProber{ready: map[string]string{"10.0.0.1": "ubuntu"}})
	inventory := inventoryWith(map[string]string{"host-a": "10.0.0.1"})
	vars := inventory["_meta"].(map[string]any)["hostvars"].(map[string]any)["host-a"].(map[string]any)
	vars["default_user"] = "amd"
	vars["image_default_user"] = "ubuntu"

	runner.setConnectionVars(context.Background(), inventory,
		&operationdomain.AutomationConfiguration{SSHUser: "ubuntu", SSHPort: 22},
		operationdomain.AutomationCredential{SSHPrivateKey: testKeyPEM(t)})

	if got := userOf(inventory, "host-a"); got != "amd" {
		t.Errorf("host-a ansible_user = %v, want the Server Default User amd", got)
	}
}
