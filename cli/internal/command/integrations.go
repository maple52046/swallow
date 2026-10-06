package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newIntegrationsCommand groups the Integrations surface (sites-integrations.md).
// Credentials are write-only and never returned; `credential set` replaces them.
func newIntegrationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integrations",
		Short: "Manage provider integrations",
	}
	cmd.AddCommand(
		integrationsListCmd(),
		integrationsGetCmd(),
		integrationsCreateCmd(),
		integrationsUpdateCmd(),
		integrationsCredentialCmd(),
		integrationsDeleteCmd(),
		integrationsEnrollBundleCmd(),
	)
	return cmd
}

// integrationsEnrollBundleCmd prints the existing-OS enrollment bundle of a provisioner
// (server-enrollment.md): its endpoint, credential, and the one-line command to run on the host,
// which downloads the enrollment script and the CLI from swallow. The command fetches them from
// --swallow-url, by default this profile's endpoint. The output contains the provisioner's
// credential; it is not stored.
func integrationsEnrollBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enroll-bundle <integrationId>",
		Short: "Print the command a host runs to enroll into this provisioner keeping its OS (contains its credential)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			swallowURL, _ := cmd.Flags().GetString("swallow-url")
			if swallowURL == "" {
				swallowURL = rt.cfg.Endpoint
			}
			var body any
			if swallowURL != "" {
				body = map[string]string{"swallowUrl": swallowURL}
			}
			return sendJSON(cmd, "POST", fmt.Sprintf("provisioning/integrations/%s/enroll-bundle", args[0]), nil, body)
		},
	}
	cmd.Flags().String("swallow-url", "", "address the host reaches swallow at (default: this profile's endpoint)")
	return cmd
}

func integrationsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List integrations, optionally filtered by Site and kind",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			kind, _ := cmd.Flags().GetString("kind")
			q := newQuery().set("siteId", defaultSite(site)).set("kind", kind).build()
			return getJSON(cmd, "integrations", q)
		},
	}
	cmd.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	cmd.Flags().String("kind", "", "filter by kind: provisioner or metrics")
	return cmd
}

func integrationsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <integrationId>",
		Short: "Get one integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "integrations/"+args[0], nil)
		},
	}
}

func integrationsCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an integration from a JSON/YAML body",
		Long:  "Create a provisioner or metrics integration. kind=platform is rejected by the server; a platform integration is created only by a platform deploy.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "integrations", nil, body)
		},
	}
	addFileFlag(cmd, "integration definition (siteId, kind, providerKind, name, endpoint, settings, credential)")
	return cmd
}

func integrationsUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <integrationId>",
		Short: "Update an integration from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", "integrations/"+args[0], nil, body)
		},
	}
	addFileFlag(cmd, "changed integration fields")
	return cmd
}

func integrationsCredentialCmd() *cobra.Command {
	parent := &cobra.Command{Use: "credential", Short: "Manage the write-only integration credential"}
	set := &cobra.Command{
		Use:   "set <integrationId>",
		Short: "Replace the integration credential from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", fmt.Sprintf("integrations/%s/credential", args[0]), nil, body)
		},
	}
	addFileFlag(set, "credential material for the provider")
	parent.AddCommand(set)
	return parent
}

func integrationsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <integrationId>",
		Short: "Delete an integration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", "integrations/"+args[0], nil, nil)
		},
	}
}
