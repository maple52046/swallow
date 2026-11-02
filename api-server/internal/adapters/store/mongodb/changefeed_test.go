package mongodb

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/core/store"
)

// The feed reports changes of the watched collections only, and a second Watch with the same
// subscriber resumes after what the first one handled instead of from the present.
func TestChangeFeedDeliversAndResumes(t *testing.T) {
	_, db := testDatabase(t)
	feed := NewChangeFeed(db)
	watched, other := db.Collection("servers"), db.Collection("platforms")

	received := make(chan store.Change, 16)
	watch := func() (context.CancelFunc, chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		errc := make(chan error, 1)
		go func() {
			errc <- feed.Watch(ctx, "test", []string{"servers"}, func(_ context.Context, c store.Change) error {
				received <- c
				return nil
			})
		}()
		return cancel, errc
	}
	expect := func(key string, op store.ChangeOperation) {
		t.Helper()
		timeout := time.After(10 * time.Second)
		for {
			select {
			case got := <-received:
				if got.Key == "probe" {
					continue
				}
				if got.Collection != "servers" || got.Key != key || got.Operation != op {
					t.Fatalf("change = %+v, want servers/%s %s", got, key, op)
				}
				return
			case <-timeout:
				t.Fatalf("no change for %s", key)
			}
		}
	}

	cancel, errc := watch()
	// A change stream only sees writes made after it opened; touch a probe document until the
	// watcher reports it, so the writes below are certainly observed.
	deadline := time.Now().Add(10 * time.Second)
	for seen := false; !seen; {
		if _, err := watched.UpdateOne(context.Background(), bson.M{"_id": "probe"},
			bson.M{"$inc": bson.M{"n": 1}}, options.Update().SetUpsert(true)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		select {
		case <-received:
			seen = true
		case <-time.After(300 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("the change stream never reported the probe")
			}
		}
	}
	if _, err := watched.InsertOne(context.Background(), bson.M{"_id": "s1"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	expect("s1", store.ChangeInsert)
	if _, err := other.InsertOne(context.Background(), bson.M{"_id": "p1"}); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	if _, err := watched.UpdateOne(context.Background(), bson.M{"_id": "s1"}, bson.M{"$set": bson.M{"x": 1}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	expect("s1", store.ChangeUpdate)
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("Watch() after cancel = %v, want context.Canceled", err)
	}

	// Written while nobody watches; the resumed watcher must still see it.
	if _, err := watched.DeleteOne(context.Background(), bson.M{"_id": "s1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	cancel, errc = watch()
	defer func() { cancel(); <-errc }()
	expect("s1", store.ChangeDelete)
}

// A live watch starts with a ChangeReset, then reports changes from the present without saving a
// position, and skips an update that touched only ignored fields.
func TestChangeFeedWatchLiveSkipsIgnoredFields(t *testing.T) {
	_, db := testDatabase(t)
	feed := NewChangeFeed(db)
	servers := db.Collection("servers")
	ctx := context.Background()
	if _, err := servers.InsertOne(ctx, bson.M{"_id": "s1", "lastSeenAt": time.Now(), "provisioning": bson.M{"state": "ready"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	received := make(chan store.Change, 16)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- feed.WatchLive(watchCtx, []string{"servers"}, []string{"lastSeenAt", "provisioning.observedAt"},
			func(_ context.Context, c store.Change) error {
				received <- c
				return nil
			})
	}()
	select {
	case got := <-received:
		if got.Operation != store.ChangeReset {
			t.Fatalf("first change = %+v, want a reset", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no reset from the live watch")
	}

	if _, err := servers.UpdateOne(ctx, bson.M{"_id": "s1"}, bson.M{"$set": bson.M{
		"lastSeenAt": time.Now().Add(time.Minute), "provisioning.observedAt": time.Now(),
	}}); err != nil {
		t.Fatalf("touch timestamps: %v", err)
	}
	if _, err := servers.UpdateOne(ctx, bson.M{"_id": "s1"}, bson.M{"$set": bson.M{"provisioning.state": "deployed"}}); err != nil {
		t.Fatalf("change state: %v", err)
	}
	select {
	case got := <-received:
		if got.Collection != "servers" || got.Key != "s1" || got.Operation != store.ChangeUpdate {
			t.Fatalf("change = %+v, want the state update of s1", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no change for the state update")
	}
	select {
	case got := <-received:
		t.Fatalf("unexpected change %+v: the timestamp-only update must be skipped", got)
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Errorf("WatchLive() = %v, want context.Canceled", err)
	}
	if n, _ := db.Collection("change_feed_checkpoints").CountDocuments(ctx, bson.M{}); n != 0 {
		t.Errorf("live watch saved %d checkpoints, want none", n)
	}
}
