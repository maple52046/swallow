package software

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
)

// A controller woken by a software_assignments change only has the record key the feed renders
// (the hex of the record's ObjectID); FindByRecordKey turns it back into the assignment, and an
// unknown key is a domain miss.
func TestMongoAssignmentRepoFindByRecordKey(t *testing.T) {
	db := credentialTestDatabase(t)
	repo, err := NewMongoAssignmentRepo(db)
	if err != nil {
		t.Fatalf("NewMongoAssignmentRepo: %v", err)
	}
	ctx := context.Background()
	if err := repo.Upsert(ctx, &softwaredomain.Assignment{
		ServerID: "srv-1", SiteID: "site-1", Kind: "node-exporter", State: softwaredomain.StatePending, LastWorkflowID: "wf-1",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	var raw struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := db.Collection("software_assignments").FindOne(ctx, bson.M{"serverId": "srv-1"}).Decode(&raw); err != nil {
		t.Fatalf("read the record id: %v", err)
	}

	got, err := repo.FindByRecordKey(ctx, raw.ID.Hex())
	if err != nil {
		t.Fatalf("FindByRecordKey(%s) error = %v", raw.ID.Hex(), err)
	}
	if got.ServerID != "srv-1" || got.Kind != "node-exporter" || got.State != softwaredomain.StatePending {
		t.Errorf("FindByRecordKey = %+v, want the srv-1 node-exporter pending record", got)
	}
	if _, err := repo.FindByRecordKey(ctx, primitive.NewObjectID().Hex()); !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		t.Errorf("FindByRecordKey(unknown) error = %v, want ErrAssignmentNotFound", err)
	}
	if _, err := repo.FindByRecordKey(ctx, "not-an-object-id"); !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		t.Errorf("FindByRecordKey(non-hex) error = %v, want ErrAssignmentNotFound", err)
	}
}
