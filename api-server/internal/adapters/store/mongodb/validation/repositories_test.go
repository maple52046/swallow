package validation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// A run round-trips with its results and parameters, and the list filters by member Server and
// activity, newest first.
func TestRunRepoRoundTripAndList(t *testing.T) {
	_, db := mongotest.Database(t, "validation_runs")
	repo, err := NewMongoRunRepo(db)
	if err != nil {
		t.Fatalf("NewMongoRunRepo() error = %v", err)
	}
	ctx := context.Background()
	requireIndex(t, ctx, repo.col, "validation_run_site_requested")
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	run := validationdomain.Run{
		ID: "r-1", SiteID: "site", Suite: validationdomain.RunSuite{ID: "slurm-acceptance", Source: validationdomain.SourceModule, Version: "0.1.0"},
		Checks:     []validationdomain.Check{{ID: "gpu-gres", Gating: true}},
		Target:     validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: "p", ServerIDs: []string{"login"}, MemberIDs: []string{"login", "c-1"}},
		Parameters: map[string]any{"nodes": []any{"a", "b"}}, Mode: validationdomain.ModeGate,
		Trigger: validationdomain.Trigger{Kind: validationdomain.TriggerDeployment, WorkflowID: "w"}, WorkflowID: "w",
		Status: validationdomain.StatusRunning, RequestedAt: now, ExpiresAt: now.Add(time.Hour),
		Results: []validationdomain.Result{{Check: "gpu-gres", Target: validationdomain.ResultTarget{Kind: "server", ID: "c-1"},
			Verdict: validationdomain.VerdictFailed, Measurements: []validationdomain.Measurement{{Name: "gpus", Value: 7, Unit: "count", Expected: "8"}},
			ObservedAt: now, TaskID: "t", Attempt: 2}},
	}
	older := validationdomain.Run{ID: "r-0", Target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: []string{"s-9"}},
		Status: validationdomain.StatusCompleted, Verdict: validationdomain.VerdictPassed, RequestedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	for _, r := range []validationdomain.Run{run, older} {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%s) error = %v", r.ID, err)
		}
	}
	got, err := repo.Get(ctx, "r-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got.RequestedAt, got.ExpiresAt = got.RequestedAt.UTC(), got.ExpiresAt.UTC()
	got.Results[0].ObservedAt = got.Results[0].ObservedAt.UTC()
	if !reflect.DeepEqual(got, run) {
		t.Errorf("Get() = %+v\nwant %+v", got, run)
	}
	scoped, total, err := repo.List(ctx, validationdomain.RunFilter{SiteID: "site"})
	if err != nil || total != 1 || scoped[0].ID != "r-1" {
		t.Errorf("List(site) = %v, %d, %v", scoped, total, err)
	}
	if _, total, err = repo.List(ctx, validationdomain.RunFilter{SiteID: "unknown"}); err != nil || total != 0 {
		t.Errorf("List(unknown site) total = %d, %v, want 0", total, err)
	}
	members, total, err := repo.List(ctx, validationdomain.RunFilter{ServerID: "c-1"})
	if err != nil || total != 1 || members[0].ID != "r-1" {
		t.Errorf("List(member) = %v, %d, %v", members, total, err)
	}
	active, total, err := repo.List(ctx, validationdomain.RunFilter{ActiveOnly: true})
	if err != nil || total != 1 || active[0].ID != "r-1" {
		t.Errorf("List(active) = %v, %d, %v", active, total, err)
	}
	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, validationdomain.ErrRunNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrRunNotFound", err)
	}
}

// A spec write increments the generation and is refused at a stale one; the status write leaves the
// spec alone.
func TestScheduleRepoSpecAndStatus(t *testing.T) {
	_, db := mongotest.Database(t, "validation_schedules")
	repo, err := NewMongoScheduleRepo(db)
	if err != nil {
		t.Fatalf("NewMongoScheduleRepo() error = %v", err)
	}
	ctx := context.Background()
	requireIndex(t, ctx, repo.col, "validation_schedule_site_name")
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	spec := validationdomain.ScheduleSpec{Name: "nightly", SuiteID: "host-baseline", ServerIDs: []string{"s-1"},
		Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleCron, Cron: "0 2 * * *", Timezone: "UTC"}, Enabled: true}
	if err := repo.Create(ctx, validationdomain.Schedule{ID: "sch", SiteID: "site", Generation: 1, Spec: spec, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	spec.Enabled = false
	if generation, err := repo.WriteSpec(ctx, "sch", 1, spec, "site", now); err != nil || generation != 2 {
		t.Fatalf("WriteSpec(1) = %d, %v, want generation 2", generation, err)
	}
	if _, err := repo.WriteSpec(ctx, "sch", 1, spec, "site", now); !errors.Is(err, validationdomain.ErrGenerationConflict) {
		t.Errorf("WriteSpec(stale) error = %v, want ErrGenerationConflict", err)
	}
	if err := repo.WriteStatus(ctx, "sch", validationdomain.ScheduleStatus{ObservedGeneration: 2, LastSkipReason: "busy"}); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}
	got, err := repo.Get(ctx, "sch")
	if err != nil || got.Spec.Enabled || got.Status.LastSkipReason != "busy" || got.Generation != 2 {
		t.Errorf("Get() = %+v, %v", got, err)
	}
	if err := repo.Create(ctx, validationdomain.Schedule{ID: "other", SiteID: "other-site", Generation: 1, Spec: spec, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}
	scoped, err := repo.List(ctx, validationdomain.ScheduleFilter{SiteID: "site"})
	if err != nil || len(scoped) != 1 || scoped[0].ID != "sch" {
		t.Errorf("List(site) = %+v, %v, want sch", scoped, err)
	}
	unknown, err := repo.List(ctx, validationdomain.ScheduleFilter{SiteID: "unknown"})
	if err != nil || len(unknown) != 0 {
		t.Errorf("List(unknown) = %+v, %v, want empty", unknown, err)
	}
	if used, err := repo.UsesSuite(ctx, "host-baseline"); err != nil || !used {
		t.Errorf("UsesSuite() = %v, %v, want true", used, err)
	}
}

func requireIndex(t *testing.T, ctx context.Context, col *mongo.Collection, name string) {
	t.Helper()
	cursor, err := col.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		if document["name"] == name {
			return
		}
	}
	t.Errorf("index %q not found", name)
}
