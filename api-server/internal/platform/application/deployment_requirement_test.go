package application

import (
	"context"
	"errors"
	"testing"

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

func TestDeploymentRequirementMissingIsDisabled(t *testing.T) {
	service := NewDeploymentRequirementService(&fakeDeploymentRequirementRepo{})

	requirement, err := service.Get(context.Background(), platformdomain.PlatformTypeSlurm)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if requirement.MinimumResources != nil || !requirement.UpdatedAt.IsZero() {
		t.Fatalf("missing requirement = %+v, want disabled representation", requirement)
	}
}

func TestDeploymentRequirementPutRoundTripAndDisable(t *testing.T) {
	repository := &fakeDeploymentRequirementRepo{}
	service := NewDeploymentRequirementService(repository)
	minimum := &platformdomain.MinimumResources{CPUCores: 4, MemoryMiB: 24576, StorageGB: 80}

	updated, err := service.Put(context.Background(), platformdomain.PlatformTypeSlurm, minimum)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if updated.UpdatedAt.IsZero() || updated.MinimumResources == minimum {
		t.Fatal("Put() must timestamp and copy the caller-owned minimum")
	}
	read, err := service.Get(context.Background(), platformdomain.PlatformTypeSlurm)
	if err != nil || read.MinimumResources.MemoryMiB != 24576 {
		t.Fatalf("Get() = %+v, %v", read, err)
	}
	disabled, err := service.Put(context.Background(), platformdomain.PlatformTypeSlurm, nil)
	if err != nil || disabled.MinimumResources != nil {
		t.Fatalf("disable = %+v, %v", disabled, err)
	}
}

func TestDeploymentRequirementRejectsInvalidMinimums(t *testing.T) {
	tests := []platformdomain.MinimumResources{
		{MemoryMiB: 24576, StorageGB: 80},
		{CPUCores: 4, StorageGB: 80},
		{CPUCores: 4, MemoryMiB: 24576},
	}
	for _, minimum := range tests {
		service := NewDeploymentRequirementService(&fakeDeploymentRequirementRepo{})
		if _, err := service.Put(context.Background(), platformdomain.PlatformTypeSlurm, &minimum); !errors.Is(err, platformdomain.ErrInvalidDeploymentRequirement) {
			t.Errorf("Put(%+v) error = %v, want ErrInvalidDeploymentRequirement", minimum, err)
		}
	}
}
