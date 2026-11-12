// Package validation is the MongoDB store of the Validation context (decision 081): user-defined
// Validation Suites, Validation Runs with their results, and Validation Schedules. The documents
// below are the persistence shape; the domain types carry no storage tags. Nothing here holds a
// business rule: the domain decides what a run records and how a schedule's spec changes.
package validation

import (
	"reflect"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"

	"github.com/maple52046/swallow/internal/core/convergence"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// registry decodes arrays held in untyped fields (run and schedule parameters) as []any, the shape
// the domain and the API read them in, instead of the driver's primitive.A.
var registry = func() *bsoncodec.Registry {
	r := bson.NewRegistry()
	r.RegisterTypeMapEntry(bson.TypeArray, reflect.TypeOf([]any{}))
	return r
}()

type suiteDoc struct {
	ID             string    `bson:"_id"`
	Name           string    `bson:"name"`
	Description    string    `bson:"description,omitempty"`
	Script         string    `bson:"script"`
	ScriptSHA256   string    `bson:"scriptSha256"`
	RunAs          string    `bson:"runAs"`
	TimeoutSeconds int       `bson:"timeoutSeconds"`
	Impact         string    `bson:"impact"`
	Revision       int       `bson:"revision"`
	Retired        bool      `bson:"retired"`
	CreatedAt      time.Time `bson:"createdAt"`
	UpdatedAt      time.Time `bson:"updatedAt"`
	CreatedBy      string    `bson:"createdBy,omitempty"`
}

func toSuiteDoc(suite validationdomain.Suite) suiteDoc {
	return suiteDoc{
		ID: suite.ID, Name: suite.Name, Description: suite.Description, Script: suite.Script, ScriptSHA256: suite.ScriptSHA256,
		RunAs: string(suite.RunAs), TimeoutSeconds: suite.TimeoutSeconds, Impact: string(suite.Impact),
		Revision: suite.Revision, Retired: suite.Retired, CreatedAt: suite.CreatedAt, UpdatedAt: suite.UpdatedAt,
		CreatedBy: suite.CreatedBy,
	}
}

// toSuite rebuilds a user suite; the domain's NewUserSuite derives the fields it does not store
// (target, the script check, the budget), so one rule produces both.
func toSuite(doc suiteDoc) validationdomain.Suite {
	suite, err := validationdomain.NewUserSuite(doc.ID, validationdomain.UserSuiteInput{
		Name: doc.Name, Description: doc.Description, Script: doc.Script, RunAs: validationdomain.RunAs(doc.RunAs),
		TimeoutSeconds: doc.TimeoutSeconds, Impact: validationdomain.Impact(doc.Impact),
	}, doc.CreatedBy, doc.CreatedAt)
	if err != nil {
		// A stored suite always passed the same rule; keep what is stored readable regardless.
		suite = validationdomain.Suite{ID: doc.ID, Source: validationdomain.SourceUser, Name: doc.Name, Script: doc.Script,
			RunAs: validationdomain.RunAs(doc.RunAs), TimeoutSeconds: doc.TimeoutSeconds, Impact: validationdomain.Impact(doc.Impact),
			Target: validationdomain.TargetServers}
	}
	suite.ScriptSHA256, suite.Revision, suite.Retired = doc.ScriptSHA256, doc.Revision, doc.Retired
	suite.CreatedAt, suite.UpdatedAt, suite.CreatedBy = doc.CreatedAt, doc.UpdatedAt, doc.CreatedBy
	return suite
}

type runSuiteDoc struct {
	ID           string `bson:"id"`
	Source       string `bson:"source"`
	Name         string `bson:"name"`
	Version      string `bson:"version,omitempty"`
	Revision     int    `bson:"revision,omitempty"`
	ScriptSHA256 string `bson:"scriptSha256,omitempty"`
	Script       string `bson:"script,omitempty"`
}

type checkDoc struct {
	ID          string `bson:"id"`
	Title       string `bson:"title"`
	Description string `bson:"description,omitempty"`
	Gating      bool   `bson:"gating"`
}

type targetDoc struct {
	Kind       string   `bson:"kind"`
	ServerIDs  []string `bson:"serverIds"`
	PlatformID string   `bson:"platformId,omitempty"`
	MemberIDs  []string `bson:"memberIds,omitempty"`
}

type triggerDoc struct {
	Kind       string `bson:"kind"`
	ScheduleID string `bson:"scheduleId,omitempty"`
	WorkflowID string `bson:"workflowId,omitempty"`
}

type measurementDoc struct {
	Name     string  `bson:"name"`
	Value    float64 `bson:"value"`
	Unit     string  `bson:"unit,omitempty"`
	Expected string  `bson:"expected,omitempty"`
}

type resultDoc struct {
	Check        string           `bson:"check"`
	TargetKind   string           `bson:"targetKind"`
	TargetID     string           `bson:"targetId"`
	Verdict      string           `bson:"verdict"`
	Summary      string           `bson:"summary,omitempty"`
	Measurements []measurementDoc `bson:"measurements,omitempty"`
	Evidence     string           `bson:"evidence,omitempty"`
	ObservedAt   time.Time        `bson:"observedAt"`
	TaskID       string           `bson:"taskId,omitempty"`
	Attempt      int              `bson:"attempt,omitempty"`
}

type runDoc struct {
	ID           string         `bson:"_id"`
	SiteID       string         `bson:"siteId"`
	Suite        runSuiteDoc    `bson:"suite"`
	Checks       []checkDoc     `bson:"checks"`
	Target       targetDoc      `bson:"target"`
	Parameters   map[string]any `bson:"parameters,omitempty"`
	Mode         string         `bson:"mode"`
	Trigger      triggerDoc     `bson:"trigger"`
	WorkflowID   string         `bson:"workflowId"`
	Status       string         `bson:"status"`
	StatusReason string         `bson:"statusReason,omitempty"`
	Verdict      string         `bson:"verdict,omitempty"`
	Results      []resultDoc    `bson:"results"`
	RequestedBy  string         `bson:"requestedBy,omitempty"`
	RequestedAt  time.Time      `bson:"requestedAt"`
	StartedAt    *time.Time     `bson:"startedAt,omitempty"`
	FinishedAt   *time.Time     `bson:"finishedAt,omitempty"`
	ExpiresAt    time.Time      `bson:"expiresAt"`
}

func toRunDoc(run validationdomain.Run) runDoc {
	doc := runDoc{
		ID: run.ID, SiteID: run.SiteID,
		Suite: runSuiteDoc{ID: run.Suite.ID, Source: string(run.Suite.Source), Name: run.Suite.Name, Version: run.Suite.Version,
			Revision: run.Suite.Revision, ScriptSHA256: run.Suite.ScriptSHA256, Script: run.Suite.Script},
		Target: targetDoc{Kind: string(run.Target.Kind), ServerIDs: nonNil(run.Target.ServerIDs), PlatformID: run.Target.PlatformID,
			MemberIDs: run.Target.MemberIDs},
		Parameters: run.Parameters, Mode: string(run.Mode),
		Trigger:    triggerDoc{Kind: string(run.Trigger.Kind), ScheduleID: run.Trigger.ScheduleID, WorkflowID: run.Trigger.WorkflowID},
		WorkflowID: run.WorkflowID, Status: string(run.Status), StatusReason: run.StatusReason, Verdict: string(run.Verdict),
		Results: []resultDoc{}, RequestedBy: run.RequestedBy, RequestedAt: run.RequestedAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ExpiresAt: run.ExpiresAt,
	}
	for _, check := range run.Checks {
		doc.Checks = append(doc.Checks, checkDoc{ID: check.ID, Title: check.Title, Description: check.Description, Gating: check.Gating})
	}
	for _, result := range run.Results {
		stored := resultDoc{Check: result.Check, TargetKind: result.Target.Kind, TargetID: result.Target.ID,
			Verdict: string(result.Verdict), Summary: result.Summary, Evidence: result.Evidence, ObservedAt: result.ObservedAt,
			TaskID: result.TaskID, Attempt: result.Attempt}
		for _, measurement := range result.Measurements {
			stored.Measurements = append(stored.Measurements, measurementDoc{Name: measurement.Name, Value: measurement.Value,
				Unit: measurement.Unit, Expected: measurement.Expected})
		}
		doc.Results = append(doc.Results, stored)
	}
	return doc
}

func toRun(doc runDoc) validationdomain.Run {
	run := validationdomain.Run{
		ID: doc.ID, SiteID: doc.SiteID,
		Suite: validationdomain.RunSuite{ID: doc.Suite.ID, Source: validationdomain.Source(doc.Suite.Source), Name: doc.Suite.Name,
			Version: doc.Suite.Version, Revision: doc.Suite.Revision, ScriptSHA256: doc.Suite.ScriptSHA256, Script: doc.Suite.Script},
		Target: validationdomain.Target{Kind: validationdomain.TargetKind(doc.Target.Kind), ServerIDs: doc.Target.ServerIDs,
			PlatformID: doc.Target.PlatformID, MemberIDs: doc.Target.MemberIDs},
		Parameters: doc.Parameters, Mode: validationdomain.Mode(doc.Mode),
		Trigger: validationdomain.Trigger{Kind: validationdomain.TriggerKind(doc.Trigger.Kind), ScheduleID: doc.Trigger.ScheduleID,
			WorkflowID: doc.Trigger.WorkflowID},
		WorkflowID: doc.WorkflowID, Status: validationdomain.Status(doc.Status), StatusReason: doc.StatusReason,
		Verdict: validationdomain.Verdict(doc.Verdict), RequestedBy: doc.RequestedBy, RequestedAt: doc.RequestedAt,
		StartedAt: doc.StartedAt, FinishedAt: doc.FinishedAt, ExpiresAt: doc.ExpiresAt,
	}
	for _, check := range doc.Checks {
		run.Checks = append(run.Checks, validationdomain.Check{ID: check.ID, Title: check.Title, Description: check.Description, Gating: check.Gating})
	}
	for _, stored := range doc.Results {
		result := validationdomain.Result{Check: stored.Check,
			Target:  validationdomain.ResultTarget{Kind: stored.TargetKind, ID: stored.TargetID},
			Verdict: validationdomain.Verdict(stored.Verdict), Summary: stored.Summary, Evidence: stored.Evidence,
			ObservedAt: stored.ObservedAt, TaskID: stored.TaskID, Attempt: stored.Attempt}
		for _, measurement := range stored.Measurements {
			result.Measurements = append(result.Measurements, validationdomain.Measurement{Name: measurement.Name,
				Value: measurement.Value, Unit: measurement.Unit, Expected: measurement.Expected})
		}
		run.Results = append(run.Results, result)
	}
	return run
}

type conditionDoc struct {
	Type               string    `bson:"type"`
	Status             string    `bson:"status"`
	Reason             string    `bson:"reason"`
	Message            string    `bson:"message,omitempty"`
	LastTransitionTime time.Time `bson:"lastTransitionTime"`
	ObservedGeneration int64     `bson:"observedGeneration"`
}

type scheduleSpecDoc struct {
	Name       string         `bson:"name"`
	SuiteID    string         `bson:"suiteId"`
	ServerIDs  []string       `bson:"serverIds"`
	PlatformID string         `bson:"platformId,omitempty"`
	Parameters map[string]any `bson:"parameters,omitempty"`
	Trigger    triggerSpecDoc `bson:"trigger"`
	Enabled    bool           `bson:"enabled"`
}

type triggerSpecDoc struct {
	Kind     string `bson:"kind"`
	Cron     string `bson:"cron,omitempty"`
	Timezone string `bson:"timezone,omitempty"`
}

type scheduleStatusDoc struct {
	ObservedGeneration int64          `bson:"observedGeneration"`
	NextRunAt          *time.Time     `bson:"nextRunAt,omitempty"`
	LastRunID          string         `bson:"lastRunId,omitempty"`
	LastRunAt          *time.Time     `bson:"lastRunAt,omitempty"`
	LastSkipReason     string         `bson:"lastSkipReason,omitempty"`
	LastDeploymentKey  string         `bson:"lastDeploymentKey,omitempty"`
	Conditions         []conditionDoc `bson:"conditions,omitempty"`
}

type scheduleDoc struct {
	ID         string            `bson:"_id"`
	SiteID     string            `bson:"siteId"`
	Generation int64             `bson:"generation"`
	Spec       scheduleSpecDoc   `bson:"spec"`
	Status     scheduleStatusDoc `bson:"status"`
	CreatedAt  time.Time         `bson:"createdAt"`
	UpdatedAt  time.Time         `bson:"updatedAt"`
}

func toScheduleSpecDoc(spec validationdomain.ScheduleSpec) scheduleSpecDoc {
	return scheduleSpecDoc{
		Name: spec.Name, SuiteID: spec.SuiteID, ServerIDs: nonNil(spec.ServerIDs), PlatformID: spec.PlatformID,
		Parameters: spec.Parameters, Enabled: spec.Enabled,
		Trigger: triggerSpecDoc{Kind: string(spec.Trigger.Kind), Cron: spec.Trigger.Cron, Timezone: spec.Trigger.Timezone},
	}
}

func toScheduleStatusDoc(status validationdomain.ScheduleStatus) scheduleStatusDoc {
	doc := scheduleStatusDoc{
		ObservedGeneration: status.ObservedGeneration, NextRunAt: status.NextRunAt, LastRunID: status.LastRunID,
		LastRunAt: status.LastRunAt, LastSkipReason: status.LastSkipReason, LastDeploymentKey: status.LastDeploymentKey,
	}
	for _, condition := range status.Conditions {
		doc.Conditions = append(doc.Conditions, conditionDoc{Type: string(condition.Type), Status: string(condition.Status),
			Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime,
			ObservedGeneration: condition.ObservedGeneration})
	}
	return doc
}

func toSchedule(doc scheduleDoc) validationdomain.Schedule {
	schedule := validationdomain.Schedule{
		ID: doc.ID, SiteID: doc.SiteID, Generation: doc.Generation, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		Spec: validationdomain.ScheduleSpec{
			Name: doc.Spec.Name, SuiteID: doc.Spec.SuiteID, ServerIDs: doc.Spec.ServerIDs, PlatformID: doc.Spec.PlatformID,
			Parameters: doc.Spec.Parameters, Enabled: doc.Spec.Enabled,
			Trigger: validationdomain.ScheduleTrigger{Kind: validationdomain.ScheduleTriggerKind(doc.Spec.Trigger.Kind),
				Cron: doc.Spec.Trigger.Cron, Timezone: doc.Spec.Trigger.Timezone},
		},
		Status: validationdomain.ScheduleStatus{
			ObservedGeneration: doc.Status.ObservedGeneration, NextRunAt: doc.Status.NextRunAt, LastRunID: doc.Status.LastRunID,
			LastRunAt: doc.Status.LastRunAt, LastSkipReason: doc.Status.LastSkipReason, LastDeploymentKey: doc.Status.LastDeploymentKey,
		},
	}
	for _, condition := range doc.Status.Conditions {
		schedule.Status.Conditions = append(schedule.Status.Conditions, convergence.Condition{
			Type: convergence.ConditionType(condition.Type), Status: convergence.ConditionStatus(condition.Status),
			Reason: condition.Reason, Message: condition.Message, LastTransitionTime: condition.LastTransitionTime,
			ObservedGeneration: condition.ObservedGeneration,
		})
	}
	return schedule
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
