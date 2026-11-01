package command

import (
	"net/url"

	"github.com/spf13/cobra"
)

// newPlansCommand groups the Change Plan operations (change-plans.md, decision 076). A
// destructive change waits as a plan until an admin approves it as a whole; the resource's
// controller then recomputes and applies it as a Workflow.
func newPlansCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plans",
		Aliases: []string{"change-plans"},
		Short:   "Review, approve, or reject Change Plans of destructive changes",
	}
	cmd.AddCommand(
		plansListCmd(),
		&cobra.Command{
			Use:   "get <planId>",
			Short: "Show a Change Plan: its actions, expected effects, and blockers",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return getJSON(cmd, "change-plans/"+url.PathEscape(args[0]), nil)
			},
		},
		&cobra.Command{
			Use:   "approve <planId>",
			Short: "Approve a pending Change Plan as a whole; its controller then applies it",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendJSON(cmd, "POST", "change-plans/"+url.PathEscape(args[0])+"/approve", nil, nil)
			},
		},
		&cobra.Command{
			Use:   "reject <planId>",
			Short: "Reject a Change Plan that has not been applied",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendJSON(cmd, "POST", "change-plans/"+url.PathEscape(args[0])+"/reject", nil, nil)
			},
		},
	)
	return cmd
}

// plansListCmd lists plans, optionally of one resource and in some states.
func plansListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Change Plans, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if kind, _ := cmd.Flags().GetString("resource-kind"); kind != "" {
				q.Set("resourceKind", kind)
			}
			if id, _ := cmd.Flags().GetString("resource-id"); id != "" {
				q.Set("resourceId", id)
			}
			states, _ := cmd.Flags().GetStringSlice("state")
			for _, state := range states {
				q.Add("state", state)
			}
			return getJSON(cmd, "change-plans", q)
		},
	}
	cmd.Flags().String("resource-kind", "", "only plans of this resource kind, for example platform")
	cmd.Flags().String("resource-id", "", "only plans of this resource")
	cmd.Flags().StringSlice("state", nil,
		"only plans in these states: pending_approval, approved, applied, rejected, superseded, expired")
	return cmd
}
