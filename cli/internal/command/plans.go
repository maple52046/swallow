package command

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

// newPlansCommand groups the Change Plan operations (change-plans.md, decision 076). A destructive
// change is a plan, approved with its write (a command's --approve) or later as a whole here; the
// resource's controller then recomputes and applies it as a Workflow. `policy` reads and sets the
// installation's safety switch over approving with the write.
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
			Short: "Approve a Change Plan that waits, as a whole; its controller then applies it",
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
		plansPolicyCmd(),
	)
	return cmd
}

// plansPolicyCmd reads and replaces the installation's Change Plan policy (change-plans.md,
// "Change Plan Policy"). Turning approve-on-submit off is the safety switch: every Change Plan then
// waits for `plans approve`, even one sent with --approve.
func plansPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Show the Change Plan policy: whether a change sent with --approve starts at once",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "change-plan-policy", nil)
		},
	}
	set := &cobra.Command{
		Use:   "set",
		Short: "Set the Change Plan policy (--approve-on-submit=false makes every plan wait for approval)",
		Long: "Set the Change Plan policy. --approve-on-submit=true (the default policy) lets a change\n" +
			"confirmed with --approve, or in the dashboard, start at once; --approve-on-submit=false is\n" +
			"the safety switch: every Change Plan waits for `swallow plans approve`. The new policy\n" +
			"applies to the changes sent afterwards; plans already proposed keep their state.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("approve-on-submit") {
				return fmt.Errorf("--approve-on-submit=true or --approve-on-submit=false is required")
			}
			approveOnSubmit, _ := cmd.Flags().GetBool("approve-on-submit")
			return sendJSON(cmd, "PUT", "change-plan-policy", nil, map[string]any{"approveOnSubmit": approveOnSubmit})
		},
	}
	set.Flags().Bool("approve-on-submit", true, "approve a change's Change Plan with the change when it is sent with --approve")
	cmd.AddCommand(set)
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
