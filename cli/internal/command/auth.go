package command

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
	"github.com/maple52046/swallow/cli/internal/config"
)

// newLoginCommand implements `swallow login`, the only command that establishes
// a session (auth-login.md). It exchanges username/password for an access token
// and persists it to the profile so subsequent commands are authenticated.
func newLoginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store an access token",
		Long: "Exchange a username and password for an access token via " +
			"POST /api/v1/auth/login and save it to the config profile. " +
			"Provide the endpoint with --endpoint on first login.",
		RunE: runLogin,
	}
	cmd.Flags().StringP("username", "u", "", "username")
	cmd.Flags().StringP("password", "p", "", "password (prefer --password-stdin to avoid shell history)")
	cmd.Flags().Bool("password-stdin", false, "read the password from stdin")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// runLogin performs the credential exchange and writes the token. It never logs
// the password or token; on success it prints only a confirmation with the
// stored endpoint.
func runLogin(cmd *cobra.Command, _ []string) error {
	username, _ := cmd.Flags().GetString("username")
	password, err := resolvePassword(cmd)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	err = c.JSON(ctx(cmd), client.Request{
		Method: "POST",
		Path:   "auth/login",
		Body:   map[string]string{"username": username, "password": password},
		Auth:   client.AuthNone,
	}, &resp)
	if err != nil {
		return err
	}
	if resp.AccessToken == "" {
		return fmt.Errorf("login succeeded but no access token was returned")
	}

	// Persist the token (and the effective endpoint) so later commands reuse the
	// session. Other stored fields are preserved by writing back the resolved cfg.
	rt.cfg.Token = resp.AccessToken
	if err := config.Save(rt.path, rt.cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Logged in to %s as %s; token saved to %s\n", rt.cfg.Endpoint, username, rt.path)
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

// newLogoutCommand implements `swallow logout`. It clears the stored token
// locally; there is no server-side session to revoke because the token is a
// stateless JWT, so this is a client-only operation.
func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored access token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt.cfg.Token = ""
			if err := config.Save(rt.path, rt.cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Cleared access token in %s\n", rt.path)
			return nil
		},
	}
}

// newAuthCommand groups identity subcommands. Currently `auth me` returns the
// caller identity (auth-me.md); login/logout are top-level for discoverability.
func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect the current session",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "me",
		Short: "Show the authenticated caller's identity and role",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "auth/me", nil)
		},
	})
	return cmd
}
