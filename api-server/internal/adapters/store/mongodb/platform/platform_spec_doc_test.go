package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// A spec round-trips with its settings' JSON types and its machine preparation; a spec write is a
// compare-and-set on the generation; the status is written on its own.
func TestPlatformSpecRoundTripsAndWritesByGeneration(t *testing.T) {
	ctx := context.Background()
	repo, err := NewMongoPlatformRepo(platformTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	ephemeral := true
	spec := platformdomain.PlatformSpec{
		Desired: platformdomain.PlatformPresent,
		Members: []platformdomain.PlatformMember{{ServerID: "s1", Roles: []string{"control-plane", "workloads"}}, {ServerID: "s2", Roles: []string{"worker"}}},
		MachinePreparation: platformdomain.MachinePreparation{
			Mode: platformdomain.MachinePreparationProvisionOS, ImageID: "ubuntu/noble", Ephemeral: &ephemeral,
			Assignments: []platformdomain.MachineNetworkAssignment{{ServerID: "s1", IPAddress: "10.0.0.5"}},
		},
		Settings: map[string]any{"k0sVersion": "v1.36.3+k0s.2", "apiVipPrefix": float64(24), "gpuOperator": map[string]any{"enabled": true}},
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Create(ctx, &platformdomain.Platform{
		ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes,
		Spec: &spec, Generation: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stored, err := repo.FindByID(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Generation != 1 || stored.Spec == nil || stored.Spec.Members[0].Roles[1] != "workloads" ||
		stored.Spec.Settings["apiVipPrefix"] != float64(24) || stored.Spec.Settings["gpuOperator"].(map[string]any)["enabled"] != true ||
		stored.Spec.MachinePreparation.ImageID != "ubuntu/noble" || stored.Spec.MachinePreparation.Ephemeral == nil ||
		stored.Spec.MachinePreparation.Assignments[0].IPAddress != "10.0.0.5" {
		t.Fatalf("stored spec = %+v (generation %d)", stored.Spec, stored.Generation)
	}

	changed := spec.Clone()
	changed.Members = append(changed.Members, platformdomain.PlatformMember{ServerID: "s3", Roles: []string{"worker"}})
	generation, err := repo.WriteSpec(ctx, "p1", 1, changed)
	if err != nil || generation != 2 {
		t.Fatalf("WriteSpec = %d, %v", generation, err)
	}
	if _, err := repo.WriteSpec(ctx, "p1", 1, changed); !errors.Is(err, platformdomain.ErrGenerationConflict) {
		t.Fatalf("stale WriteSpec: err = %v, want ErrGenerationConflict", err)
	}

	condition := convergence.Condition{Type: convergence.ConditionRealized, Status: convergence.ConditionFalse, Reason: convergence.ReasonAwaitingApproval, ObservedGeneration: 2, LastTransitionTime: now}
	if err := repo.WriteStatus(ctx, "p1", platformdomain.PlatformStatus{ObservedGeneration: 2, Conditions: []convergence.Condition{condition}, Applied: &spec}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	stored, _ = repo.FindByID(ctx, "p1")
	if len(stored.Spec.Members) != 3 || stored.Status.ObservedGeneration != 2 || stored.Status.Applied == nil ||
		len(stored.Status.Applied.Members) != 2 || stored.Status.Conditions[0].Reason != convergence.ReasonAwaitingApproval {
		t.Errorf("after writes: spec members %d, status %+v", len(stored.Spec.Members), stored.Status)
	}
}
