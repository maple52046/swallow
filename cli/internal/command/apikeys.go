package command

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newAPIKeysCommand groups the API Key operations (api-keys.md, decision 042).
// An API Key lets scripts and CI call swallow as the signed-in user without a
// password; it acts with that user's role.
//
// Security: the secret exists in plaintext only in the create response. `create
// --secret-out` writes it to a new file with 0600 permissions (never overwriting
// one); without it the secret is printed once. The CLI never stores a created key
// in the profile — use `swallow login --api-key-stdin` for that. Creating a key
// requires a password Session: the server refuses it when the CLI itself is
// authenticated with an API Key.
func newAPIKeysCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api-keys",
		Short: "Manage your API keys for non-interactive use",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List your API keys (the secrets are never shown again)",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return getJSON(cmd, "api-keys", nil)
			},
		},
		apiKeysCreateCmd(),
		&cobra.Command{
			Use:     "delete <keyId>",
			Aliases: []string{"revoke"},
			Short:   "Delete (revoke) one of your API keys; it stops working immediately",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendNoContent(cmd, "DELETE", "api-keys/"+args[0], nil, nil)
			},
		},
	)
	return cmd
}

// apiKeysCreateCmd creates an API Key. The secret output file is opened before
// the request, so an unusable path fails before a key exists whose secret would
// then be lost.
func apiKeysCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an API key; its secret is shown only once",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			expiresIn, _ := cmd.Flags().GetString("expires-in")
			secretPath, _ := cmd.Flags().GetString("secret-out")

			body := map[string]any{"name": name}
			if expiresIn != "" {
				d, err := parseExpiresIn(expiresIn)
				if err != nil {
					return err
				}
				body["expiresAt"] = time.Now().Add(d).UTC().Format(time.RFC3339)
			}

			var file *os.File
			if secretPath != "" {
				f, err := os.OpenFile(secretPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return fmt.Errorf("create secret file: %w", err)
				}
				file = f
				defer file.Close()
			}

			c, err := newClient()
			if err != nil {
				return err
			}
			var out struct {
				Key    any    `json:"key"`
				Secret string `json:"secret"`
			}
			if err := c.JSON(ctx(cmd), client.Request{
				Method: "POST", Path: "api-keys", Body: body, Auth: client.AuthBearer,
			}, &out); err != nil {
				if file != nil {
					_ = os.Remove(secretPath)
				}
				return err
			}
			if file == nil {
				fmt.Fprintln(os.Stderr, "The secret below is shown only once; swallow keeps no copy.")
				return printResult(map[string]any{"key": out.Key, "secret": out.Secret})
			}
			if _, err := file.WriteString(out.Secret + "\n"); err != nil {
				return fmt.Errorf("write secret to %s (the key exists; delete it and create another): %w", secretPath, err)
			}
			fmt.Fprintf(os.Stderr, "Secret written to %s (mode 0600). Store it with: swallow login --api-key-stdin < %s\n", secretPath, secretPath)
			return printResult(out.Key)
		},
	}
	cmd.Flags().String("name", "", "a name for the key, unique among your API keys")
	cmd.Flags().String("expires-in", "", "lifetime such as 90d, 12h, or 30m (default: never expires)")
	cmd.Flags().String("secret-out", "", "write the secret to this new file (0600) instead of printing it")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// parseExpiresIn accepts Go durations (12h, 30m) plus whole days (90d), since
// key lifetimes are usually expressed in days.
func parseExpiresIn(value string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid --expires-in %q: use a positive number of days such as 90d", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid --expires-in %q: use a duration such as 90d, 12h, or 30m", value)
	}
	return d, nil
}
