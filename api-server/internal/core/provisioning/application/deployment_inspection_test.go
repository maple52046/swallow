package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

const testInspectionReason = "MAAS boots this Server with UEFI, but its storage layout has no EFI system partition."

// TestDiskDeploymentRefusesLayoutThatNeedsInspection guards lab-afde-mi308-3: a layout the
// provider flags as needing a new inspection failed every disk deployment in curtin after
// minutes of work. The deployment preflight that acceptance, platform validation, and Step
// execution share refuses it before any provider write; a memory deployment never partitions
// the disk and is still allowed.
func TestDiskDeploymentRefusesLayoutThatNeedsInspection(t *testing.T) {
	imageID := "ubuntu/noble"

	t.Run("disk deployment is refused before any write", func(t *testing.T) {
		uc, provider := setupNetworkWorkflow(1)
		provider.inspectionReason = testInspectionReason
		disk := false
		input := DeployServersInput{
			ServerIDs: []string{"a"},
			Settings:  DeploymentSettingsInput{ImageID: &imageID, Ephemeral: &disk},
		}
		if err := uc.Validate(context.Background(), input); !errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) ||
			!strings.Contains(err.Error(), testInspectionReason) {
			t.Fatalf("Validate error = %v, want a batch conflict carrying the inspection reason", err)
		}
		_, err := uc.Execute(context.Background(), input)
		if !errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) {
			t.Fatalf("Execute error = %v, want a batch conflict", err)
		}
		if len(provider.configureRequests) != 0 || len(provider.deployRequests) != 0 {
			t.Fatalf("provider writes = %d network, %d deploy; want none",
				len(provider.configureRequests), len(provider.deployRequests))
		}
	})

	t.Run("an omitted deploy target is a disk deployment", func(t *testing.T) {
		uc, provider := setupNetworkWorkflow(1)
		provider.inspectionReason = testInspectionReason
		err := uc.Validate(context.Background(), DeployServersInput{
			ServerIDs: []string{"a"},
			Settings:  DeploymentSettingsInput{ImageID: &imageID},
		})
		if !errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) {
			t.Fatalf("Validate error = %v, want a batch conflict", err)
		}
	})

	t.Run("memory deployment is allowed", func(t *testing.T) {
		uc, provider := setupNetworkWorkflow(1)
		provider.inspectionReason = testInspectionReason
		provider.ephemeralDeploy = true
		ram := true
		result, err := uc.Execute(context.Background(), DeployServersInput{
			ServerIDs: []string{"a"},
			Settings:  DeploymentSettingsInput{ImageID: &imageID, Ephemeral: &ram},
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(result.Accepted) != 1 || len(provider.deployRequests) != 1 || !provider.deployRequests[0].Ephemeral {
			t.Fatalf("accepted = %d, deploy requests = %#v; want one memory deployment",
				len(result.Accepted), provider.deployRequests)
		}
	})

	t.Run("the target-agnostic preflight does not block a memory deployment", func(t *testing.T) {
		_, provider := setupNetworkWorkflow(1)
		provider.inspectionReason = testInspectionReason
		repo := &networkWorkflowServerRepo{servers: map[string]*serverdomain.Server{"a": workflowServer("a")}}
		result, err := NewDeploymentTargetPreflightService(repo, networkWorkflowFactory{provider: provider}).
			Execute(context.Background(), []string{"a"})
		if err != nil {
			t.Fatalf("preflight: %v", err)
		}
		if !result.Valid || len(result.Issues) != 0 {
			t.Fatalf("preflight = %#v, want valid with no issues", result)
		}
	})
}
