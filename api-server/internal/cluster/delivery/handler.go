package delivery

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/AFDEAPAC/swallow/internal/cluster/application"
	clusterdomain "github.com/AFDEAPAC/swallow/internal/cluster/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/apierror"
	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

type ClusterHandler struct {
	clusters   *application.ClusterService
	membership *application.MembershipSyncUseCase
}

func NewClusterHandler(
	clusters *application.ClusterService,
	membership *application.MembershipSyncUseCase,
) *ClusterHandler {
	return &ClusterHandler{clusters: clusters, membership: membership}
}

type createClusterRequest struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IntegrationID string `json:"integrationId"`
	GPUStackOwner string `json:"gpuStackOwner"`
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
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
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
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

func (h *ClusterHandler) Delete(c *fiber.Ctx) error {
	if err := h.clusters.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// SyncMembership reads the cluster's membership now instead of waiting for the interval.
// The report names members gdcm could not match to a server, which is the part that
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

	case errors.Is(err, application.ErrInvalidCluster),
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
