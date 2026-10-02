# Session

- Bounded context: Access.
- Definition: One authenticated sign-in of a swallow User, created by a username and password
  login and lasting until the User logs out, stops using it for the idle limit, or reaches the
  maximum age.
- Allowed meaning: A client holds a Session through two credentials. The **access token** is a
  short-lived bearer credential (minutes) sent with every API call. The **refresh token** is a
  longer-lived secret used only to obtain a new access token; each use replaces it with a new
  one. A browser keeps the refresh token in a cookie that page scripts cannot read; the CLI keeps
  it in its private profile file. Logging out revokes the Session; access tokens already issued
  remain valid until they expire. Presenting a refresh token that was already replaced (outside
  a short grace period) is treated as theft and revokes the Session.
- Disallowed meaning: Not an API Key, which a User creates explicitly and which lasts until it
  expires or is deleted. Not a Server Event Stream connection or any other open connection. Not
  a workflow or Temporal execution. Not a role: the Session carries the User's role, it does not
  define one.
- Synonyms: Login session.
- Deprecated terms: None.
- Examples: "The dashboard's access token expired after 15 minutes; it refreshed the Session in
  the background and retried the request." / "After seven days without use the Session expired
  and the operator signed in again." / "Logging out of the CLI revoked its Session, so its saved
  refresh token no longer works."
- Related terms: User, API Key.
- Change note: Added when login moved from one 24-hour token to short access tokens plus a
  rotating refresh token
  ([decision 042](../../../decisions/042-authentication-sessions-and-api-keys.md)).
