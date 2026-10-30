package mongodb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

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
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_tx_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
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
