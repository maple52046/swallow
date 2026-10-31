package convergence

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/convergence"
)

// testDatabase connects to the replica set named by SWALLOW_TEST_MONGO_URI and returns a throwaway
// database dropped when the test ends. Without the variable the test is skipped.
func testDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := storemongo.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_convergence_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// A lease held by one process refuses another until it expires or is released, and its holder can
// renew it.
func TestLeaseStoreKeepsOneHolder(t *testing.T) {
	leases := NewMongoLeaseStore(testDatabase(t))
	ctx := context.Background()
	ttl := 500 * time.Millisecond
	check := func(holder string, want bool) {
		t.Helper()
		got, err := leases.TryAcquire(ctx, "controller:test", holder, ttl)
		if err != nil || got != want {
			t.Fatalf("TryAcquire(%s) = %v, %v; want %v", holder, got, err, want)
		}
	}
	check("p1", true)
	check("p2", false)
	check("p1", true)
	time.Sleep(ttl + 100*time.Millisecond)
	check("p2", true)
	if err := leases.Release(ctx, "controller:test", "p1"); err != nil {
		t.Fatal(err)
	}
	check("p1", false)
	if err := leases.Release(ctx, "controller:test", "p2"); err != nil {
		t.Fatal(err)
	}
	check("p1", true)
}

func TestEventRecorderListsNewestFirst(t *testing.T) {
	recorder, err := NewMongoEventRecorder(context.Background(), testDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)
	for i, typ := range []convergence.EventType{convergence.EventDriftDetected, convergence.EventDriftCorrected} {
		if err := recorder.Record(ctx, convergence.Event{ResourceKind: "server", ResourceID: "s1", Type: typ, Fields: []string{"tags"}, Generation: 2, At: at.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Record(ctx, convergence.Event{ResourceKind: "server", ResourceID: "s2", Type: convergence.EventAdopted}); err != nil {
		t.Fatal(err)
	}
	events, err := recorder.List(ctx, "server", "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != convergence.EventDriftCorrected || events[1].Type != convergence.EventDriftDetected {
		t.Fatalf("List() = %+v, want the two s1 events newest first", events)
	}
}
