package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// approveFlag is the CLI spelling of a spec write's approve=true (change-plans.md, "Approval On
// Submit"): the user approves the Change Plan the write proposes with the write itself, as a user
// who confirms a dashboard dialog does. Without it a destructive change waits for `plans approve`.
const approveFlag = "approve"

func addApproveFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(approveFlag, false,
		"approve the Change Plan this change proposes, so it starts at once unless the Change Plan policy requires a separate approval")
}

// specWritePlan is the part of a spec write's response (platforms.md "Spec And Status") that says
// what happens next: the proposed Change Plan, or null for a change converged without one.
type specWritePlan struct {
	Plan *struct {
		ID          string   `json:"id"`
		State       string   `json:"state"`
		StateReason string   `json:"stateReason"`
		Blockers    []string `json:"blockers"`
	} `json:"plan"`
}

// sendSpecWrite sends a Platform spec write — with approve=true when --approve is given — prints
// the response like sendJSON, and then tells the user on stderr what happens to its Change Plan, so
// the printed JSON/YAML stays machine-readable. A 204 (a Platform whose install never started,
// deleted at once) prints the uniform success object and no note.
func sendSpecWrite(cmd *cobra.Command, method, path string, q url.Values, body any) error {
	if on, _ := cmd.Flags().GetBool(approveFlag); on {
		if q == nil {
			q = url.Values{}
		}
		q.Set("approve", "true")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := c.JSON(ctx(cmd), client.Request{Method: method, Path: path, Query: q, Body: body, Auth: client.AuthBearer}, &raw); err != nil {
		return err
	}
	var out any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return fmt.Errorf("decode the spec write response: %w", err)
		}
	}
	if out == nil {
		return printResult(map[string]any{"success": true})
	}
	if err := printResult(out); err != nil {
		return err
	}
	// The note is a convenience; a response without the plan's fields still printed in full above.
	var written specWritePlan
	if err := json.Unmarshal(raw, &written); err == nil && written.Plan != nil {
		writePlanNote(cmd.ErrOrStderr(), written.Plan.ID, written.Plan.State, written.Plan.StateReason, len(written.Plan.Blockers) > 0)
	}
	return nil
}

// writePlanNote is the next step for a spec write's Change Plan: approved with the write (its
// controller carries it out), or waiting, with the API's reason and the command that approves it.
// A blocked plan cannot be approved, so it points to the plan's blockers instead.
func writePlanNote(w io.Writer, planID, state, reason string, blocked bool) {
	switch {
	case state == "approved":
		fmt.Fprintf(w, "Change Plan %s was approved with this change; its controller carries it out now.\n", planID)
	case blocked:
		fmt.Fprintf(w, "Change Plan %s waits: %s. See its blockers with `swallow plans get %s`.\n", planID, reason, planID)
	case state == "pending_approval":
		fmt.Fprintf(w, "Change Plan %s waits for approval: %s. Approve it with `swallow plans approve %s`.\n", planID, reason, planID)
	}
}
