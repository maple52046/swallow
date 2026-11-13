package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// DirectoryInfo is what the access contract reports about the installation's Directory besides the
// controller's status: whether one is configured, the URL the Servers connect to, and its base DN.
type DirectoryInfo struct {
	Configured bool
	HostURL    string
	BaseDN     string
}

// HandlerDeps are the access Handler's dependencies; every field is required.
type HandlerDeps struct {
	Users       *accessapp.UserService
	Memberships *accessapp.MembershipService
	Statuses    accessdomain.DirectoryStatusRepository
	Requests    convergence.Requester
	Directory   DirectoryInfo
	// POSIXIDStart reports the first number of the installation's POSIX ID range: the recorded one
	// once allocated, else the configured one.
	POSIXIDStart func() int
}

// Handler serves the users, pool-access, and access contracts and the caller's password change. The
// routes it serves are guarded by Guard (admin, or Pool access for the pool-access routes, which the
// MembershipService re-checks against the Pool).
type Handler struct {
	deps HandlerDeps
}

// NewHandler returns the Handler.
func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{deps: deps}
}

type userResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	UIDNumber *int   `json:"uidNumber"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toUserResponse(user *authdomain.User) userResponse {
	out := userResponse{
		ID: user.ID, Username: user.Username, Role: string(user.Role),
		CreatedAt: wire.Time(user.CreatedAt), UpdatedAt: wire.Time(user.UpdatedAt),
	}
	if user.UIDNumber > 0 {
		uid := user.UIDNumber
		out.UIDNumber = &uid
	}
	return out
}

// ListUsers implements GET /api/v1/users.
func (h *Handler) ListUsers(c *fiber.Ctx) error {
	users, err := h.deps.Users.List(c.UserContext())
	if err != nil {
		return internal(c, "list users", err)
	}
	items := make([]userResponse, 0, len(users))
	for _, user := range users {
		items = append(items, toUserResponse(user))
	}
	return c.JSON(fiber.Map{"items": items})
}

// GetUser implements GET /api/v1/users/{id}.
func (h *Handler) GetUser(c *fiber.Ctx) error {
	user, err := h.deps.Users.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toUserResponse(user))
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUser implements POST /api/v1/users.
func (h *Handler) CreateUser(c *fiber.Ctx) error {
	var req createUserRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return validation(c, "Invalid request body.")
	}
	user, err := h.deps.Users.Create(c.UserContext(), scopeOf(c), accessapp.CreateUserInput{
		Username: req.Username, Password: req.Password, Role: authdomain.Role(req.Role),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toUserResponse(user))
}

type updateUserRequest struct {
	Role     *string `json:"role"`
	Password *string `json:"password"`
}

// UpdateUser implements PATCH /api/v1/users/{id}.
func (h *Handler) UpdateUser(c *fiber.Ctx) error {
	var req updateUserRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return validation(c, "Invalid request body.")
	}
	input := accessapp.UpdateUserInput{Password: req.Password}
	if req.Role != nil {
		role := authdomain.Role(*req.Role)
		input.Role = &role
	}
	user, err := h.deps.Users.Update(c.UserContext(), scopeOf(c), c.Params("id"), input)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toUserResponse(user))
}

// DeleteUser implements DELETE /api/v1/users/{id}.
func (h *Handler) DeleteUser(c *fiber.Ctx) error {
	if err := h.deps.Users.Delete(c.UserContext(), scopeOf(c), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// ChangeOwnPassword implements PUT /api/v1/auth/me/password; the route runs SessionOnly, so an API
// Key never changes its owner's password. A wrong current password is 400, not 401, so a client does
// not mistake it for an expired Session.
func (h *Handler) ChangeOwnPassword(c *fiber.Ctx) error {
	principal := middleware.GetPrincipal(c)
	if principal == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
	}
	var req changePasswordRequest
	if err := decodeStrict(c.Body(), &req); err != nil || req.CurrentPassword == "" || req.NewPassword == "" {
		return validation(c, "currentPassword and newPassword are required.")
	}
	if err := h.deps.Users.ChangePassword(c.UserContext(), principal.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type effectiveValue struct {
	Setting   string `json:"setting"`
	Effective bool   `json:"effective"`
	Source    string `json:"source"`
}

type memberResponse struct {
	UserID           string         `json:"userId"`
	Username         string         `json:"username"`
	PoolID           string         `json:"poolId"`
	Role             string         `json:"role"`
	Sudo             effectiveValue `json:"sudo"`
	SudoPasswordless effectiveValue `json:"sudoPasswordless"`
	CreatedAt        string         `json:"createdAt"`
	UpdatedAt        string         `json:"updatedAt"`
}

func toMemberResponse(view accessapp.MemberView) memberResponse {
	m := view.Membership
	return memberResponse{
		UserID: m.UserID, Username: view.Username, PoolID: m.PoolID, Role: string(m.Role),
		Sudo:             effectiveValue{Setting: string(m.Sudo), Effective: view.Effective.Sudo, Source: string(view.Effective.SudoSource)},
		SudoPasswordless: effectiveValue{Setting: string(m.SudoPasswordless), Effective: view.Effective.Passwordless, Source: string(view.Effective.PasswordlessSource)},
		CreatedAt:        wire.Time(m.CreatedAt), UpdatedAt: wire.Time(m.UpdatedAt),
	}
}

// ListPoolMembers implements GET /api/v1/infrastructure/pools/{id}/members.
func (h *Handler) ListPoolMembers(c *fiber.Ctx) error {
	views, err := h.deps.Memberships.Members(c.UserContext(), scopeOf(c), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	items := make([]memberResponse, 0, len(views))
	for _, view := range views {
		items = append(items, toMemberResponse(view))
	}
	return c.JSON(fiber.Map{"items": items})
}

type addMemberRequest struct {
	Username         string `json:"username"`
	Role             string `json:"role"`
	Sudo             string `json:"sudo"`
	SudoPasswordless string `json:"sudoPasswordless"`
}

// AddPoolMember implements POST /api/v1/infrastructure/pools/{id}/members.
func (h *Handler) AddPoolMember(c *fiber.Ctx) error {
	var req addMemberRequest
	if err := decodeStrict(c.Body(), &req); err != nil || req.Username == "" {
		return validation(c, "username and role are required.")
	}
	view, err := h.deps.Memberships.AddMember(c.UserContext(), scopeOf(c), c.Params("id"), accessapp.AddMemberInput{
		Username: req.Username, Role: accessdomain.PoolRole(req.Role),
		Sudo: accessdomain.SudoSetting(req.Sudo), SudoPasswordless: accessdomain.SudoSetting(req.SudoPasswordless),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toMemberResponse(*view))
}

type updateMemberRequest struct {
	Role             *string `json:"role"`
	Sudo             *string `json:"sudo"`
	SudoPasswordless *string `json:"sudoPasswordless"`
}

// UpdatePoolMember implements PATCH /api/v1/infrastructure/pools/{id}/members/{userId}.
func (h *Handler) UpdatePoolMember(c *fiber.Ctx) error {
	var req updateMemberRequest
	if err := decodeStrict(c.Body(), &req); err != nil {
		return validation(c, "Invalid request body.")
	}
	var input accessapp.UpdateMemberInput
	if req.Role != nil {
		role := accessdomain.PoolRole(*req.Role)
		input.Role = &role
	}
	if req.Sudo != nil {
		setting := accessdomain.SudoSetting(*req.Sudo)
		input.Sudo = &setting
	}
	if req.SudoPasswordless != nil {
		setting := accessdomain.SudoSetting(*req.SudoPasswordless)
		input.SudoPasswordless = &setting
	}
	view, err := h.deps.Memberships.UpdateMember(c.UserContext(), scopeOf(c), c.Params("id"), c.Params("userId"), input)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toMemberResponse(*view))
}

// RemovePoolMember implements DELETE /api/v1/infrastructure/pools/{id}/members/{userId}.
func (h *Handler) RemovePoolMember(c *fiber.Ctx) error {
	if err := h.deps.Memberships.RemoveMember(c.UserContext(), scopeOf(c), c.Params("id"), c.Params("userId")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type policyResponse struct {
	PoolID           string  `json:"poolId"`
	MemberSudo       bool    `json:"memberSudo"`
	SudoPasswordless bool    `json:"sudoPasswordless"`
	UpdatedAt        *string `json:"updatedAt"`
	UpdatedBy        string  `json:"updatedBy"`
}

func toPolicyResponse(policy accessdomain.PoolPolicy) policyResponse {
	out := policyResponse{PoolID: policy.PoolID, MemberSudo: policy.MemberSudo, SudoPasswordless: policy.SudoPasswordless, UpdatedBy: policy.UpdatedBy}
	if !policy.UpdatedAt.IsZero() {
		out.UpdatedAt = wire.TimePtr(policy.UpdatedAt)
	}
	return out
}

// GetPoolAccessPolicy implements GET /api/v1/infrastructure/pools/{id}/access-policy.
func (h *Handler) GetPoolAccessPolicy(c *fiber.Ctx) error {
	policy, err := h.deps.Memberships.Policy(c.UserContext(), scopeOf(c), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toPolicyResponse(policy))
}

type policyRequest struct {
	MemberSudo       *bool `json:"memberSudo"`
	SudoPasswordless *bool `json:"sudoPasswordless"`
}

// SetPoolAccessPolicy implements PUT /api/v1/infrastructure/pools/{id}/access-policy.
func (h *Handler) SetPoolAccessPolicy(c *fiber.Ctx) error {
	var req policyRequest
	if err := decodeStrict(c.Body(), &req); err != nil || req.MemberSudo == nil || req.SudoPasswordless == nil {
		return validation(c, "memberSudo and sudoPasswordless are required.")
	}
	policy, err := h.deps.Memberships.SetPolicy(c.UserContext(), scopeOf(c), c.Params("id"), *req.MemberSudo, *req.SudoPasswordless)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toPolicyResponse(policy))
}

type settingsResponse struct {
	AdminSudoPasswordless bool    `json:"adminSudoPasswordless"`
	POSIXIDStart          int     `json:"posixIdStart"`
	UpdatedAt             *string `json:"updatedAt"`
	UpdatedBy             string  `json:"updatedBy"`
}

func (h *Handler) toSettingsResponse(settings accessdomain.Settings) settingsResponse {
	out := settingsResponse{AdminSudoPasswordless: settings.AdminSudoPasswordless, POSIXIDStart: h.deps.POSIXIDStart(), UpdatedBy: settings.UpdatedBy}
	if !settings.UpdatedAt.IsZero() {
		out.UpdatedAt = wire.TimePtr(settings.UpdatedAt)
	}
	return out
}

// GetAccessSettings implements GET /api/v1/access/settings.
func (h *Handler) GetAccessSettings(c *fiber.Ctx) error {
	settings, err := h.deps.Memberships.Settings(c.UserContext())
	if err != nil {
		return internal(c, "read access settings", err)
	}
	return c.JSON(h.toSettingsResponse(settings))
}

type settingsRequest struct {
	AdminSudoPasswordless *bool `json:"adminSudoPasswordless"`
}

// SetAccessSettings implements PUT /api/v1/access/settings.
func (h *Handler) SetAccessSettings(c *fiber.Ctx) error {
	var req settingsRequest
	if err := decodeStrict(c.Body(), &req); err != nil || req.AdminSudoPasswordless == nil {
		return validation(c, "adminSudoPasswordless is required.")
	}
	settings, err := h.deps.Memberships.SetSettings(c.UserContext(), scopeOf(c), *req.AdminSudoPasswordless)
	if err != nil {
		return internal(c, "save access settings", err)
	}
	return c.JSON(h.toSettingsResponse(settings))
}

type auditResponse struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	PoolID string `json:"poolId"`
	Detail string `json:"detail"`
}

// ListAccessAudit implements GET /api/v1/access/audit.
func (h *Handler) ListAccessAudit(c *fiber.Ctx) error {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 500 {
			return validation(c, "limit must be an integer from 1 to 500.")
		}
		limit = value
	}
	entries, err := h.deps.Memberships.Audit(c.UserContext(), limit)
	if err != nil {
		return internal(c, "list access audit", err)
	}
	items := make([]auditResponse, 0, len(entries))
	for _, entry := range entries {
		items = append(items, auditResponse{
			ID: entry.ID, At: wire.Time(entry.At), Actor: entry.Actor, Action: string(entry.Action),
			Target: entry.Target, PoolID: entry.PoolID, Detail: entry.Detail,
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

type directoryResponse struct {
	Configured    bool                        `json:"configured"`
	HostURL       string                      `json:"hostUrl"`
	BaseDN        string                      `json:"baseDn"`
	Conditions    []convergence.ConditionItem `json:"conditions"`
	LastAppliedAt *string                     `json:"lastAppliedAt"`
	ObservedAt    *string                     `json:"observedAt"`
	Users         int                         `json:"users"`
	Groups        int                         `json:"groups"`
	SudoRules     int                         `json:"sudoRules"`
	Hosts         int                         `json:"hosts"`
}

// GetDirectory implements GET /api/v1/access/directory.
func (h *Handler) GetDirectory(c *fiber.Ctx) error {
	out := directoryResponse{Conditions: []convergence.ConditionItem{}}
	if !h.deps.Directory.Configured {
		return c.JSON(out)
	}
	status, err := h.deps.Statuses.Find(c.UserContext())
	if err != nil {
		return internal(c, "read directory status", err)
	}
	out.Configured, out.HostURL, out.BaseDN = true, h.deps.Directory.HostURL, h.deps.Directory.BaseDN
	out.Conditions = convergence.ConditionItems(status.Conditions)
	if status.LastAppliedAt != nil {
		out.LastAppliedAt = wire.TimePtr(*status.LastAppliedAt)
	}
	if status.ObservedAt != nil {
		out.ObservedAt = wire.TimePtr(*status.ObservedAt)
	}
	out.Users, out.Groups, out.SudoRules, out.Hosts = status.Users, status.Groups, status.SudoRules, status.Hosts
	return c.JSON(out)
}

// SyncDirectory implements POST /api/v1/access/directory/sync.
func (h *Handler) SyncDirectory(c *fiber.Ctx) error {
	if !h.deps.Directory.Configured {
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "The installation has no Directory configured."))
	}
	if err := h.deps.Requests.Request(c.UserContext(), accessapp.DirectoryControllerName); err != nil {
		return internal(c, "request directory sync", err)
	}
	return c.SendStatus(fiber.StatusAccepted)
}

// scopeOf returns the caller's Scope; routes are guarded by Guard.Scope, so a missing one is an
// empty Scope that grants nothing.
func scopeOf(c *fiber.Ctx) accessdomain.Scope {
	if v, ok := VisibilityFrom(c); ok {
		return v.Scope()
	}
	if principal := middleware.GetPrincipal(c); principal != nil {
		return accessdomain.Scope{UserID: principal.UserID, Username: principal.Username}
	}
	return accessdomain.Scope{}
}

// respondError maps the access and auth domain errors onto the contracts.
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, accessdomain.ErrNotVisible), errors.Is(err, accessdomain.ErrForbidden):
		return RespondDenied(c, err)
	case errors.Is(err, authdomain.ErrUserNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "User not found."))
	case errors.Is(err, accessdomain.ErrMembershipNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The user is not a member of the pool."))
	case errors.Is(err, accessdomain.ErrMembershipExists):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "The user is already a member of the pool."))
	case errors.Is(err, authdomain.ErrUsernameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "Another user has this username."))
	case errors.Is(err, authdomain.ErrLastAdmin):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "The installation must keep at least one admin."))
	case errors.Is(err, authdomain.ErrDeleteSelf):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "You cannot delete your own account."))
	case errors.Is(err, authdomain.ErrWrongPassword):
		return validation(c, "The current password is wrong.")
	case errors.Is(err, authdomain.ErrInvalidUsername), errors.Is(err, authdomain.ErrReservedUsername),
		errors.Is(err, authdomain.ErrInvalidPassword):
		return validation(c, capitalize(err.Error())+".")
	case errors.Is(err, authdomain.ErrInvalidRole):
		return validation(c, "role must be admin or user.")
	case errors.Is(err, accessdomain.ErrInvalidPoolRole):
		return validation(c, "role must be owner or member.")
	case errors.Is(err, accessdomain.ErrInvalidSudoSetting):
		return validation(c, "sudo and sudoPasswordless must be inherit, on, or off.")
	default:
		return internal(c, "request", err)
	}
}

func validation(c *fiber.Ctx, message string) error {
	return apierror.Respond(c, apierror.New(apierror.CodeValidation, message))
}

// decodeStrict decodes a JSON body, refusing unknown fields, matching the closed request schemas.
func decodeStrict(body []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
