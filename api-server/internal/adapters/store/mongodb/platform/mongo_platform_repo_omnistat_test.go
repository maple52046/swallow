package platform

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The hardware counters ride on the Omnistat record: the request round-trips with the record, the
// nodes are written only under the record's lastWorkflowId fence and keep the request, and a record
// stored before the counters existed reads as requested false with no nodes.
func TestOmnistatHardwareCountersAreFencedOnTheRecord(t *testing.T) {
	ctx := context.Background()
	db := platformTestDatabase(t)
	repo, err := NewMongoPlatformRepo(db, nil)
	if err != nil {
		t.Fatalf("NewMongoPlatformRepo: %v", err)
	}
	for _, id := range []string{"p1", "p2"} {
		platform := &platformdomain.Platform{
			ID: id, SiteID: "site-1", Name: "lab-slurm-" + id, Type: platformdomain.PlatformTypeSlurm,
			GPUStackOwner: platformdomain.GPUStackOwnerProvisioning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Create(ctx, platform); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}
	record := &platformdomain.Omnistat{
		Enabled: true, Version: platformdomain.OmnistatVersion, InstallMethod: platformdomain.ObservabilityInstallContainer,
		ObservabilityServerID: "s1", State: platformdomain.OmnistatInstalling,
		HardwareCounters: platformdomain.HardwareCounters{Requested: true},
		LastWorkflowID:   "wf-2", UpdatedAt: time.Now().UTC(),
	}
	if err := repo.SetOmnistat(ctx, "p1", record); err != nil {
		t.Fatalf("SetOmnistat: %v", err)
	}
	nodes := []platformdomain.HardwareCounterNode{
		{ServerID: "s2", State: platformdomain.HardwareCountersCollecting},
		{ServerID: "s3", State: platformdomain.HardwareCountersUnavailable, Reason: "probe_failed"},
	}
	if updated, err := repo.SetHardwareCounterNodes(ctx, "p1", "wf-1", nodes, time.Now().UTC()); err != nil || updated {
		t.Errorf("SetHardwareCounterNodes(superseded wf-1) = %v, %v; want false, nil", updated, err)
	}
	if read, _ := repo.FindByID(ctx, "p1"); len(read.Omnistat.HardwareCounters.Nodes) != 0 {
		t.Errorf("a superseded Workflow wrote %v", read.Omnistat.HardwareCounters.Nodes)
	}
	if updated, err := repo.SetHardwareCounterNodes(ctx, "p1", "wf-2", nodes, time.Now().UTC()); err != nil || !updated {
		t.Errorf("SetHardwareCounterNodes(wf-2) = %v, %v; want true, nil", updated, err)
	}
	read, err := repo.FindByID(ctx, "p1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got := read.Omnistat.HardwareCounters; !got.Requested || !reflect.DeepEqual(got.Nodes, nodes) {
		t.Errorf("hardware counters = %+v, want requested with %v", got, nodes)
	}

	if _, err := db.Collection("platforms").UpdateOne(ctx, bson.M{"_id": "p2"}, bson.M{"$set": bson.M{"omnistat": bson.M{
		"enabled": true, "version": "1.15.0", "observabilityServerId": "s1", "state": "ready", "lastWorkflowId": "wf-old",
		"updatedAt": time.Now().UTC(),
	}}}); err != nil {
		t.Fatalf("store an older record: %v", err)
	}
	older, err := repo.FindByID(ctx, "p2")
	if err != nil {
		t.Fatalf("FindByID(p2): %v", err)
	}
	if got := older.Omnistat.HardwareCounters; got.Requested || len(got.Nodes) != 0 {
		t.Errorf("an older record's hardware counters = %+v, want none requested", got)
	}
}
