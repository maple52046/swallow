package domain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// MinimumResources is the observed-hardware floor a Server must meet to be eligible for a
// platform deployment. It is neither a resource reservation nor scheduler capacity.
type MinimumResources struct {
	CPUCores  int
	MemoryMiB int64
	StorageGB float64
}

// Validate rejects partially specified and non-positive resource floors.
func (r MinimumResources) Validate() error {
	switch {
	case r.CPUCores <= 0:
		return fmt.Errorf("%w: minimumResources.cpuCores must be greater than zero", ErrInvalidDeploymentRequirement)
	case r.MemoryMiB <= 0:
		return fmt.Errorf("%w: minimumResources.memoryMiB must be greater than zero", ErrInvalidDeploymentRequirement)
	case r.StorageGB <= 0 || math.IsNaN(r.StorageGB) || math.IsInf(r.StorageGB, 0):
		return fmt.Errorf("%w: minimumResources.storageGB must be greater than zero", ErrInvalidDeploymentRequirement)
	default:
		return nil
	}
}

// DeploymentRequirement is the current system-wide eligibility policy for one Platform type.
// A nil MinimumResources disables enforcement while preserving the last update timestamp.
type DeploymentRequirement struct {
	PlatformType     PlatformType
	MinimumResources *MinimumResources
	UpdatedAt        time.Time
}

var (
	ErrDeploymentRequirementNotFound = errors.New("deployment requirement not found")
	ErrInvalidDeploymentRequirement  = errors.New("invalid deployment requirement")
)

// DeploymentRequirementReader exposes the policy to deployment preflight without granting
// write access.
type DeploymentRequirementReader interface {
	FindByPlatformType(context.Context, PlatformType) (*DeploymentRequirement, error)
}

// DeploymentRequirementRepository persists the single current policy for each Platform type.
type DeploymentRequirementRepository interface {
	DeploymentRequirementReader
	Upsert(context.Context, *DeploymentRequirement) error
}
