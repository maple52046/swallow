package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// DeploymentRequirementService manages the global deployment eligibility policy.
type DeploymentRequirementService struct {
	repository platformdomain.DeploymentRequirementRepository
}

func NewDeploymentRequirementService(repository platformdomain.DeploymentRequirementRepository) *DeploymentRequirementService {
	return &DeploymentRequirementService{repository: repository}
}

// Get returns a disabled representation when a policy has never been configured.
func (s *DeploymentRequirementService) Get(
	ctx context.Context,
	platformType platformdomain.PlatformType,
) (*platformdomain.DeploymentRequirement, error) {
	if platformType != platformdomain.PlatformTypeSlurm {
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platformType)
	}
	requirement, err := s.repository.FindByPlatformType(ctx, platformType)
	if errors.Is(err, platformdomain.ErrDeploymentRequirementNotFound) {
		return &platformdomain.DeploymentRequirement{PlatformType: platformType}, nil
	}
	return requirement, err
}

// Put validates and replaces the current policy. A nil minimum disables it.
func (s *DeploymentRequirementService) Put(
	ctx context.Context,
	platformType platformdomain.PlatformType,
	minimum *platformdomain.MinimumResources,
) (*platformdomain.DeploymentRequirement, error) {
	if platformType != platformdomain.PlatformTypeSlurm {
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platformType)
	}
	if minimum != nil {
		copy := *minimum
		if err := copy.Validate(); err != nil {
			return nil, err
		}
		minimum = &copy
	}
	requirement := &platformdomain.DeploymentRequirement{
		PlatformType: platformType, MinimumResources: minimum, UpdatedAt: time.Now().UTC(),
	}
	if err := s.repository.Upsert(ctx, requirement); err != nil {
		return nil, err
	}
	return requirement, nil
}
