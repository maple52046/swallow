package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

const (
	posixIDCollection   = "posix_ids"
	posixUIDCounterID   = "uid"
	posixGIDCounterID   = "gid"
	posixGroupKeyPrefix = "group:"
)

// MongoPOSIXIDs allocates POSIX uidNumbers and gidNumbers from `posix_ids` (decision 082): one
// counter document per kind ("uid", "gid") and one document per allocated group. A counter records
// the range's start with its first allocation; later start values are ignored, so numbers already on
// the Servers never move. Every allocation is one findOneAndUpdate, so it joins the caller's
// transaction through ctx and two allocators never get the same number.
type MongoPOSIXIDs struct {
	col   *mongo.Collection
	start int
}

var (
	_ authdomain.UIDAllocator     = (*MongoPOSIXIDs)(nil)
	_ accessdomain.GroupAllocator = (*MongoPOSIXIDs)(nil)
)

// NewMongoPOSIXIDs returns the allocator; start is the installation's configured first number, used
// only when a counter does not exist yet.
func NewMongoPOSIXIDs(db *mongo.Database, start int) *MongoPOSIXIDs {
	return &MongoPOSIXIDs{col: db.Collection(posixIDCollection), start: start}
}

type posixCounterDoc struct {
	ID    string `bson:"_id"`
	Start int    `bson:"start"`
	Next  int    `bson:"next"`
}

// AllocateUID implements authdomain.UIDAllocator.
func (r *MongoPOSIXIDs) AllocateUID(ctx context.Context) (int, error) {
	return r.allocate(ctx, posixUIDCounterID)
}

// RecordedStart returns the start the uid counter recorded with its first allocation, and false
// before any allocation; the API compares it with the configured start to warn about a change that
// no longer has any effect.
func (r *MongoPOSIXIDs) RecordedStart(ctx context.Context) (int, bool, error) {
	var doc posixCounterDoc
	err := r.col.FindOne(ctx, bson.M{"_id": posixUIDCounterID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return doc.Start, true, nil
}

// allocate increments the counter and returns start+next-1. The first allocation creates the
// counter with the configured start through $setOnInsert on a separate field, so the increment and
// the insert never touch the same field.
func (r *MongoPOSIXIDs) allocate(ctx context.Context, counter string) (int, error) {
	var doc posixCounterDoc
	err := r.col.FindOneAndUpdate(ctx, bson.M{"_id": counter},
		bson.M{"$inc": bson.M{"next": 1}, "$setOnInsert": bson.M{"start": r.start}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return 0, fmt.Errorf("allocate %s: %w", counter, err)
	}
	return doc.Start + doc.Next - 1, nil
}

// posixGroupDoc is one allocated group: its key, the name it got, and its gidNumber, all fixed at
// allocation.
type posixGroupDoc struct {
	ID        string `bson:"_id"`
	Name      string `bson:"name"`
	GIDNumber int    `bson:"gidNumber"`
}

// Groups implements accessdomain.GroupAllocator.
func (r *MongoPOSIXIDs) Groups(ctx context.Context) (map[string]accessdomain.GroupAllocation, error) {
	cursor, err := r.col.Find(ctx, bson.M{"_id": bson.M{"$regex": "^" + posixGroupKeyPrefix}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []posixGroupDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make(map[string]accessdomain.GroupAllocation, len(docs))
	for _, doc := range docs {
		key := strings.TrimPrefix(doc.ID, posixGroupKeyPrefix)
		out[key] = accessdomain.GroupAllocation{Key: key, Name: doc.Name, GIDNumber: doc.GIDNumber}
	}
	return out, nil
}

// Allocate implements accessdomain.GroupAllocator. It runs inside the caller's transaction: the
// gidNumber is taken from the counter and the group recorded together, and a key that already has a
// group returns it without taking a number.
func (r *MongoPOSIXIDs) Allocate(ctx context.Context, key, name string) (accessdomain.GroupAllocation, error) {
	var existing posixGroupDoc
	err := r.col.FindOne(ctx, bson.M{"_id": posixGroupKeyPrefix + key}).Decode(&existing)
	if err == nil {
		return accessdomain.GroupAllocation{Key: key, Name: existing.Name, GIDNumber: existing.GIDNumber}, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return accessdomain.GroupAllocation{}, err
	}
	gid, err := r.allocate(ctx, posixGIDCounterID)
	if err != nil {
		return accessdomain.GroupAllocation{}, err
	}
	if _, err := r.col.InsertOne(ctx, posixGroupDoc{ID: posixGroupKeyPrefix + key, Name: name, GIDNumber: gid}); err != nil {
		return accessdomain.GroupAllocation{}, err
	}
	return accessdomain.GroupAllocation{Key: key, Name: name, GIDNumber: gid}, nil
}
