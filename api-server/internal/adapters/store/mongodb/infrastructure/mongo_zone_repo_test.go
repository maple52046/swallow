package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
)

// A spec write increments the generation in the same update, and the controller's status round
// trips without touching the spec. Skipped without SWALLOW_TEST_MONGO_URI.
func TestZoneRepo_SpecAndStatusRoundTrip(t *testing.T) {
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx := context.Background()
	client, err := storemongo.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_zone_repo_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	repo, err := NewMongoZoneRepo(db)
	if err != nil {
		t.Fatalf("NewMongoZoneRepo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.Create(ctx, &infradomain.Zone{ID: "z1", SiteID: "s", Generation: 1, Name: "rack-a", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, &infradomain.Zone{ID: "z2", SiteID: "s", Generation: 1, Name: "rack-a"}); !errors.Is(err, infradomain.ErrZoneNameTaken) {
		t.Fatalf("duplicate Create: err = %v, want ErrZoneNameTaken", err)
	}

	zone := &infradomain.Zone{ID: "z1", Name: "rack-b", Description: "moved", UpdatedAt: now}
	if err := repo.UpdateSpec(ctx, zone); err != nil || zone.Generation != 2 {
		t.Fatalf("UpdateSpec: generation %d, err %v; want 2", zone.Generation, err)
	}
	status := infradomain.GroupStatus{
		ObservedGeneration: 2,
		Conditions:         []convergence.Condition{{Type: convergence.ConditionRealized, Status: convergence.ConditionTrue, Reason: convergence.ReasonConverged, LastTransitionTime: now, ObservedGeneration: 2}},
		Provisioners: []infradomain.GroupRealization{{
			TargetStatus: convergence.TargetStatus{TargetID: "maas-a", ObservedGeneration: 2, ObservedAt: &now},
			ProviderName: "rack-b",
		}},
	}
	if err := repo.SetStatus(ctx, "z1", status); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := repo.FindByName(ctx, "s", "rack-b")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if got.Generation != 2 || got.Description != "moved" || got.Status.ObservedGeneration != 2 {
		t.Errorf("zone = %+v", got)
	}
	r := got.Status.Realization("maas-a")
	if r.ProviderName != "rack-b" || r.ObservedAt == nil || !r.ObservedAt.Equal(now) {
		t.Errorf("realization = %+v, want rack-b observed at %s", r, now)
	}
	if !convergence.IsTrue(got.Status.Conditions, convergence.ConditionRealized) {
		t.Errorf("conditions = %+v, want realized", got.Status.Conditions)
	}
}

// AdoptSpec stores the observed description only on a Zone that has no generation yet, whether the
// field is missing (a migrated document) or zero. Skipped without SWALLOW_TEST_MONGO_URI.
func TestZoneRepo_AdoptSpecOnlyBeforeTheFirstGeneration(t *testing.T) {
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx := context.Background()
	client, err := storemongo.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_zone_adopt_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	repo, err := NewMongoZoneRepo(db)
	if err != nil {
		t.Fatalf("NewMongoZoneRepo: %v", err)
	}
	if _, err := db.Collection("zones").InsertOne(ctx, map[string]any{
		"_id": "migrated", "siteId": "s", "name": "default", "description": "swallow's copy",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)

	if err := repo.AdoptSpec(ctx, "migrated", "", now); err != nil {
		t.Fatalf("AdoptSpec: %v", err)
	}
	if err := repo.AdoptSpec(ctx, "migrated", "a later observation", now); err != nil {
		t.Fatalf("second AdoptSpec: %v", err)
	}

	zone, err := repo.FindByID(ctx, "migrated")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if zone.Generation != 1 || zone.Description != "" {
		t.Errorf("zone = generation %d, description %q; want generation 1 with the first observation", zone.Generation, zone.Description)
	}
}
