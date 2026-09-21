package delivery

import (
	"strings"

	"github.com/maple52046/swallow/internal/shared/middleware"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

type createDeploymentTemplateRequest struct {
	IntegrationID string                            `json:"integrationId"`
	Name          string                            `json:"name"`
	Description   string                            `json:"description"`
	ImageID       string                            `json:"imageId"`
	Ephemeral     bool                              `json:"ephemeral"`
	DeployTarget  *string                           `json:"deployTarget"`
	UserData      string                            `json:"userData"`
	Network       *deploymentNetworkSettingsRequest `json:"network"`
}

type updateDeploymentTemplateRequest struct {
	Name         *string                           `json:"name"`
	Description  *string                           `json:"description"`
	ImageID      *string                           `json:"imageId"`
	Ephemeral    *bool                             `json:"ephemeral"`
	DeployTarget *string                           `json:"deployTarget"`
	Network      *deploymentNetworkSettingsRequest `json:"network"`
}

type replaceTemplateUserDataRequest struct {
	UserData string `json:"userData"`
}

type deploymentSettingsRequest struct {
	ImageID   *string `json:"imageId"`
	Ephemeral *bool   `json:"ephemeral"`
	// DeployTarget is the preferred deploy-mode vocabulary ("disk"/"ram"). When present it is
	// authoritative and maps onto Ephemeral (disk=false, ram=true); Ephemeral remains accepted as
	// a deprecated alias for existing clients.
	DeployTarget *string `json:"deployTarget"`
}

// resolveEphemeral folds the deploy-target vocabulary onto the internal ephemeral flag. When
// deployTarget is provided it is authoritative (disk=false, ram=true) and must be valid; otherwise
// the legacy ephemeral field is used as-is. ok=false means an unknown deployTarget the handler must
// answer with 400 rather than silently choosing a deploy mode.
func resolveEphemeral(deployTarget *string, ephemeral *bool) (*bool, bool) {
	if deployTarget == nil {
		return ephemeral, true
	}
	target, valid := provisioningdomain.ParseDeployTarget(*deployTarget)
	if !valid {
		return nil, false
	}
	value := target.Ephemeral()
	return &value, true
}

type deploymentNetworkSettingsRequest struct {
	Mode           string `json:"mode"`
	SubnetID       string `json:"subnetId"`
	DefaultGateway bool   `json:"defaultGateway"`
}

type deploymentNetworkAssignmentRequest struct {
	ServerID    string `json:"serverId"`
	InterfaceID string `json:"interfaceId"`
	SubnetID    string `json:"subnetId"`
	IPAddress   string `json:"ipAddress"`
}

type deploymentNetworkRequest struct {
	Mode           string                               `json:"mode"`
	SubnetID       string                               `json:"subnetId"`
	DefaultGateway bool                                 `json:"defaultGateway"`
	Assignments    []deploymentNetworkAssignmentRequest `json:"assignments"`
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
	Network    *deploymentNetworkRequest `json:"network"`
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
	// A template stores a bool ephemeral; the deploy-target alias, when sent, is authoritative.
	ephemeral := req.Ephemeral
	if req.DeployTarget != nil {
		target, valid := provisioningdomain.ParseDeployTarget(*req.DeployTarget)
		if !valid {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
		}
		ephemeral = target.Ephemeral()
	}
	item, err := h.templates.Create(c.Context(), application.CreateDeploymentTemplateInput{
		IntegrationID: req.IntegrationID,
		Name:          req.Name,
		Description:   req.Description,
		ImageID:       req.ImageID,
		Ephemeral:     ephemeral,
		UserData:      req.UserData,
		Network:       deploymentTemplateNetworkInput(req.Network),
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
	// A deploy-target alias, when sent, is authoritative and sets the ephemeral override.
	ephemeral := req.Ephemeral
	if req.DeployTarget != nil {
		target, valid := provisioningdomain.ParseDeployTarget(*req.DeployTarget)
		if !valid {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
		}
		value := target.Ephemeral()
		ephemeral = &value
	}
	item, err := h.templates.Update(c.Context(), c.Params("id"), application.UpdateDeploymentTemplateInput{
		Name: req.Name, Description: req.Description, ImageID: req.ImageID,
		Ephemeral: ephemeral, Network: deploymentTemplateNetworkInput(req.Network),
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
	input, ok := deployServersInput(req)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
	}
	result, err := h.deployments.Execute(c.Context(), input)
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

// deployServersInput maps the HTTP request onto the application input, resolving the deploy-target
// vocabulary onto the internal ephemeral flag. ok=false signals an invalid deployTarget value.
func deployServersInput(req deployServersRequest) (application.DeployServersInput, bool) {
	ephemeral, ok := resolveEphemeral(req.Settings.DeployTarget, req.Settings.Ephemeral)
	if !ok {
		return application.DeployServersInput{}, false
	}
	return application.DeployServersInput{
		ServerIDs:  req.ServerIDs,
		TemplateID: req.TemplateID,
		Settings: application.DeploymentSettingsInput{
			ImageID: req.Settings.ImageID, Ephemeral: ephemeral,
		},
		UserData: application.DeploymentUserDataInput{
			Mode: req.UserData.Mode, Value: req.UserData.Value,
		},
		Network: deploymentNetworkInput(req.Network),
	}, true
}

func deploymentTemplateNetworkInput(
	req *deploymentNetworkSettingsRequest,
) *application.DeploymentNetworkSettingsInput {
	if req == nil {
		return nil
	}
	return &application.DeploymentNetworkSettingsInput{
		Mode: req.Mode, SubnetID: req.SubnetID, DefaultGateway: req.DefaultGateway,
	}
}

func deploymentNetworkInput(
	req *deploymentNetworkRequest,
) *application.DeploymentNetworkInput {
	if req == nil {
		return nil
	}
	assignments := make([]application.DeploymentNetworkAssignmentInput, len(req.Assignments))
	for index, assignment := range req.Assignments {
		assignments[index] = application.DeploymentNetworkAssignmentInput{
			ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID,
			SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
		}
	}
	return &application.DeploymentNetworkInput{
		Mode: req.Mode, SubnetID: req.SubnetID,
		DefaultGateway: req.DefaultGateway, Assignments: assignments,
	}
}

// CreateDeploymentOperation validates the full batch and persists one durable Operation.
func (h *ProvisioningHandler) CreateDeploymentOperation(c *fiber.Ctx) error {
	if h.durable == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Durable provisioning is unavailable."))
	}
	var req deployServersRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	input, ok := deployServersInput(req)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
	}
	result, err := h.durable.LaunchDeployment(c.Context(), input, requestedBy, c.GetRespHeader(fiber.HeaderXRequestID))
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

type imageVerificationRequest struct {
	IntegrationID string `json:"integrationId"`
	ImageID       string `json:"imageId"`
	Architecture  string `json:"architecture"`
	DeployTarget  string `json:"deployTarget"`
	ServerID      string `json:"serverId"`
}

// CreateImageVerification launches a verify-os-image Operation that proves a custom OS Image works
// for one deploy target by deploying it on the chosen ready Server, recording the verification, and
// auto-releasing the Server. Responds 202 with the operationId.
func (h *ProvisioningHandler) CreateImageVerification(c *fiber.Ctx) error {
	if h.durable == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Durable provisioning is unavailable."))
	}
	var req imageVerificationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.IntegrationID) == "" || strings.TrimSpace(req.ImageID) == "" ||
		strings.TrimSpace(req.Architecture) == "" || strings.TrimSpace(req.ServerID) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId, imageId, architecture, and serverId are required."))
	}
	target, valid := provisioningdomain.ParseDeployTarget(req.DeployTarget)
	if !valid {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
	}
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	result, err := h.durable.LaunchImageVerification(c.Context(), application.ImageVerificationInput{
		IntegrationID: req.IntegrationID,
		ImageID:       req.ImageID,
		Architecture:  req.Architecture,
		DeployTarget:  target,
		ServerID:      req.ServerID,
		RequestID:     c.GetRespHeader(fiber.HeaderXRequestID),
	}, requestedBy, c.GetRespHeader(fiber.HeaderXRequestID))
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

type releaseOperationsRequest struct {
	ServerIDs       []string `json:"serverIds"`
	Erase           bool     `json:"erase"`
	SecureErase     bool     `json:"secureErase"`
	QuickErase      bool     `json:"quickErase"`
	Comment         string   `json:"comment"`
	UnbindStaticIPs bool     `json:"unbindStaticIPs"`
}

// CreateReleaseOperation accepts a bounded batch of provider-neutral release intents.
func (h *ProvisioningHandler) CreateReleaseOperation(c *fiber.Ctx) error {
	if h.durable == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Durable provisioning is unavailable."))
	}
	var req releaseOperationsRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if len(req.ServerIDs) == 0 || len(req.ServerIDs) > 100 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "serverIds must contain between 1 and 100 Servers."))
	}
	inputs := make([]application.ReleaseServerInput, len(req.ServerIDs))
	for index, serverID := range req.ServerIDs {
		inputs[index] = application.ReleaseServerInput{ServerID: serverID, Erase: req.Erase,
			SecureErase: req.SecureErase, QuickErase: req.QuickErase, Comment: req.Comment,
			UnbindStaticIPs: req.UnbindStaticIPs, RequestID: c.GetRespHeader(fiber.HeaderXRequestID)}
	}
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	result, err := h.durable.LaunchRelease(c.Context(), inputs, requestedBy, c.GetRespHeader(fiber.HeaderXRequestID))
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

