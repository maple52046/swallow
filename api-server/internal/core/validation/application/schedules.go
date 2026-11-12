package application

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// ScheduleWrite is a schedule spec as the API writes it, with the generation a replace expects
// (0 accepts any).
type ScheduleWrite struct {
	Spec               validationdomain.ScheduleSpec
	ExpectedGeneration int64
}

// CreateSchedule stores a new schedule at generation 1, after checking its spec against its suite
// and targets as a run would be (without the busy, lock, and automation checks, which each
// occurrence makes).
func (s *Service) CreateSchedule(ctx context.Context, write ScheduleWrite) (validationdomain.Schedule, error) {
	spec, siteID, err := s.checkScheduleSpec(ctx, write.Spec)
	if err != nil {
		return validationdomain.Schedule{}, err
	}
	now := s.now().UTC()
	schedule := validationdomain.Schedule{ID: uuid.NewString(), SiteID: siteID, Generation: 1, Spec: spec, CreatedAt: now, UpdatedAt: now}
	if err := s.schedules.Create(ctx, schedule); err != nil {
		return validationdomain.Schedule{}, err
	}
	return schedule, nil
}

// ReplaceSchedule replaces a schedule's spec and returns the schedule at its new generation.
func (s *Service) ReplaceSchedule(ctx context.Context, id string, write ScheduleWrite) (validationdomain.Schedule, error) {
	if _, err := s.schedules.Get(ctx, id); err != nil {
		return validationdomain.Schedule{}, err
	}
	spec, siteID, err := s.checkScheduleSpec(ctx, write.Spec)
	if err != nil {
		return validationdomain.Schedule{}, err
	}
	if _, err := s.schedules.WriteSpec(ctx, id, write.ExpectedGeneration, spec, siteID, s.now().UTC()); err != nil {
		return validationdomain.Schedule{}, err
	}
	return s.schedules.Get(ctx, id)
}

// GetSchedule returns one schedule.
func (s *Service) GetSchedule(ctx context.Context, id string) (validationdomain.Schedule, error) {
	return s.schedules.Get(ctx, id)
}

// ListSchedules returns the matching schedules by name.
func (s *Service) ListSchedules(ctx context.Context, filter validationdomain.ScheduleFilter) ([]validationdomain.Schedule, error) {
	schedules, err := s.schedules.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(schedules, func(i, j int) bool { return schedules[i].Spec.Name < schedules[j].Spec.Name })
	return schedules, nil
}

// DeleteSchedule removes a schedule; its runs stay.
func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	return s.schedules.Delete(ctx, id)
}

// checkScheduleSpec normalizes the spec and checks it against the suite and the targets: the suite
// exists and is not retired, the parameters match it, and the targets fit its kind and exist (a
// target that is not runnable right now is accepted: each occurrence checks again).
func (s *Service) checkScheduleSpec(ctx context.Context, spec validationdomain.ScheduleSpec) (validationdomain.ScheduleSpec, string, error) {
	spec, err := spec.Normalize()
	if err != nil {
		return validationdomain.ScheduleSpec{}, "", err
	}
	suite, err := s.catalog.Get(ctx, spec.SuiteID)
	if err != nil {
		return validationdomain.ScheduleSpec{}, "", err
	}
	if suite.Retired {
		return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: suite %s is retired", validationdomain.ErrSuiteNotFound, suite.ID)
	}
	if err := suite.ValidateParameters(spec.Parameters); err != nil {
		return validationdomain.ScheduleSpec{}, "", err
	}
	if suite.Impact == validationdomain.ImpactDisruptive {
		return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: a disruptive suite is started only by a user who confirms it", validationdomain.ErrInvalidRequest)
	}
	switch suite.Target {
	case validationdomain.TargetServers:
		if spec.PlatformID != "" || len(spec.ServerIDs) == 0 {
			return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: suite %s takes serverIds and no platformId", validationdomain.ErrInvalidRequest, suite.ID)
		}
		siteID := ""
		for _, id := range spec.ServerIDs {
			server, err := s.targets.Server(ctx, id)
			if err != nil {
				return validationdomain.ScheduleSpec{}, "", err
			}
			if siteID == "" {
				siteID = server.SiteID
			}
			if server.SiteID != siteID {
				return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: the Servers belong to more than one Site", validationdomain.ErrInvalidRequest)
			}
		}
		return spec, siteID, nil
	case validationdomain.TargetPlatform:
		if spec.PlatformID == "" || len(spec.ServerIDs) > 0 {
			return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: suite %s takes a platformId and no serverIds", validationdomain.ErrInvalidRequest, suite.ID)
		}
		platform, err := s.targets.Platform(ctx, spec.PlatformID)
		if err != nil {
			return validationdomain.ScheduleSpec{}, "", err
		}
		if platform.Type != suite.PlatformType {
			return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: Platform %s is %s; suite %s validates %s",
				validationdomain.ErrInvalidRequest, platform.Name, platform.Type, suite.ID, suite.PlatformType)
		}
		return spec, platform.SiteID, nil
	}
	return validationdomain.ScheduleSpec{}, "", fmt.Errorf("%w: suite %s has an unknown target kind", validationdomain.ErrInvalidRequest, suite.ID)
}
