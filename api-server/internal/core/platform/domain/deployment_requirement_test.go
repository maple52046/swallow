package domain

import (
	"errors"
	"math"
	"testing"
)

func TestMinimumResourcesRejectsNonFiniteStorage(t *testing.T) {
	for _, storage := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		minimum := MinimumResources{CPUCores: 4, MemoryMiB: 24576, StorageGB: storage}
		if err := minimum.Validate(); !errors.Is(err, ErrInvalidDeploymentRequirement) {
			t.Errorf("Validate(storage=%v) error = %v, want ErrInvalidDeploymentRequirement", storage, err)
		}
	}
}
