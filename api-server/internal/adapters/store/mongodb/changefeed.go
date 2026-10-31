package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/core/store"
)

// checkpointInterval bounds how often a subscriber's resume token is written: saving after every
// event would double the write load of a busy collection, and at-least-once delivery means a
// restart only redelivers the events after the last save.
const checkpointInterval = 5 * time.Second

// Server error codes meaning a change stream cannot resume from the saved token.
const (
	codeInvalidResumeToken      = 260
	codeChangeStreamFatal       = 280
	codeChangeStreamHistoryLost = 286
)

// ChangeFeed implements store.ChangeFeed with a MongoDB change stream on one database. Each
// subscriber's resume token is kept in `change_feed_checkpoints`, keyed by subscriber name; the token
// never leaves this adapter (desired-state-model.md §10.3).
type ChangeFeed struct {
	db          *mongo.Database
	checkpoints *mongo.Collection
}

var _ store.ChangeFeed = (*ChangeFeed)(nil)

// NewChangeFeed returns the change feed of db. The connection must be to a replica set, which
// Connect already enforces.
func NewChangeFeed(db *mongo.Database) *ChangeFeed {
	return &ChangeFeed{db: db, checkpoints: db.Collection("change_feed_checkpoints")}
}

type checkpointDoc struct {
	ID        string    `bson:"_id"`
	Token     bson.Raw  `bson:"token"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

type changeEvent struct {
	OperationType string `bson:"operationType"`
	NS            struct {
		Coll string `bson:"coll"`
	} `bson:"ns"`
	DocumentKey struct {
		ID any `bson:"_id"`
	} `bson:"documentKey"`
}

// Watch implements store.ChangeFeed.
func (f *ChangeFeed) Watch(ctx context.Context, subscriber string, collections []string, handle func(context.Context, store.Change) error) error {
	pipeline := mongo.Pipeline{{{Key: "$match", Value: bson.D{
		{Key: "ns.coll", Value: bson.D{{Key: "$in", Value: collections}}},
		{Key: "operationType", Value: bson.D{{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}}}},
	}}}}
	token, err := f.loadToken(ctx, subscriber)
	if err != nil {
		return err
	}
	opts := options.ChangeStream()
	if token != nil {
		opts.SetResumeAfter(token)
	}
	stream, err := f.db.Watch(ctx, pipeline, opts)
	if err != nil && token != nil && cannotResume(err) {
		if err := f.reset(ctx, subscriber, handle); err != nil {
			return err
		}
		stream, err = f.db.Watch(ctx, pipeline)
	}
	if err != nil {
		return fmt.Errorf("open change stream: %w", err)
	}
	defer stream.Close(context.Background())

	var lastSaved time.Time
	for stream.Next(ctx) {
		var event changeEvent
		if err := stream.Decode(&event); err != nil {
			return fmt.Errorf("decode change event: %w", err)
		}
		change := store.Change{
			Collection: event.NS.Coll,
			Key:        documentKey(event.DocumentKey.ID),
			Operation:  store.ChangeOperation(event.OperationType),
		}
		if err := handle(ctx, change); err != nil {
			return err
		}
		if time.Since(lastSaved) >= checkpointInterval {
			if err := f.saveToken(ctx, subscriber, stream.ResumeToken()); err != nil {
				return err
			}
			lastSaved = time.Now()
		}
	}
	// Save the position reached before stopping, so a restart redelivers as little as possible.
	if token := stream.ResumeToken(); token != nil {
		saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = f.saveToken(saveCtx, subscriber, token)
		cancel()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := stream.Err(); err != nil {
		if cannotResume(err) {
			_, _ = f.checkpoints.DeleteOne(context.Background(), bson.M{"_id": subscriber})
		}
		return fmt.Errorf("change stream: %w", err)
	}
	return nil
}

// reset forgets a token the server can no longer resume from and tells the subscriber that changes
// may have been missed.
func (f *ChangeFeed) reset(ctx context.Context, subscriber string, handle func(context.Context, store.Change) error) error {
	if _, err := f.checkpoints.DeleteOne(ctx, bson.M{"_id": subscriber}); err != nil {
		return fmt.Errorf("clear change feed checkpoint: %w", err)
	}
	return handle(ctx, store.Change{Operation: store.ChangeReset})
}

func (f *ChangeFeed) loadToken(ctx context.Context, subscriber string) (bson.Raw, error) {
	var doc checkpointDoc
	err := f.checkpoints.FindOne(ctx, bson.M{"_id": subscriber}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load change feed checkpoint: %w", err)
	}
	return doc.Token, nil
}

func (f *ChangeFeed) saveToken(ctx context.Context, subscriber string, token bson.Raw) error {
	if token == nil {
		return nil
	}
	_, err := f.checkpoints.ReplaceOne(ctx, bson.M{"_id": subscriber},
		checkpointDoc{ID: subscriber, Token: token, UpdatedAt: time.Now().UTC()},
		options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("save change feed checkpoint: %w", err)
	}
	return nil
}

// documentKey renders a document's _id as the string key repositories use.
func documentKey(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case primitive.ObjectID:
		return v.Hex()
	default:
		return fmt.Sprint(v)
	}
}

func cannotResume(err error) bool {
	var serverErr mongo.ServerError
	return errors.As(err, &serverErr) &&
		(serverErr.HasErrorCode(codeChangeStreamHistoryLost) ||
			serverErr.HasErrorCode(codeInvalidResumeToken) ||
			serverErr.HasErrorCode(codeChangeStreamFatal))
}
