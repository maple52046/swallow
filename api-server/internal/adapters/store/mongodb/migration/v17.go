package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// v17Suite is the part of a stored user suite v17 reads. Sudo is absent and RunAs present on every
// suite written before v17's release.
type v17Suite struct {
	ID             string    `bson:"_id"`
	Script         string    `bson:"script"`
	ScriptSHA256   string    `bson:"scriptSha256"`
	Sudo           *bool     `bson:"sudo"`
	StarterLevel   string    `bson:"starterLevel"`
	RunAs          string    `bson:"runAs"`
	TimeoutSeconds int       `bson:"timeoutSeconds"`
	Impact         string    `bson:"impact"`
	Revision       int       `bson:"revision"`
	UpdatedAt      time.Time `bson:"updatedAt"`
	CreatedBy      string    `bson:"createdBy"`
	CreatedByID    string    `bson:"createdById"`
}

// migrateV16ToV17 prepares the user suites for authors and kept revisions (validation.md §2.2):
//
//  1. A suite without createdById gets the id of the User whose username is its createdBy. A suite
//     whose createdBy names no User keeps none: its author is treated as deleted, so nobody may
//     replace it and an admin may retire it.
//  2. Each suite's current revision is written as its first kept revision in
//     validation_suite_revisions, with sudo read from the retired runAs (root is sudo) and the
//     starter level owner, as the store reads them. Earlier revisions were never kept.
//
// The suites themselves keep runAs: the store maps it at read time and drops it on the suite's next
// write. createdById is set only where it is missing, and a revision is inserted only where none
// exists ($setOnInsert), so an interrupted run resumes and a second run changes nothing.
func migrateV16ToV17(ctx context.Context, db *mongo.Database) error {
	cursor, err := db.Collection("validation_suites").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	var suites []v17Suite
	if err := cursor.All(ctx, &suites); err != nil {
		return err
	}
	for _, suite := range suites {
		if suite.CreatedByID == "" && suite.CreatedBy != "" {
			if err := v17BackfillAuthor(ctx, db, suite); err != nil {
				return err
			}
		}
		if suite.Revision < 1 {
			continue
		}
		sudo := suite.RunAs == "root"
		if suite.Sudo != nil {
			sudo = *suite.Sudo
		}
		level := suite.StarterLevel
		if level == "" {
			level = "owner"
		}
		id := fmt.Sprintf("%s/%d", suite.ID, suite.Revision)
		if _, err := db.Collection("validation_suite_revisions").UpdateOne(ctx, bson.M{"_id": id},
			bson.M{"$setOnInsert": bson.M{
				"suiteId": suite.ID, "revision": suite.Revision, "script": suite.Script, "scriptSha256": suite.ScriptSHA256,
				"sudo": sudo, "starterLevel": level, "timeoutSeconds": suite.TimeoutSeconds, "impact": suite.Impact,
				"createdAt": suite.UpdatedAt,
			}}, options.Update().SetUpsert(true)); err != nil {
			return err
		}
	}
	return nil
}

// v17BackfillAuthor sets the suite's createdById from the User named by its createdBy, if one exists.
func v17BackfillAuthor(ctx context.Context, db *mongo.Database, suite v17Suite) error {
	var user struct {
		ID string `bson:"_id"`
	}
	err := db.Collection("users").FindOne(ctx, bson.M{"username": suite.CreatedBy}).Decode(&user)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = db.Collection("validation_suites").UpdateOne(ctx,
		bson.M{"_id": suite.ID, "createdById": bson.M{"$in": bson.A{nil, ""}}},
		bson.M{"$set": bson.M{"createdById": user.ID}})
	return err
}
