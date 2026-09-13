package application

import (
	"fmt"
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

func validateMinimumResources(server *serverdomain.Server, minimum platformdomain.MinimumResources) error {
	actual := server.Observed
	shortfalls := make([]string, 0, 3)
	if actual.CPUCores < minimum.CPUCores {
		shortfalls = append(shortfalls, fmt.Sprintf("CPU %d cores (minimum %d)", actual.CPUCores, minimum.CPUCores))
	}
	if actual.MemoryMiB < minimum.MemoryMiB {
		shortfalls = append(shortfalls, fmt.Sprintf("memory %d MiB (minimum %d MiB)", actual.MemoryMiB, minimum.MemoryMiB))
	}
	if actual.StorageGB < minimum.StorageGB {
		shortfalls = append(shortfalls, fmt.Sprintf("storage %.2f GB (minimum %.2f GB)", actual.StorageGB, minimum.StorageGB))
	}
	if len(shortfalls) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: server %s does not meet the Slurm minimum resource requirement: %s",
		platformdomain.ErrInvalidDeployment, server.DisplayName(), strings.Join(shortfalls, "; "),
	)
}
