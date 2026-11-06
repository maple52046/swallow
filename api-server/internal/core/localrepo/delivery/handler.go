// Package delivery maps the Local Repository routes (contract local-repository.md) onto the
// localrepo use cases: it parses path, query, and body, shapes the documented JSON, and translates
// the use cases' errors into the shared error envelope. It holds no rule of its own; validation
// beyond the wire shape belongs to the application package.
package delivery

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/localrepo/application"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// Handler serves the admin routes under /api/v1/sites/{siteId}/local-repository. The routes are
// mounted behind the admin middleware, so a handler reads no principal.
type Handler struct {
	service *application.Service
}

// NewHandler returns the handler over the Local Repository use cases.
func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts the routes on the sites group (already behind the admin middleware), with
// the Site id as the siteId parameter.
func (h *Handler) RegisterRoutes(sites fiber.Router) {
	sites.Get("/:siteId/local-repository", h.Get)
	sites.Get("/:siteId/local-repository/content", h.Browse)
	sites.Put("/:siteId/local-repository/spec", h.PutSpec)
	sites.Post("/:siteId/local-repository/sync", h.Sync)
}

// sourceItem is a Repository Source's definition; lists are [] rather than null when empty.
type sourceItem struct {
	Name          string   `json:"name"`
	Format        string   `json:"format"`
	URL           string   `json:"url"`
	Suites        []string `json:"suites"`
	Components    []string `json:"components"`
	Architectures []string `json:"architectures"`
}

type specItem struct {
	Generation int64        `json:"generation"`
	Sources    []sourceItem `json:"sources"`
}

type effectiveSourceItem struct {
	sourceItem
	DeclaredBy   []string `json:"declaredBy"`
	State        string   `json:"state"`
	Message      *string  `json:"message"`
	LastSyncedAt *string  `json:"lastSyncedAt"`
	ContentCount int      `json:"contentCount"`
}

type fileItem struct {
	Path       string   `json:"path"`
	URL        string   `json:"url"`
	SHA256     string   `json:"sha256"`
	DeclaredBy []string `json:"declaredBy"`
	State      string   `json:"state"`
	Message    *string  `json:"message"`
	SizeBytes  int64    `json:"sizeBytes"`
}

type imageItem struct {
	Reference  string   `json:"reference"`
	DeclaredBy []string `json:"declaredBy"`
	State      string   `json:"state"`
	Message    *string  `json:"message"`
	SizeBytes  int64    `json:"sizeBytes"`
}

type pythonItem struct {
	Requirement string   `json:"requirement"`
	DeclaredBy  []string `json:"declaredBy"`
}

// localRepositoryResponse is the LocalRepository schema: the user's spec, the effective content
// with its sync state, readiness, and the controller's conditions with the spec generation its last
// complete pass converged (observedGeneration).
type localRepositoryResponse struct {
	SiteID             string                      `json:"siteId"`
	IntegrationID      string                      `json:"integrationId"`
	Ready              bool                        `json:"ready"`
	ContentOrigin      *string                     `json:"contentOrigin"`
	Spec               specItem                    `json:"spec"`
	Sources            []effectiveSourceItem       `json:"sources"`
	Files              []fileItem                  `json:"files"`
	Images             []imageItem                 `json:"images"`
	Python             []pythonItem                `json:"python"`
	ObservedAt         *string                     `json:"observedAt"`
	ObservedGeneration int64                       `json:"observedGeneration"`
	Conditions         []convergence.ConditionItem `json:"conditions"`
}

type contentItem struct {
	Name         string  `json:"name"`
	Version      *string `json:"version"`
	Architecture *string `json:"architecture"`
	Source       *string `json:"source"`
	Path         *string `json:"path"`
	SizeBytes    *int64  `json:"sizeBytes"`
}

// sourceRequest is one source of a spec write; omitted lists are empty.
type sourceRequest struct {
	Name          string   `json:"name"`
	Format        string   `json:"format"`
	URL           string   `json:"url"`
	Suites        []string `json:"suites"`
	Components    []string `json:"components"`
	Architectures []string `json:"architectures"`
}

type putSpecRequest struct {
	Generation *int64          `json:"generation"`
	Sources    []sourceRequest `json:"sources"`
}

// Get serves GET /sites/{siteId}/local-repository with the stored spec and status; it never calls
// the provider.
func (h *Handler) Get(c *fiber.Ctx) error {
	view, err := h.service.Get(c.Context(), c.Params("siteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toResponse(view))
}

// Browse serves GET /sites/{siteId}/local-repository/content: one page of content of a required
// format, read live from the provider. page and pageSize follow the shared pagination rules.
func (h *Handler) Browse(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	page, err := pageFromQuery(c)
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	}
	result, err := h.service.Browse(c.Context(), c.Params("siteId"), localrepodomain.ContentQuery{
		Format: localrepodomain.Format(c.Query("format")), Source: c.Query("source"), Search: c.Query("search"),
		Offset: page.Offset(), Limit: page.PageSize,
	})
	if err != nil {
		return respondError(c, err)
	}
	items := make([]contentItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, contentItem{
			Name: item.Name, Version: wire.String(item.Version), Architecture: wire.String(item.Architecture),
			Source: wire.String(item.Source), Path: wire.String(item.Path), SizeBytes: positive(item.SizeBytes),
		})
	}
	return c.JSON(pagination.NewResult(items, result.Total, page))
}

