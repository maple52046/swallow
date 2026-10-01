package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newSSHKeysCommand groups the SSH Keys surface (ssh-keys.md, decision 039): the
// system-owned Deployment Key swallow logs in to Servers with, and the signed-in
// admin's public-key-only Access Keys. All routes are admin-only and act as the
// caller, so `list` shows only the caller's Access Keys.
//
// Security: the CLI handles a private key in exactly two places — reading one
// from a file for `deployment replace`, and receiving the one-time private key
// from `generate`. Neither is cached or written to the profile; `generate
// --private-key-out` writes it only to the file the operator names, with 0600
// permissions.
func newSSHKeysCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh-keys",
		Short: "Manage the Deployment Key and your SSH Access Keys",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List the Deployment Key and your Access Keys with provisioner sync status",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return getJSON(cmd, "ssh-keys", nil)
			},
		},
		&cobra.Command{
			Use:   "get <keyId>",
			Short: "Show one key (the Deployment Key or one of yours) with its provisioner sync status",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return getJSON(cmd, "ssh-keys/"+args[0], nil)
			},
		},
		sshKeysImportCmd(),
		sshKeysGenerateCmd(),
		&cobra.Command{
			Use:   "delete <keyId>",
			Short: "Delete one of your Access Keys (the Deployment Key cannot be deleted)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendNoContent(cmd, "DELETE", "ssh-keys/"+args[0], nil, nil)
			},
		},
		&cobra.Command{
			Use:   "sync",
			Short: "Request an immediate sync of every key to key-capable provisioners",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return sendNoContent(cmd, "POST", "ssh-keys/sync", nil, nil)
			},
		},
		sshKeysDeploymentCmd(),
	)
	return cmd
}

func sshKeysImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing public key as one of your Access Keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			path, _ := cmd.Flags().GetString("public-key-file")
			data, err := readFileOrStdin(path)
			if err != nil {
				return err
			}
			body := map[string]string{"name": name, "publicKey": strings.TrimSpace(string(data))}
			return sendJSON(cmd, "POST", "ssh-keys", nil, body)
		},
	}
	cmd.Flags().String("name", "", "a name for the key, unique among your Access Keys")
	cmd.Flags().String("public-key-file", "", "path to a .pub file, or - for stdin")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("public-key-file")
	return cmd
}

// sshKeysGenerateCmd generates an Access Key pair. The API returns the private
// key exactly once. With --private-key-out it is written to that file (created
// exclusively with 0600, never overwriting an existing file) and only the stored
// key is printed; without it the full response, private key included, is printed
// so the operator can capture it from stdout. The flag is not named --output
// because that is the global output-format flag.
func sshKeysGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate an ed25519 Access Key pair; the private key is shown once",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			outputPath, _ := cmd.Flags().GetString("private-key-out")

			// Opened before the request so an unusable path fails before a key pair is
			// created whose private half would then be lost.
			var file *os.File
			if outputPath != "" {
				f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if err != nil {
					return fmt.Errorf("create private key file: %w", err)
				}
				file = f
				defer file.Close()
			}

			c, err := newClient()
			if err != nil {
				return err
			}
			var out struct {
				Key        any    `json:"key"`
				PrivateKey string `json:"privateKey"`
			}
			if err := c.JSON(ctx(cmd), client.Request{
				Method: "POST", Path: "ssh-keys/generate", Body: map[string]string{"name": name}, Auth: client.AuthBearer,
			}, &out); err != nil {
				if file != nil {
					_ = os.Remove(outputPath)
				}
				return err
			}
			if file == nil {
				fmt.Fprintln(os.Stderr, "The private key below is shown only once; swallow keeps no copy.")
				return printResult(map[string]any{"key": out.Key, "privateKey": out.PrivateKey})
			}
			if _, err := file.WriteString(out.PrivateKey); err != nil {
				return fmt.Errorf("write private key to %s (the key pair exists; delete it and generate again): %w", outputPath, err)
			}
			fmt.Fprintf(os.Stderr, "Private key written to %s (mode 0600).\n", outputPath)
			return printResult(out.Key)
		},
	}
	cmd.Flags().String("name", "", "a name for the key, unique among your Access Keys")
	cmd.Flags().String("private-key-out", "", "write the private key to this new file (0600) instead of printing it")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// sshKeysDeploymentCmd groups the Deployment Key operations. The Deployment Key
// is system-owned: it can be shown, regenerated, or replaced, never deleted, and
// its private key is never returned. Replacing it does not re-authorize Servers
// deployed earlier, which keep only the previous public key.
func sshKeysDeploymentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployment",
		Short: "Show, regenerate, or replace the Deployment Key",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the Deployment Key and its provisioner sync status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			var keys []map[string]any
			if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "ssh-keys", Auth: client.AuthBearer}, &keys); err != nil {
				return err
			}
			for _, key := range keys {
				if key["purpose"] == "deployment" {
					return printResult(key)
				}
			}
			return fmt.Errorf("the installation has no Deployment Key yet; run `swallowctl install`/`upgrade` " +
				"(or `swallow-api deployment-key ensure`) on the installation host")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "regenerate",
		Short: "Replace the Deployment Key with a new ed25519 key pair",
		Long: "Replace the Deployment Key with a new ed25519 key pair. Servers deployed " +
			"before this change authorize only the previous key, so swallow cannot log in " +
			"to them with the new one unless their Site overrides the key or they are redeployed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sendJSON(cmd, "POST", "ssh-keys/deployment/regenerate", nil, nil)
		},
	})

	replace := &cobra.Command{
		Use:   "replace",
		Short: "Replace the Deployment Key with an existing unencrypted private key",
		Long: "Replace the Deployment Key with an existing unencrypted private key. The key is " +
			"stored encrypted and never returned. Servers deployed before this change keep only " +
			"the previous public key.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("private-key-file")
			name, _ := cmd.Flags().GetString("name")
			data, err := readFileOrStdin(path)
			if err != nil {
				return err
			}
			body := map[string]string{"privateKey": string(data)}
			if name != "" {
				body["name"] = name
			}
			return sendJSON(cmd, "PUT", "ssh-keys/deployment", nil, body)
		},
	}
	replace.Flags().String("private-key-file", "", "path to the private key file, or - for stdin")
	replace.Flags().String("name", "", "optional new name (defaults to the current name)")
	_ = replace.MarkFlagRequired("private-key-file")
	cmd.AddCommand(replace)
	return cmd
}
