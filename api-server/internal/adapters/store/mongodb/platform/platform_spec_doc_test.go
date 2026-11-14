package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// A spec round-trips with its settings' JSON types and its machine preparation; a spec write is a
// compare-and-set on the generation; the status is written on its own.
func TestPlatformSpecRoundTripsAndWritesByGeneration(t *testing.T) {
	ctx := context.Background()
	repo, err := NewMongoPlatformRepo(platformTestDatabase(t), nil)
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

// Inline cloud-init user data in a spec is sealed at rest, in the spec, the applied spec, and the
// realizing spec alike, and opened on read; without the credential key it is refused rather than
// stored in the clear.
func TestPlatformSpecSealsInlineUserData(t *testing.T) {
	ctx := context.Background()
	db := platformTestDatabase(t)
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := NewMongoPlatformRepo(db, sealer)
	if err != nil {
		t.Fatal(err)
	}
	const userData = "#cloud-config\npassword: hunter2\n"
	spec := platformdomain.PlatformSpec{
		Desired:            platformdomain.PlatformPresent,
		MachinePreparation: platformdomain.MachinePreparation{Mode: platformdomain.MachinePreparationProvisionOS, UserDataMode: "replace", UserData: userData},
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Create(ctx, &platformdomain.Platform{
		ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm, Spec: &spec, Generation: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.WriteStatus(ctx, "p1", platformdomain.PlatformStatus{
		Applied:   &spec,
		Realizing: &platformdomain.PlatformRealizing{Generation: 1, WorkflowID: "w1", Spec: spec, StartedAt: now},
	}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	raw, err := db.Collection("platforms").FindOne(ctx, bson.M{"_id": "p1"}).Raw()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw.String(), "hunter2") {
		t.Fatalf("the stored document holds the user data in the clear: %s", raw)
	}
	stored, err := repo.FindByID(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Spec.MachinePreparation.UserData != userData || stored.Status.Applied.MachinePreparation.UserData != userData ||
		stored.Status.Realizing == nil || stored.Status.Realizing.WorkflowID != "w1" || stored.Status.Realizing.Spec.MachinePreparation.UserData != userData {
		t.Errorf("read back spec %+v status %+v", stored.Spec.MachinePreparation, stored.Status)
	}

	keyless, err := NewMongoPlatformRepo(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyless.WriteSpec(ctx, "p1", 1, spec); !errors.Is(err, errUserDataNeedsKey) {
		t.Errorf("keyless WriteSpec error = %v, want errUserDataNeedsKey", err)
	}
}

// A spec's member removals, generated templates, member template references, and an explicit
// default gateway round-trip; a member template record replaces the member's previous one.
func TestPlatformSpecStoresNodeGroupState(t *testing.T) {
	ctx := context.Background()
	repo, err := NewMongoPlatformRepo(platformTestDatabase(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway := false
	spec := platformdomain.PlatformSpec{
		Desired: platformdomain.PlatformPresent,
		Members: []platformdomain.PlatformMember{{ServerID: "s1", Roles: []string{"compute"}}},
		MachinePreparation: platformdomain.MachinePreparation{
			Mode: platformdomain.MachinePreparationProvisionOS, ImageID: "ubuntu/noble", DefaultGateway: &gateway,
			Assignments:        []platformdomain.MachineNetworkAssignment{{ServerID: "s1", TemplateID: "t-gpu"}},
			GeneratedTemplates: []platformdomain.GeneratedTemplate{{ID: "t-gpu", Name: "lab-gpu", IntegrationID: "maas-1", Group: "gpu"}},
		},
		MemberRemovals: []platformdomain.MemberRemoval{{
			ServerID: "s2", ReleaseServer: true, ReleaseOptions: platformdomain.ServerReleaseOptions{Erase: true},
			DrainTimeoutSeconds: 1800, Force: true,
		}},
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Create(ctx, &platformdomain.Platform{
		ID: "p1", SiteID: "site-1", Name: "lab", Type: "slurm", Spec: &spec, Generation: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stored, err := repo.FindByID(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	preparation := stored.Spec.MachinePreparation
	if preparation.DefaultGateway == nil || *preparation.DefaultGateway || preparation.Assignments[0].TemplateID != "t-gpu" ||
		len(preparation.GeneratedTemplates) != 1 || preparation.GeneratedTemplates[0] != spec.MachinePreparation.GeneratedTemplates[0] {
		t.Errorf("stored machine preparation = %+v", preparation)
	}
	if len(stored.Spec.MemberRemovals) != 1 || stored.Spec.MemberRemovals[0] != spec.MemberRemovals[0] {
		t.Errorf("stored member removals = %+v", stored.Spec.MemberRemovals)
	}

	first := platformdomain.MemberTemplateRecord{ServerID: "s1", TemplateID: "t-gpu", Digest: "d1", DeployedAt: now}
	other := platformdomain.MemberTemplateRecord{ServerID: "s3", TemplateID: "t-gpu", Digest: "d1", DeployedAt: now}
	if err := repo.RecordMemberTemplates(ctx, "p1", []platformdomain.MemberTemplateRecord{first, other}); err != nil {
		t.Fatalf("RecordMemberTemplates: %v", err)
	}
	second := platformdomain.MemberTemplateRecord{ServerID: "s1", TemplateID: "t-gpu", Digest: "d2", DeployedAt: now.Add(time.Hour)}
	if err := repo.RecordMemberTemplates(ctx, "p1", []platformdomain.MemberTemplateRecord{second}); err != nil {
		t.Fatalf("RecordMemberTemplates: %v", err)
	}
	stored, _ = repo.FindByID(ctx, "p1")
	records := map[string]platformdomain.MemberTemplateRecord{}
	for _, record := range stored.MemberTemplates {
		records[record.ServerID] = record
	}
	if len(stored.MemberTemplates) != 2 || records["s1"].Digest != "d2" || records["s3"].Digest != "d1" {
		t.Errorf("member templates = %+v", stored.MemberTemplates)
	}
	// s3 installed again from the machine preparation: its record goes.
	if err := repo.RecordMemberTemplates(ctx, "p1", []platformdomain.MemberTemplateRecord{{ServerID: "s3"}}); err != nil {
		t.Fatalf("RecordMemberTemplates: %v", err)
	}
	if stored, _ = repo.FindByID(ctx, "p1"); len(stored.MemberTemplates) != 1 || stored.MemberTemplates[0].ServerID != "s1" {
		t.Errorf("after s3 installed without a template: member templates = %+v", stored.MemberTemplates)
	}
}
