package command

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newPlatformsCommand groups the Platforms surface plus the live Kubernetes cluster explorer and
// the Slurm views (platforms.md, platforms-kubernetes.md). A Platform is written as a spec:
// `create` declares one, `set-spec` changes it, and `delete` declares it absent. Creating it and
// destructive changes answer with a Change Plan: --approve approves it with the write, and
// otherwise `plans approve` does.
func newPlatformsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "platforms",
		Short: "Create and manage Kubernetes and Slurm platforms",
	}
	cmd.AddCommand(
		platformsListCmd(),
		platformsGetCmd(),
		platformsCreateCmd(),
		platformsSetSpecCmd(),
		platformsUpdateCmd(),
		platformsDeleteCmd(),
		platformsSyncCmd(),
		platformsSyncAllCmd(),
		platformsSlurmCmd(),
		platformsSlurmRequirementsCmd(),
		platformsGPUOperatorCmd(),
		platformsKubernetesCmd(),
	)
	return cmd
}

// platformsGPUOperatorCmd groups the swallow-managed AMD GPU Operator of a Kubernetes Platform
// (platforms.md, "Manage The GPU Operator"; decision 062). The operator is the spec's
// settings.gpuOperator: `set` and `uninstall` read the Platform and write its spec back with the
// setting changed, at the generation they read. Turning the operator on converges at once;
// changing or turning it off answers with a Change Plan, which --approve approves with the write.
func platformsGPUOperatorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gpu-operator",
		Short: "Install, change, or uninstall a Kubernetes platform's AMD GPU Operator",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "releases",
		Short: "List the AMD GPU Operator releases Swallow ships (version and DRA driver choices)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "platforms/gpu-operator/releases", nil)
		},
	})
	set := &cobra.Command{
		Use:   "set <platformId>",
		Short: "Install or change the GPU Operator from a JSON/YAML settings body",
		Long: "Install or change the GPU Operator from a JSON/YAML settings body.\n\n" +
			"The body is the gpuOperator setting of the platforms contract: enabled (required),\n" +
			"version, allocationMode (device-plugin or dra), draDriverVersion, resourceNaming\n" +
			"(single or mixed), nodeLabeller, and metricsExporter. Omitted fields take the\n" +
			"documented defaults. No field names nodes: the operator manages every node its Node\n" +
			"Feature Discovery marks as an AMD GPU node. Switching the allocation mode is refused\n" +
			"while a Pod still uses an AMD GPU.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return changePlatformSetting(cmd, args[0], "gpuOperator", body)
		},
	}
	addFileFlag(set, "GPU Operator settings (enabled, version, allocationMode, draDriverVersion, resourceNaming, nodeLabeller, metricsExporter)")
	addApproveFlag(set)
	uninstall := &cobra.Command{
		Use:   "uninstall <platformId>",
		Short: "Uninstall the GPU Operator; the platform keeps running (answers with a Change Plan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return changePlatformSetting(cmd, args[0], "gpuOperator", map[string]any{"enabled": false})
		},
	}
	addApproveFlag(uninstall)
	cmd.AddCommand(set, uninstall)
	return cmd
}

// changePlatformSetting reads a Platform and writes its spec back with one setting replaced, at the
// generation it read, and prints the result (the Platform and the Change Plan, if any) and what
// happens to the plan. The calling command registers --approve.
func changePlatformSetting(cmd *cobra.Command, platformID, key string, value any) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	var platform struct {
		Generation int64          `json:"generation"`
		Spec       map[string]any `json:"spec"`
	}
	if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "platforms/" + platformID, Auth: client.AuthBearer}, &platform); err != nil {
		return err
	}
	if platform.Spec == nil {
		return fmt.Errorf("platform %s has no spec yet; wait for the platform controller to adopt it", platformID)
	}
	settings, _ := platform.Spec["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
	}
	settings[key] = value
	spec := map[string]any{"members": platform.Spec["members"], "settings": settings}
	if preparation := platform.Spec["machinePreparation"]; preparation != nil {
		spec["machinePreparation"] = preparation
	}
	return sendSpecWrite(cmd, "PUT", fmt.Sprintf("platforms/%s/spec", platformID), nil,
		map[string]any{"generation": platform.Generation, "spec": spec})
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

func platformsCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Kubernetes or Slurm platform from a JSON/YAML body (answers with its install plan)",
		Long: "Create a Kubernetes or Slurm platform from a JSON/YAML body: siteId, name, type, and spec\n" +
			"(members with their roles, the type's settings, and an optional machinePreparation). The\n" +
			"platform is validated and stored, and its install is the returned Change Plan. With\n" +
			"--approve the plan is approved with the request and the install starts at once, unless\n" +
			"the Change Plan policy requires a separate approval; otherwise approve it with\n" +
			"`swallow plans approve <planId>`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendSpecWrite(cmd, "POST", "platforms/", nil, body)
		},
	}
	addFileFlag(cmd, "create body (siteId, name, type, spec: {members, settings, machinePreparation})")
	addApproveFlag(cmd)
	return cmd
}

func platformsSetSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-spec <platformId>",
		Short: "Replace a platform's spec from a JSON/YAML body ({generation, spec})",
		Long: "Replace a platform's spec. The body names the generation the spec was read at (from\n" +
			"`platforms get`) and the new spec. A non-destructive change, such as a new member, is\n" +
			"converged at once; a destructive one answers with a Change Plan, which --approve approves\n" +
			"with the write unless the Change Plan policy requires a separate approval.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendSpecWrite(cmd, "PUT", fmt.Sprintf("platforms/%s/spec", args[0]), nil, body)
		},
	}
	addFileFlag(cmd, "spec change body (generation, spec: {members, settings, machinePreparation})")
	addApproveFlag(cmd)
	return cmd
}

func platformsUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <platformId>",
		Short: "Update name, gpuStackOwner, or monitoringLinks from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", "platforms/"+args[0], nil, body)
		},
	}
	// gpuStackOwner cannot be set back to provisioning while swallow manages a GPU Operator; use
	// `platforms gpu-operator uninstall` instead.
	addFileFlag(cmd, "changed platform fields (name, gpuStackOwner, monitoringLinks)")
	return cmd
}

func platformsDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <platformId>",
		Short: "Delete a platform: its uninstall is a Change Plan (--approve starts it at once)",
		Long: "Delete a platform. Its spec is marked absent and its uninstall is the returned Change Plan;\n" +
			"the record is deleted once the uninstall succeeds. With --approve the plan is approved with\n" +
			"the request and the uninstall starts at once, unless the Change Plan policy requires a\n" +
			"separate approval. Deleting a platform already marked absent with the same flags returns\n" +
			"the plan it waits on, approving it with --approve; different flags, or a plan that was\n" +
			"rejected or expired, or an uninstall that failed, propose the uninstall again. While the\n" +
			"uninstall runs nothing changes. A platform whose install never started is deleted at once.\n" +
			"--release-servers also releases the members (with the erase flags), and\n" +
			"--cancel-active-workflows cancels the Workflows holding them first. --record-only deletes\n" +
			"only the record and leaves the hosts as they are.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if recordOnly, _ := cmd.Flags().GetBool("record-only"); recordOnly {
				return sendJSON(cmd, "DELETE", "platforms/"+args[0], newQuery().set("recordOnly", "true").build(), nil)
			}
			release, _ := cmd.Flags().GetBool("release-servers")
			body := map[string]any{"releaseServers": release}
			options := map[string]any{}
			for flag, field := range map[string]string{
				"erase": "erase", "secure-erase": "secureErase", "quick-erase": "quickErase", "unbind-static-ips": "unbindStaticIps",
			} {
				if on, _ := cmd.Flags().GetBool(flag); on {
					options[field] = true
				}
			}
			if len(options) > 0 {
				body["releaseOptions"] = options
			}
			withCancel, err := withCancelActiveWorkflows(cmd, body)
			if err != nil {
				return err
			}
			return sendSpecWrite(cmd, "DELETE", "platforms/"+args[0], nil, withCancel)
		},
	}
	addApproveFlag(cmd)
	cmd.Flags().Bool("record-only", false, "delete only the record; the hosts are left as they are")
	cmd.Flags().Bool("release-servers", false, "release every member to the provider in the uninstall")
	cmd.Flags().Bool("unbind-static-ips", false, "unbind static IPs on release (requires --release-servers)")
	addEraseFlags(cmd)
	addCancelActiveWorkflowsFlag(cmd)
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
