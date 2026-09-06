package temporalworkflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// AnsibleInventorySource creates the canonical inventory frozen for one Step attempt.
type AnsibleInventorySource interface {
	Inventory(ctx context.Context, siteID string) (map[string]any, error)
}

// AnsibleStepExecutor enqueues once and observes the standalone executor. An unknown
// executor outcome becomes requires_attention and is never automatically re-enqueued.
type AnsibleStepExecutor struct {
	executions     operationdomain.AnsibleExecutionRepository
	configurations operationdomain.AutomationConfigurationRepository
	inventory      AnsibleInventorySource
	artifactRoot   string
	poll           time.Duration
}

func NewAnsibleStepExecutor(executions operationdomain.AnsibleExecutionRepository, configurations operationdomain.AutomationConfigurationRepository, inventory AnsibleInventorySource, artifactRoot string, poll time.Duration) *AnsibleStepExecutor {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	return &AnsibleStepExecutor{executions: executions, configurations: configurations, inventory: inventory, artifactRoot: artifactRoot, poll: poll}
}

func (e *AnsibleStepExecutor) Execute(ctx context.Context, input StepExecutionInput) StepExecutionResult {
	playbook, _ := input.Step.Parameters["playbook"].(string)
	if playbook == "" {
		return failedStep("invalid_step", "The Ansible Step has no playbook.", false)
	}
	targets := make([]string, 0, len(input.Step.Targets))
	for _, target := range input.Step.Targets {
		if target.Kind == "server" {
			targets = append(targets, target.ID)
		}
	}
	configuration, err := e.configurations.FindBySiteID(ctx, input.SiteID)
	if err != nil {
		return failedStep("automation_configuration_unavailable", err.Error(), true)
	}
	if !configuration.Enabled || !configuration.HasCredential {
		return failedStep("automation_configuration_unavailable", "Site automation must be enabled and have a credential before this Step can run.", true)
	}
	inventory, err := e.inventory.Inventory(ctx, input.SiteID)
	if err != nil {
		return failedStep("inventory_snapshot_failed", err.Error(), true)
	}
	now := time.Now().UTC()
	extraVars, _ := input.Step.Parameters["extraVars"].(map[string]any)
	execution, err := e.executions.CreateOrGet(ctx, &operationdomain.AnsibleExecution{
		ID: uuid.NewString(), IdempotencyKey: fmt.Sprintf("%s/%s/%d", input.OperationID, input.Step.ID, input.Step.Attempt),
		OperationID: input.OperationID, Kind: input.Kind, PlatformID: input.PlatformID, StepID: input.Step.ID, Attempt: input.Step.Attempt,
		SiteID: input.SiteID, TargetServerIDs: targets, ResourceLeases: input.Leases, Playbook: playbook,
		ExtraVars: extraVars, Inventory: inventory, Configuration: &operationdomain.AutomationInputSnapshot{
			SSHUser: configuration.SSHUser, SSHPort: configuration.SSHPort, KnownHosts: configuration.KnownHosts,
		}, SecretRefs: input.Step.SecretRefs, RunID: uuid.NewString(),
		Status: operationdomain.AnsibleQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return failedStep("executor_queue_unavailable", "The Ansible execution queue is unavailable.", true)
	}
	reference := &operationdomain.ExternalExecutionReference{Provider: "ansible-runner", ID: execution.RunID, Generation: input.Step.Attempt}

	ticker := time.NewTicker(e.poll)
	defer ticker.Stop()
	for {
		execution, err = e.executions.FindByID(ctx, execution.ID)
		if err != nil {
			return failedStep("executor_observation_failed", "The Ansible execution could not be observed.", true)
		}
		activity.RecordHeartbeat(ctx, execution.Status)
		switch execution.Status {
		case operationdomain.AnsibleSucceeded:
			return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100, ExternalExecution: reference, Artifacts: e.artifacts(execution.RunID)}
		case operationdomain.AnsibleFailed:
			result := failedStep("ansible_failed", execution.StatusReason, true)
			result.ExternalExecution = reference
			result.Artifacts = e.artifacts(execution.RunID)
			return result
		case operationdomain.AnsibleCanceled:
			return StepExecutionResult{Status: operationdomain.TaskCanceled, ExternalExecution: reference}
		case operationdomain.AnsibleRequiresAttention:
			return StepExecutionResult{Status: operationdomain.TaskRequiresAttention, ExternalExecution: reference, Artifacts: e.artifacts(execution.RunID),
				Error: &operationdomain.NormalizedError{Code: "executor_outcome_unknown", Message: execution.StatusReason, Retryable: true}}
		}
		select {
		case <-ctx.Done():
			_ = e.executions.RequestCancel(context.Background(), execution.ID)
			return StepExecutionResult{Status: operationdomain.TaskCanceled, ExternalExecution: reference}
		case <-ticker.C:
		}
	}
}

func failedStep(code, message string, retryable bool) StepExecutionResult {
	if message == "" {
		message = "The Ansible Step failed."
	}
	return StepExecutionResult{Status: operationdomain.TaskFailed,
		Error: &operationdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}}
}

func (e *AnsibleStepExecutor) artifacts(runID string) []operationdomain.ArtifactMetadata {
	if e.artifactRoot == "" || filepath.Base(runID) != runID {
		return []operationdomain.ArtifactMetadata{}
	}
	type candidate struct {
		name      string
		mediaType string
	}
	candidates := []candidate{
		{name: "stdout", mediaType: "text/plain"},
		{name: "process.log", mediaType: "text/plain"},
		{name: "job_events", mediaType: "application/vnd.swallow.ansible-events"},
	}
	artifacts := make([]operationdomain.ArtifactMetadata, 0, len(candidates))
	for _, candidate := range candidates {
		path := filepath.Join(e.artifactRoot, runID, candidate.name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := info.Size()
		if info.IsDir() {
			size = 0
			_ = filepath.WalkDir(path, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil || entry.IsDir() {
					return nil
				}
				if fileInfo, statErr := entry.Info(); statErr == nil {
					size += fileInfo.Size()
				}
				return nil
			})
		}
		artifacts = append(artifacts, operationdomain.ArtifactMetadata{
			ID: candidate.name, Name: candidate.name, MediaType: candidate.mediaType,
			SizeBytes: size, RelativeKey: filepath.Join(runID, candidate.name), CreatedAt: info.ModTime().UTC(),
		})
	}
	return artifacts
}
