// Package delivery exposes the SSH Key use cases over HTTP (Fiber), implementing the ssh-keys.md
// contract. It resolves the caller's user id from the authenticated Principal (the routes are mounted
// behind Auth and AdminOnly), parses request bodies, and maps domain errors onto the shared API
// error envelope. No key rule lives here, and no response ever carries a stored private key.
package delivery

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/sshkey/application"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// SSHKeyHandler serves /api/v1/ssh-keys.
type SSHKeyHandler struct {
	keys *application.Service
}

// NewSSHKeyHandler wires the handler to the SSH Key service.
func NewSSHKeyHandler(keys *application.Service) *SSHKeyHandler {
	return &SSHKeyHandler{keys: keys}
}

type importRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type generateRequest struct {
	Name string `json:"name"`
}

type replaceDeploymentRequest struct {
	PrivateKey string `json:"privateKey"`
	Name       string `json:"name"`
}

// List handles GET /ssh-keys: the Deployment Key plus the caller's Access Keys.
func (h *SSHKeyHandler) List(c *fiber.Ctx) error {
	owner, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	items, err := h.keys.List(c.Context(), owner)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

// Get handles GET /ssh-keys/{keyId}: one key the caller may see, used to follow its provisioner
// sync without re-reading the list.
func (h *SSHKeyHandler) Get(c *fiber.Ctx) error {
	owner, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	item, err := h.keys.Get(c.Context(), owner, c.Params("keyId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// Import handles POST /ssh-keys, storing a public key as one of the caller's Access Keys.
func (h *SSHKeyHandler) Import(c *fiber.Ctx) error {
	owner, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	var req importRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := h.keys.ImportAccessKey(c.Context(), owner, req.Name, req.PublicKey)
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// Generate handles POST /ssh-keys/generate. The response is the only time the private key exists
// outside the caller; it is not logged and swallow keeps no copy.
func (h *SSHKeyHandler) Generate(c *fiber.Ctx) error {
	owner, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	var req generateRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	generated, err := h.keys.GenerateAccessKey(c.Context(), owner, req.Name)
	if err != nil {
		return respondError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(fiber.StatusCreated).JSON(generated)
}

// Delete handles DELETE /ssh-keys/{keyId} for one of the caller's Access Keys.
func (h *SSHKeyHandler) Delete(c *fiber.Ctx) error {
	owner, ok := callerID(c)
	if !ok {
		return unauthorized(c)
	}
	if err := h.keys.DeleteAccessKey(c.Context(), owner, c.Params("keyId")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ReplaceDeployment handles PUT /ssh-keys/deployment with an existing private key.
func (h *SSHKeyHandler) ReplaceDeployment(c *fiber.Ctx) error {
	var req replaceDeploymentRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := h.keys.ReplaceDeploymentKey(c.Context(), req.PrivateKey, req.Name)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// RegenerateDeployment handles POST /ssh-keys/deployment/regenerate.
func (h *SSHKeyHandler) RegenerateDeployment(c *fiber.Ctx) error {
	item, err := h.keys.RegenerateDeploymentKey(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// Sync handles POST /ssh-keys/sync. It only requests a pass from this process's sync loop and
// returns 202 at once; results appear in providerSync on the next list.
func (h *SSHKeyHandler) Sync(c *fiber.Ctx) error {
	h.keys.RequestSync()
	return c.SendStatus(fiber.StatusAccepted)
}

// callerID returns the authenticated user's id. Auth has already verified the token, so a missing
// id means the route was mounted without it — treated as unauthenticated rather than as a user
// with an empty id, which would otherwise address a shared bucket of keys.
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

// respondError maps SSH Key domain errors onto the contract's status codes. Parse errors carry a
// client-safe reason (they describe the submitted key, never stored material) and are returned
// verbatim as validation errors.
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, sshkeydomain.ErrInvalidName):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"A name of 1 to 100 characters is required."))
	case errors.Is(err, sshkeydomain.ErrPassphraseProtected):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"The private key is passphrase protected; swallow needs an unencrypted key."))
	case errors.Is(err, sshkeydomain.ErrInvalidKey), errors.Is(err, sshkeydomain.ErrUnsupportedKey):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, sshkeydomain.ErrDuplicateKey):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "This key already exists in swallow."))
	case errors.Is(err, sshkeydomain.ErrDuplicateName):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "You already have an SSH key with this name."))
	case errors.Is(err, sshkeydomain.ErrDeploymentKeyImmutable):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The deployment key cannot be deleted; replace or regenerate it instead."))
	case errors.Is(err, sshkeydomain.ErrKeyNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "SSH key not found."))
	case errors.Is(err, sshkeydomain.ErrDeploymentKeyNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The deployment key does not exist yet."))
	}
	slog.Error("ssh key request failed", "requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
