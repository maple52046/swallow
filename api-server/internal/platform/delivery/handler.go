package delivery

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type PlatformHandler struct {
	platforms  *application.PlatformService
	uninstall  *application.UninstallService
	membership *application.MembershipSyncUseCase
	deploy     *application.DeployService
}

func NewPlatformHandler(
	platforms *application.PlatformService,
	membership *application.MembershipSyncUseCase,
	deploy *application.DeployService,
	uninstall *application.UninstallService,
) *PlatformHandler {
	return &PlatformHandler{
		platforms: platforms, membership: membership, deploy: deploy, uninstall: uninstall,
	}
}

// acceptedOperationResponse keeps the one-release clusterId alias at the HTTP boundary.
type acceptedOperationResponse struct {
	PlatformID  string `json:"platformId"`
	ClusterID   string `json:"clusterId"`
	OperationID string `json:"operationId"`
}

// membershipReportResponse keeps the former clusterId field out of the canonical
// application DTO while preserving the published compatibility projection.
type membershipReportResponse struct {
	PlatformID   string   `json:"platformId"`
	ClusterID    string   `json:"clusterId"`
	PlatformName string   `json:"platformName"`
	Members      int      `json:"members"`
	Matched      int      `json:"matched"`
	Cleared      int      `json:"cleared"`
	Unmatched    []string `json:"unmatched"`
	Error        *string  `json:"error"`
}

func membershipResponse(report application.MembershipReport) membershipReportResponse {
	return membershipReportResponse{
		PlatformID: report.PlatformID, ClusterID: report.PlatformID,
		PlatformName: report.PlatformName, Members: report.Members,
		Matched: report.Matched, Cleared: report.Cleared,
		Unmatched: report.Unmatched, Error: report.Error,
	}
}

type createPlatformRequest struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IntegrationID string `json:"integrationId"`
	GPUStackOwner string `json:"gpuStackOwner"`
	ExporterOwner string `json:"exporterOwner"`
}

