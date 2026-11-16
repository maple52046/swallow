package validation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// A user suite round-trips with its author, sudo, and starter level; a replace applies only at the
// revision it read and never over a retire; each revision is kept once.
func TestSuiteRepoRoundTripAndRevisions(t *testing.T) {
	_, db := mongotest.Database(t, "validation_suites")
	repo := NewMongoSuiteRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	suite, err := validationdomain.NewUserSuite("u-1", validationdomain.UserSuiteInput{Name: "id", Description: "who am I", Script: "#!/bin/sh\nid\n",
		Sudo: true, StarterLevel: validationdomain.StarterMember, TimeoutSeconds: 60, Impact: validationdomain.ImpactObserve},
		validationdomain.Author{UserID: "user-alice", Username: "alice"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, suite); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.AddRevision(ctx, suite.CurrentRevision(now)); err != nil {
		t.Fatalf("AddRevision() error = %v", err)
	}
	got, err := repo.Get(ctx, suite.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got.CreatedAt, got.UpdatedAt = got.CreatedAt.UTC(), got.UpdatedAt.UTC()
	if !reflect.DeepEqual(got, suite) {
		t.Errorf("Get() = %+v\nwant %+v", got, suite)
	}

	next, err := suite.Replace(validationdomain.UserSuiteInput{Name: "id", Script: "#!/bin/sh\nid -u\n", Sudo: true,
		StarterLevel: validationdomain.StarterMember, TimeoutSeconds: 60, Impact: validationdomain.ImpactObserve}, "user-alice", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(ctx, next, 1); err != nil {
		t.Fatalf("Replace(at revision 1) error = %v", err)
	}
	if err := repo.Replace(ctx, next, 1); !errors.Is(err, validationdomain.ErrRevisionConflict) {
		t.Errorf("Replace(at a stale revision) error = %v, want ErrRevisionConflict", err)
	}
	if err := repo.AddRevision(ctx, next.CurrentRevision(now.Add(time.Minute))); err != nil {
		t.Fatalf("AddRevision(2) error = %v", err)
	}
	if err := repo.AddRevision(ctx, next.CurrentRevision(now)); !errors.Is(err, validationdomain.ErrRevisionConflict) {
		t.Errorf("AddRevision(2 again) error = %v, want ErrRevisionConflict", err)
	}
	for revision, script := range map[int]string{1: suite.Script, 2: next.Script} {
		kept, err := repo.Revision(ctx, suite.ID, revision)
		if err != nil || kept.Script != script || kept.Revision != revision || !kept.Sudo || kept.StarterLevel != validationdomain.StarterMember ||
			kept.TimeoutSeconds != 60 || kept.Impact != validationdomain.ImpactObserve {
			t.Errorf("Revision(%d) = %+v, %v", revision, kept, err)
		}
	}
	if _, err := repo.Revision(ctx, suite.ID, 3); !errors.Is(err, validationdomain.ErrRevisionNotFound) {
		t.Errorf("Revision(3) error = %v, want ErrRevisionNotFound", err)
	}

	if err := repo.Retire(ctx, suite.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Retire() error = %v", err)
	}
	if err := repo.Replace(ctx, next, 2); !errors.Is(err, validationdomain.ErrSuiteImmutable) {
		t.Errorf("Replace(retired) error = %v, want ErrSuiteImmutable", err)
	}
	if retired, err := repo.Get(ctx, suite.ID); err != nil || !retired.Retired || retired.Script != next.Script {
		t.Errorf("Get(retired) = %+v, %v, want the revised suite retired", retired, err)
	}
	if err := repo.Retire(ctx, "missing", now); !errors.Is(err, validationdomain.ErrSuiteNotFound) {
		t.Errorf("Retire(missing) error = %v, want ErrSuiteNotFound", err)
	}
	if err := repo.Replace(ctx, validationdomain.Suite{ID: "missing", Source: validationdomain.SourceUser}, 1); !errors.Is(err, validationdomain.ErrSuiteNotFound) {
		t.Errorf("Replace(missing) error = %v, want ErrSuiteNotFound", err)
	}
}

// A suite written before user suites ran as their starter reads its retired runAs as sudo (root is
// sudo), its missing starter level as owner, and its missing author id as none; its next write drops
// runAs.
func TestSuiteRepoReadsTheRetiredRunAs(t *testing.T) {
	_, db := mongotest.Database(t, "validation_suites")
	repo := NewMongoSuiteRepo(db)
	ctx := context.Background()
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	for id, runAs := range map[string]string{"root-suite": "root", "automation-suite": "automation-user"} {
		if _, err := db.Collection(suitesCollection).InsertOne(ctx, bson.M{
			"_id": id, "name": id, "script": "#!/bin/sh\nexit 0\n", "scriptSha256": "abc", "runAs": runAs,
			"timeoutSeconds": 600, "impact": "workload", "revision": 3, "retired": false, "createdAt": now, "updatedAt": now, "createdBy": "admin",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for id, wantSudo := range map[string]bool{"root-suite": true, "automation-suite": false} {
		suite, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
		if suite.Sudo != wantSudo || suite.StarterLevel != validationdomain.StarterOwner || suite.CreatedByID != "" || suite.CreatedBy != "admin" || suite.Revision != 3 {
			t.Errorf("Get(%s) = sudo %v, starter level %q, author %q (%q), revision %d; want sudo %v, owner, no author id",
				id, suite.Sudo, suite.StarterLevel, suite.CreatedByID, suite.CreatedBy, suite.Revision, wantSudo)
		}
	}
	suite, _ := repo.Get(ctx, "automation-suite")
	suite.Name = "renamed"
	if err := repo.Replace(ctx, suite, 3); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	var raw bson.M
	if err := db.Collection(suitesCollection).FindOne(ctx, bson.M{"_id": "automation-suite"}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if _, kept := raw["runAs"]; kept || raw["sudo"] != false || raw["starterLevel"] != "owner" {
		t.Errorf("the rewritten suite = %v, want sudo and starterLevel and no runAs", raw)
	}
}

// A run records its starter's id and the account it runs as, and the list keeps one starter's runs.
func TestRunRepoRecordsTheStarter(t *testing.T) {
	_, db := mongotest.Database(t, "validation_runs")
	repo, err := NewMongoRunRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requireIndex(t, ctx, repo.col, "validation_run_requester")
	now := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	mine := validationdomain.Run{ID: "r-1", SiteID: "site", Suite: validationdomain.RunSuite{ID: "u-1", Source: validationdomain.SourceUser, Revision: 2,
		ScriptSHA256: "abc", Script: "#!/bin/sh\nid\n"}, Checks: []validationdomain.Check{{ID: "script"}},
		Target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: []string{"s-1"}}, Parameters: map[string]any{},
		Mode: validationdomain.ModeReport, Trigger: validationdomain.Trigger{Kind: validationdomain.TriggerManual}, WorkflowID: "w-1",
		Status: validationdomain.StatusPending, RequestedBy: "alice", RequestedByID: "user-alice",
		RunAs: &validationdomain.RunAccount{Username: "alice", UIDNumber: 10005, Sudo: true}, RequestedAt: now, ExpiresAt: now.Add(time.Hour)}
	theirs := validationdomain.Run{ID: "r-2", Target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: []string{"s-1"}},
		Status: validationdomain.StatusPending, RequestedBy: "validation-schedule", RequestedAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour)}
	for _, run := range []validationdomain.Run{mine, theirs} {
		if err := repo.Create(ctx, run); err != nil {
			t.Fatalf("Create(%s) error = %v", run.ID, err)
		}
	}
	got, err := repo.Get(ctx, "r-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestedBy != "alice" || got.RequestedByID != "user-alice" || got.RunAs == nil || *got.RunAs != *mine.RunAs ||
		got.Suite != mine.Suite {
		t.Errorf("Get() = requested by %q (%q), run as %+v, suite %+v; want %+v", got.RequestedBy, got.RequestedByID, got.RunAs, got.Suite, mine)
	}
	if other, err := repo.Get(ctx, "r-2"); err != nil || other.RunAs != nil || other.RequestedByID != "" {
		t.Errorf("Get(scheduled run) = run as %+v, requested by %q, %v; want neither", other.RunAs, other.RequestedByID, err)
	}
	runs, total, err := repo.List(ctx, validationdomain.RunFilter{RequestedByID: "user-alice"})
	if err != nil || total != 1 || runs[0].ID != "r-1" {
		t.Errorf("List(requested by alice) = %v, %d, %v", runs, total, err)
	}
}
