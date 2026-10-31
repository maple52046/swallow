package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// A stored claim reads back onto the Server, and a document without one reads as unclaimed.
func TestPlatformClaimDocRoundTrip(t *testing.T) {
	claimedAt := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	got := toServer(&serverDoc{ID: "srv-1", PlatformClaim: &platformClaimDoc{PlatformID: "platform-a", ClaimedAt: claimedAt}})
	if got.PlatformClaim == nil || got.PlatformClaim.PlatformID != "platform-a" || !got.PlatformClaim.ClaimedAt.Equal(claimedAt) {
		t.Errorf("toServer() claim = %+v, want platform-a at %s", got.PlatformClaim, claimedAt)
	}
	if unclaimed := toServer(&serverDoc{ID: "srv-2"}); unclaimed.PlatformClaim != nil {
		t.Errorf("toServer() claim = %+v, want nil without a stored claim", unclaimed.PlatformClaim)
	}
}

// claimTestDatabase connects to the replica set named by SWALLOW_TEST_MONGO_URI and returns a
// Transactor, the Server repository, and its throwaway database (dropped when the test ends).
// Without the variable the test is skipped.
func claimTestDatabase(t *testing.T) (store.Transactor, *MongoServerRepo, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := storemongo.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_claim_tx_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	repo, err := NewMongoServerRepo(db)
	if err != nil {
		t.Fatalf("NewMongoServerRepo() error = %v", err)
	}
	if err := db.CreateCollection(ctx, "platforms"); err != nil {
		t.Fatalf("CreateCollection(platforms) error = %v", err)
	}
	return storemongo.NewTransactor(client), repo, db
}

// insertServers stores minimal Server documents; each gets its own source key, which the
// collection's unique index requires.
func insertServers(t *testing.T, db *mongo.Database, docs ...bson.M) {
	t.Helper()
	for _, doc := range docs {
		doc["source"] = bson.M{"siteId": "site-1", "integrationId": "maas-1", "providerMachineId": doc["_id"]}
		if _, err := db.Collection("servers").InsertOne(context.Background(), doc); err != nil {
			t.Fatalf("insert server %v: %v", doc["_id"], err)
		}
	}
}

func claimOf(t *testing.T, repo *MongoServerRepo, platformID string) []string {
	t.Helper()
	ids, err := repo.ClaimedServerIDs(context.Background(), platformID)
	if err != nil {
		t.Fatalf("ClaimedServerIDs(%s) error = %v", platformID, err)
	}
	return ids
}

// A deployment that loses one target's claim is rolled back whole: the claims it already wrote
// and the Platform record it stored in the same transaction are gone (decision 069).
func TestClaimForPlatformRefusalRollsBackTheWholeAcceptance(t *testing.T) {
	tx, repo, db := claimTestDatabase(t)
	insertServers(t, db,
		bson.M{"_id": "s1"},
		bson.M{"_id": "s2", "platformClaim": bson.M{"platformId": "rival", "claimedAt": time.Now().UTC()}},
	)

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if _, err := db.Collection("platforms").InsertOne(ctx, bson.M{"_id": "p-new"}); err != nil {
			return err
		}
		return repo.ClaimForPlatform(ctx, "p-new", []string{"s1", "s2"}, time.Now().UTC())
	})
	if !errors.Is(err, serverdomain.ErrServerClaimed) {
		t.Fatalf("InTransaction() error = %v, want ErrServerClaimed", err)
	}
	if ids := claimOf(t, repo, "p-new"); len(ids) != 0 {
		t.Errorf("claims of the refused Platform = %v, want none", ids)
	}
	if n, err := db.Collection("platforms").CountDocuments(context.Background(), bson.M{}); err != nil || n != 0 {
		t.Errorf("Platforms after the refusal = %d (err %v), want 0", n, err)
	}
	if ids := claimOf(t, repo, "rival"); len(ids) != 1 || ids[0] != "s2" {
		t.Errorf("claims of the rival = %v, want [s2]", ids)
	}
}

// Two deployments accepted at the same time for the same Server: exactly one claims it. The loser
// conflicts on the Server's document, is retried by the driver, and then sees the winner's claim.
func TestConcurrentClaimTransactionsLetExactlyOneDeploymentWin(t *testing.T) {
	tx, repo, db := claimTestDatabase(t)
	insertServers(t, db, bson.M{"_id": "s1"}, bson.M{"_id": "s2"})

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, platformID := range []string{"p-a", "p-b"} {
		wg.Add(1)
		go func(i int, platformID string) {
			defer wg.Done()
			results[i] = tx.InTransaction(context.Background(), func(ctx context.Context) error {
				return repo.ClaimForPlatform(ctx, platformID, []string{"s1", "s2"}, time.Now().UTC())
			})
		}(i, platformID)
	}
	wg.Wait()

	wins := 0
	for i, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, serverdomain.ErrServerClaimed):
		default:
			t.Errorf("deployment %d error = %v, want nil or ErrServerClaimed", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("deployments that claimed the Servers = %d, want exactly 1 (errors %v)", wins, results)
	}
	a, b := claimOf(t, repo, "p-a"), claimOf(t, repo, "p-b")
	if len(a)+len(b) != 2 || (len(a) != 0 && len(b) != 0) {
		t.Errorf("claims p-a=%v p-b=%v, want both Servers held by one Platform", a, b)
	}
}
