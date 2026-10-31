package server

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/maple52046/swallow/internal/core/convergence"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// A document written before decision 071 has no generation: adoption takes it exactly once, and
// an API spec write races adoption only through the generation.
func TestSpecWritesAreConditionalOnTheGeneration(t *testing.T) {
	_, repo, db := claimTestDatabase(t)
	insertServers(t, db, bson.M{"_id": "s1"})
	ctx := context.Background()

	if _, err := repo.UpdateSpec(ctx, "missing", 0, serverdomain.ServerSpec{}); !errors.Is(err, serverdomain.ErrServerNotFound) {
		t.Fatalf("UpdateSpec(missing) error = %v, want ErrServerNotFound", err)
	}
	adopted, err := repo.AdoptSpec(ctx, "s1", serverdomain.ServerSpec{Tags: []string{"gpu"}})
	if err != nil || !adopted {
		t.Fatalf("AdoptSpec() = %v, %v, want adopted", adopted, err)
	}
	if again, err := repo.AdoptSpec(ctx, "s1", serverdomain.ServerSpec{Tags: []string{"other"}}); err != nil || again {
		t.Fatalf("second AdoptSpec() = %v, %v, want refused", again, err)
	}
	if _, err := repo.UpdateSpec(ctx, "s1", 0, serverdomain.ServerSpec{Tags: []string{"stale"}}); !errors.Is(err, serverdomain.ErrSpecConflict) {
		t.Fatalf("UpdateSpec(stale generation) error = %v, want ErrSpecConflict", err)
	}
	generation, err := repo.UpdateSpec(ctx, "s1", 1, serverdomain.ServerSpec{Tags: []string{"gpu", "ml"}})
	if err != nil || generation != 2 {
		t.Fatalf("UpdateSpec() = %d, %v, want generation 2", generation, err)
	}

	server, err := repo.FindByID(ctx, "s1")
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if server.Generation != 2 || !slices.Equal(server.Spec.Tags, []string{"gpu", "ml"}) {
		t.Errorf("stored spec = generation %d tags %v, want generation 2 tags [gpu ml]", server.Generation, server.Spec.Tags)
	}
}

// The reconcile Upsert rewrites the observation only: the spec, generation, conditions, and origin
// survive it, and a new machine is recorded as discovered unless stated otherwise.
func TestUpsertKeepsSpecAndConvergence(t *testing.T) {
	_, repo, _ := claimTestDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	server := &serverdomain.Server{
		ID:         "s1",
		Source:     serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: "m1"},
		Observed:   serverdomain.Observed{Hostname: "node-1", Tags: []string{"gpu", "amd-gpu"}, AutomaticTags: []string{"amd-gpu"}},
		LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Upsert(ctx, server); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if _, err := repo.AdoptSpec(ctx, "s1", serverdomain.ServerSpec{Tags: []string{"gpu"}}); err != nil {
		t.Fatalf("AdoptSpec() error = %v", err)
	}
	condition := convergence.Condition{
		Type: convergence.ConditionRealized, Status: convergence.ConditionTrue, Reason: convergence.ReasonConverged,
		LastTransitionTime: now, ObservedGeneration: 1,
	}
	if err := repo.SetConvergence(ctx, "s1", serverdomain.ServerConvergence{ObservedGeneration: 1, Conditions: []convergence.Condition{condition}}); err != nil {
		t.Fatalf("SetConvergence() error = %v", err)
	}

	server.Observed.Hostname = "node-1-renamed"
	server.Origin = serverdomain.OriginImported
	if err := repo.Upsert(ctx, server); err != nil {
		t.Fatalf("second Upsert() error = %v", err)
	}

	got, err := repo.FindByID(ctx, "s1")
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if got.Observed.Hostname != "node-1-renamed" {
		t.Errorf("hostname = %q, want the new observation", got.Observed.Hostname)
	}
	if got.Origin != serverdomain.OriginDiscovered {
		t.Errorf("origin = %q, want discovered kept from the first insert", got.Origin)
	}
	if got.Generation != 1 || !slices.Equal(got.Spec.Tags, []string{"gpu"}) {
		t.Errorf("spec = generation %d tags %v, want generation 1 tags [gpu]", got.Generation, got.Spec.Tags)
	}
	if !slices.Equal(got.Observed.AutomaticTags, []string{"amd-gpu"}) {
		t.Errorf("automatic tags = %v, want [amd-gpu]", got.Observed.AutomaticTags)
	}
	stored, ok := convergence.FindCondition(got.Convergence.Conditions, convergence.ConditionRealized)
	if got.Convergence.ObservedGeneration != 1 || !ok || stored.Reason != convergence.ReasonConverged || !stored.LastTransitionTime.Equal(now) {
		t.Errorf("convergence = %+v, want observedGeneration 1 and the realized condition", got.Convergence)
	}
}
