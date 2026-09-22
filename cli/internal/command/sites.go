package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newSitesCommand groups the Sites surface plus each Site's Ansible automation
// configuration (sites-integrations.md, site-automation.md). All routes are
// admin-only.
func newSitesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sites",
		Short: "Manage Sites and their automation configuration",
	}
	cmd.AddCommand(
		sitesListCmd(),
		sitesGetCmd(),
		sitesCreateCmd(),
		sitesUpdateCmd(),
		sitesDeleteCmd(),
		sitesAutomationCmd(),
	)
	return cmd
}

func sitesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List Sites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "sites", nil)
		},
	}
}

func sitesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <siteId>",
		Short: "Get one Site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "sites/"+args[0], nil)
		},
	}
}

func sitesCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Site from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "sites", nil, body)
		},
	}
	addFileFlag(cmd, "Site definition (name, description)")
	return cmd
}

func sitesUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <siteId>",
		Short: "Update a Site from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", "sites/"+args[0], nil, body)
		},
	}
	addFileFlag(cmd, "changed Site fields")
	return cmd
}

func sitesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <siteId>",
		Short: "Delete a Site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", "sites/"+args[0], nil, nil)
		},
	}
}

// sitesAutomationCmd groups the per-Site automation configuration and its
// write-only credential (site-automation.md).
func sitesAutomationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "automation",
		Short: "Read and write a Site's Ansible automation configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get <siteId>",
		Short: "Read the automation configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("sites/%s/automation", args[0]), nil)
		},
	})

	set := &cobra.Command{
		Use:   "set <siteId>",
		Short: "Replace the automation configuration from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", fmt.Sprintf("sites/%s/automation", args[0]), nil, body)
		},
	}
	addFileFlag(set, "automation configuration (enabled, sshUser, sshPort, knownHosts, playbookMappings)")
	cmd.AddCommand(set)

	// The credential is exposed as `automation credential set` so it reads as a
	// distinct two-word action rather than crowding the top-level automation verbs.
	credentialSet := &cobra.Command{
		Use:   "set <siteId>",
		Short: "Write the write-only automation credential from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendNoContent(cmd, "PUT", fmt.Sprintf("sites/%s/automation/credential", args[0]), nil, body)
		},
	}
	addFileFlag(credentialSet, "credential material (sshPrivateKey, optional becomePassword)")
	credential := &cobra.Command{Use: "credential", Short: "Manage the automation credential"}
	credential.AddCommand(credentialSet)
	cmd.AddCommand(credential)

	return cmd
}
