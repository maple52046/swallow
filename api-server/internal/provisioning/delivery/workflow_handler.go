package delivery

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

type createDeploymentTemplateRequest struct {
	IntegrationID string `json:"integrationId"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ImageID       string `json:"imageId"`
	Ephemeral     bool   `json:"ephemeral"`
	UserData      string `json:"userData"`
}

type updateDeploymentTemplateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	ImageID     *string `json:"imageId"`
	Ephemeral   *bool   `json:"ephemeral"`
}

type replaceTemplateUserDataRequest struct {
	UserData string `json:"userData"`
}

type deploymentSettingsRequest struct {
	ImageID   *string `json:"imageId"`
	Ephemeral *bool   `json:"ephemeral"`
}

type deploymentUserDataRequest struct {
	Mode  string `json:"mode"`
	Value string `json:"value"`
}

type preflightDeployServersRequest struct {
	ServerIDs []string `json:"serverIds"`
}
type deploymentTargetIssueResponse struct {
	ServerID string `json:"serverId"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type preflightDeployServersResponse struct {
	Valid         bool                            `json:"valid"`
	IntegrationID string                          `json:"integrationId"`
	Issues        []deploymentTargetIssueResponse `json:"issues"`
}

// toPreflightDeployServersResponse owns the application-to-HTTP mapping so the
// use case remains independent of JSON field names.
func toPreflightDeployServersResponse(
	result *application.PreflightDeploymentTargetsResult,
) preflightDeployServersResponse {
	issues := make([]deploymentTargetIssueResponse, len(result.Issues))
	for index, issue := range result.Issues {
		issues[index] = deploymentTargetIssueResponse{
			ServerID: issue.ServerID,
			Code:     issue.Code,
			Message:  issue.Message,
		}
	}
	return preflightDeployServersResponse{
		Valid:         result.Valid,
		IntegrationID: result.IntegrationID,
		Issues:        issues,
	}
}

type deployServersRequest struct {
	ServerIDs  []string                  `json:"serverIds"`
	TemplateID string                    `json:"templateId"`
	Settings   deploymentSettingsRequest `json:"settings"`
	UserData   deploymentUserDataRequest `json:"userData"`
}

// CreateTemplate stores reusable deployment intent after validating its live image.
func (h *ProvisioningHandler) CreateTemplate(c *fiber.Ctx) error {
	var req createDeploymentTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.IntegrationID) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}
	item, err := h.templates.Create(c.Context(), application.CreateDeploymentTemplateInput{
		IntegrationID: req.IntegrationID,
		Name:          req.Name,
		Description:   req.Description,
		ImageID:       req.ImageID,
		Ephemeral:     req.Ephemeral,
		UserData:      req.UserData,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// ListTemplates returns deployment templates within optional site and integration scope.
func (h *ProvisioningHandler) ListTemplates(c *fiber.Ctx) error {
	items, err := h.templates.List(c.Context(), c.Query("siteId"), c.Query("integrationId"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(items)
}

// GetTemplate returns template metadata without cloud-init content.
func (h *ProvisioningHandler) GetTemplate(c *fiber.Ctx) error {
	item, err := h.templates.Get(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// UpdateTemplate changes non-secret template intent.
func (h *ProvisioningHandler) UpdateTemplate(c *fiber.Ctx) error {
	var req updateDeploymentTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := h.templates.Update(c.Context(), c.Params("id"), application.UpdateDeploymentTemplateInput{
		Name: req.Name, Description: req.Description, ImageID: req.ImageID, Ephemeral: req.Ephemeral,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// DeleteTemplate removes reusable intent and its encrypted cloud-init.
func (h *ProvisioningHandler) DeleteTemplate(c *fiber.Ctx) error {
	if err := h.templates.Delete(c.Context(), c.Params("id")); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ReplaceTemplateUserData replaces write-only cloud-init.
func (h *ProvisioningHandler) ReplaceTemplateUserData(c *fiber.Ctx) error {
	var req replaceTemplateUserDataRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if err := h.templates.ReplaceUserData(c.Context(), c.Params("id"), req.UserData); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ClearTemplateUserData removes write-only cloud-init.
func (h *ProvisioningHandler) ClearTemplateUserData(c *fiber.Ctx) error {
	if err := h.templates.ClearUserData(c.Context(), c.Params("id")); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// PreflightDeployServers returns HTTP 200 for every completed inspection, including
// a false-valid report. Input and dependency failures use the shared API error mapping;
// the handler never dispatches, reserves, or mutates a provider machine.
func (h *ProvisioningHandler) PreflightDeployServers(c *fiber.Ctx) error {
	var req preflightDeployServersRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	result, err := h.targetPreflight.Execute(c.Context(), req.ServerIDs)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(toPreflightDeployServersResponse(result))
}

// DeployServers starts an all-or-nothing-preflight deployment batch.
func (h *ProvisioningHandler) DeployServers(c *fiber.Ctx) error {
	var req deployServersRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	result, err := h.deployments.Execute(c.Context(), application.DeployServersInput{
		ServerIDs:  req.ServerIDs,
		TemplateID: req.TemplateID,
		Settings: application.DeploymentSettingsInput{
			ImageID: req.Settings.ImageID, Ephemeral: req.Settings.Ephemeral,
		},
		UserData: application.DeploymentUserDataInput{
			Mode: req.UserData.Mode, Value: req.UserData.Value,
		},
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}
