package temporalworkflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// TestOperationWorkflowV1ParkedWaitRenewsLeasesRarely guards the history growth of an Operation
// parked in requires_attention. Renewing every leaseDuration/3 wrote about 11 history events
// every 30 seconds, so an Operation left parked for about 39 hours hit Temporal's 51,200-event
// limit and was terminated by the server, leaving its projection stuck in requires_attention.
// A parked wait now renews as it parks and then every parkedRenewInterval to parkedLeaseTTL,
// while a history recorded before the change keeps its original cadence.
func TestOperationWorkflowV1ParkedWaitRenewsLeasesRarely(t *testing.T) {
	tests := []struct {
		name          string
		legacyHistory bool
		parkFor       time.Duration
		wantRenewals  int
		wantTTL       time.Duration
	}{
		{
			// One renewal as it parks, then one at every 8h mark before the cancel at 719h.
			name: "parked wait renews about three times a day", parkFor: 30*24*time.Hour - time.Hour,
			wantRenewals: 90, wantTTL: parkedLeaseTTL,
		},
		{
			// LeaseDuration is one minute, so the legacy cadence renews at 20s, 40s, ... 100s.
			name: "history recorded before the change keeps the leaseDuration/3 cadence", legacyHistory: true,
			parkFor: 2*time.Minute - time.Second, wantRenewals: 5, wantTTL: time.Minute,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			if tc.legacyHistory {
				env.OnGetVersion("parked-lease-renewal", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
			}
			var mu sync.Mutex
			renewals := []LeaseRenewal{}
			states := []operationdomain.WorkflowStatus{}
			env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRequest) ([]operationdomain.ResourceLease, error) {
				return []operationdomain.ResourceLease{{ResourceKey: "server:a", Owner: "workflow", FencingToken: 1}}, nil
			}, activity.RegisterOptions{Name: ActivityAcquireLeases})
			env.RegisterActivityWithOptions(func(_ context.Context, input LeaseRenewal) error {
				mu.Lock()
				renewals = append(renewals, input)
				mu.Unlock()
				return nil
			}, activity.RegisterOptions{Name: ActivityRenewLeases})
			env.RegisterActivityWithOptions(func(_ context.Context, _ []operationdomain.ResourceLease) error { return nil },
				activity.RegisterOptions{Name: ActivityReleaseLeases})
			env.RegisterActivityWithOptions(func(_ context.Context, _ StepUpdate) error { return nil },
				activity.RegisterOptions{Name: ActivityUpdateStep})
			env.RegisterActivityWithOptions(func(_ context.Context, input StateUpdate) error {
				mu.Lock()
				states = append(states, input.Status)
				mu.Unlock()
				return nil
			}, activity.RegisterOptions{Name: ActivityUpdateState})
			env.RegisterActivityWithOptions(func(_ context.Context, _ StepExecutionInput) (StepExecutionResult, error) {
				return StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
					Code: "release_failed", Message: "The provisioner reported that release failed.", Retryable: true,
				}}, nil
			}, activity.RegisterOptions{Name: ActivityExecuteStep})
			env.RegisterDelayedCallback(env.CancelWorkflow, tc.parkFor)

			env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
				OperationID: "operation-parked", Kind: operationdomain.WorkflowKindReleaseOS,
				SiteID: "site-a", Definition: "os-release", DefinitionVersion: 1,
				LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
				Steps: []operationdomain.Task{{
					ID: "release-a", Kind: "release-os", Name: "Release a", Executor: operationdomain.RunnerKindProvisioner,
					Status: operationdomain.TaskPending, Attempt: 1,
				}},
			})
			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("parked workflow returned an error: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(states) == 0 || states[len(states)-1] != operationdomain.WorkflowCanceled {
				t.Errorf("Operation states = %v, want to end canceled", states)
			}
			if len(renewals) != tc.wantRenewals {
				t.Errorf("lease renewals while parked = %d, want %d", len(renewals), tc.wantRenewals)
			}
			for index, renewal := range renewals {
				if renewal.Duration != tc.wantTTL {
					t.Errorf("renewal %d Duration = %v, want %v", index, renewal.Duration, tc.wantTTL)
				}
			}
		})
	}
}
