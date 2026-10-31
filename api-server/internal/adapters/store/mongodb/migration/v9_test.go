package migration

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// decodeV9 reads a seeded Workflow document the way declareWorkflowPlans does.
func decodeV9(t *testing.T, doc bson.M) v9Workflow {
	t.Helper()
	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var w v9Workflow
	if err := bson.Unmarshal(raw, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return w
}

func typesOf(t *testing.T, value any) []string {
	t.Helper()
	completion, _ := value.(bson.A)
	types := make([]string, 0, len(completion))
	for _, item := range completion {
		types = append(types, item.(bson.M)["type"].(string))
	}
	return types
}

// The declarations v9 writes reproduce what the old driver derived at run time from Task kinds,
// Task ids, and parameters, so an adopted Workflow behaves as it would have.
func TestV9DeclarationsReproduceTheOldRules(t *testing.T) {
	deploy := decodeV9(t, bson.M{"_id": "wf-deploy", "kind": "deploy-kubernetes", "tasks": bson.A{
		bson.M{"id": "provision-s1", "kind": "provision-os", "runner": "provisioner", "job": "ensure-os"},
		bson.M{"id": "configure-ntp-s1", "kind": "ansible-playbook", "runner": "ansible", "job": "ensure-os"},
		bson.M{"id": "install-platform", "kind": "ansible-playbook", "runner": "ansible", "job": "install-platform"},
		bson.M{"id": "install-gpu-operator", "kind": "ansible-playbook", "runner": "ansible", "parameters": bson.M{
			"gpuOperatorPlatformId": "plat-1", "gpuOperatorRecordId": "wf-0", "gpuOperatorMode": "present",
		}},
	}})
	set := v9Declarations(deploy)
	if want := (bson.A{bson.M{"name": "ensure-os", "retry": "job"}, bson.M{"name": "install-platform", "retry": "job"}}); !reflect.DeepEqual(set["jobs"], want) {
		t.Errorf("jobs = %v, want %v", set["jobs"], want)
	}
	for path, want := range map[string][]string{
		"tasks.0.completion": {v9ServerDeployment},
		"tasks.2.completion": {v9KubernetesCredential},
		"tasks.3.completion": {v9GPUOperatorState},
	} {
		if got := typesOf(t, set[path]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	if _, declared := set["tasks.1.completion"]; declared {
		t.Errorf("the time Task got a completion: %v", set["tasks.1.completion"])
	}
	gpu := set["tasks.3.completion"].(bson.A)[0].(bson.M)["params"]
	if want := (bson.M{"platformId": "plat-1", "recordId": "wf-0", "mode": "present"}); !reflect.DeepEqual(gpu, want) {
		t.Errorf("gpu params = %v, want %v", gpu, want)
	}
	if want := (bson.M{"compensation": v9ReturnCanceledDeployment}); !reflect.DeepEqual(set["tasks.0.onCancel"], want) {
		t.Errorf("provision onCancel = %v, want %v", set["tasks.0.onCancel"], want)
	}

	slurm := decodeV9(t, bson.M{"_id": "wf-slurm", "kind": "configure-slurm", "tasks": bson.A{
		bson.M{"id": "install-platform", "kind": "ansible-playbook", "runner": "ansible"},
		bson.M{"id": "install-omnistat", "kind": "ansible-playbook", "runner": "ansible", "parameters": bson.M{"omnistatPlatformId": "plat-2"}},
		bson.M{"id": "install-device-metrics-exporter", "kind": "ansible-playbook", "runner": "ansible", "parameters": bson.M{
			"deviceMetricsExporterPlatformId": "plat-2", "deviceMetricsExporterRecordId": "wf-1",
		}},
		bson.M{"id": "install-docker-ce", "kind": "ansible-playbook", "runner": "ansible", "parameters": bson.M{
			"softwareMode": "install", "softwareKind": "docker-ce", "serverIds": bson.A{"s1"},
		}},
	}})
	set = v9Declarations(slurm)
	for path, want := range map[string][]string{
		"tasks.0.completion": {v9SlurmCredential},
		"tasks.1.completion": {v9OmnistatState, v9OmnistatResult},
		"tasks.2.completion": {v9DeviceMetricsExporterState, v9DeviceMetricsExporterResult},
		"tasks.3.completion": {v9SoftwareFailure},
	} {
		if got := typesOf(t, set[path]); !reflect.DeepEqual(got, want) {
			t.Errorf("slurm %s = %v, want %v", path, got, want)
		}
	}
	software := set["tasks.3.completion"].(bson.A)[0].(bson.M)["params"]
	if want := (bson.M{"softwareKind": "docker-ce", "serverIds": bson.A{"s1"}}); !reflect.DeepEqual(software, want) {
		t.Errorf("software params = %v, want %v", software, want)
	}
	if _, declared := set["jobs"]; declared {
		t.Errorf("a Workflow without named Jobs got jobs: %v", set["jobs"])
	}

	verify := decodeV9(t, bson.M{"_id": "wf-verify", "kind": "verify-os-image", "tasks": bson.A{
		bson.M{"id": "provision", "kind": "provision-os", "runner": "provisioner"},
		bson.M{"id": "return", "kind": "recover-server", "runner": "provisioner"},
		bson.M{"id": "release", "kind": "release-os", "runner": "provisioner"},
	}})
	set = v9Declarations(verify)
	if _, declared := set["tasks.0.onCancel"]; declared {
		t.Errorf("a verification's provision got a compensation: %v", set["tasks.0.onCancel"])
	}
	if want := (bson.M{"run": true}); !reflect.DeepEqual(set["tasks.1.onCancel"], want) {
		t.Errorf("verification recover onCancel = %v, want %v", set["tasks.1.onCancel"], want)
	}
	if got := typesOf(t, set["tasks.2.completion"]); !reflect.DeepEqual(got, []string{v9ServerDeploymentCleared}) {
		t.Errorf("release completion = %v", got)
	}

	vm := decodeV9(t, bson.M{"_id": "wf-vm", "kind": "enroll-virtual-machines", "tasks": bson.A{
		bson.M{"id": "enroll-a", "kind": "enroll-virtual-machine", "runner": "internal", "job": "vm-a"},
	}})
	if want := (bson.A{bson.M{"name": "vm-a", "retry": "task"}}); !reflect.DeepEqual(v9Declarations(vm)["jobs"], want) {
		t.Errorf("enrollment jobs = %v, want %v", v9Declarations(vm)["jobs"], want)
	}

	uninstall := decodeV9(t, bson.M{"_id": "wf-uninstall", "kind": "uninstall-slurm", "tasks": bson.A{
		bson.M{"id": "uninstall-platform", "kind": "ansible-playbook", "runner": "ansible"},
	}})
	if got := typesOf(t, v9Declarations(uninstall)["tasks.0.completion"]); !reflect.DeepEqual(got, []string{v9PlatformUninstall}) {
		t.Errorf("uninstall completion = %v", got)
	}

	declared := decodeV9(t, bson.M{"_id": "wf-new", "kind": "deploy-os", "jobs": bson.A{}, "tasks": bson.A{
		bson.M{"id": "provision-s1", "kind": "provision-os", "runner": "provisioner", "completion": bson.A{}, "onCancel": bson.M{}},
	}})
	if set := v9Declarations(declared); len(set) != 0 {
		t.Errorf("a Workflow that already declares its plan got %v", set)
	}
}

// TestMigrateV8ToV9_AdoptsWorkflowsAndConvertsLeases runs the whole step against MongoDB: plans
// are declared, only the unexpired leases of unfinished Workflows become ownership, and the old
// queue and indexes are gone. A second run changes nothing.
func TestMigrateV8ToV9_AdoptsWorkflowsAndConvertsLeases(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	now := time.Now().UTC()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	workflows := db.Collection("workflows")
	if _, err := workflows.InsertMany(ctx, []any{
		bson.M{"_id": "wf-deploy", "schemaVersion": 4, "kind": "deploy-os", "status": "running", "tasks": bson.A{
			bson.M{"id": "provision-s1", "kind": "provision-os", "runner": "provisioner", "status": "running", "attempt": 1},
		}},
		bson.M{"_id": "wf-slurm", "schemaVersion": 4, "kind": "configure-slurm", "status": "requires_attention", "tasks": bson.A{}},
		bson.M{"_id": "wf-done", "schemaVersion": 4, "kind": "deploy-os", "status": "succeeded", "tasks": bson.A{}},
	}); err != nil {
		t.Fatalf("seed workflows: %v", err)
	}
	if _, err := workflows.Indexes().CreateOne(ctx, mongoIndex("operation_v3_starter", "startState")); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	if _, err := db.Collection("resource_leases").InsertMany(ctx, []any{
		bson.M{"_id": "server:s1", "owner": "swallow-operation/wf-deploy", "expiresAt": future, "fencingToken": 4, "updatedAt": past},
		bson.M{"_id": "server:s2", "owner": "swallow-operation/wf-done", "expiresAt": future, "fencingToken": 2},
		bson.M{"_id": "server:s3", "owner": "swallow-operation/wf-slurm", "expiresAt": past, "fencingToken": 7},
		bson.M{"_id": "server:s4", "fencingToken": 9},
		bson.M{"_id": "server:s5", "owner": "swallow-operation/wf-slurm/job/install", "expiresAt": future, "fencingToken": 1},
		bson.M{"_id": "server:s6", "owner": "wf-new", "acquiredAt": past},
	}); err != nil {
		t.Fatalf("seed leases: %v", err)
	}
	if _, err := db.Collection("ansible_executions").InsertOne(ctx, bson.M{"_id": "run-1"}); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{"_id": "database", "version": 8, "updatedAt": now}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if err := Migrate(ctx, db, true); err != nil {
			t.Fatalf("Migrate run %d: %v", run, err)
		}
		if run == 1 {
			if err := migrateV8ToV9(ctx, db); err != nil {
				t.Fatalf("rerun of v9: %v", err)
			}
		}
	}

	var deployed bson.M
	if err := workflows.FindOne(ctx, bson.M{"_id": "wf-deploy"}).Decode(&deployed); err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	task := deployed["tasks"].(bson.A)[0].(bson.M)
	if got := typesOf(t, task["completion"]); !reflect.DeepEqual(got, []string{v9ServerDeployment}) {
		t.Errorf("completion = %v, want one server-deployment", got)
	}
	if _, dispatched := deployed["dispatch"]; dispatched {
		t.Errorf("dispatch = %v, want none so the controller adopts the Workflow", deployed["dispatch"])
	}

	owners := map[string]string{}
	cursor, err := db.Collection("resource_leases").Find(ctx, bson.M{})
	if err != nil {
		t.Fatalf("read leases: %v", err)
	}
	var leases []bson.M
	if err := cursor.All(ctx, &leases); err != nil {
		t.Fatalf("decode leases: %v", err)
	}
	for _, lease := range leases {
		owners[lease["_id"].(string)] = lease["owner"].(string)
		for _, field := range []string{"expiresAt", "fencingToken"} {
			if _, present := lease[field]; present {
				t.Errorf("lease %v still has %s", lease["_id"], field)
			}
		}
	}
	if want := map[string]string{"server:s1": "wf-deploy", "server:s5": "wf-slurm", "server:s6": "wf-new"}; !reflect.DeepEqual(owners, want) {
		t.Errorf("owners = %v, want %v", owners, want)
	}

	if names, _ := db.ListCollectionNames(ctx, bson.M{"name": "ansible_executions"}); len(names) != 0 {
		t.Error("ansible_executions still exists")
	}
	specs, err := workflows.Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	for _, spec := range specs {
		if spec.Name == "operation_v3_starter" {
			t.Error("the Starter index still exists")
		}
	}
}

func mongoIndex(name, field string) mongo.IndexModel {
	return mongo.IndexModel{Keys: bson.D{{Key: field, Value: 1}}, Options: options.Index().SetName(name)}
}
