package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// cancelActiveWorkflowsFlag is the CLI spelling of the api-server's cancelActiveWorkflows (decision
// 061): the action cancels the unfinished Workflows holding its Servers and starts once they end.
const cancelActiveWorkflowsFlag = "cancel-active-workflows"

func addCancelActiveWorkflowsFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(cancelActiveWorkflowsFlag, false,
		"cancel the unfinished Workflows holding the target Servers first; the action starts once they end")
}

// withCancelActiveWorkflows sets cancelActiveWorkflows on the request body when the flag is given,
// whether the body came from --file, from other flags, or is otherwise empty.
func withCancelActiveWorkflows(cmd *cobra.Command, body any) (any, error) {
	if on, _ := cmd.Flags().GetBool(cancelActiveWorkflowsFlag); !on {
		return body, nil
	}
	if body == nil {
		return map[string]any{"cancelActiveWorkflows": true}, nil
	}
	object, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("--%s needs the request body to be an object", cancelActiveWorkflowsFlag)
	}
	object["cancelActiveWorkflows"] = true
	return object, nil
}

// holdingWorkflowsConflict reports whether err is the api-server refusing an action because a
// Workflow still holds its Servers (or, for an uninstall, because the Platform is still deploying),
// the case --cancel-active-workflows resolves.
func holdingWorkflowsConflict(err error) (*client.APIError, bool) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 409 {
		return nil, false
	}
	return apiErr, strings.Contains(apiErr.Message, "active durable work") || strings.Contains(apiErr.Message, "cancelActiveWorkflows")
}

// hintCancelActiveWorkflows adds the next step to such a conflict when the flag was not given;
// held, when not empty, names the Workflows found holding the Servers.
func hintCancelActiveWorkflows(cmd *cobra.Command, err error, held string) error {
	apiErr, ok := holdingWorkflowsConflict(err)
	if on, _ := cmd.Flags().GetBool(cancelActiveWorkflowsFlag); !ok || on {
		return err
	}
	hint := fmt.Sprintf("rerun with --%s to cancel the Workflows holding the Servers first", cancelActiveWorkflowsFlag)
	if held != "" {
		hint = "held by " + held + "; " + hint
	}
	apiErr.Hint = hint
	return apiErr
}

// platformHoldingWorkflows lists the Platform's unfinished Workflows as "ID (intent, status)", for
// the uninstall conflict hint; a failed read yields nothing rather than hiding the conflict.
func platformHoldingWorkflows(cmd *cobra.Command, platformID string) string {
	c, err := newClient()
	if err != nil {
		return ""
	}
	var page struct {
		Items []struct {
			ID     string `json:"id"`
			Intent string `json:"intent"`
			Status string `json:"status"`
		} `json:"items"`
	}
	q := newQuery().set("platformId", platformID).set("active", "true").build()
	if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "workflows/", Query: q, Auth: client.AuthBearer}, &page); err != nil {
		return ""
	}
	held := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		held = append(held, fmt.Sprintf("%s (%s, %s)", item.ID, item.Intent, item.Status))
	}
	return strings.Join(held, "; ")
}
