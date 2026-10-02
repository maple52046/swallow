# 042. Authentication sessions and API keys

- Status: Accepted
- Date: 2026-10-02

## Context

Login exchanged a username and password for one HS256 JWT that lived 24 hours by default.
The dashboard kept it in `localStorage`, so any script injected into the page could read a
credential valid for a day, and nothing could revoke it: there was no logout on the server,
no session record, and no refresh. When the token expired the operator was sent back to the
login page.

The `swallow` CLI stored the same token in its profile and had no way to authenticate
non-interactively other than repeating the password login. The static machine token exists,
but it only opens discovery and `/metrics` and is an installation secret, not a person's
credential.

## Decision

**A login creates a Session.** `POST /api/v1/auth/login` records a Session for the User and
returns a short-lived access token (JWT, default 15 minutes, claims `typ=access`, `sid`, `jti`,
`iss`) and a refresh token. The refresh token is an opaque random value; swallow stores only
its SHA-256 hash.

- Browsers receive the refresh token as an HttpOnly, `SameSite=Strict` cookie scoped to
  `/api/v1/auth` (`Secure` whenever the request is HTTPS, including behind the TLS proxy).
  The dashboard keeps the access token in memory only.
- Non-browser clients ask for `refreshTokenDelivery: "body"`; the CLI stores the refresh
  token in its `0600` profile.
- `POST /api/v1/auth/refresh` rotates the refresh token on every use. A rotated-out token
  presented within 30 seconds yields a new access token without another rotation, so
  concurrent tabs or CLI processes do not log each other out; presented later it is treated
  as theft and revokes the whole Session.
- A Session ends at logout (`POST /api/v1/auth/logout`), after 7 days without a refresh, or
  30 days after login. All three durations are configuration. Access tokens already issued
  stay valid until they expire.

**API keys for non-interactive clients.** A User creates named API keys
(`swk_` + 32 random bytes). swallow shows the secret once and stores only its SHA-256 hash
plus a display prefix. A key acts with its owner's current role (today admin), may carry an
expiry, and is revoked by deleting it. It is sent like any bearer value
(`Authorization: Bearer swk_…`). A request authenticated by an API key cannot create API
keys, so a leaked key cannot mint replacements. Scopes are deferred to the multi-user work.

**One verifier.** The API's auth middleware accepts either an access token or an API key and
produces one principal (user, role, authentication method). The SSE query-parameter token
accepts access tokens only, so API keys never travel in URLs. The machine token is unchanged.

**Compatibility.** Login keeps returning `accessToken`, so installer tooling and seeds are
unaffected. Tokens issued before the upgrade (no `typ` claim) remain valid until they expire.
The dashboard no longer reads the old `localStorage` token; operators sign in once after the
upgrade.

## Alternatives considered

- **Keep the refresh token in `localStorage`:** rejected — it makes the longest-lived
  credential readable by any injected script, which is exactly what short access tokens are
  meant to prevent.
- **Only lengthen or shorten the single JWT:** rejected — a long token cannot be revoked and a
  short one forces frequent re-login; neither gives logout on the server.
- **Give the CLI the machine token:** rejected — it is an installation secret shared by
  scrapers, not tied to a User, and widening it to every endpoint would turn discovery
  credentials into admin credentials.
- **A separate `X-API-Key` header:** rejected — every consumer and proxy already handles the
  bearer header; the `swk_` prefix is enough to route verification.
- **bcrypt for API keys:** rejected — keys carry 256 bits of randomness, so a fast hash is
  safe and lets the verifier look a key up by its hash on every request.
- **Scoped keys now:** deferred — there is one role in use today; scopes belong with the
  multi-user model.

## Consequences

- New `auth_sessions` and `api_keys` collections; new contracts `auth-refresh.md`,
  `auth-logout.md`, `api-keys.md`; `auth-login.md`, `auth-me.md`, `conventions.md`, and
  `servers-stream.md` change additively.
- The dashboard must retry once on `401` after refreshing and re-authenticate its event
  stream; the CLI refreshes before a token expires and after a `401`.
- Logging out does not invalidate an access token already issued; its lifetime bounds the
  exposure.
- Configuration gains `accessTokenTTL`, `refreshTokenTTL`, and `sessionMaxAge`;
  `jwtExpiryHours` is deprecated.

## Current status

Implemented.
