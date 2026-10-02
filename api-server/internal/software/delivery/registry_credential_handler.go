package delivery

import (
	"errors"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// RegistryCredentialHandler serves /software/docker-ce/registry-credentials (contract
// registry-credentials.md, decision 044). Passwords are accepted on create and replace and never
// returned or logged; every response uses registryCredentialResponse, which has no password field.
// Routes are mounted on the admin-only /software group.
type RegistryCredentialHandler struct {
	credentials *softwareapp.RegistryCredentialService
}

// NewRegistryCredentialHandler wires the Registry Credential use cases to HTTP.
func NewRegistryCredentialHandler(credentials *softwareapp.RegistryCredentialService) *RegistryCredentialHandler {
	return &RegistryCredentialHandler{credentials: credentials}
}

type registryCredentialResponse struct {
	ID        string `json:"id"`
	Registry  string `json:"registry"`
	Username  string `json:"username"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

func toRegistryCredentialResponse(credential *softwaredomain.RegistryCredential) registryCredentialResponse {
	return registryCredentialResponse{
		ID: credential.ID, Registry: credential.Registry, Username: credential.Username,
		CreatedAt: credential.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: credential.UpdatedAt.UTC().Format(time.RFC3339),
		UpdatedBy: credential.UpdatedBy,
	}
}

// List returns every credential ordered by registry.
func (h *RegistryCredentialHandler) List(c *fiber.Ctx) error {
	credentials, err := h.credentials.List(c.Context())
	if err != nil {
		return respondRegistryCredentialError(c, err)
	}
	items := make([]registryCredentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		items = append(items, toRegistryCredentialResponse(credential))
	}
	return c.JSON(fiber.Map{"items": items})
}

type createRegistryCredentialRequest struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Create stores a credential and returns it (201) without its password.
func (h *RegistryCredentialHandler) Create(c *fiber.Ctx) error {
	var req createRegistryCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	credential, err := h.credentials.Create(c.Context(), softwareapp.CreateRegistryCredentialInput{
		Registry: req.Registry, Username: req.Username, Password: req.Password, RequestedBy: requestedBy(c),
	})
	if err != nil {
		return respondRegistryCredentialError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toRegistryCredentialResponse(credential))
}

type replaceRegistryCredentialRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Replace overwrites a credential's username and password and returns it without the password.
func (h *RegistryCredentialHandler) Replace(c *fiber.Ctx) error {
	var req replaceRegistryCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	credential, err := h.credentials.Replace(c.Context(), softwareapp.ReplaceRegistryCredentialInput{
		ID: pathParam(c, "credentialId"), Username: req.Username, Password: req.Password, RequestedBy: requestedBy(c),
	})
	if err != nil {
		return respondRegistryCredentialError(c, err)
	}
	return c.JSON(toRegistryCredentialResponse(credential))
}

// Delete removes a credential (204).
func (h *RegistryCredentialHandler) Delete(c *fiber.Ctx) error {
	if err := h.credentials.Delete(c.Context(), pathParam(c, "credentialId")); err != nil {
		return respondRegistryCredentialError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// respondRegistryCredentialError maps credential errors to the contract's codes. A storage failure
// is logged without the request body, so no password can reach the log.
func respondRegistryCredentialError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, softwaredomain.ErrInvalidRegistryCredential):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, softwaredomain.ErrRegistryCredentialNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Registry credential not found."))
	case errors.Is(err, softwaredomain.ErrRegistryCredentialExists):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "A credential for this registry already exists. Replace it instead."))
	}
	log.Printf("registry credentials: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
