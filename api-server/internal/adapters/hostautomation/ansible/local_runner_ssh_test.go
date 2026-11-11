package ansible

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// A run's ssh checks host keys against the run's known_hosts and gives up within two minutes on a
// host that stops answering in the middle of a session, instead of leaving its Task running until
// TCP keepalive notices, about two hours later.
func TestLocalRunnerSSHGivesUpOnAHostThatStopsAnswering(t *testing.T) {
	root := t.TempDir()
	captured := filepath.Join(root, "ssh-args")
	command := filepath.Join(root, "runner")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$ANSIBLE_SSH_ARGS\" > %q\n", captured)
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewLocalRunner(command, root, filepath.Join(root, "runtime"), filepath.Join(root, "artifacts"))
	_, err := runner.Run(context.Background(), workflowdomain.RunnerInput{
		Operation: &workflowdomain.ExecutionOperation{
			ID: "operation-1", TargetServerIDs: []string{"server-1"},
			Execution: workflowdomain.Execution{RunID: "run-1", Playbook: "ping"},
		},
		Configuration: &workflowdomain.AutomationConfiguration{SSHUser: "ubuntu", SSHPort: 22, KnownHosts: "host ssh-ed25519 AAAA"},
		Credential:    workflowdomain.AutomationCredential{SSHPrivateKey: "private-key"},
		PlaybookPath:  "platform/slurm/playbooks/ping.yml",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}

	options := map[string]string{}
	fields := strings.Fields(string(raw))
	for i := 0; i < len(fields); i += 2 {
		if fields[i] != "-o" || i+1 == len(fields) {
			t.Fatalf("ANSIBLE_SSH_ARGS = %q, want only -o name=value options", raw)
		}
		name, value, _ := strings.Cut(fields[i+1], "=")
		options[name] = value
	}
	if !strings.HasSuffix(options["UserKnownHostsFile"], "known_hosts") {
		t.Errorf("UserKnownHostsFile = %q, want the run's known_hosts", options["UserKnownHostsFile"])
	}
	// A missing or malformed value parses as 0, which the check below rejects.
	interval, _ := strconv.Atoi(options["ServerAliveInterval"])
	count, _ := strconv.Atoi(options["ServerAliveCountMax"])
	if interval <= 0 || count <= 0 || interval*count > 120 {
		t.Errorf("ServerAliveInterval = %q, ServerAliveCountMax = %q; want ssh to give up on a silent host within 120 seconds",
			options["ServerAliveInterval"], options["ServerAliveCountMax"])
	}
}
