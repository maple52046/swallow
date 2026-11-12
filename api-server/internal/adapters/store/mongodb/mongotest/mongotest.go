// Package mongotest gives the MongoDB integration tests their throwaway databases. It is imported
// only by tests (the store adapter's repositories and the composition root's assembly tests); no
// production code may depend on it.
//
// Every database it creates is named swallow_<name>_test_<unix nanoseconds> and is dropped when the
// test ends. A test run that is interrupted (Ctrl-C, a killed process) never runs its cleanup, and
// the databases it leaves make the next run more likely to exhaust the server's file descriptors
// (each database's collections and indexes are files). So each new database first drops the ones a
// run left behind: every swallow_*_test_* database older than staleAfter. A run in progress
// elsewhere is younger than that and keeps its databases.
package mongotest

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// URIVariable names the environment variable that enables the integration tests: the URI of a
// replica set (for the dev stack, mongodb://localhost:27017/?directConnection=true). Without it
// the tests that need MongoDB are skipped.
const URIVariable = "SWALLOW_TEST_MONGO_URI"

// staleAfter is how old a test database must be before another run may drop it. No single test
// package runs anywhere near this long, so a database this old belongs to a run that stopped.
const staleAfter = time.Hour

// testDatabaseName matches the databases this package and the tests before it created, capturing
// the creation time in unix nanoseconds.
var testDatabaseName = regexp.MustCompile(`^swallow_[a-z0-9_]*test_([0-9]{16,20})$`)

// Database connects to the replica set URIVariable names and returns the client and a new empty
// database for this test, both released when the test ends (the database dropped, the client
// disconnected). name tells the test's databases apart in the server, for example "workflow_repo";
// it must be lowercase letters, digits, and underscores. Without the variable the test is skipped.
// It fails the test when the server cannot be reached.
func Database(t testing.TB, name string) (*mongo.Client, *mongo.Database) {
	t.Helper()
	uri := os.Getenv(URIVariable)
	if uri == "" {
		t.Skip(URIVariable + " is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect to %s: %v", URIVariable, err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("ping %s: %v", URIVariable, err)
	}
	dropStale(ctx, t, client, time.Now())
	db := client.Database(fmt.Sprintf("swallow_%s_test_%d", name, time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		// Best effort: a database that cannot be dropped now is dropped by a later run as stale.
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return client, db
}

// dropStale drops the test databases an interrupted run left behind. Failing to list or drop them
// only logs: the test can still run, and the next run tries again.
func dropStale(ctx context.Context, t testing.TB, client *mongo.Client, now time.Time) {
	t.Helper()
	names, err := client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		t.Logf("list databases to drop stale test databases: %v", err)
		return
	}
	for _, name := range StaleDatabases(names, now) {
		if err := client.Database(name).Drop(ctx); err != nil {
			t.Logf("drop stale test database %s: %v", name, err)
		}
	}
}

// StaleDatabases returns the names among names that are test databases created more than
// staleAfter before now. Names that are not test databases, or carry no readable time, are never
// returned, so a real swallow database cannot match.
func StaleDatabases(names []string, now time.Time) []string {
	var stale []string
	for _, name := range names {
		match := testDatabaseName.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		created, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			continue
		}
		if now.Sub(time.Unix(0, created)) > staleAfter {
			stale = append(stale, name)
		}
	}
	return stale
}
