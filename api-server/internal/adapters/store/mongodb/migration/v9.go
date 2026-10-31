package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// The plan declarations schema v9 writes onto Workflows accepted before decision 072, which
// derived the same behavior from Task kinds, Task ids, and parameters at run time. The values are
// those of the plan catalog at v9 and stay frozen with the migration.
const (
	v9RetryTask = "task"
	v9RetryJob  = "job"

	v9ReturnCanceledDeployment = "return-canceled-deployment"

	v9ServerDeployment            = "server-deployment"
	v9ServerDeploymentCleared     = "server-deployment-cleared"
	v9SoftwareFailure             = "software-assignment-failure"
	v9GPUOperatorState            = "gpu-operator-state"
	v9OmnistatState               = "omnistat-state"
	v9OmnistatResult              = "omnistat-result"
	v9DeviceMetricsExporterState  = "device-metrics-exporter-state"
	v9DeviceMetricsExporterResult = "device-metrics-exporter-result"
	v9KubernetesCredential        = "kubernetes-credential"
	v9SlurmCredential             = "slurm-credential"
	v9PlatformUninstall           = "platform-uninstall"
)

// v9Workflow is the part of a schema-v4 Workflow document v9 reads.
type v9Workflow struct {
	ID     string        `bson:"_id"`
	Kind   string        `bson:"kind"`
	Status string        `bson:"status"`
	Jobs   bson.RawValue `bson:"jobs"`
	Tasks  []v9Task      `bson:"tasks"`
}

// v9Task is the part of a Task v9 reads; an absent declaration has a zero RawValue.
type v9Task struct {
	ID         string        `bson:"id"`
	Kind       string        `bson:"kind"`
	Runner     string        `bson:"runner"`
	Job        string        `bson:"job"`
	Parameters bson.M        `bson:"parameters"`
	Completion bson.RawValue `bson:"completion"`
	OnCancel   bson.RawValue `bson:"onCancel"`
}

// migrateV8ToV9 moves the database to store-driven Workflow execution (decision 072):
//
//   - every Workflow gets the Job declarations, Task completions, and cancel behaviors its
//     launcher now declares, derived by the rules the old driver applied at run time;
//   - each `resource_leases` document of the old lease shape (a Temporal execution owner, an
//     expiry, a fencing token) becomes the ownership of its Workflow when that Workflow is
//     unfinished and the lease had not expired, and is deleted otherwise;
//   - the indexes of the old Starter and Reconciler queries and the `ansible_executions` queue are
//     dropped.
//
// The dispatch record is left empty, so the workflow-dispatch controller adopts every unfinished
// Workflow at its first pass. Every step writes only what is absent or of the old shape, so an
// interrupted run resumes safely.
func migrateV8ToV9(ctx context.Context, db *mongo.Database) error {
	if err := declareWorkflowPlans(ctx, db); err != nil {
		return fmt.Errorf("declare workflow plans: %w", err)
	}
	if err := convertResourceLeases(ctx, db, time.Now().UTC()); err != nil {
		return fmt.Errorf("convert resource leases: %w", err)
	}
	workflows := db.Collection("workflows")
	for _, name := range []string{"operation_v3_starter", "operation_v3_recovery"} {
		if err := dropIndexIfExists(ctx, workflows, name); err != nil {
			return err
		}
	}
	return db.Collection("ansible_executions").Drop(ctx)
}

