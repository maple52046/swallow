package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newPlatformsCommand groups the Platforms lifecycle surface plus the live
// Kubernetes cluster explorer and the Slurm views (platforms.md,
// platforms-kubernetes.md). A Platform is created only by `deploy`; there is no
// generic create. The deprecated /clusters alias is intentionally not exposed.
func newPlatformsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platforms",
		Short: "Deploy and manage Kubernetes and Slurm platforms",
	}
	cmd.AddCommand(
		platformsListCmd(),
		platformsGetCmd(),
		platformsDeployCmd(),
		platformsUpdateCmd(),
		platformsDeleteCmd(),
		platformsUninstallCmd(),
		platformsSyncCmd(),
		platformsSyncAllCmd(),
		platformsSlurmCmd(),
		platformsSlurmRequirementsCmd(),
		platformsKubernetesCmd(),
	)
	return cmd
}

func platformsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List platforms, optionally filtered by Site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			return getJSON(cmd, "platforms/", q)
		},
	}
	cmd.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	return cmd
}

func platformsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <platformId>",
		Short: "Get one platform",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "platforms/"+args[0], nil)
		},
	}
}

func platformsDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a new Kubernetes or Slurm platform from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "platforms/deploy", nil, body)
		},
	}
	addFileFlag(cmd, "deploy body (siteId, name, type, roleAssignments/slurm, machinePreparation, ...)")
	return cmd
}

func platformsUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <platformId>",
		Short: "Update name, gpuStackOwner, or exporterOwner from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", "platforms/"+args[0], nil, body)
		},
	}
	addFileFlag(cmd, "changed platform fields (name, gpuStackOwner, exporterOwner)")
	return cmd
}

func platformsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <platformId>",
		Short: "Delete a platform record (cancels in-flight operations first)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", "platforms/"+args[0], nil, nil)
		},
	}
}

func platformsUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <platformId>",
		Short: "Uninstall a deployed platform (optionally releasing members)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The body is optional: an absent body removes the platform software only.
			body, _, err := readBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", fmt.Sprintf("platforms/%s/uninstall", args[0]), nil, body)
		},
	}
	addFileFlag(cmd, "optional uninstall body (releaseServers, releaseOptions)")
	return cmd
}

func platformsSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <platformId>",
		Short: "Sync one platform's membership now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("platforms/%s/sync", args[0]), nil, nil)
		},
	}
}

func platformsSyncAllCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-all",
		Short: "Sync membership for every platform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sendJSON(cmd, "POST", "platforms/sync", nil, nil)
		},
	}
}

func platformsSlurmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "slurm <platformId>",
		Short: "Read a Slurm platform's live cluster state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("platforms/%s/slurm", args[0]), nil)
		},
	}
}

func platformsSlurmRequirementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slurm-requirements",
		Short: "Read or set the system-wide Slurm deployment requirement",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Read the Slurm deployment requirement policy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "platforms/deployment-requirements/slurm", nil)
		},
	})
	set := &cobra.Command{
		Use:   "set",
		Short: "Replace the Slurm deployment requirement from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", "platforms/deployment-requirements/slurm", nil, body)
		},
	}
	addFileFlag(set, "policy body ({minimumResources: {cpuCores, memoryMiB, storageGB}} or {minimumResources: null})")
	cmd.AddCommand(set)
	return cmd
}