type recoverOperationsRequest struct {
	ServerIDs       []string `json:"serverIds"`
	Comment         string   `json:"comment"`
	UnbindStaticIPs bool     `json:"unbindStaticIPs"`
}

// CreateRecoverOperation accepts a bounded batch of "Return to Ready" intents for Servers
// whose provisioning axis is not usable. The durable launcher gates each target on the
// recovery policy before persisting one recover-server Step per Server.
func (h *ProvisioningHandler) CreateRecoverOperation(c *fiber.Ctx) error {
	if h.durable == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Durable provisioning is unavailable."))
	}
	var req recoverOperationsRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if len(req.ServerIDs) == 0 || len(req.ServerIDs) > 100 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "serverIds must contain between 1 and 100 Servers."))
	}
	inputs := make([]application.RecoverServerInput, len(req.ServerIDs))
	for index, serverID := range req.ServerIDs {
		inputs[index] = application.RecoverServerInput{ServerID: serverID, Comment: req.Comment,
			UnbindStaticIPs: req.UnbindStaticIPs, RequestID: c.GetRespHeader(fiber.HeaderXRequestID)}
	}
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	result, err := h.durable.LaunchRecover(c.Context(), inputs, requestedBy, c.GetRespHeader(fiber.HeaderXRequestID))
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}