func declareWorkflowPlans(ctx context.Context, db *mongo.Database) error {
	workflows := db.Collection("workflows")
	cursor, err := workflows.Find(ctx, bson.M{"schemaVersion": 4})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var w v9Workflow
		if err := cursor.Decode(&w); err != nil {
			return err
		}
		set := v9Declarations(w)
		if len(set) == 0 {
			continue
		}
		if _, err := workflows.UpdateOne(ctx, bson.M{"_id": w.ID}, bson.M{"$set": set}); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// v9Declarations returns the fields to set on one Workflow, by path.
func v9Declarations(w v9Workflow) bson.M {
	set := bson.M{}
	if w.Jobs.Type == 0 {
		if jobs := v9Jobs(w); len(jobs) > 0 {
			set["jobs"] = jobs
		}
	}
	for i, task := range w.Tasks {
		if task.Completion.Type == 0 {
			if completion := v9Completion(w.Kind, task); len(completion) > 0 {
				set[fmt.Sprintf("tasks.%d.completion", i)] = completion
			}
		}
		if task.OnCancel.Type == 0 {
			if onCancel := v9OnCancel(w.Kind, task); onCancel != nil {
				set[fmt.Sprintf("tasks.%d.onCancel", i)] = onCancel
			}
		}
	}
	return set
}

// v9Jobs declares each named Job once, in Task order. A failed Task of a named Job reran its whole
// Job, except in a virtual machine enrollment, whose Jobs group independent per-VM Tasks.
func v9Jobs(w v9Workflow) bson.A {
	retry := v9RetryJob
	if w.Kind == "enroll-virtual-machines" {
		retry = v9RetryTask
	}
	var jobs bson.A
	seen := map[string]bool{}
	for _, task := range w.Tasks {
		if task.Job != "" && !seen[task.Job] {
			seen[task.Job] = true
			jobs = append(jobs, bson.M{"name": task.Job, "retry": retry})
		}
	}
	return jobs
}

// v9Completion returns the completions the old observers and the Ansible completion switch applied
// to the Task.
func v9Completion(workflowKind string, task v9Task) bson.A {
	switch task.Kind {
	case "provision-os":
		return bson.A{bson.M{"type": v9ServerDeployment}}
	case "release-os":
		return bson.A{bson.M{"type": v9ServerDeploymentCleared}}
	}
	if task.Runner != "ansible" {
		return nil
	}
	param := func(name string) string {
		value, _ := task.Parameters[name].(string)
		return value
	}
	feature := func(kind, platformParam, recordParam string, extra bson.M) bson.M {
		params := bson.M{"platformId": param(platformParam)}
		if record := param(recordParam); record != "" {
			params["recordId"] = record
		}
		for name, value := range extra {
			params[name] = value
		}
		return bson.M{"type": kind, "params": params}
	}

	var completion bson.A
	if param("softwareMode") == "install" && param("softwareKind") != "" {
		completion = append(completion, bson.M{"type": v9SoftwareFailure, "params": bson.M{
			"softwareKind": param("softwareKind"), "serverIds": task.Parameters["serverIds"],
		}})
	}
	switch {
	case task.ID == "install-gpu-operator" && param("gpuOperatorPlatformId") != "":
		completion = append(completion, feature(v9GPUOperatorState, "gpuOperatorPlatformId", "gpuOperatorRecordId",
			bson.M{"mode": param("gpuOperatorMode")}))
	case task.ID == "install-omnistat" && param("omnistatPlatformId") != "":
		completion = append(completion, feature(v9OmnistatState, "omnistatPlatformId", "omnistatRecordId", nil))
		if workflowKind == "configure-slurm" {
			completion = append(completion, bson.M{"type": v9OmnistatResult})
		}
	case task.ID == "install-device-metrics-exporter" && param("deviceMetricsExporterPlatformId") != "":
		completion = append(completion, feature(v9DeviceMetricsExporterState, "deviceMetricsExporterPlatformId",
			"deviceMetricsExporterRecordId", nil))
		if workflowKind == "configure-slurm" {
			completion = append(completion, bson.M{"type": v9DeviceMetricsExporterResult})
		}
	}
	switch workflowKind {
	case "deploy-kubernetes":
		if task.ID == "install-platform" {
			completion = append(completion, bson.M{"type": v9KubernetesCredential})
		}
	case "configure-slurm":
		if task.ID == "install-platform" {
			completion = append(completion, bson.M{"type": v9SlurmCredential})
		}
	case "uninstall-kubernetes", "uninstall-slurm":
		completion = append(completion, bson.M{"type": v9PlatformUninstall})
	}
	return completion
}

// v9OnCancel returns the cancel behavior the old driver applied to the Task: an interrupted
// deployment is returned to Ready, except in an OS Image verification, whose recover-server Task
// runs on cancel instead.
func v9OnCancel(workflowKind string, task v9Task) bson.M {
	switch {
	case workflowKind == "verify-os-image" && task.Kind == "recover-server":
		return bson.M{"run": true}
	case workflowKind != "verify-os-image" && task.Kind == "provision-os":
		return bson.M{"compensation": v9ReturnCanceledDeployment}
	}
	return nil
}

// v9Lease is a `resource_leases` document; the old lease shape has a fencing token and, while
// held, an expiry.
type v9Lease struct {
	ResourceKey string     `bson:"_id"`
	Owner       string     `bson:"owner"`
	ExpiresAt   *time.Time `bson:"expiresAt"`
	UpdatedAt   *time.Time `bson:"updatedAt"`
}

func convertResourceLeases(ctx context.Context, db *mongo.Database, now time.Time) error {
	leases := db.Collection("resource_leases")
	cursor, err := leases.Find(ctx, bson.M{"$or": bson.A{
		bson.M{"fencingToken": bson.M{"$exists": true}}, bson.M{"expiresAt": bson.M{"$exists": true}},
	}})
	if err != nil {
		return err
	}
	var docs []v9Lease
	if err := cursor.All(ctx, &docs); err != nil {
		return err
	}
	for _, lease := range docs {
		owner, err := v9LeaseOwner(ctx, db, lease, now)
		if err != nil {
			return err
		}
		if owner == "" {
			if _, err := leases.DeleteOne(ctx, bson.M{"_id": lease.ResourceKey}); err != nil {
				return err
			}
			continue
		}
		acquiredAt := now
		if lease.UpdatedAt != nil {
			acquiredAt = *lease.UpdatedAt
		}
		if _, err := leases.UpdateOne(ctx, bson.M{"_id": lease.ResourceKey}, bson.M{
			"$set":   bson.M{"owner": owner, "acquiredAt": acquiredAt, "updatedAt": now},
			"$unset": bson.M{"expiresAt": "", "fencingToken": ""},
		}); err != nil {
			return err
		}
	}
	return nil
}

// v9LeaseOwner returns the Workflow that keeps the lease's resource, or "" when none does: a
// released or expired lease, or one whose Workflow is gone or finished. An old owner is the
// Temporal execution id, `swallow-operation/<workflow>` with an optional `/job/<name>` suffix.
func v9LeaseOwner(ctx context.Context, db *mongo.Database, lease v9Lease, now time.Time) (string, error) {
	if lease.Owner == "" || lease.ExpiresAt == nil || !lease.ExpiresAt.After(now) {
		return "", nil
	}
	id := strings.TrimPrefix(lease.Owner, "swallow-operation/")
	if i := strings.Index(id, "/job/"); i >= 0 {
		id = id[:i]
	}
	var w v9Workflow
	err := db.Collection("workflows").FindOne(ctx, bson.M{"_id": id, "schemaVersion": 4}).Decode(&w)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch w.Status {
	case "succeeded", "failed", "partially_succeeded", "canceled":
		return "", nil
	}
	return id, nil
}
