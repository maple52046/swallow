package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/sshprobe"
)

// fakeAutomationConfigRepo serves a fixed automation configuration and credential for the
// wait-for-ssh readiness tests. Only FindBySiteID and Credential are exercised.
type fakeAutomationConfigRepo struct {
	config     operationdomain.AutomationConfiguration
	credential operationdomain.AutomationCredential
}

func (r fakeAutomationConfigRepo) FindBySiteID(_ context.Context, _ string) (*operationdomain.AutomationConfiguration, error) {
	config := r.config
	return &config, nil
}
func (r fakeAutomationConfigRepo) Upsert(_ context.Context, _ *operationdomain.AutomationConfiguration) error {
	return nil
}
func (r fakeAutomationConfigRepo) ReplaceCredential(_ context.Context, _ string, _ operationdomain.AutomationCredential) error {
	return nil
}
func (r fakeAutomationConfigRepo) Credential(_ context.Context, _ string) (operationdomain.AutomationCredential, error) {
	return r.credential, nil
}

// fakeReadinessServerRepo returns deployed Server projections by id; the rest of the interface is
// embedded so an unexpected call panics.
type fakeReadinessServerRepo struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
}

func (r fakeReadinessServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := r.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

// fakeSSHProber returns a programmed outcome per address, standing in for a live SSH handshake.
// It ignores the candidate user, so a Ready address authenticates on the first candidate and an
// AuthFailed address rejects every candidate (which sshprobe.Resolve reports as AuthFailed).
type fakeSSHProber struct{ outcomes map[string]sshprobe.Outcome }

func (p fakeSSHProber) Probe(_ context.Context, address string, _ int, _ string, _ ssh.Signer) sshprobe.Outcome {
	return p.outcomes[address]
}

func deployedReadinessServer(id, name, address string) *serverdomain.Server {
	return &serverdomain.Server{
		ID:           id,
		Observed:     serverdomain.Observed{Hostname: name, Addresses: []string{address}},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}
}

// testAutomationKey returns a valid, parseable OpenSSH private key so waitForSSH can build a signer.
func testAutomationKey(t *testing.T) string {
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

func readinessInput() temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		SiteID: "site-1",
		Step: operationdomain.Task{
			ID: "wait-for-ssh", Kind: "wait-for-ssh",
			Targets: []operationdomain.ResourceReference{
				{Kind: "server", ID: "srv-a"},
				{Kind: "server", ID: "srv-b"},
			},
		},
	}
}

func TestWaitForSSHSucceedsWhenEveryHostAuthenticates(t *testing.T) {
	executor := platformWorkflowStepExecutor{
		servers: fakeReadinessServerRepo{servers: map[string]*serverdomain.Server{
			"srv-a": deployedReadinessServer("srv-a", "lab-a", "192.0.2.1"),
			"srv-b": deployedReadinessServer("srv-b", "lab-b", "192.0.2.2"),
		}},
		configurations: fakeAutomationConfigRepo{
			config:     operationdomain.AutomationConfiguration{SSHUser: "cloud-user", SSHPort: 22},
			credential: operationdomain.AutomationCredential{SSHPrivateKey: testAutomationKey(t)},
		},
		sshProber: fakeSSHProber{outcomes: map[string]sshprobe.Outcome{
			"192.0.2.1": sshprobe.Ready,
			"192.0.2.2": sshprobe.Ready,
		}},
		poll: time.Millisecond,
	}

	result := executor.waitForSSH(context.Background(), readinessInput())

	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("status = %v (err=%+v), want succeeded", result.Status, result.Error)
	}
}

func TestWaitForSSHFailsFastOnAuthenticationRejection(t *testing.T) {
	executor := platformWorkflowStepExecutor{
		servers: fakeReadinessServerRepo{servers: map[string]*serverdomain.Server{
			"srv-a": deployedReadinessServer("srv-a", "lab-a", "192.0.2.1"),
			"srv-b": deployedReadinessServer("srv-b", "lab-b", "192.0.2.2"),
		}},
		configurations: fakeAutomationConfigRepo{
			config:     operationdomain.AutomationConfiguration{SSHUser: "cloud-user", SSHPort: 22},
			credential: operationdomain.AutomationCredential{SSHPrivateKey: testAutomationKey(t)},
		},
		sshProber: fakeSSHProber{outcomes: map[string]sshprobe.Outcome{
			"192.0.2.1": sshprobe.Ready,
			"192.0.2.2": sshprobe.AuthFailed,
		}},
		// A tiny grace makes the persisted rejection fail fast on the first pass instead of waiting.
		sshAuthGrace: time.Nanosecond,
		poll:         time.Millisecond,
	}

	result := executor.waitForSSH(context.Background(), readinessInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed result", result)
	}
	if result.Error.Code != "ssh_authentication_failed" {
		t.Errorf("code = %q, want ssh_authentication_failed", result.Error.Code)
	}
	if result.Error.Stage != "ssh_authentication" {
		t.Errorf("stage = %q, want ssh_authentication", result.Error.Stage)
	}
	if !result.Error.Retryable {
		t.Errorf("retryable = false, want true so the operator can fix the key and retry")
	}
	// The message must name the rejecting host and the SSH user so the operator fixes the credential.
	if !strings.Contains(result.Error.Message, "lab-b") || !strings.Contains(result.Error.Message, "cloud-user") {
		t.Errorf("message = %q, want it to name lab-b and cloud-user", result.Error.Message)
	}
}

func TestWaitForSSHRejectsUnparseableKey(t *testing.T) {
	executor := platformWorkflowStepExecutor{
		servers: fakeReadinessServerRepo{servers: map[string]*serverdomain.Server{
			"srv-a": deployedReadinessServer("srv-a", "lab-a", "192.0.2.1"),
			"srv-b": deployedReadinessServer("srv-b", "lab-b", "192.0.2.2"),
		}},
		configurations: fakeAutomationConfigRepo{
			config:     operationdomain.AutomationConfiguration{SSHUser: "cloud-user", SSHPort: 22},
			credential: operationdomain.AutomationCredential{SSHPrivateKey: "not-a-valid-key"},
		},
		poll: time.Millisecond,
	}

	result := executor.waitForSSH(context.Background(), readinessInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed result", result)
	}
	if result.Error.Code != "ssh_key_invalid" || result.Error.Retryable {
		t.Errorf("error = %+v, want non-retryable ssh_key_invalid", result.Error)
	}
}
