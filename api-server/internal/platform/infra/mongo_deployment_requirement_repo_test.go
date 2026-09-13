package infra

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

func TestDeploymentRequirementDocUsesPlatformTypeAsUniqueID(t *testing.T) {
	requirement := &platformdomain.DeploymentRequirement{
		PlatformType: platformdomain.PlatformTypeSlurm,
		MinimumResources: &platformdomain.MinimumResources{
			CPUCores: 4, MemoryMiB: 24576, StorageGB: 80,
		},
		UpdatedAt: time.Now().UTC(),
	}
	doc := deploymentRequirementToDoc(requirement)
	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded bson.M
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["_id"] != "slurm" {
		t.Errorf("_id = %v, want slurm", decoded["_id"])
	}
	if !reflect.DeepEqual(deploymentRequirementFromDoc(&doc), requirement) {
		t.Fatalf("requirement did not round trip")
	}
}