// PutSpec serves PUT /sites/{siteId}/local-repository/spec: it replaces the user's sources when the
// body's generation is the stored one and answers with the Local Repository.
func (h *Handler) PutSpec(c *fiber.Ctx) error {
	var req putSpecRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Generation == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "generation is required."))
	}
	sources := make([]localrepodomain.Source, 0, len(req.Sources))
	for _, source := range req.Sources {
		sources = append(sources, localrepodomain.Source{
			Name: source.Name, Format: localrepodomain.Format(source.Format), URL: source.URL,
			Suites: source.Suites, Components: source.Components, Architectures: source.Architectures,
		})
	}
	view, err := h.service.PutSpec(c.Context(), c.Params("siteId"), application.PutSpecInput{Generation: *req.Generation, Sources: sources})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toResponse(view))
}

// Sync serves POST /sites/{siteId}/local-repository/sync. It only asks the controller, through the
// store, to converge now and answers 202 at once; results appear in a later read.
func (h *Handler) Sync(c *fiber.Ctx) error {
	if err := h.service.RequestSync(c.Context(), c.Params("siteId")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusAccepted)
}

// pageFromQuery reads page and pageSize, refusing values outside the shared pagination bounds
// rather than silently replacing them, since a browsing client would otherwise page wrongly.
func pageFromQuery(c *fiber.Ctx) (pagination.Page, error) {
	page := pagination.Page{Page: 1, PageSize: 20}
	for _, param := range []struct {
		name  string
		value *int
		max   int
	}{{"page", &page.Page, 0}, {"pageSize", &page.PageSize, application.MaxContentPageSize}} {
		raw := c.Query(param.name)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || (param.max > 0 && value > param.max) {
			return page, errors.New(param.name + " is out of range.")
		}
		*param.value = value
	}
	return page, nil
}

// toResponse shapes a view as the LocalRepository schema: every list is [] rather than null, and an
// empty message or content origin is null.
func toResponse(view *application.View) localRepositoryResponse {
	response := localRepositoryResponse{
		SiteID:             view.SiteID,
		IntegrationID:      view.Integration.ID,
		Ready:              view.Ready,
		ContentOrigin:      wire.String(view.Integration.Setting(localrepodomain.SettingContentOrigin, "")),
		Spec:               specItem{Generation: view.Generation, Sources: []sourceItem{}},
		Sources:            []effectiveSourceItem{},
		Files:              []fileItem{},
		Images:             []imageItem{},
		Python:             []pythonItem{},
		ObservedAt:         wire.OptionalTime(view.ObservedAt),
		ObservedGeneration: view.ObservedGeneration,
		Conditions:         convergence.ConditionItems(view.Conditions),
	}
	for _, source := range view.Spec.Sources {
		response.Spec.Sources = append(response.Spec.Sources, toSourceItem(source))
	}
	for _, source := range view.Sources {
		response.Sources = append(response.Sources, effectiveSourceItem{
			sourceItem: toSourceItem(source.Source), DeclaredBy: nonNil(source.DeclaredBy),
			State: string(source.State), Message: wire.String(source.Message),
			LastSyncedAt: wire.OptionalTime(source.LastSyncedAt), ContentCount: source.ContentCount,
		})
	}
	for _, file := range view.Files {
		response.Files = append(response.Files, fileItem{
			Path: file.Path, URL: file.URL, SHA256: file.SHA256, DeclaredBy: nonNil(file.DeclaredBy),
			State: string(file.State), Message: wire.String(file.Message), SizeBytes: file.SizeBytes,
		})
	}
	for _, image := range view.Images {
		response.Images = append(response.Images, imageItem{
			Reference: image.Reference, DeclaredBy: nonNil(image.DeclaredBy),
			State: string(image.State), Message: wire.String(image.Message), SizeBytes: image.SizeBytes,
		})
	}
	for _, requirement := range view.Python {
		response.Python = append(response.Python, pythonItem{Requirement: requirement.Requirement, DeclaredBy: nonNil(requirement.DeclaredBy)})
	}
	return response
}

func toSourceItem(source localrepodomain.Source) sourceItem {
	return sourceItem{
		Name: source.Name, Format: string(source.Format), URL: source.URL,
		Suites: nonNil(source.Suites), Components: nonNil(source.Components), Architectures: nonNil(source.Architectures),
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func positive(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

// respondError maps the Local Repository errors onto the contract: a missing Site or repository
// Integration is 404, an invalid spec or query 400, a stale generation 409, and a provider that
// cannot be reached or refuses the Integration's credential 503. A provider's refusal of a content
// query is 400 with the adapter's client-safe detail.
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))
	case errors.Is(err, localrepodomain.ErrNoRepositoryIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound,
			"This site has no repository integration. Register one to give it a local repository."))
	case errors.Is(err, localrepodomain.ErrInvalidSpec), errors.Is(err, localrepodomain.ErrInvalidContentQuery):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, localrepodomain.ErrGenerationConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The local repository spec changed since it was read. Read it again and reapply the change."))
	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "The repository integration has no credential configured."))
	}
	var providerErr *localrepodomain.ProviderError
	if errors.As(err, &providerErr) {
		slog.Warn("local repository provider request failed",
			"requestId", c.GetRespHeader(fiber.HeaderXRequestID), "path", c.Path(), "error", providerErr.Error())
		message := providerErr.Detail
		if message == "" {
			message = "The local repository could not be read."
		}
		if providerErr.Kind == localrepodomain.ProviderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, message))
		}
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, message))
	}
	slog.Error("local repository request failed", "requestId", c.GetRespHeader(fiber.HeaderXRequestID), "path", c.Path(), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
