package ansible

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// cancelTestRun starts a run of a fake ansible-runner script that writes started once running,
// cancels it, and returns the run's error and how long the run took to return after the cancel.
func cancelTestRun(t *testing.T, script string, stopGrace time.Duration) (string, error, time.Duration) {
	t.Helper()
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	command := filepath.Join(root, "runner")
	if err := os.WriteFile(command, []byte(fmt.Sprintf(script, marker)), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewLocalRunner(command, root, filepath.Join(root, "runtime"), filepath.Join(root, "artifacts"))
	runner.stopGrace = stopGrace
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, workflowdomain.RunnerInput{
			Operation: &workflowdomain.ExecutionOperation{
				ID: "operation-1", TargetServerIDs: []string{"server-1"},
				Execution: workflowdomain.Execution{RunID: "run-1", Playbook: "ping"},
			},
			Configuration: &workflowdomain.AutomationConfiguration{SSHUser: "ubuntu", SSHPort: 22, KnownHosts: "host ssh-ed25519 AAAA"},
			Credential:    workflowdomain.AutomationCredential{SSHPrivateKey: "private-key"},
			PlaybookPath:  "platform/slurm/playbooks/ping.yml",
		})
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if content, _ := os.ReadFile(marker); string(content) == "started" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake runner never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	canceled := time.Now()
	cancel()
	select {
	case err := <-done:
		content, _ := os.ReadFile(marker)
		return string(content), err, time.Since(canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("the canceled run did not return")
		return "", nil, 0
	}
}

// A canceled run asks ansible-runner to stop with SIGTERM: ansible-runner then stops the playbook,
// which runs in a session of its own and would survive a kill of ansible-runner alone.
func TestLocalRunnerStopsACanceledRunWithSIGTERM(t *testing.T) {
	marker, err, _ := cancelTestRun(t, "#!/bin/sh\ntrap 'printf terminated > %[1]q; exit 254' TERM\nprintf started > %[1]q\nwhile :; do sleep 0.05; done\n", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if marker != "terminated" {
		t.Errorf("runner marker = %q, want terminated: the runner was not asked to stop", marker)
	}
}

// A runner that ignores SIGTERM is killed once the grace period ends.
func TestLocalRunnerKillsARunnerThatIgnoresSIGTERM(t *testing.T) {
	_, err, took := cancelTestRun(t, "#!/bin/sh\ntrap '' TERM\nprintf started > %[1]q\nwhile :; do sleep 0.05; done\n", 200*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
	if took > 5*time.Second {
		t.Errorf("the run returned %s after the cancel, want the kill after the 200ms grace", took)
	}
}
