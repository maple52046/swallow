package mongodb

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

var (
	_ store.ChangeFeed     = (*ChangeFeed)(nil)
	_ store.LiveChangeFeed = (*ChangeFeed)(nil)
)

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

// liveChangeEvent is a change event with the fields an update touched, which a live watch reads to
// skip updates of ignored fields only.
type liveChangeEvent struct {
	OperationType string `bson:"operationType"`
	NS            struct {
		Coll string `bson:"coll"`
	} `bson:"ns"`
	DocumentKey struct {
		ID any `bson:"_id"`
	} `bson:"documentKey"`
	UpdateDescription struct {
		UpdatedFields bson.Raw `bson:"updatedFields"`
		RemovedFields []string `bson:"removedFields"`
	} `bson:"updateDescription"`
}

// changePipeline matches the record changes of the named collections.
func changePipeline(collections []string) mongo.Pipeline {
	return mongo.Pipeline{{{Key: "$match", Value: bson.D{
		{Key: "ns.coll", Value: bson.D{{Key: "$in", Value: collections}}},
		{Key: "operationType", Value: bson.D{{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}}}},
	}}}}
}

// Watch implements store.ChangeFeed.
func (f *ChangeFeed) Watch(ctx context.Context, subscriber string, collections []string, handle func(context.Context, store.Change) error) error {
	pipeline := changePipeline(collections)
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

// WatchLive implements store.LiveChangeFeed: a change stream opened at the present that saves no
// resume token. The ChangeReset is delivered once the stream is open, so a change committed after
// the subscriber re-reads is never lost between the two.
func (f *ChangeFeed) WatchLive(ctx context.Context, collections, ignoredFields []string, handle func(context.Context, store.Change) error) error {
	stream, err := f.db.Watch(ctx, changePipeline(collections))
	if err != nil {
		return fmt.Errorf("open change stream: %w", err)
	}
	defer stream.Close(context.Background())
	if err := handle(ctx, store.Change{Operation: store.ChangeReset}); err != nil {
		return err
	}
	for stream.Next(ctx) {
		var event liveChangeEvent
		if err := stream.Decode(&event); err != nil {
			return fmt.Errorf("decode change event: %w", err)
		}
		if event.OperationType == string(store.ChangeUpdate) &&
			onlyIgnoredFields(event.UpdateDescription.UpdatedFields, event.UpdateDescription.RemovedFields, ignoredFields) {
			continue
		}
		change := store.Change{
			Collection: event.NS.Coll,
			Key:        documentKey(event.DocumentKey.ID),
			Operation:  store.ChangeOperation(event.OperationType),
		}
		if err := handle(ctx, change); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("change stream: %w", err)
	}
	return nil
}

// onlyIgnoredFields reports whether an update touched at least one field and every field it set or
// removed is one of ignored or below one of them. An update that names no field is delivered.
func onlyIgnoredFields(updated bson.Raw, removed []string, ignored []string) bool {
	if len(ignored) == 0 {
		return false
	}
	paths := append([]string(nil), removed...)
	if len(updated) > 0 {
		elements, err := updated.Elements()
		if err != nil {
			return false
		}
		for _, element := range elements {
			paths = append(paths, element.Key())
		}
	}
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !underAny(path, ignored) {
			return false
		}
	}
	return true
}

// underAny reports whether path is one of prefixes or a field below one of them.
func underAny(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
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
