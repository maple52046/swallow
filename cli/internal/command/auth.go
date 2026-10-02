package command

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
	"github.com/maple52046/swallow/cli/internal/config"
)

// apiKeyPrefix starts every API Key secret (api-keys.md); it lets login reject a
// pasted password or access token before contacting the server.
const apiKeyPrefix = "swk_"

// newLoginCommand implements `swallow login`, which stores one credential in the
// profile: a Session from a username and password (auth-login.md), or an API Key
// read from stdin and checked against auth-me.md. Storing one clears the other so
// the profile never holds a stale credential that silently takes precedence.
func newLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with a password, or store an API key",
		Long: "With --username, exchange a username and password for a Session via " +
			"POST /api/v1/auth/login and save its access and refresh tokens to the config profile; " +
			"the CLI then renews the access token automatically until the Session ends. " +
			"With --api-key-stdin, read an API key (swk_…) from stdin, verify it, and save it instead. " +
			"Provide the endpoint with --endpoint on first login.",
		RunE: runLogin,
	}
	cmd.Flags().StringP("username", "u", "", "username (password sign-in)")
	cmd.Flags().StringP("password", "p", "", "password (prefer --password-stdin to avoid shell history)")
	cmd.Flags().Bool("password-stdin", false, "read the password from stdin")
	cmd.Flags().Bool("api-key-stdin", false, "read an API key from stdin and store it instead of signing in with a password")
	cmd.MarkFlagsMutuallyExclusive("username", "api-key-stdin")
	cmd.MarkFlagsMutuallyExclusive("password", "api-key-stdin")
	cmd.MarkFlagsMutuallyExclusive("password-stdin", "api-key-stdin")
	return cmd
}

func runLogin(cmd *cobra.Command, _ []string) error {
	if useKey, _ := cmd.Flags().GetBool("api-key-stdin"); useKey {
		return loginWithAPIKey(cmd)
	}
	username, _ := cmd.Flags().GetString("username")
	if username == "" {
		return fmt.Errorf("pass --username (with --password or --password-stdin), or --api-key-stdin")
	}
	return loginWithPassword(cmd, username)
}

// loginWithPassword creates a Session with body delivery, because the CLI stores
// the refresh token itself. It never prints the password or the tokens.
func loginWithPassword(cmd *cobra.Command, username string) error {
	password, err := resolvePassword(cmd)
	if err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}

	var resp struct {
		AccessToken          string    `json:"accessToken"`
		AccessTokenExpiresAt time.Time `json:"accessTokenExpiresAt"`
		RefreshToken         string    `json:"refreshToken"`
	}
	err = c.JSON(ctx(cmd), client.Request{
		Method: "POST",
		Path:   "auth/login",
		Body: map[string]string{
			"username": username, "password": password, "refreshTokenDelivery": "body",
		},
		Auth: client.AuthNone,
	}, &resp)
	if err != nil {
		return err
	}
	if resp.AccessToken == "" {
		return fmt.Errorf("login succeeded but no access token was returned")
	}

	rt.cfg.Token = resp.AccessToken
	rt.cfg.TokenExpiresAt = resp.AccessTokenExpiresAt
	rt.cfg.RefreshToken = resp.RefreshToken
	rt.cfg.APIKey = ""
	if err := config.Save(rt.path, rt.cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Logged in to %s as %s; session saved to %s\n", rt.cfg.Endpoint, username, rt.path)
	if os.Getenv("SWALLOW_API_KEY") != "" {
		fmt.Fprintln(os.Stderr, "Note: SWALLOW_API_KEY is set and takes precedence over the saved session.")
	}
	return nil
}

// loginWithAPIKey verifies the key with GET /auth/me before saving it, so a typo
// or a deleted key is reported now rather than on the next command.
func loginWithAPIKey(cmd *cobra.Command) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read API key from stdin: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if !strings.HasPrefix(key, apiKeyPrefix) {
		return fmt.Errorf("stdin does not contain an API key (expected a value starting with %s)", apiKeyPrefix)
	}

	opts := clientOptions(gf.timeout)
	opts.APIKey = key
	c, err := client.New(opts)
	if err != nil {
		return err
	}
	var me struct {
		Username   string `json:"username"`
		AuthMethod string `json:"authMethod"`
	}
	if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "auth/me", Auth: client.AuthBearer}, &me); err != nil {
		return err
	}
	if me.AuthMethod != "api_key" {
		return fmt.Errorf("the server did not accept the value as an API key")
	}

	rt.cfg.APIKey = key
	rt.cfg.Token = ""
	rt.cfg.TokenExpiresAt = time.Time{}
	rt.cfg.RefreshToken = ""
	if err := config.Save(rt.path, rt.cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "API key for %s on %s saved to %s\n", me.Username, rt.cfg.Endpoint, rt.path)
	return nil
}

// resolvePassword returns the password from the flag, from stdin when
// --password-stdin is set, and errors when neither supplies one. Reading from
// stdin keeps the secret out of shell history and process listings.
func resolvePassword(cmd *cobra.Command) (string, error) {
	if stdin, _ := cmd.Flags().GetBool("password-stdin"); stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	password, _ := cmd.Flags().GetString("password")
	if password == "" {
		return "", fmt.Errorf("no password provided: pass --password or --password-stdin")
	}
	return password, nil
}

// newLogoutCommand implements `swallow logout`. It ends the stored Session on the
// server (auth-logout.md) so its refresh token stops working, then removes every
// stored credential. Server-side revocation is best effort: an unreachable server
// must not leave the credentials on disk, so a failure is reported as a warning.
// A stored API Key is only removed locally; delete it with `swallow api-keys
// delete` to revoke it.
func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "End the stored session and remove stored credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stored, err := config.LoadFile(rt.path)
			if err != nil {
				return err
			}
			if stored.RefreshToken != "" && rt.cfg.Endpoint != "" {
				if c, err := newClient(); err == nil {
					err = c.Discard(ctx(cmd), client.Request{
						Method: "POST", Path: "auth/logout",
						Body: map[string]string{"refreshToken": stored.RefreshToken}, Auth: client.AuthNone,
					})
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not end the session on the server: %v\n", err)
					}
				}
			}
			if err := config.Update(rt.path, func(c *config.Config) {
				c.Token = ""
				c.TokenExpiresAt = time.Time{}
				c.RefreshToken = ""
				c.APIKey = ""
			}); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Removed stored credentials from %s\n", rt.path)
			return nil
		},
	}
}

// newAuthCommand groups identity subcommands. Currently `auth me` returns the
// caller identity (auth-me.md); login/logout are top-level for discoverability.
func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect the current credential",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "me",
		Short: "Show the authenticated caller's identity, role, and how it authenticated",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "auth/me", nil)
		},
	})
	return cmd
}
