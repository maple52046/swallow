package delivery

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/cluster/application"
	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type ClusterHandler struct {
	clusters   *application.ClusterService
	uninstall  *application.UninstallService
	membership *application.MembershipSyncUseCase
	deploy     *application.DeployService
}

func NewClusterHandler(
	clusters *application.ClusterService,
	membership *application.MembershipSyncUseCase,
	deploy *application.DeployService,
	uninstall *application.UninstallService,
) *ClusterHandler {
	return &ClusterHandler{
		clusters: clusters, membership: membership, deploy: deploy, uninstall: uninstall,
	}
}

type createClusterRequest struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IntegrationID string `json:"integrationId"`
	GPUStackOwner string `json:"gpuStackOwner"`
	ExporterOwner string `json:"exporterOwner"`
}

func (h *ClusterHandler) Create(c *fiber.Ctx) error {
	var req createClusterRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	item, err := h.clusters.Create(c.Context(), application.CreateClusterInput{
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

// deployClusterRequest is the stable wire shape; zero VIP fields mean omitted and the
// application decides whether the inferred topology requires them.
type deployClusterRequest struct {
	SiteID          string                  `json:"siteId"`
	Name            string                  `json:"name"`
	GPUStackOwner   string                  `json:"gpuStackOwner"`
	K0sVersion      string                  `json:"k0sVersion"`
	PodCIDR         string                  `json:"podCidr"`
	ServiceCIDR     string                  `json:"serviceCidr"`
	APIVIP          string                  `json:"apiVip"`
	APIVIPPrefix    int                     `json:"apiVipPrefix"`
	RoleAssignments []roleAssignmentRequest `json:"roleAssignments"`
}

// Deploy maps transport data into deployment intent; topology and network rules stay in
// the application use case so every delivery shares one policy.
func (h *ClusterHandler) Deploy(c *fiber.Ctx) error {
	var req deployClusterRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	assignments := make([]clusterdomain.RoleAssignment, len(req.RoleAssignments))
	for i, assignment := range req.RoleAssignments {
		assignments[i] = clusterdomain.RoleAssignment{
			ServerID:     assignment.ServerID,
			Role:         clusterdomain.NodeRole(assignment.Role),
			RunWorkloads: assignment.RunWorkloads,
		}
	}

	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}

	result, err := h.deploy.Deploy(c.Context(), application.DeployClusterInput{
		SiteID:        req.SiteID,
		Name:          req.Name,
		GPUStackOwner: req.GPUStackOwner,
		Spec: clusterdomain.DeploymentSpec{
			K0sVersion:      req.K0sVersion,
			PodCIDR:         req.PodCIDR,
			ServiceCIDR:     req.ServiceCIDR,
			APIVIP:          req.APIVIP,
			APIVIPPrefix:    req.APIVIPPrefix,
			RoleAssignments: assignments,
		},
		RequestedBy: requestedBy,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

func (h *ClusterHandler) List(c *fiber.Ctx) error {
	items, err := h.clusters.List(c.Context(), c.Query("siteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

func (h *ClusterHandler) Get(c *fiber.Ctx) error {
	item, err := h.clusters.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type updateClusterRequest struct {
	Name          *string `json:"name"`
	IntegrationID *string `json:"integrationId"`
	GPUStackOwner *string `json:"gpuStackOwner"`
	ExporterOwner *string `json:"exporterOwner"`
}

func (h *ClusterHandler) Update(c *fiber.Ctx) error {
	var req updateClusterRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	item, err := h.clusters.Update(c.Context(), c.Params("id"), application.UpdateClusterInput{
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

// Uninstall starts removal of a Swallow-deployed k0s cluster from its original targets.
func (h *ClusterHandler) Uninstall(c *fiber.Ctx) error {
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	result, err := h.uninstall.Uninstall(c.Context(), application.UninstallClusterInput{
		ClusterID: c.Params("id"), RequestedBy: requestedBy,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(result)
}

func (h *ClusterHandler) Delete(c *fiber.Ctx) error {
	if err := h.clusters.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// SyncMembership reads the cluster's membership now instead of waiting for the interval.
// The report names members swallow could not match to a server, which is the part that
// needs a human.
func (h *ClusterHandler) SyncMembership(c *fiber.Ctx) error {
	report, err := h.membership.Execute(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(report)
}

func (h *ClusterHandler) SyncAllMembership(c *fiber.Ctx) error {
	reports, err := h.membership.ExecuteAll(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(reports)
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, clusterdomain.ErrClusterNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Cluster not found."))

	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, clusterdomain.ErrClusterNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A cluster with this name already exists at this site."))

	case errors.Is(err, clusterdomain.ErrClusterNotDeployManaged),
		errors.Is(err, clusterdomain.ErrClusterAlreadyUninstalled),
		errors.Is(err, clusterdomain.ErrClusterUninstallConflict),
		errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, operationdomain.ErrPolicyConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrAutomationConfigNotFound),
		errors.Is(err, operationdomain.ErrAutomationDisabled),
		errors.Is(err, operationdomain.ErrAutomationCredentialMissing),
		errors.Is(err, operationdomain.ErrPlaybookNotAllowed),
		errors.Is(err, operationapp.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))

	case errors.Is(err, application.ErrInvalidCluster),
		errors.Is(err, clusterdomain.ErrInvalidDeployment),
		errors.Is(err, serverdomain.ErrServerNotFound),
		errors.Is(err, clusterdomain.ErrUnsupportedClusterType):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, clusterdomain.ErrNoClusterIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"This cluster has no integration configured, so its membership cannot be read."))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The cluster integration has no credential configured."))
	}

	var readerErr *clusterdomain.ReaderError
	if errors.As(err, &readerErr) {
		if readerErr.Kind == clusterdomain.ReaderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, readerErr.Detail))
		}
		log.Printf("cluster api %s: %v", readerErr.Kind, readerErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, readerErr.Detail))
	}

	log.Printf("cluster: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
