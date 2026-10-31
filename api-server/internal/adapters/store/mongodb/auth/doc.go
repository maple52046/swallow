// Package auth is the MongoDB store of the authentication context: users (`users`) and sessions
// (`auth_sessions`). Passwords are stored only as hashes and refresh tokens only as SHA-256 hashes
// (decision 042), so a database dump never yields a usable credential.
//
// It maps documents to and from the auth domain and owns the collections' indexes. Login, token
// rotation, and reuse detection belong to the core auth context, not here.
package auth
