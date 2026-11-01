package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// A deploy is prepared before the Platform it creates is written (decision 069), so it may name a
// Platform that does not exist yet; an uninstall must name one that still exists.
func TestPolicyCheckerLetsADeployNameItsNewPlatform(t *testing.T) {
	checker := NewPolicyChecker(&deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{}}, nil)
	ctx := context.Background()
	if err := checker.CheckOperation(ctx, workflowdomain.WorkflowKindDeployKubernetes, "new-platform", nil); err != nil {
		t.Errorf("deploy of a new Platform: %v", err)
	}
	for _, kind := range []workflowdomain.WorkflowKind{workflowdomain.WorkflowKindUninstallKubernetes, workflowdomain.WorkflowKindUninstallSlurm} {
		if err := checker.CheckOperation(ctx, kind, "gone", nil); !errors.Is(err, workflowdomain.ErrPolicyConflict) {
			t.Errorf("%s of a missing Platform: err = %v, want ErrPolicyConflict", kind, err)
		}
	}
}
