package delivery

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/site/application"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type SiteHandler struct {
	sites        *application.SiteService
	integrations *application.IntegrationService
}

func NewSiteHandler(
	sites *application.SiteService,
	integrations *application.IntegrationService,
) *SiteHandler {
	return &SiteHandler{sites: sites, integrations: integrations}
}

// --- Sites ---

type createSiteRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *SiteHandler) CreateSite(c *fiber.Ctx) error {
	var req createSiteRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Name == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "name is required."))
	}

	item, err := h.sites.Create(c.Context(), application.CreateSiteInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (h *SiteHandler) ListSites(c *fiber.Ctx) error {
	items, err := h.sites.List(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

func (h *SiteHandler) GetSite(c *fiber.Ctx) error {
	item, err := h.sites.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type updateSiteRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (h *SiteHandler) UpdateSite(c *fiber.Ctx) error {
	var req updateSiteRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	item, err := h.sites.Update(c.Context(), c.Params("id"), application.UpdateSiteInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

func (h *SiteHandler) DeleteSite(c *fiber.Ctx) error {
	if err := h.sites.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// --- Integrations ---

type createIntegrationRequest struct {
	SiteID       string            `json:"siteId"`
	Kind         string            `json:"kind"`
	ProviderKind string            `json:"providerKind"`
	Name         string            `json:"name"`
	Endpoint     string            `json:"endpoint"`
	Credential   string            `json:"credential"`
	Settings     map[string]string `json:"settings"`
	Enabled      *bool             `json:"enabled"`
}

func (h *SiteHandler) CreateIntegration(c *fiber.Ctx) error {
	var req createIntegrationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	kind := req.Kind
	if kind == "cluster" {
		kind = "platform"
		c.Set("Deprecation", "true")
	}

	item, err := h.integrations.Create(c.Context(), application.CreateIntegrationInput{
		SiteID:       req.SiteID,
		Kind:         kind,
		ProviderKind: req.ProviderKind,
		Name:         req.Name,
		Endpoint:     req.Endpoint,
		Credential:   req.Credential,
		Settings:     req.Settings,
		Enabled:      req.Enabled,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (h *SiteHandler) ListIntegrations(c *fiber.Ctx) error {
	kind := c.Query("kind")
	if kind == "cluster" {
		kind = "platform"
		c.Set("Deprecation", "true")
	}
	items, err := h.integrations.List(c.Context(), c.Query("siteId"), kind)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

func (h *SiteHandler) GetIntegration(c *fiber.Ctx) error {
	item, err := h.integrations.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type updateIntegrationRequest struct {
	Name     *string           `json:"name"`
	Endpoint *string           `json:"endpoint"`
	Enabled  *bool             `json:"enabled"`
	Settings map[string]string `json:"settings"`
}

func (h *SiteHandler) UpdateIntegration(c *fiber.Ctx) error {
	var req updateIntegrationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	item, err := h.integrations.Update(c.Context(), c.Params("id"), application.UpdateIntegrationInput{
		Name:     req.Name,
		Endpoint: req.Endpoint,
		Enabled:  req.Enabled,
		Settings: req.Settings,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type replaceCredentialRequest struct {
	Credential string `json:"credential"`
}

// ReplaceCredential is write-only by design: there is no endpoint that returns a
// credential, in any form.
func (h *SiteHandler) ReplaceCredential(c *fiber.Ctx) error {
	var req replaceCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	if err := h.integrations.ReplaceCredential(c.Context(), c.Params("id"), req.Credential); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *SiteHandler) DeleteIntegration(c *fiber.Ctx) error {
	if err := h.integrations.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))
	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))
	case errors.Is(err, sitedomain.ErrSiteNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "A site with this name already exists."))
	case errors.Is(err, sitedomain.ErrSiteHasIntegrations):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"This site still has integrations. Delete them first."))
	case errors.Is(err, sitedomain.ErrIntegrationHasServers):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"Servers are still projected from this integration. Deleting it would strand them."))
	case errors.Is(err, sitedomain.ErrIntegrationHasDeploymentTemplates):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"Deployment templates still reference this integration. Delete them first."))
	case errors.Is(err, application.ErrInvalidIntegration):
		// The message names what was wrong and what the valid values are, which is
		// the whole value of validating the kind and provider kind together.
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	default:
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
}
