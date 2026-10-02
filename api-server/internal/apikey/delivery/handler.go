// Package delivery exposes the API Key use cases over HTTP (Fiber), implementing the
// api-keys.md contract. The routes run behind authentication and AdminOnly, and create also
// behind SessionOnly; this package resolves the caller from the Principal, parses bodies, and
// maps domain errors onto the shared envelope. The secret appears only in the create response.
package delivery

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/apikey/application"
	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// Handler serves /api/v1/api-keys.
type Handler struct {
	keys *application.Service
}

// NewHandler wires the handler to the API Key service.
func NewHandler(keys *application.Service) *Handler {
	return &Handler{keys: keys}
}

type createRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// keyResponse is the contract's API Key resource. Optional timestamps are JSON null, never
// omitted, so consumers can rely on the keys being present.
type keyResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  string  `json:"createdAt"`
	ExpiresAt  *string `json:"expiresAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

// List handles GET /api-keys.
func (h *Handler) List(c *fiber.Ctx) error {
	userID, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	keys, err := h.keys.List(c.UserContext(), userID)
	if err != nil {
		return respondError(c, err)
	}
	items := make([]keyResponse, 0, len(keys))
	for _, key := range keys {
		items = append(items, toResponse(key))
	}
	return c.JSON(items)
}

// Create handles POST /api-keys. The response is no-store because it carries the only copy of
// the secret.
func (h *Handler) Create(c *fiber.Ctx) error {
	userID, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	var req createRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"Invalid request body: expiresAt must be an ISO 8601 timestamp."))
	}
	key, secret, err := h.keys.Create(c.UserContext(), application.CreateInput{
		UserID: userID, Name: req.Name, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return respondError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"key": toResponse(key), "secret": secret})
}

// Delete handles DELETE /api-keys/{keyId}.
func (h *Handler) Delete(c *fiber.Ctx) error {
	userID, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	if err := h.keys.Delete(c.UserContext(), userID, c.Params("keyId")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func toResponse(key *apikeydomain.APIKey) keyResponse {
	return keyResponse{
		ID:         key.ID,
		Name:       key.Name,
		Prefix:     key.Prefix,
		CreatedAt:  key.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:  formatOptional(key.ExpiresAt),
		LastUsedAt: formatOptional(key.LastUsedAt),
	}
}

func formatOptional(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// respondError maps domain errors onto the contract's status codes.
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, apikeydomain.ErrInvalidName):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"name is required and must be at most 64 characters without control characters."))
	case errors.Is(err, apikeydomain.ErrInvalidExpiry):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "expiresAt must be in the future."))
	case errors.Is(err, apikeydomain.ErrDuplicateName):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "You already have an API key with this name."))
	case errors.Is(err, apikeydomain.ErrKeyLimit):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"You already have the maximum number of API keys; delete one first."))
	case errors.Is(err, apikeydomain.ErrKeyNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "API key not found."))
	default:
		slog.Error("api key request failed",
			"requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
}

// callerID returns the authenticated user's id; an empty id means the route was mounted without
// authentication and is treated as unauthenticated rather than as a shared, empty owner.
func callerID(c *fiber.Ctx) (string, bool) {
	principal := middleware.GetPrincipal(c)
	if principal == nil || principal.UserID == "" {
		return "", false
	}
	return principal.UserID, true
}

func unauthorized(c *fiber.Ctx) error {
	return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
}
