package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

// platformsKubernetesCmd builds the live Kubernetes cluster explorer subtree
// (platforms-kubernetes.md). Every route reads or writes the deployed cluster's
// Kubernetes API on demand; nothing is stored in swallow. It is available only
// for a swallow-deployed Kubernetes platform with a recorded credential.
func platformsKubernetesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubernetes",
		Short: "Explore a deployed Kubernetes platform (nodes, namespaces, apps, pods, apply)",
	}
	cmd.AddCommand(
		k8sSummaryCmd(),
		k8sNodesCmd(),
		k8sNamespacesCmd(),
		k8sApplicationsCmd(),
		k8sPodsCmd(),
		k8sApplyCmd(),
	)
	cmd.AddCommand(k8sSubsidiaryListCmds()...)
	return cmd
}

// kubeBase returns the explorer path prefix for one platform.
func kubeBase(platformID string) string {
	return fmt.Sprintf("platforms/%s/kubernetes", platformID)
}

func k8sSummaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary <platformId>",
		Short: "Read the cluster summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, kubeBase(args[0]), nil)
		},
	}
}

func k8sNodesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List nodes and cordon/uncordon them",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <platformId>",
		Short: "List cluster nodes correlated to Servers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, kubeBase(args[0])+"/nodes", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "cordon <platformId> <nodeName>",
		Short: "Mark a node unschedulable",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("%s/nodes/%s/cordon", kubeBase(args[0]), args[1]), nil, nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "uncordon <platformId> <nodeName>",
		Short: "Clear a node's unschedulable mark",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("%s/nodes/%s/uncordon", kubeBase(args[0]), args[1]), nil, nil)
		},
	})
	return cmd
}

func k8sNamespacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "namespaces",
		Short: "List, create, and delete namespaces",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <platformId>",
		Short: "List namespaces",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, kubeBase(args[0])+"/namespaces", nil)
		},
	})
	create := &cobra.Command{
		Use:   "create <platformId>",
		Short: "Create a namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			return sendJSON(cmd, "POST", kubeBase(args[0])+"/namespaces", nil, map[string]string{"name": name})
		},
	}
	create.Flags().String("name", "", "namespace name")
	cmd.AddCommand(create)
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <platformId> <namespace>",
		Short: "Delete a namespace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", fmt.Sprintf("%s/namespaces/%s", kubeBase(args[0]), args[1]), nil, nil)
		},
	})
	return cmd
}

func k8sApplicationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "applications",
		Short: "List and manage aggregated applications",
	}

	list := &cobra.Command{
		Use:   "list <platformId>",
		Short: "List applications, optionally filtered by namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, kubeBase(args[0])+"/applications", namespaceScopeQuery(cmd))
		},
	}
	addNamespaceScopeFlags(list)
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <platformId> <namespace> <kind> <name>",
		Short: "Get one application with its pods",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, applicationPath(args), nil)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <platformId> <namespace> <kind> <name>",
		Short: "Delete an application workload",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", applicationPath(args), nil, nil)
		},
	})

	scale := &cobra.Command{
		Use:   "scale <platformId> <namespace> <kind> <name>",
		Short: "Scale a Deployment or StatefulSet",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("replicas") {
				return fmt.Errorf("--replicas is required")
			}
			replicas, _ := cmd.Flags().GetInt("replicas")
			return sendJSON(cmd, "POST", applicationPath(args)+"/scale", nil, map[string]int{"replicas": replicas})
		},
	}
	scale.Flags().Int("replicas", 0, "desired replica count")
	cmd.AddCommand(scale)

	cmd.AddCommand(&cobra.Command{
		Use:   "restart <platformId> <namespace> <kind> <name>",
		Short: "Trigger a rolling restart",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", applicationPath(args)+"/restart", nil, nil)
		},
	})

	return cmd
}

// applicationPath builds the namespace/kind/name application route from the
// positional args [platformId, namespace, kind, name].
func applicationPath(args []string) string {
	return fmt.Sprintf("%s/applications/%s/%s/%s", kubeBase(args[0]), args[1], args[2], args[3])
}

func k8sPodsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pods",
		Short: "List pods, read logs, and delete pods",
	}

	list := &cobra.Command{
		Use:   "list <platformId>",
		Short: "List pods, optionally filtered by namespace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, kubeBase(args[0])+"/pods", namespaceScopeQuery(cmd))
		},
	}
	addNamespaceScopeFlags(list)
	cmd.AddCommand(list)

	logs := &cobra.Command{
		Use:   "logs <platformId> <namespace> <name>",
		Short: "Read a bounded pod log snapshot",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			container, _ := cmd.Flags().GetString("container")
			q := newQuery().set("container", container).setInt(cmd, "tailLines", "tail").build()
			return getJSON(cmd, fmt.Sprintf("%s/pods/%s/%s/logs", kubeBase(args[0]), args[1], args[2]), q)
		},
	}
	logs.Flags().String("container", "", "container name (defaults to the first container)")
	logs.Flags().Int("tail", 200, "number of trailing lines (max 2000)")
	cmd.AddCommand(logs)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <platformId> <namespace> <name>",
		Short: "Delete a pod",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "DELETE", fmt.Sprintf("%s/pods/%s/%s", kubeBase(args[0]), args[1], args[2]), nil, nil)
		},
	})

	return cmd
}

// k8sSubsidiaryListCmds builds the read-only subsidiary resource list commands.
// They share one shape, so they are generated from a table.
func k8sSubsidiaryListCmds() []*cobra.Command {
	type resource struct {
		use  string
		path string
	}
	resources := []resource{
		{"services", "services"},
		{"ingresses", "ingresses"},
		{"configmaps", "configmaps"},
		{"secrets", "secrets"},
		{"pvcs", "persistentvolumeclaims"},
	}
	cmds := make([]*cobra.Command, 0, len(resources))
	for _, r := range resources {
		path := r.path
		c := &cobra.Command{
			Use:   r.use + " <platformId>",
			Short: "List " + r.use + ", optionally filtered by namespace",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return getJSON(cmd, kubeBase(args[0])+"/"+path, namespaceScopeQuery(cmd))
			},
		}
		addNamespaceScopeFlags(c)
		cmds = append(cmds, c)
	}
	return cmds
}

func k8sApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <platformId>",
		Short: "Server-side apply a YAML manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				return fmt.Errorf("--file with a manifest is required")
			}
			data, err := readFileOrStdin(path)
			if err != nil {
				return err
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			body := map[string]any{"manifest": string(data), "dryRun": dryRun}
			return sendJSON(cmd, "POST", kubeBase(args[0])+"/apply", nil, body)
		},
	}
	addFileFlag(cmd, "path to a YAML manifest (one or more documents), or - for stdin")
	cmd.Flags().Bool("dry-run", false, "validate without persisting (server-side dry run)")
	return cmd
}

// addNamespaceScopeFlags registers the --namespace and --include-system filters
// shared by the namespaced list routes.
func addNamespaceScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("namespace", "", "restrict to one namespace")
	cmd.Flags().Bool("include-system", false, "include system namespaces when listing across all namespaces")
}

func namespaceScopeQuery(cmd *cobra.Command) map[string][]string {
	ns, _ := cmd.Flags().GetString("namespace")
	q := newQuery().set("namespace", ns)
	q.setBool(cmd, "includeSystem", "include-system")
	return q.build()
}
