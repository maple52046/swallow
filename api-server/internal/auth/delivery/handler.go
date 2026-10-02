// Package delivery exposes Session login, refresh, logout, and the caller identity over HTTP
// (Fiber), implementing the auth-login, auth-refresh, auth-logout, and auth-me contracts
// (decision 042).
//
// It owns the wire shape of token delivery: the browser refresh cookie (`swallow_refresh`,
// HttpOnly, SameSite=Strict, scoped to /api/v1/auth, Secure on HTTPS) versus the body field for
// the CLI. Session rules live in the application SessionService; this package never inspects a
// refresh token beyond passing it through, and never logs one.
package delivery

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/auth/application"
	authdomain "github.com/maple52046/swallow/internal/auth/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

const (
	// RefreshCookieName is the browser refresh-token cookie of the auth-login contract.
	RefreshCookieName = "swallow_refresh"
	// refreshCookiePath limits the cookie to the auth endpoints, so ordinary API calls and
	// proxies never see it.
	refreshCookiePath = "/api/v1/auth"

	deliveryCookie = "cookie"
	deliveryBody   = "body"
)

// AuthHandler serves the /api/v1/auth routes.
type AuthHandler struct {
	sessions *application.SessionService
	me       *application.MeUseCase
}

// NewAuthHandler wires the handler.
func NewAuthHandler(sessions *application.SessionService, me *application.MeUseCase) *AuthHandler {
	return &AuthHandler{sessions: sessions, me: me}
}

type loginRequest struct {
	Username             string `json:"username"`
	Password             string `json:"password"`
	RefreshTokenDelivery string `json:"refreshTokenDelivery"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// Login implements POST /api/v1/auth/login. The default delivery is the cookie, so a browser
// that sends only a username and password never sees its refresh token.
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Username == "" || req.Password == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "username and password are required."))
	}
	delivery := req.RefreshTokenDelivery
	if delivery == "" {
		delivery = deliveryCookie
	}
	if delivery != deliveryCookie && delivery != deliveryBody {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "refreshTokenDelivery must be cookie or body."))
	}
	client := authdomain.SessionClientBrowser
	if delivery == deliveryBody {
		client = authdomain.SessionClientCLI
	}

	tokens, err := h.sessions.Login(c.UserContext(), application.LoginInput{
		Username: req.Username, Password: req.Password, Client: client,
	})
	if errors.Is(err, authdomain.ErrInvalidCredentials) {
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Invalid credentials."))
	}
	if err != nil {
		return internalError(c, "login", err)
	}
	return h.respondTokens(c, tokens, delivery == deliveryBody)
}

// Refresh implements POST /api/v1/auth/refresh. A body token wins over the cookie, and the
// response uses the same delivery the request used.
func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	req, ok := parseRefreshBody(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	fromBody := req.RefreshToken != ""
	refreshToken := req.RefreshToken
	if !fromBody {
		refreshToken = c.Cookies(RefreshCookieName)
	}

	tokens, err := h.sessions.Refresh(c.UserContext(), refreshToken)
	if errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		if !fromBody && refreshToken != "" {
			clearRefreshCookie(c)
		}
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Session expired. Sign in again."))
	}
	if err != nil {
		return internalError(c, "refresh", err)
	}
	return h.respondTokens(c, tokens, fromBody)
}

// Logout implements POST /api/v1/auth/logout. It always answers 204; the route runs the
// optional authenticator so an access token can name its Session.
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	req, ok := parseRefreshBody(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	input := application.LogoutInput{RefreshToken: req.RefreshToken}
	cookie := c.Cookies(RefreshCookieName)
	if input.RefreshToken == "" {
		input.RefreshToken = cookie
	}
	if principal := middleware.GetPrincipal(c); principal != nil {
		input.SessionID = principal.SessionID
	}
	if err := h.sessions.Logout(c.UserContext(), input); err != nil {
		return internalError(c, "logout", err)
	}
	if cookie != "" {
		clearRefreshCookie(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// Me implements GET /api/v1/auth/me. Identity comes from the user record; authMethod comes from
// the credential that authenticated this request.
func (h *AuthHandler) Me(c *fiber.Ctx) error {
	principal := middleware.GetPrincipal(c)
	if principal == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
	}

	out, err := h.me.Execute(c.UserContext(), principal.UserID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "User not found."))
	}
	if err != nil {
		return internalError(c, "me", err)
	}
	out.AuthMethod = string(principal.Method)
	return c.JSON(out)
}

// respondTokens writes a login or refresh result. With body delivery the refresh token goes in
// the body; otherwise it is set as the cookie. An empty RefreshToken (rotation grace) sets
// nothing, because the caller already holds the newest token.
func (h *AuthHandler) respondTokens(c *fiber.Ctx, tokens *application.Tokens, bodyDelivery bool) error {
	// Token responses must never be cached by the browser or an intermediary.
	c.Set(fiber.HeaderCacheControl, "no-store")
	resp := fiber.Map{
		"accessToken":          tokens.AccessToken,
		"accessTokenExpiresAt": tokens.AccessTokenExpiresAt.UTC().Format(time.RFC3339),
	}
	if tokens.RefreshToken != "" {
		if bodyDelivery {
			resp["refreshToken"] = tokens.RefreshToken
		} else {
			setRefreshCookie(c, tokens.RefreshToken, tokens.RefreshTokenExpiresAt)
		}
	}
	return c.JSON(resp)
}

// parseRefreshBody reads the optional `{refreshToken}` body. An empty body is valid (cookie
// delivery); a non-empty body that is not JSON is not.
func parseRefreshBody(c *fiber.Ctx) (refreshRequest, bool) {
	var req refreshRequest
	if len(strings.TrimSpace(string(c.Body()))) == 0 {
		return req, true
	}
	if err := c.BodyParser(&req); err != nil {
		return req, false
	}
	return req, true
}

// setRefreshCookie stores the refresh token for a browser. Secure follows the request scheme
// (including X-Forwarded-Proto from the TLS proxy) so a plain-HTTP development stack still works,
// while production over HTTPS never sends the cookie in clear text.
func setRefreshCookie(c *fiber.Ctx, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   maxAge,
		Secure:   c.Protocol() == "https",
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})
}

// clearRefreshCookie expires the cookie with the same name, path, and attributes it was set with;
// a mismatched path would leave the original cookie in place.
func clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		Expires:  time.Unix(0, 0),
		Secure:   c.Protocol() == "https",
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func internalError(c *fiber.Ctx, action string, err error) error {
	slog.Error("auth "+action+" failed",
		"requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
