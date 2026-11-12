package mongodb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	"github.com/maple52046/swallow/internal/core/store"
)

func TestTopologyErrorAcceptsOnlyTopologiesWithTransactions(t *testing.T) {
	tests := []struct {
		name  string
		reply helloReply
		want  error
	}{
		{name: "replica set member", reply: helloReply{SetName: "rs0"}},
		{name: "mongos", reply: helloReply{Msg: "isdbgrid"}},
		{name: "standalone", reply: helloReply{}, want: ErrNoTransactions},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reply.topologyError(); !errors.Is(got, tc.want) {
				t.Errorf("topologyError(%+v) = %v, want %v", tc.reply, got, tc.want)
			}
		})
	}
}

// testDatabase connects to the replica set named by SWALLOW_TEST_MONGO_URI (for the dev stack,
// mongodb://localhost:27017/?directConnection=true) and returns a throwaway database that is
// dropped when the test ends. Without the variable the test is skipped.
func testDatabase(t *testing.T) (*mongo.Client, *mongo.Database) {
	t.Helper()
	client, db := mongotest.Database(t, "tx")
	// Transactions cannot create collections on every server version; create them up front.
	for _, name := range []string{"platforms", "servers"} {
		if err := db.CreateCollection(context.Background(), name); err != nil {
			t.Fatalf("CreateCollection(%q) error = %v", name, err)
		}
	}
	return client, db
}

func count(t *testing.T, col *mongo.Collection) int64 {
	t.Helper()
	n, err := col.CountDocuments(context.Background(), bson.D{})
	if err != nil {
		t.Fatalf("CountDocuments(%s) error = %v", col.Name(), err)
	}
	return n
}

func TestTransactorRollsBackEveryWriteWhenTheUnitFails(t *testing.T) {
	client, db := testDatabase(t)
	tx := NewTransactor(client)
	errConflict := errors.New("claim conflict")
	published := false

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if _, err := db.Collection("platforms").InsertOne(ctx, bson.D{{Key: "_id", Value: "p1"}}); err != nil {
			return err
		}
		if _, err := db.Collection("servers").InsertOne(ctx, bson.D{{Key: "_id", Value: "s1"}}); err != nil {
			return err
		}
		store.AfterCommit(ctx, func() { published = true })
		return errConflict
	})
	if !errors.Is(err, errConflict) {
		t.Fatalf("InTransaction() error = %v, want %v", err, errConflict)
	}
	if got := count(t, db.Collection("platforms")) + count(t, db.Collection("servers")); got != 0 {
		t.Errorf("documents after rollback = %d, want 0", got)
	}
	if published {
		t.Error("an AfterCommit hook ran for a rolled-back unit of work")
	}
}

// A write the server rejects inside a transaction (here a duplicate key) aborts the transaction
// server-side. The unit must fail at once with that error, not be retried as transient.
func TestTransactorReturnsAServerRejectedWritePromptly(t *testing.T) {
	client, db := testDatabase(t)
	tx := NewTransactor(client)
	if _, err := db.Collection("platforms").InsertOne(context.Background(), bson.D{{Key: "_id", Value: "p1"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	started := time.Now()
	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if _, err := db.Collection("servers").InsertOne(ctx, bson.D{{Key: "_id", Value: "s1"}}); err != nil {
			return err
		}
		_, err := db.Collection("platforms").InsertOne(ctx, bson.D{{Key: "_id", Value: "p1"}})
		return err
	})
	if !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("InTransaction() error = %v, want the duplicate key error", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("InTransaction() took %s; a rejected write must not be retried", elapsed)
	}
	if got := count(t, db.Collection("servers")); got != 0 {
		t.Errorf("servers after the rejected unit = %d, want 0", got)
	}
}

// The spec writes that must not race an uninstall rely on this: a unit reads a snapshot, another
// unit then writes a Workflow, moves a document the first one is about to write, and commits. The
// first unit's write conflicts, the whole unit is retried, and its second attempt reads the
// Workflow, so a check made at the top of the unit cannot be overtaken.
func TestTransactorRetriesAUnitWhoseSnapshotWasOvertaken(t *testing.T) {
	client, db := testDatabase(t)
	for _, name := range []string{"workflows", "change_plans"} {
		if err := db.CreateCollection(context.Background(), name); err != nil {
			t.Fatalf("CreateCollection(%q) error = %v", name, err)
		}
	}
	plans, workflows := db.Collection("change_plans"), db.Collection("workflows")
	if _, err := plans.InsertOne(context.Background(), bson.D{{Key: "_id", Value: "plan-1"}, {Key: "state", Value: "approved"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx := NewTransactor(client)
	errUninstallStarted := errors.New("uninstall started")
	attempts := 0

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		attempts++
		running, err := workflows.CountDocuments(ctx, bson.D{})
		if err != nil {
			return err
		}
		if running > 0 {
			return errUninstallStarted
		}
		if attempts == 1 {
			// Another session applies the plan as a Workflow and commits after this unit's read.
			if err := tx.InTransaction(context.Background(), func(other context.Context) error {
				if _, err := workflows.InsertOne(other, bson.D{{Key: "_id", Value: "wf-1"}}); err != nil {
					return err
				}
				_, err := plans.UpdateOne(other, bson.D{{Key: "_id", Value: "plan-1"}, {Key: "state", Value: "approved"}},
					bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: "applied"}}}})
				return err
			}); err != nil {
				return fmt.Errorf("the other unit: %w", err)
			}
		}
		_, err = plans.UpdateOne(ctx, bson.D{{Key: "_id", Value: "plan-1"}, {Key: "state", Value: "approved"}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: "superseded"}}}})
		return err
	})
	if !errors.Is(err, errUninstallStarted) || attempts != 2 {
		t.Fatalf("InTransaction() = %v after %d attempts, want the second attempt to see the Workflow", err, attempts)
	}
	var plan struct {
		State string `bson:"state"`
	}
	if err := plans.FindOne(context.Background(), bson.D{{Key: "_id", Value: "plan-1"}}).Decode(&plan); err != nil || plan.State != "applied" {
		t.Errorf("plan state = %q, %v; want the other unit's applied", plan.State, err)
	}
}

func TestTransactorCommitsTogetherAndJoinsNestedCalls(t *testing.T) {
	client, db := testDatabase(t)
	tx := NewTransactor(client)
	published := 0

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if _, err := db.Collection("platforms").InsertOne(ctx, bson.D{{Key: "_id", Value: "p1"}}); err != nil {
			return err
		}
		// A composed use case opens its own unit of work; it must join this one.
		return tx.InTransaction(ctx, func(inner context.Context) error {
			if _, err := db.Collection("servers").InsertOne(inner, bson.D{{Key: "_id", Value: "s1"}}); err != nil {
				return err
			}
			// Not yet visible outside the transaction.
			if n, err := db.Collection("servers").CountDocuments(context.Background(), bson.D{}); err != nil || n != 0 {
				t.Errorf("servers visible outside the transaction before commit: n=%d err=%v", n, err)
			}
			store.AfterCommit(inner, func() { published++ })
			return nil
		})
	})
	if err != nil {
		t.Fatalf("InTransaction() error = %v", err)
	}
	if got := count(t, db.Collection("platforms")) + count(t, db.Collection("servers")); got != 2 {
		t.Errorf("documents after commit = %d, want 2", got)
	}
	if published != 1 {
		t.Errorf("AfterCommit hooks ran %d times, want 1", published)
	}
}
