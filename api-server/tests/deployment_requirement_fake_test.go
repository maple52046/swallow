package tests

import (
	"context"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

type fakeDeploymentRequirementRepo struct {
	requirement *platformdomain.DeploymentRequirement
	findErr     error
	upsertErr   error
}

func (r *fakeDeploymentRequirementRepo) FindByPlatformType(
	context.Context,
	platformdomain.PlatformType,
) (*platformdomain.DeploymentRequirement, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.requirement == nil {
		return nil, platformdomain.ErrDeploymentRequirementNotFound
	}
	return r.requirement, nil
}

func (r *fakeDeploymentRequirementRepo) Upsert(
	_ context.Context,
	requirement *platformdomain.DeploymentRequirement,
) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.requirement = requirement
	return nil
}
