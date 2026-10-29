package command

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// claimNextStep names, for a claiming Platform's id, what the operator runs instead of the
// refused command.
type claimNextStep func(platformID string) string

// releaseThroughUninstall is the next step for a refused release or recover: the Platform's
// uninstall with releaseServers releases its Servers.
func releaseThroughUninstall(platformID string) string {
	return fmt.Sprintf("release a Platform's Servers through it: swallow platforms uninstall %s -f <body> "+
		"with \"releaseServers\": true", platformID)
}

// deleteAfterPlatform is the next step for a refused Server delete: either removal of the Platform
// releases the claim, after which the Server can be deleted.
func deleteAfterPlatform(platformID string) string {
	return fmt.Sprintf("uninstall it first (swallow platforms uninstall %s, with \"releaseServers\": true to also "+
		"release its Servers) or delete its record (swallow platforms delete %s), then delete the Server", platformID, platformID)
}

// hintPlatformClaim adds the next step to a release, recover, or Server delete the api-server
// refused because a target Server is claimed by a Platform (its platformClaim, decision 067).
// --cancel-active-workflows does not get past that refusal, so the hint names the Platform and
// what to run instead.
//
// It reads the targets' platformClaim (and the claiming Platform's name) only for a 409 that is
// not a holding-Workflow conflict. A failed read, or no claimed target, returns err unchanged.
func hintPlatformClaim(cmd *cobra.Command, err error, serverIDs []string, next claimNextStep) error {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Hint != "" {
		return err
	}
	if _, busy := holdingWorkflowsConflict(err); busy {
		return err
	}
	c, clientErr := newClient()
	if clientErr != nil {
		return err
	}
	for _, serverID := range serverIDs {
		var server struct {
			PlatformClaim *struct {
				PlatformID string `json:"platformId"`
			} `json:"platformClaim"`
		}
		read := client.Request{Method: "GET", Path: "servers/" + url.PathEscape(serverID), Auth: client.AuthBearer}
		if c.JSON(ctx(cmd), read, &server) != nil || server.PlatformClaim == nil || server.PlatformClaim.PlatformID == "" {
			continue
		}
		platformID := server.PlatformClaim.PlatformID
		owner := platformID
		var platform struct {
			Name string `json:"name"`
		}
		read = client.Request{Method: "GET", Path: "platforms/" + url.PathEscape(platformID), Auth: client.AuthBearer}
		if c.JSON(ctx(cmd), read, &platform) == nil && platform.Name != "" {
			owner = fmt.Sprintf("%s (%s)", platform.Name, platformID)
		}
		apiErr.Hint = fmt.Sprintf("%s belongs to Platform %s; %s", serverID, owner, next(platformID))
		return apiErr
	}
	return err
}

// bodyServerIDs returns the serverIds of a release or recover body, whether it was built from
// --server flags or read from --file; any other shape yields none.
func bodyServerIDs(body any) []string {
	object, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	switch ids := object["serverIds"].(type) {
	case []string:
		return ids
	case []any:
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if s, ok := id.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
