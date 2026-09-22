package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newWorkflowsCommand groups the durable Workflow surface (workflows.md):
// create, list, inspect, cancel, rerun, and per-Task retry and diagnostics. The
// deprecated /operations alias is intentionally not exposed.
func newWorkflowsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflows",
		Short: "Create, observe, and control durable Workflows",
	}
	cmd.AddCommand(
		workflowsListCmd(),
		workflowsGetCmd(),
		workflowsCreateCmd(),
		workflowsCancelCmd(),
		workflowsRerunCmd(),
		workflowsTimelineCmd(),
		workflowsTaskCmd(),
	)
	return cmd
}

func workflowsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Workflows with filtering and pagination",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := newQuery()
			applyPagination(cmd, q)
			site, _ := cmd.Flags().GetString("site-id")
			platform, _ := cmd.Flags().GetString("platform")
			server, _ := cmd.Flags().GetString("server")
			kind, _ := cmd.Flags().GetString("kind")
			status, _ := cmd.Flags().GetString("status")
			q.set("siteId", defaultSite(site)).
				set("platformId", platform).
				set("serverId", server).
				set("kind", kind).
				set("status", status).
				setBool(cmd, "active", "active")
			return getJSON(cmd, "workflows/", q.build())
		},
	}
	addPagination(cmd)
	cmd.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	cmd.Flags().String("platform", "", "filter by platform id")
	cmd.Flags().String("server", "", "filter by target Server id")
	cmd.Flags().String("kind", "", "filter by Workflow kind")
	cmd.Flags().String("status", "", "filter by Workflow status")
	cmd.Flags().Bool("active", false, "only active Workflows")
	return cmd
}

func workflowsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <workflowId>",
		Short: "Get one Workflow with its Tasks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "workflows/"+args[0], nil)
		},
	}
}

func workflowsCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Workflow from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "workflows/", nil, body)
		},
	}
	addFileFlag(cmd, "Workflow intent (kind, intent, targetServerIds, playbookName, extraVars)")
	return cmd
}

func workflowsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <workflowId>",
		Short: "Cancel a Workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("workflows/%s/cancel", args[0]), nil, nil)
		},
	}
}

func workflowsRerunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rerun <workflowId>",
		Short: "Rerun a Workflow that can no longer advance in place",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("workflows/%s/rerun", args[0]), nil, nil)
		},
	}
}

func workflowsTimelineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timeline <workflowId>",
		Short: "Read a Workflow's normalized timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("workflows/%s/timeline", args[0]), nil)
		},
	}
}

// workflowsTaskCmd groups the per-Task diagnostics. logs and stderr are
// text/plain and are streamed to stdout verbatim; events and artifacts are JSON.
func workflowsTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Retry a Task and read its logs, stderr, events, and artifacts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "retry <workflowId> <taskId>",
		Short: "Retry a failed or attention-needing Task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", taskPath(args, "retry"), nil, nil)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "logs <workflowId> <taskId>",
		Short: "Print a Task's full runner log",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printText(cmd, taskPath(args, "logs"))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "stderr <workflowId> <taskId>",
		Short: "Print a Task's error-only report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printText(cmd, taskPath(args, "stderr"))
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "events <workflowId> <taskId>",
		Short: "Read a Task's per-host results",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, taskPath(args, "events"), nil)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "artifacts <workflowId> <taskId>",
		Short: "Read a Task's artifact metadata",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, taskPath(args, "artifacts"), nil)
		},
	})

	return cmd
}

// taskPath builds a Task subresource path from [workflowId, taskId].
func taskPath(args []string, sub string) string {
	return fmt.Sprintf("workflows/%s/tasks/%s/%s", args[0], args[1], sub)
}

// printText fetches a text/plain endpoint and writes it to stdout unchanged, for
// Task logs and stderr where the payload is not JSON.
func printText(cmd *cobra.Command, path string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	body, err := c.Text(ctx(cmd), client.Request{Method: "GET", Path: path, Auth: client.AuthBearer})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, body)
	return err
}
