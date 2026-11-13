package command

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newUsersCommand groups User administration (users.md, decision 082). Every
// route requires the admin role. A username is also the person's Linux login
// on the Servers the Directory reaches, so it cannot be renamed.
//
// Security: a password is sent only in the request body and never printed. Pass
// it with --password-stdin; --password exists for scripts that already hold it
// in memory and leaves it in shell history.
func newUsersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage the Users who sign in to swallow (admin)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List every User, ordered by username",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return getJSON(cmd, "users", nil)
			},
		},
		&cobra.Command{
			Use:   "get <id>",
			Short: "Get one User",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return getJSON(cmd, "users/"+args[0], nil)
			},
		},
		usersCreateCmd(),
		usersUpdateCmd(),
		&cobra.Command{
			Use:   "delete <id>",
			Short: "Delete a User with its memberships, keys, and Sessions; its uid is never reused",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendNoContent(cmd, "DELETE", "users/"+args[0], nil, nil)
			},
		},
	)
	return cmd
}

// usersCreateCmd creates a User with an initial password the admin hands over
// out of band. The server validates the username (and refuses reserved names)
// and the password rule; the CLI only requires that both are present.
func usersCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a User with an initial password",
		RunE: func(cmd *cobra.Command, _ []string) error {
			username, _ := cmd.Flags().GetString("username")
			role, _ := cmd.Flags().GetString("role")
			password, err := resolvePassword(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "users", nil, map[string]any{
				"username": username, "password": password, "role": role,
			})
		},
	}
	cmd.Flags().String("username", "", "username, also the Linux login (cannot be changed later)")
	cmd.Flags().String("role", "user", "User Role: user or admin")
	addPasswordFlags(cmd, "initial password")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// usersUpdateCmd changes a User's role, resets its password, or both. Only the
// flags given are sent, so an omitted field keeps its value. Demoting the last
// admin is refused by the server with `conflict`.
func usersUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Change a User's role or reset its password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if cmd.Flags().Changed("role") {
				role, _ := cmd.Flags().GetString("role")
				body["role"] = role
			}
			if cmd.Flags().Changed("password") || cmd.Flags().Changed("password-stdin") {
				password, err := resolvePassword(cmd)
				if err != nil {
					return err
				}
				body["password"] = password
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: pass --role, --password, or --password-stdin")
			}
			return sendJSON(cmd, "PATCH", "users/"+args[0], nil, body)
		},
	}
	cmd.Flags().String("role", "", "new User Role: user or admin")
	addPasswordFlags(cmd, "new password")
	return cmd
}

// addPasswordFlags adds the --password / --password-stdin pair resolvePassword
// reads; what names the password in the help text.
func addPasswordFlags(cmd *cobra.Command, what string) {
	cmd.Flags().StringP("password", "p", "", what+" (prefer --password-stdin to avoid shell history)")
	cmd.Flags().Bool("password-stdin", false, "read the "+what+" from stdin")
	cmd.MarkFlagsMutuallyExclusive("password", "password-stdin")
}

// newPasswordCommand implements `swallow auth password`: the signed-in User
// changes its own password (auth-me.md). The server requires a password
// Session, so it refuses the request when the CLI is authenticated with an API
// Key. Both passwords are read from stdin, one per line, so neither reaches the
// shell history or the process list.
func newPasswordCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Change your own password (reads the current and the new password from stdin, one per line)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, next, err := readPasswordPair(os.Stdin)
			if err != nil {
				return err
			}
			return sendNoContent(cmd, "PUT", "auth/me/password", nil, map[string]any{
				"currentPassword": current, "newPassword": next,
			})
		},
	}
	return cmd
}

// readPasswordPair reads the current password and the new one from the first
// two lines of r. A missing line is an error, so a truncated input never sends
// an empty new password.
func readPasswordPair(r io.Reader) (current, next string, err error) {
	scanner := bufio.NewScanner(r)
	lines := make([]string, 0, 2)
	for len(lines) < 2 && scanner.Scan() {
		lines = append(lines, strings.TrimRight(scanner.Text(), "\r"))
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("read passwords from stdin: %w", err)
	}
	if len(lines) < 2 || lines[0] == "" || lines[1] == "" {
		return "", "", fmt.Errorf("stdin must hold the current password and the new password, one per line")
	}
	return lines[0], lines[1], nil
}
