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
