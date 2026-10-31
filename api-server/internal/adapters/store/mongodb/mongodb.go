// Package mongodb is the MongoDB adapter of swallow's store ports (decision 069,
// docs/development/desired-state-model.md §10): it opens the client every process uses, refuses a
// MongoDB that cannot serve transactions and change streams, and implements store.Transactor with
// multi-document transactions.
//
// It is a provider adapter (docs/development/core-and-extensions.md §3): MongoDB types, sessions,
// and error labels stay here and in the repositories, and never reach the core. Each context's
// repositories live in a subpackage named after the context (apikey, auth, infrastructure,
// platform, provisioning, server, site, software, sshkey, workflow) and schema evolution lives in
// migration; a subpackage may import this package but not another context's subpackage. Nothing
// in this package or its subpackages may hold business rules.
package mongodb

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"github.com/maple52046/swallow/internal/core/store"
)

// ErrNoTransactions reports a MongoDB deployment that cannot run multi-document transactions or
// change streams, which is any standalone mongod. swallow refuses to start against one; the fix is
// to run it as a replica set (the installation runs the single-node replica set rs0).
var ErrNoTransactions = errors.New("MongoDB must run as a replica set (or sharded cluster): swallow needs transactions and change streams (decision 069)")

// Connect opens a client for uri and verifies, within ctx's deadline, that the server answers and
// can serve swallow: a replica set member or a mongos. It is the only way a swallow process
// connects, so every role fails the same way at startup instead of at its first transaction. On
// error the client is already disconnected; on success the caller owns it and must Disconnect it
// at shutdown. The URI needs no replicaSet parameter: given one seed, the driver discovers the set
// through the member names the set advertises.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := checkTopology(ctx, client); err != nil {
		// Best effort: the connection failed its check, and the caller gets the reason.
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return client, nil
}

// helloReply is the part of the hello command's reply that identifies the topology.
type helloReply struct {
	// SetName is the replica set's name; empty on a standalone mongod and on mongos.
	SetName string `bson:"setName"`
	// Msg is "isdbgrid" when the server is a mongos.
	Msg string `bson:"msg"`
}

func checkTopology(ctx context.Context, client *mongo.Client) error {
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("mongo ping: %w", err)
	}
	var reply helloReply
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&reply); err != nil {
		return fmt.Errorf("mongo hello: %w", err)
	}
	return reply.topologyError()
}

func (r helloReply) topologyError() error {
	if r.SetName == "" && r.Msg != "isdbgrid" {
		return ErrNoTransactions
	}
	return nil
}

// Transactor implements store.Transactor with MongoDB multi-document transactions. The ctx it
// passes to fn is a mongo.SessionContext, so repositories join the transaction simply by using
// that ctx for their collection calls; their signatures do not change. Repositories that are
// handed a different ctx (for example a fresh context.Background()) write outside the
// transaction, which is a bug in the caller.
//
// Each transaction reads at snapshot and commits with majority write concern. The driver retries
// the whole unit on a transient transaction error (a write conflict with a concurrent transaction,
// a primary step-down) and retries the commit on an unknown commit result, for up to 120 seconds;
// the AfterCommit hooks of an attempt that did not commit are dropped. A Transactor is safe for
// concurrent use; each call has its own session.
type Transactor struct {
	client *mongo.Client
}

var _ store.Transactor = (*Transactor)(nil)

// NewTransactor returns a Transactor over client, which must come from Connect so the deployment
// is known to support transactions.
func NewTransactor(client *mongo.Client) *Transactor {
	return &Transactor{client: client}
}

// transactionOptions are the guarantees every swallow transaction runs with: a consistent snapshot
// for its reads and a commit that survives a failover.
var transactionOptions = options.Transaction().
	SetReadConcern(readconcern.Snapshot()).
	SetWriteConcern(writeconcern.Majority())

// InTransaction implements store.Transactor.
func (t *Transactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if store.InTransaction(ctx) {
		return fn(ctx)
	}
	session, err := t.client.StartSession()
	if err != nil {
		return fmt.Errorf("mongo start session: %w", err)
	}
	defer session.EndSession(context.Background())

	scopeCtx, scope := store.WithCommitScope(ctx)
	_, err = session.WithTransaction(scopeCtx, func(sessionCtx mongo.SessionContext) (any, error) {
		scope.Reset()
		return nil, fn(sessionCtx)
	}, transactionOptions)
	if err != nil {
		return err
	}
	scope.Run()
	return nil
}
