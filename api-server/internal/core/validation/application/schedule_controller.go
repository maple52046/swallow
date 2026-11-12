package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/store"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// ScheduleControllerName is the controller that starts Validation Schedules' runs (validation.md §6).
const ScheduleControllerName = "validation-schedule"

// scheduleResync is how often every schedule is reconciled: cron times and new deployments are not
// on the schedule's change feed, so a minute bounds how late a run starts.
const scheduleResync = time.Minute

// scheduleRequester is the RequestedBy of the runs a schedule starts.
const scheduleRequester = "validation-schedule"

// The schedule condition reasons (contract validation.md, Schedules).
const (
	ReasonScheduleDisabled = "disabled"
	ReasonSuiteMissing     = "suite_missing"
	ReasonTargetMissing    = "target_missing"
)

// Deployments reads the latest deployment of a schedule's target as a key that changes with each one:
// for a Platform, its newest Workflow that succeeded in realizing its spec; for Servers, the newest
// succeeded deploy-os Workflow of each. "" when the target was never deployed.
type Deployments interface {
	PlatformDeployment(ctx context.Context, platformID string) (string, error)
	ServersDeployment(ctx context.Context, serverIDs []string) (string, error)
}

// ScheduleController realizes Validation Schedules: it starts a run when a cron time comes due or the
// target was deployed again, at most one unfinished run per schedule, and skips an occurrence whose
// run is refused (validation.md §6). Its runs go through Service.Start like any other.
type ScheduleController struct {
	service     *Service
	deployments Deployments
	now         func() time.Time
}

var (
	_ convergence.Controller   = (*ScheduleController)(nil)
	_ convergence.ResyncPeriod = (*ScheduleController)(nil)
)

// NewScheduleController builds the controller over the service and the deployment reader.
func NewScheduleController(service *Service, deployments Deployments) (*ScheduleController, error) {
	if service == nil || deployments == nil {
		return nil, errors.New("validation schedule controller: every dependency is required")
	}
	return &ScheduleController{service: service, deployments: deployments, now: time.Now}, nil
}

// Name implements convergence.Controller.
func (c *ScheduleController) Name() string { return ScheduleControllerName }

// Collections implements convergence.Controller: a spec change is reconciled at once; times and
// deployments are found by the resync.
func (c *ScheduleController) Collections() []string { return []string{"validation_schedules"} }

// ResyncInterval implements convergence.ResyncPeriod.
func (c *ScheduleController) ResyncInterval() time.Duration { return scheduleResync }

