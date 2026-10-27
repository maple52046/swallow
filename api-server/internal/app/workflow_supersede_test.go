package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// uninstallLikeInput is the shape LaunchUninstall creates: one Task per target Server, which Create
// refuses while another Workflow still holds a target unless that Workflow is superseded.
func uninstallLikeInput(superseded []string) operationapp.CreateWorkflowInput {
	return operationapp.CreateWorkflowInput{
		Kind: operationdomain.WorkflowKindUninstallSlurm, IntentSummary: "Uninstall Slurm Platform lab",
		Definition: "platform-uninstall", DefinitionVersion: 1, SiteID: "site-id", PlatformID: "platform-id",
		TargetServerIDs: []string{"server-id"},
		Steps: []operationdomain.Task{{
			ID: "release-server-id", Kind: "release-os", Name: "Release lab-afde-mi308-3",
			Executor: operationdomain.RunnerKindProvisioner,
			Targets:  []operationdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
		}},
		SupersededOperationIDs: superseded,
	}
}

// Platform uninstall reuses decision 061's supersede: the deployment parked in requires_attention
// that holds the Platform's Servers is canceled once, even when several targets name it, and the
// uninstall is created at once instead of being refused; without it the refusal names the holder.
func TestUninstallSupersedesTheParkedDeployment(t *testing.T) {
	launcher, repo, controller := supersedeLauncher()
	superseded, err := supersedeBlockingWorkflows(context.Background(), launcher.operations, []string{"server-id", "server-id"})
	if err != nil {
		t.Fatalf("supersede error = %v", err)
	}
	if len(controller.canceled) != 1 || len(superseded) != 1 || superseded[0] != "op-parked" {
		t.Fatalf("canceled = %v, superseded = %v; want op-parked canceled once", controller.canceled, superseded)
	}
	if _, err := launcher.operations.Create(context.Background(), uninstallLikeInput(superseded)); err != nil {
		t.Fatalf("create after supersede error = %v", err)
	}
	if repo.created == nil {
		t.Fatal("the uninstall was not created")
	}

	launcher, repo, _ = supersedeLauncher()
	_, err = launcher.operations.Create(context.Background(), uninstallLikeInput(nil))
	if !errors.Is(err, operationdomain.ErrTargetsBusy) || repo.created != nil {
		t.Fatalf("create without supersede: err = %v, created = %v; want busy and nothing created", err, repo.created != nil)
	}
	for _, want := range []string{"already has active durable work", "op-parked", "Deploy k0s Platform test-amd-operator", "requires_attention"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("busy message %q does not name %q", err.Error(), want)
		}
	}
}