func (h *PlatformHandler) Create(c *fiber.Ctx) error {
	var req createPlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	item, err := h.platforms.Create(c.Context(), application.CreatePlatformInput{
		SiteID:        req.SiteID,
		Name:          req.Name,
		Type:          req.Type,
		IntegrationID: req.IntegrationID,
		GPUStackOwner: req.GPUStackOwner,
		ExporterOwner: req.ExporterOwner,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// roleAssignmentRequest keeps workload co-location additive: an omitted JSON boolean maps
// to false and preserves existing dedicated control-plane requests.
type roleAssignmentRequest struct {
	ServerID     string `json:"serverId"`
	Role         string `json:"role"`
	RunWorkloads bool   `json:"runWorkloads"`
}

// deployPlatformRequest is the stable wire shape; zero VIP fields mean omitted and the
// application decides whether the inferred topology requires them.
type machinePreparationRequest struct {
	Mode       string                            `json:"mode"`
	TemplateID string                            `json:"templateId"`
	Settings   machinePreparationSettingsRequest `json:"settings"`
	UserData   machinePreparationUserDataRequest `json:"userData"`
	Network    *machinePreparationNetworkRequest `json:"network"`
}

type machinePreparationSettingsRequest struct {
	ImageID   string `json:"imageId"`
	Ephemeral *bool  `json:"ephemeral"`
}

type machinePreparationUserDataRequest struct {
	Mode  string `json:"mode"`
	Value string `json:"value"`
}

type machinePreparationNetworkRequest struct {
	Mode           string                                `json:"mode"`
	SubnetID       string                                `json:"subnetId"`
	DefaultGateway bool                                  `json:"defaultGateway"`
	Assignments    []machinePreparationAssignmentRequest `json:"assignments"`
}

type machinePreparationAssignmentRequest struct {
	ServerID    string `json:"serverId"`
	InterfaceID string `json:"interfaceId"`
	SubnetID    string `json:"subnetId"`
	IPAddress   string `json:"ipAddress"`
}

func machinePreparationFromRequest(request machinePreparationRequest) platformdomain.MachinePreparation {
	preparation := platformdomain.MachinePreparation{
		Mode: platformdomain.MachinePreparationMode(request.Mode), TemplateID: request.TemplateID,
		ImageID: request.Settings.ImageID, Ephemeral: request.Settings.Ephemeral,
		UserDataMode: request.UserData.Mode, UserData: request.UserData.Value,
	}
	if request.Network != nil {
		preparation.NetworkMode = request.Network.Mode
		preparation.SubnetID = request.Network.SubnetID
		preparation.DefaultGateway = request.Network.DefaultGateway
		preparation.Assignments = make([]platformdomain.MachineNetworkAssignment, len(request.Network.Assignments))
		for index, assignment := range request.Network.Assignments {
			preparation.Assignments[index] = platformdomain.MachineNetworkAssignment{
				ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID,
				SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
			}
		}
	}
	return preparation
}

type deployPlatformRequest struct {
	SiteID             string                    `json:"siteId"`
	Name               string                    `json:"name"`
	GPUStackOwner      string                    `json:"gpuStackOwner"`
	K0sVersion         string                    `json:"k0sVersion"`
	PodCIDR            string                    `json:"podCidr"`
	ServiceCIDR        string                    `json:"serviceCidr"`
	APIVIP             string                    `json:"apiVip"`
	APIVIPPrefix       int                       `json:"apiVipPrefix"`
	RoleAssignments    []roleAssignmentRequest   `json:"roleAssignments"`
	MachinePreparation machinePreparationRequest `json:"machinePreparation"`
}

// Deploy maps transport data into deployment intent; topology and network rules stay in
// the application use case so every delivery shares one policy.
func (h *PlatformHandler) Deploy(c *fiber.Ctx) error {
	var req deployPlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	assignments := make([]platformdomain.RoleAssignment, len(req.RoleAssignments))
	for i, assignment := range req.RoleAssignments {
		assignments[i] = platformdomain.RoleAssignment{
			ServerID:     assignment.ServerID,
			Role:         platformdomain.NodeRole(assignment.Role),
			RunWorkloads: assignment.RunWorkloads,
		}
	}

	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}

	result, err := h.deploy.Deploy(c.Context(), application.DeployPlatformInput{
		SiteID:        req.SiteID,
		Name:          req.Name,
		GPUStackOwner: req.GPUStackOwner,
		Spec: platformdomain.DeploymentSpec{
			K0sVersion:      req.K0sVersion,
			PodCIDR:         req.PodCIDR,
			ServiceCIDR:     req.ServiceCIDR,
			APIVIP:          req.APIVIP,
			APIVIPPrefix:    req.APIVIPPrefix,
			RoleAssignments: assignments,
		},
		RequestedBy: requestedBy, RequestCorrelation: c.GetRespHeader(fiber.HeaderXRequestID),
		MachinePreparation: machinePreparationFromRequest(req.MachinePreparation),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(acceptedOperationResponse{
		PlatformID: result.PlatformID, ClusterID: result.PlatformID,
		OperationID: result.OperationID,
	})
}

func (h *PlatformHandler) List(c *fiber.Ctx) error {
	items, err := h.platforms.List(c.Context(), c.Query("siteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

func (h *PlatformHandler) Get(c *fiber.Ctx) error {
	item, err := h.platforms.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type updatePlatformRequest struct {
	Name          *string `json:"name"`
	IntegrationID *string `json:"integrationId"`
	GPUStackOwner *string `json:"gpuStackOwner"`
	ExporterOwner *string `json:"exporterOwner"`
}

func (h *PlatformHandler) Update(c *fiber.Ctx) error {
	var req updatePlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	item, err := h.platforms.Update(c.Context(), c.Params("id"), application.UpdatePlatformInput{
		Name:          req.Name,
		IntegrationID: req.IntegrationID,
		GPUStackOwner: req.GPUStackOwner,
		ExporterOwner: req.ExporterOwner,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// Uninstall starts removal of a Swallow-deployed k0s platform from its original targets.
func (h *PlatformHandler) Uninstall(c *fiber.Ctx) error {
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	result, err := h.uninstall.Uninstall(c.Context(), application.UninstallPlatformInput{
		PlatformID: c.Params("id"), RequestedBy: requestedBy,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(acceptedOperationResponse{
		PlatformID: result.PlatformID, ClusterID: result.PlatformID,
		OperationID: result.OperationID,
	})
}

func (h *PlatformHandler) Delete(c *fiber.Ctx) error {
	if err := h.platforms.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// SyncMembership reads the platform's membership now instead of waiting for the interval.
// The report names members swallow could not match to a server, which is the part that
// needs a human.
func (h *PlatformHandler) SyncMembership(c *fiber.Ctx) error {
	report, err := h.membership.Execute(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(membershipResponse(*report))
}

func (h *PlatformHandler) SyncAllMembership(c *fiber.Ctx) error {
	reports, err := h.membership.ExecuteAll(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	responses := make([]membershipReportResponse, len(reports))
	for index, report := range reports {
		responses[index] = membershipResponse(report)
	}
	return c.JSON(responses)
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, platformdomain.ErrPlatformNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Platform not found."))

	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, platformdomain.ErrPlatformNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A platform with this name already exists at this site."))

	case errors.Is(err, platformdomain.ErrPlatformNotDeployManaged),
		errors.Is(err, platformdomain.ErrPlatformAlreadyUninstalled),
		errors.Is(err, platformdomain.ErrPlatformUninstallConflict),
		errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, operationdomain.ErrPolicyConflict),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrAutomationConfigNotFound),
		errors.Is(err, operationdomain.ErrAutomationDisabled),
		errors.Is(err, operationdomain.ErrAutomationCredentialMissing),
		errors.Is(err, operationdomain.ErrPlaybookNotAllowed),
		errors.Is(err, operationapp.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))

	case errors.Is(err, application.ErrInvalidPlatform),
		errors.Is(err, platformdomain.ErrInvalidDeployment),
		errors.Is(err, serverdomain.ErrServerNotFound),
		errors.Is(err, platformdomain.ErrUnsupportedPlatformType):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, platformdomain.ErrNoPlatformIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"This platform has no integration configured, so its membership cannot be read."))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The platform integration has no credential configured."))
	case errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			err.Error()))
	}

	var readerErr *platformdomain.ReaderError
	if errors.As(err, &readerErr) {
		if readerErr.Kind == platformdomain.ReaderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, readerErr.Detail))
		}
		log.Printf("platform api %s: %v", readerErr.Kind, readerErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, readerErr.Detail))
	}

	log.Printf("platform: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
