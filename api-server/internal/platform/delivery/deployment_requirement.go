package delivery

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

type minimumResourcesResponse struct {
	CPUCores  int     `json:"cpuCores"`
	MemoryMiB int64   `json:"memoryMiB"`
	StorageGB float64 `json:"storageGB"`
}

type deploymentRequirementResponse struct {
	PlatformType     platformdomain.PlatformType `json:"platformType"`
	MinimumResources *minimumResourcesResponse   `json:"minimumResources"`
	UpdatedAt        *time.Time                  `json:"updatedAt"`
}

func deploymentRequirementBody(requirement *platformdomain.DeploymentRequirement) deploymentRequirementResponse {
	response := deploymentRequirementResponse{PlatformType: requirement.PlatformType}
	if requirement.MinimumResources != nil {
		response.MinimumResources = &minimumResourcesResponse{
			CPUCores:  requirement.MinimumResources.CPUCores,
			MemoryMiB: requirement.MinimumResources.MemoryMiB,
			StorageGB: requirement.MinimumResources.StorageGB,
		}
	}
	if !requirement.UpdatedAt.IsZero() {
		updatedAt := requirement.UpdatedAt
		response.UpdatedAt = &updatedAt
	}
	return response
}

type putDeploymentRequirementRequest struct {
	MinimumResources json.RawMessage `json:"minimumResources"`
}

type minimumResourcesRequest struct {
	CPUCores  int     `json:"cpuCores"`
	MemoryMiB int64   `json:"memoryMiB"`
	StorageGB float64 `json:"storageGB"`
}

// GetSlurmDeploymentRequirement returns the current global Slurm eligibility policy.
func (h *PlatformHandler) GetSlurmDeploymentRequirement(c *fiber.Ctx) error {
	requirement, err := h.requirements.Get(c.Context(), platformdomain.PlatformTypeSlurm)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(deploymentRequirementBody(requirement))
}

// PutSlurmDeploymentRequirement replaces or disables the global Slurm eligibility policy.
func (h *PlatformHandler) PutSlurmDeploymentRequirement(c *fiber.Ctx) error {
	var request putDeploymentRequirementRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if len(request.MinimumResources) == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "minimumResources is required."))
	}
	var minimum *platformdomain.MinimumResources
	rawMinimum := bytes.TrimSpace(request.MinimumResources)
	if !bytes.Equal(rawMinimum, []byte("null")) {
		var value minimumResourcesRequest
		if err := json.Unmarshal(rawMinimum, &value); err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid minimumResources."))
		}
		minimum = &platformdomain.MinimumResources{
			CPUCores:  value.CPUCores,
			MemoryMiB: value.MemoryMiB,
			StorageGB: value.StorageGB,
		}
	}
	requirement, err := h.requirements.Put(c.Context(), platformdomain.PlatformTypeSlurm, minimum)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(deploymentRequirementBody(requirement))
}
