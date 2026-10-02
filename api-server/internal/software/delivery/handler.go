// Package delivery exposes the Managed Software HTTP surface: the catalog, the Software Assignment
// list, and the install/uninstall commands that create durable Workflows. It maps domain and
// operation errors to the shared API error envelope (contract: software.md).
package delivery

import (
	"errors"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// SoftwareHandler serves the /software surface.
type SoftwareHandler struct {
	software *softwareapp.SoftwareService
}

// NewSoftwareHandler wires the Managed Software use cases to HTTP.
func NewSoftwareHandler(software *softwareapp.SoftwareService) *SoftwareHandler {
	return &SoftwareHandler{software: software}
}

// catalogEntryResponse is the wire shape of one catalog entry.
type catalogEntryResponse struct {
	Kind                        string   `json:"kind"`
	Label                       string   `json:"label"`
	Roles                       []string `json:"roles"`
	MutuallyExclusiveWith       []string `json:"mutuallyExclusiveWith"`
	RefusedForKubernetesMembers bool     `json:"refusedForKubernetesMembers"`
	SpecFields                  []string `json:"specFields"`
}

// assignmentResponse is the wire shape of one Software Assignment.
type assignmentResponse struct {
	ServerID       string         `json:"serverId"`
	Kind           string         `json:"kind"`
	Roles          []string       `json:"roles"`
	Spec           map[string]any `json:"spec"`
	State          string         `json:"state"`
	LastWorkflowID string         `json:"lastWorkflowId"`
	LastAppliedAt  *string        `json:"lastAppliedAt"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
}

// operationAccepted is the shared 202 body pointing at the created Workflow.
type operationAccepted struct {
	OperationID string `json:"operationId"`
}

// Catalog returns the installable software kinds and their rules.
func (h *SoftwareHandler) Catalog(c *fiber.Ctx) error {
	entries := h.software.Catalog()
	items := make([]catalogEntryResponse, 0, len(entries))
	for _, entry := range entries {
		items = append(items, catalogEntryResponse{
			Kind: string(entry.Kind), Label: entry.Label,
			Roles: rolesToStrings(entry.Roles), MutuallyExclusiveWith: kindsToStrings(entry.MutuallyExclusiveWith),
			RefusedForKubernetesMembers: entry.RefusedForKubernetesMembers, SpecFields: stringsOrEmpty(entry.SpecFields),
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

// ListAssignments returns Software Assignments, optionally filtered by serverId/kind. Absent
// records are omitted unless includeAbsent=true.
func (h *SoftwareHandler) ListAssignments(c *fiber.Ctx) error {
	filter := softwaredomain.AssignmentFilter{
		ServerID:      c.Query("serverId"),
		Kind:          softwaredomain.Kind(c.Query("kind")),
		IncludeAbsent: c.Query("includeAbsent") == "true",
	}
	assignments, err := h.software.ListAssignments(c.Context(), filter)
	if err != nil {
		return respondError(c, err)
	}
	items := make([]assignmentResponse, 0, len(assignments))
	for _, assignment := range assignments {
		items = append(items, toAssignmentResponse(assignment))
	}
	return c.JSON(fiber.Map{"items": items})
}

// installRequest is the POST /software/assignments body.
type installRequest struct {
	Kind        string                  `json:"kind"`
	Assignments []installAssignmentBody `json:"assignments"`
	Spec        map[string]any          `json:"spec"`
}

type installAssignmentBody struct {
	ServerID string   `json:"serverId"`
	Roles    []string `json:"roles"`
}

// Install installs one software kind on one or more deployed Servers.
func (h *SoftwareHandler) Install(c *fiber.Ctx) error {
	var req installRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	targets := make([]softwareapp.InstallTarget, 0, len(req.Assignments))
	for _, assignment := range req.Assignments {
		targets = append(targets, softwareapp.InstallTarget{
			ServerID: assignment.ServerID, Roles: toRoles(assignment.Roles),
		})
	}
	operationID, err := h.software.Install(c.Context(), softwareapp.InstallInput{
		Kind: softwaredomain.Kind(req.Kind), Targets: targets, Spec: req.Spec,
		RequestedBy: requestedBy(c),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(operationAccepted{OperationID: operationID})
}

// uninstallRequest is the POST /software/uninstall body.
type uninstallRequest struct {
	Kind      string   `json:"kind"`
	ServerIDs []string `json:"serverIds"`
}

// Uninstall removes one software kind from one or more Servers on which it is installed.
func (h *SoftwareHandler) Uninstall(c *fiber.Ctx) error {
	var req uninstallRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	operationID, err := h.software.Uninstall(c.Context(), softwareapp.UninstallInput{
		Kind: softwaredomain.Kind(req.Kind), ServerIDs: req.ServerIDs, RequestedBy: requestedBy(c),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(operationAccepted{OperationID: operationID})
}

func requestedBy(c *fiber.Ctx) string {
	if principal := middleware.GetPrincipal(c); principal != nil {
		return principal.Username
	}
	return ""
}

func toAssignmentResponse(assignment *softwaredomain.Assignment) assignmentResponse {
	return assignmentResponse{
		ServerID: assignment.ServerID, Kind: string(assignment.Kind),
		Roles: rolesToStrings(assignment.Roles), Spec: assignment.Spec,
		State: string(assignment.State), LastWorkflowID: assignment.LastWorkflowID,
		LastAppliedAt: formatOptionalTime(assignment.LastAppliedAt),
		CreatedAt:     assignment.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     assignment.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toRoles(values []string) []softwaredomain.Role {
	out := make([]softwaredomain.Role, 0, len(values))
	for _, value := range values {
		out = append(out, softwaredomain.Role(value))
	}
	return out
}

func rolesToStrings(roles []softwaredomain.Role) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		out = append(out, string(role))
	}
	return out
}

func kindsToStrings(kinds []softwaredomain.Kind) []string {
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

// respondError maps software domain and operation errors to the shared envelope (software.md).
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, softwaredomain.ErrUnknownKind),
		errors.Is(err, softwaredomain.ErrInvalidRole),
		errors.Is(err, softwaredomain.ErrSpecInvalid),
		errors.Is(err, softwareapp.ErrInvalidRequest):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, softwaredomain.ErrAssignmentNotFound),
		errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, err.Error()))

	case errors.Is(err, softwaredomain.ErrMutuallyExclusive),
		errors.Is(err, softwaredomain.ErrKubernetesMember),
		errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, operationdomain.ErrTargetStateInvalid),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrAutomationConfigNotFound),
		errors.Is(err, operationdomain.ErrAutomationDisabled),
		errors.Is(err, operationdomain.ErrAutomationCredentialMissing),
		errors.Is(err, operationdomain.ErrPlaybookNotAllowed),
		errors.Is(err, operationapp.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}
	log.Printf("software: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
