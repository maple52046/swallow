package infra

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// platformTestDatabase returns a throwaway database on SWALLOW_TEST_MONGO_URI, dropped when the
// test ends. The test skips without the variable so `go test ./...` stays green without Mongo.
func platformTestDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI not set; skipping platform repository integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_platform_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	})
	return db
}

func TestGPUOperatorRecordIsFencedOnItsWorkflow(t *testing.T) {
	ctx := context.Background()
	repo, err := NewMongoPlatformRepo(platformTestDatabase(t))
	if err != nil {
		t.Fatalf("NewMongoPlatformRepo: %v", err)
	}
	platform := &platformdomain.Platform{
		ID: "p1", SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes,
		GPUStackOwner: platformdomain.GPUStackOwnerProvisioning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Create(ctx, platform); err != nil {
		t.Fatalf("Create: %v", err)
	}
	record := &platformdomain.GPUOperator{
		Settings: platformdomain.GPUOperatorSettings{
			Enabled: true, Version: "v1.5.1", AllocationMode: platformdomain.GPUAllocationDRA,
			DRADriverVersion: "v1.0.1", ResourceNaming: platformdomain.GPUResourceNamingSingle,
			NodeLabeller: true,
		},
		State: platformdomain.GPUOperatorInstalling, LastWorkflowID: "wf-2", UpdatedAt: time.Now().UTC(),
	}
	if err := repo.SetGPUOperator(ctx, "p1", record, platformdomain.GPUStackOwnerGPUOperator); err != nil {
		t.Fatalf("SetGPUOperator: %v", err)
	}

	if updated, err := repo.UpdateGPUOperatorState(ctx, "p1", "wf-1", platformdomain.GPUOperatorFailed, time.Now().UTC()); err != nil || updated {
		t.Errorf("UpdateGPUOperatorState(superseded wf-1) = %v, %v; want false, nil", updated, err)
	}
	if updated, err := repo.UpdateGPUOperatorState(ctx, "p1", "wf-2", platformdomain.GPUOperatorReady, time.Now().UTC()); err != nil || !updated {
		t.Errorf("UpdateGPUOperatorState(wf-2) = %v, %v; want true, nil", updated, err)
	}
	read, err := repo.FindByID(ctx, "p1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	got := read.GPUOperator
	if got == nil || got.State != platformdomain.GPUOperatorReady || got.Settings.DRADriverVersion != "v1.0.1" ||
		read.GPUStackOwner != platformdomain.GPUStackOwnerGPUOperator {
		t.Fatalf("FindByID GPU Operator = %+v, owner %s; want ready DRA v1.0.1 under gpu-operator", got, read.GPUStackOwner)
	}

	if _, err := repo.UpdateGPUOperatorState(ctx, "p1", "wf-2", platformdomain.GPUOperatorAbsent, time.Now().UTC()); err != nil {
		t.Fatalf("UpdateGPUOperatorState(absent): %v", err)
	}
	if read, _ = repo.FindByID(ctx, "p1"); read.GPUStackOwner != platformdomain.GPUStackOwnerProvisioning {
		t.Errorf("owner after absent = %s, want provisioning", read.GPUStackOwner)
	}

	if err := repo.SetGPUOperator(ctx, "p1", nil, platformdomain.GPUStackOwnerProvisioning); err != nil {
		t.Fatalf("SetGPUOperator(nil): %v", err)
	}
	if read, _ = repo.FindByID(ctx, "p1"); read.GPUOperator != nil {
		t.Errorf("GPU Operator after nil write = %+v, want removed", read.GPUOperator)
	}
	if err := repo.SetGPUOperator(ctx, "missing", record, platformdomain.GPUStackOwnerGPUOperator); err != platformdomain.ErrPlatformNotFound {
		t.Errorf("SetGPUOperator(missing) error = %v, want ErrPlatformNotFound", err)
	}
}
