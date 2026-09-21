package app

import (
	"context"
	"errors"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// recordingImageVerificationRepo captures RecordTarget calls for the internal finalize step test.
// Only RecordTarget is exercised; the rest satisfy the interface.
type recordingImageVerificationRepo struct {
	integrationID string
	imageID       string
	architecture  string
	target        provisioningdomain.DeployTarget
	evidence      provisioningdomain.OSImageVerificationEvidence
	calls         int
	err           error
	// Failure captures the mirror path so the failure-record step can be asserted independently.
	failureCalls  int
	failureTarget provisioningdomain.DeployTarget
	failure       provisioningdomain.OSImageVerificationFailure
}

func (r *recordingImageVerificationRepo) ListByIntegration(context.Context, string) ([]*provisioningdomain.OSImageVerification, error) {
	return nil, nil
}
func (r *recordingImageVerificationRepo) Find(context.Context, string, string, string) (*provisioningdomain.OSImageVerification, error) {
	return nil, nil
}
func (r *recordingImageVerificationRepo) RecordTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget, evidence provisioningdomain.OSImageVerificationEvidence,
) error {
	r.calls++
	r.integrationID, r.imageID, r.architecture, r.target, r.evidence = integrationID, imageID, architecture, target, evidence
	return r.err
}
func (r *recordingImageVerificationRepo) RecordFailedTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget, failure provisioningdomain.OSImageVerificationFailure,
) error {
	r.failureCalls++
	r.integrationID, r.imageID, r.architecture, r.failureTarget, r.failure = integrationID, imageID, architecture, target, failure
	return r.err
}
func (r *recordingImageVerificationRepo) Delete(context.Context, string, string, string) error {
	return nil
}
func (r *recordingImageVerificationRepo) DeleteByIntegration(context.Context, string) error {
	return nil
}

func recordVerificationInput() temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		OperationID: "op-1",
		Step: operationdomain.Task{
			ID: "record-verification", Kind: "record-image-verification",
			Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "srv-1"}},
			Parameters: map[string]any{
				"integrationId": "int-1", "imageId": "custom/rocky",
				"architecture": "amd64", "deployTarget": "ram",
			},
		},
	}
}

func TestRecordImageVerificationStepWritesEvidence(t *testing.T) {
	repo := &recordingImageVerificationRepo{}
	executor := platformWorkflowStepExecutor{imageVerifications: repo}

	result := executor.Execute(context.Background(), recordVerificationInput())

	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("status = %v, want succeeded", result.Status)
	}
	if repo.calls != 1 || repo.integrationID != "int-1" || repo.imageID != "custom/rocky" ||
		repo.architecture != "amd64" || repo.target != provisioningdomain.DeployTargetRAM {
		t.Fatalf("recorded = %+v, want the step identity for the ram target", repo)
	}
	if repo.evidence.OperationID != "op-1" || repo.evidence.ServerID != "srv-1" || repo.evidence.VerifiedAt.IsZero() {
		t.Errorf("evidence = %+v, want operation/server/time captured", repo.evidence)
	}
}

func TestRecordImageVerificationStepRetriesOnWriteFailure(t *testing.T) {
	repo := &recordingImageVerificationRepo{err: errors.New("mongo down")}
	executor := platformWorkflowStepExecutor{imageVerifications: repo}

	result := executor.Execute(context.Background(), recordVerificationInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed result", result)
	}
	// The proving deploy already succeeded; only the attestation write failed, so retry.
	if !result.Error.Retryable {
		t.Error("write failure must be retryable so verification is not lost after a successful deploy")
	}
}

func TestRecordImageVerificationStepRejectsBadDeployTarget(t *testing.T) {
	repo := &recordingImageVerificationRepo{}
	executor := platformWorkflowStepExecutor{imageVerifications: repo}
	input := recordVerificationInput()
	input.Step.Parameters["deployTarget"] = "memory"

	result := executor.Execute(context.Background(), input)

	if result.Status != operationdomain.TaskFailed || result.Error == nil || result.Error.Retryable {
		t.Fatalf("result = %+v, want a non-retryable failure for an invalid deploy target", result)
	}
	if repo.calls != 0 {
		t.Error("must not record a verification when the deploy target is invalid")
	}
}

func recordVerificationFailureInput() temporalworkflow.StepExecutionInput {
	in := recordVerificationInput()
	in.Step.ID = "record-verification-failure"
	in.Step.Kind = "record-image-verification-failure"
	in.Step.Parameters["reason"] = "proof failed"
	return in
}

// The failure-record step must persist the failed outcome and still succeed, so the verify
// Workflow can advance to the return-to-ready step; recording the failure is the step's whole job.
func TestRecordImageVerificationFailureStepWritesFailure(t *testing.T) {
	repo := &recordingImageVerificationRepo{}
	executor := platformWorkflowStepExecutor{imageVerifications: repo}

	result := executor.Execute(context.Background(), recordVerificationFailureInput())

	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("status = %v, want succeeded (the failure was recorded)", result.Status)
	}
	if repo.failureCalls != 1 || repo.integrationID != "int-1" || repo.imageID != "custom/rocky" ||
		repo.architecture != "amd64" || repo.failureTarget != provisioningdomain.DeployTargetRAM {
		t.Fatalf("recorded = %+v, want the step identity for the ram target", repo)
	}
	if repo.failure.OperationID != "op-1" || repo.failure.ServerID != "srv-1" ||
		repo.failure.FailedAt.IsZero() || repo.failure.Reason != "proof failed" {
		t.Errorf("failure = %+v, want operation/server/time/reason captured", repo.failure)
	}
}

// A failure-record write error is retryable: the failure fact is worth persisting so the operator
// is not misled into thinking the verification never ran.
func TestRecordImageVerificationFailureStepRetriesOnWriteFailure(t *testing.T) {
	repo := &recordingImageVerificationRepo{err: errors.New("mongo down")}
	executor := platformWorkflowStepExecutor{imageVerifications: repo}

	result := executor.Execute(context.Background(), recordVerificationFailureInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil || !result.Error.Retryable {
		t.Fatalf("result = %+v, want a retryable failed result", result)
	}
}

func TestArchitectureMatches(t *testing.T) {
	cases := []struct {
		server, image string
		want          bool
	}{
		{"amd64/generic", "amd64", true},
		{"amd64", "amd64", true},
		{"ARM64/generic", "arm64", true},
		{"amd64/generic", "arm64", false},
		{"", "amd64", false},
		{"amd64", "", false},
	}
	for _, tc := range cases {
		if got := architectureMatches(tc.server, tc.image); got != tc.want {
			t.Errorf("architectureMatches(%q,%q) = %v, want %v", tc.server, tc.image, got, tc.want)
		}
	}
}