// KeysForChange implements convergence.Controller: a changed schedule is its own key.
func (c *ScheduleController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller.
func (c *ScheduleController) Keys(ctx context.Context) ([]string, error) {
	schedules, err := c.service.schedules.List(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(schedules))
	for _, schedule := range schedules {
		keys = append(keys, schedule.ID)
	}
	return keys, nil
}

// Reconcile implements convergence.Controller. It writes the status only when it changed.
func (c *ScheduleController) Reconcile(ctx context.Context, request convergence.Request) error {
	schedule, err := c.service.schedules.Get(ctx, request.Key)
	if errors.Is(err, validationdomain.ErrScheduleNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := c.now().UTC()
	status, err := c.next(ctx, schedule, now)
	if err != nil {
		return err
	}
	status.ObservedGeneration = schedule.Generation
	if reflect.DeepEqual(status, schedule.Status) {
		return nil
	}
	return c.service.schedules.WriteStatus(ctx, schedule.ID, status)
}

// next is the schedule's status after this pass, starting its run when one is due.
func (c *ScheduleController) next(ctx context.Context, schedule validationdomain.Schedule, now time.Time) (validationdomain.ScheduleStatus, error) {
	status := schedule.Status
	status.Conditions = slices.Clone(status.Conditions)
	spec := schedule.Spec
	fresh := status.ObservedGeneration != schedule.Generation
	realize := func(ok bool, reason, message string) {
		verdict := convergence.ConditionTrue
		if !ok {
			verdict = convergence.ConditionFalse
		}
		status.Conditions, _ = convergence.SetCondition(status.Conditions, convergence.Condition{
			Type: convergence.ConditionRealized, Status: verdict, Reason: reason, Message: message,
			ObservedGeneration: schedule.Generation,
		}, now)
	}
	if !spec.Enabled {
		status.NextRunAt = nil
		realize(false, ReasonScheduleDisabled, "The schedule is disabled.")
		return status, nil
	}
	if reason, message, err := c.missing(ctx, spec); err != nil || reason != "" {
		status.NextRunAt = nil
		if err == nil {
			realize(false, reason, message)
		}
		return status, err
	}
	realize(true, convergence.ReasonConverged, "")

	due := false
	switch spec.Trigger.Kind {
	case validationdomain.ScheduleCron:
		// A new or changed spec starts counting from now; a time missed while the worker was down
		// runs once, because the next time is counted from now again.
		due = !fresh && status.NextRunAt != nil && !now.Before(*status.NextRunAt)
		if fresh || status.NextRunAt == nil || due {
			status.NextRunAt = spec.NextAfter(now)
		}
	case validationdomain.ScheduleAfterDeployment:
		status.NextRunAt = nil
		key, err := c.deploymentKey(ctx, spec)
		if err != nil {
			return status, err
		}
		// The first observation of a spec only remembers the current deployment.
		due = !fresh && key != "" && key != status.LastDeploymentKey
		status.LastDeploymentKey = key
	}
	if !due {
		return status, nil
	}
	return c.occur(ctx, schedule, status, now)
}

// missing reports why the schedule cannot start runs: its suite was retired or its target is gone.
func (c *ScheduleController) missing(ctx context.Context, spec validationdomain.ScheduleSpec) (string, string, error) {
	suite, err := c.service.catalog.Get(ctx, spec.SuiteID)
	if errors.Is(err, validationdomain.ErrSuiteNotFound) || err == nil && suite.Retired {
		return ReasonSuiteMissing, fmt.Sprintf("Suite %s no longer exists.", spec.SuiteID), nil
	}
	if err != nil {
		return "", "", err
	}
	if spec.PlatformID != "" {
		if _, err := c.service.targets.Platform(ctx, spec.PlatformID); errors.Is(err, validationdomain.ErrTargetNotFound) {
			return ReasonTargetMissing, fmt.Sprintf("Platform %s no longer exists.", spec.PlatformID), nil
		} else if err != nil {
			return "", "", err
		}
	}
	for _, id := range spec.ServerIDs {
		if _, err := c.service.targets.Server(ctx, id); errors.Is(err, validationdomain.ErrTargetNotFound) {
			return ReasonTargetMissing, fmt.Sprintf("Server %s no longer exists.", id), nil
		} else if err != nil {
			return "", "", err
		}
	}
	return "", "", nil
}

func (c *ScheduleController) deploymentKey(ctx context.Context, spec validationdomain.ScheduleSpec) (string, error) {
	if spec.PlatformID != "" {
		return c.deployments.PlatformDeployment(ctx, spec.PlatformID)
	}
	return c.deployments.ServersDeployment(ctx, spec.ServerIDs)
}

// occur starts the run of one occurrence, or records why it is skipped: the previous run has not
// ended, or acceptance refused the run (a busy, locked, or not deployed target). A skipped
// occurrence is not retried; another error is, by returning it.
func (c *ScheduleController) occur(ctx context.Context, schedule validationdomain.Schedule, status validationdomain.ScheduleStatus,
	now time.Time,
) (validationdomain.ScheduleStatus, error) {
	if status.LastRunID != "" {
		previous, err := c.service.runs.Get(ctx, status.LastRunID)
		if err != nil && !errors.Is(err, validationdomain.ErrRunNotFound) {
			return status, err
		}
		if err == nil && previous.Status.Active() {
			status.LastSkipReason = fmt.Sprintf("Run %s had not ended.", previous.ID)
			return status, nil
		}
	}
	spec := schedule.Spec
	run, err := c.service.Start(ctx, StartInput{
		SuiteID: spec.SuiteID, ServerIDs: spec.ServerIDs, PlatformID: spec.PlatformID, Parameters: spec.Parameters,
		Trigger: validationdomain.Trigger{Kind: validationdomain.TriggerSchedule, ScheduleID: schedule.ID}, RequestedBy: scheduleRequester,
	})
	switch {
	case err == nil:
		status.LastRunID, status.LastRunAt, status.LastSkipReason = run.ID, &now, ""
		return status, nil
	case errors.Is(err, validationdomain.ErrRunRefused), errors.Is(err, validationdomain.ErrTargetNotRunnable),
		errors.Is(err, validationdomain.ErrTargetNotFound), errors.Is(err, validationdomain.ErrInvalidRequest),
		errors.Is(err, validationdomain.ErrSuiteNotFound):
		slog.Info("validation schedule occurrence skipped", "scheduleId", schedule.ID, "reason", err)
		status.LastSkipReason = err.Error()
		return status, nil
	default:
		return status, err
	}
}
