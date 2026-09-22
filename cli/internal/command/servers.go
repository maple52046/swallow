package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newServersCommand groups the Servers surface: the inventory list and stream,
// one Server's detail and diagnostics, provider-backed lifecycle actions,
// structured network configuration, and placement (servers-list.md,
// servers-stream.md, server-detail-actions.md, infrastructure.md).
//
// There is intentionally no `servers create`: Servers are produced by
// reconciling provisioner inventory, not registered by a caller. The deprecated
// single-server deploy/release routes are also omitted; use the durable
// provisioning operations instead (`swallow provisioning deploy|release`).
func newServersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "servers",
		Short: "Inspect and act on Servers",
	}
	cmd.AddCommand(
		serversListCmd(),
		serversGetCmd(),
		serversWatchCmd(),
		serversRefreshCmd(),
		serversDeleteCmd(),
		serversProvisionerDetailCmd(),
		serversEventsCmd(),
		serversPowerStateCmd(),
		serversProvisioningTasksCmd(),
		serversNetworkCmd(),
		serversPlacementCmd(),
	)
	cmd.AddCommand(serversActionCmds()...)
	return cmd
}

func serversListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Server projections with filtering and pagination",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := newQuery()
			applyPagination(cmd, q)
			site, _ := cmd.Flags().GetString("site-id")
			integration, _ := cmd.Flags().GetString("integration")
			state, _ := cmd.Flags().GetString("provisioning-state")
			platform, _ := cmd.Flags().GetString("platform")
			keyword, _ := cmd.Flags().GetString("keyword")
			q.set("siteId", defaultSite(site)).
				set("integrationId", integration).
				set("provisioningState", state).
				set("platformId", platform).
				set("keyword", keyword).
				setBool(cmd, "includeAbsent", "include-absent")
			return getJSON(cmd, "servers/", q.build())
		},
	}
	addPagination(cmd)
	cmd.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	cmd.Flags().String("integration", "", "filter by integration id")
	cmd.Flags().String("provisioning-state", "", "filter by provisioning axis state")
	cmd.Flags().String("platform", "", "filter by platform membership id")
	cmd.Flags().String("keyword", "", "case-insensitive match on hostname, FQDN, address, serial, or UUID")
	cmd.Flags().Bool("include-absent", false, "include Servers absent from the latest inventory")
	return cmd
}

func serversGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <serverId>",
		Short: "Get one Server projection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "servers/"+args[0], nil)
		},
	}
}

// serversWatchCmd streams live Server changes over SSE. Each frame is printed as
// a single JSON line regardless of --output, because a continuous stream does
// not fit the table/object rendering used for one-shot reads. The stream ends on
// Ctrl-C (context cancellation) or when the server closes the connection.
func serversWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live Server projection changes (SSE)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			enc := json.NewEncoder(os.Stdout)
			return c.Stream(ctx(cmd), "servers/stream", q, func(evt client.StreamEvent) error {
				var payload any
				if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
					// Emit the raw frame so an unexpected shape is still visible.
					fmt.Fprintln(os.Stdout, evt.Data)
					return nil
				}
				return enc.Encode(payload)
			})
		},
	}
	cmd.Flags().String("site-id", "", "scope the stream to one Site (defaults to the global --site)")
	return cmd
}

func serversRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <serverId>",
		Short: "Trigger a targeted live provisioner refresh",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("servers/%s/refresh", args[0]), nil, nil)
		},
	}
}

func serversDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <serverId>",
		Short: "Delete a Server and its backing provisioner Machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", "servers/"+args[0], nil, nil)
		},
	}
}

func serversProvisionerDetailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provisioner-detail <serverId>",
		Short: "Read live provider detail (including allowlisted BMC fields)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("servers/%s/provisioner-detail", args[0]), nil)
		},
	}
}

func serversEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events <serverId>",
		Short: "Read recent provider machine events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := newQuery().setInt(cmd, "limit", "limit").build()
			return getJSON(cmd, fmt.Sprintf("servers/%s/events", args[0]), q)
		},
	}
	cmd.Flags().Int("limit", 50, "number of events (1-100)")
	return cmd
}

func serversPowerStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "power-state <serverId>",
		Short: "Read the current power state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("servers/%s/power-state", args[0]), nil)
		},
	}
}

func serversProvisioningTasksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provisioning-tasks <serverId>",
		Short: "List provisioning tasks for a Server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("servers/%s/provisioning-tasks", args[0]), nil)
		},
	}
}

// serversActionCmds builds the provider-backed lifecycle actions that take no
// request body. Each posts to servers/{id}/{verb} and prints the accepted
// provisioning snapshot. They are generated from a table so every verb behaves
// and documents itself identically.
func serversActionCmds() []*cobra.Command {
	type action struct {
		verb  string
		short string
	}
	actions := []action{
		{"power-on", "Power the Server on"},
		{"power-off", "Power the Server off"},
		{"commission", "Commission the Server"},
		{"test", "Run hardware testing"},
		{"abort", "Abort the current provider operation"},
		{"override-failed-testing", "Override a failed testing result"},
		{"lock", "Lock the Server (deployed state only)"},
		{"unlock", "Unlock the Server"},
		{"mark-broken", "Mark the Server broken"},
		{"mark-fixed", "Mark a broken Server fixed"},
		{"rescue-mode", "Enter rescue mode"},
		{"exit-rescue-mode", "Exit rescue mode"},
	}
	cmds := make([]*cobra.Command, 0, len(actions))
	for _, a := range actions {
		verb := a.verb
		cmds = append(cmds, &cobra.Command{
			Use:   verb + " <serverId>",
			Short: a.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendJSON(cmd, "POST", fmt.Sprintf("servers/%s/%s", args[0], verb), nil, nil)
			},
		})
	}
	return cmds
}

// serversNetworkCmd groups the structured network configuration reads and link
// mutations (server-detail-actions.md).
func serversNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Read and mutate a Server's network configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get <serverId>",
		Short: "Read the network configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("servers/%s/network", args[0]), nil)
		},
	})

	addLink := &cobra.Command{
		Use:   "add-link <serverId> <interfaceId>",
		Short: "Add a subnet link on an interface",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", fmt.Sprintf("servers/%s/network/interfaces/%s/links", args[0], args[1]), nil, body)
		},
	}
	addFileFlag(addLink, "link definition (mode, subnetId, optional ipAddress/defaultGateway)")
	cmd.AddCommand(addLink)

	updateLink := &cobra.Command{
		Use:   "update-link <serverId> <interfaceId> <linkId>",
		Short: "Replace a subnet link",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", fmt.Sprintf("servers/%s/network/interfaces/%s/links/%s", args[0], args[1], args[2]), nil, body)
		},
	}
	addFileFlag(updateLink, "replacement link definition")
	cmd.AddCommand(updateLink)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete-link <serverId> <interfaceId> <linkId>",
		Short: "Delete a subnet link",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", fmt.Sprintf("servers/%s/network/interfaces/%s/links/%s", args[0], args[1], args[2]), nil, nil)
		},
	})

	return cmd
}

// serversPlacementCmd assigns or clears a Server's Zone and Pool (infrastructure.md).
// A body file carries the exact contract shape; the --zone/--pool and
// --clear-zone/--clear-pool flags are a convenience for the common case.
func serversPlacementCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "placement <serverId>",
		Short: "Assign or clear a Server's Zone and Pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ok, err := readBodyFile(cmd)
			if err != nil {
				return err
			}
			if !ok {
				body, err = placementBodyFromFlags(cmd)
				if err != nil {
					return err
				}
			}
			return sendJSON(cmd, "PUT", fmt.Sprintf("servers/%s/placement", args[0]), nil, body)
		},
	}
	addFileFlag(cmd, "placement body ({zoneId, poolId}); omit to use the flags below")
	cmd.Flags().String("zone", "", "assign this Zone id")
	cmd.Flags().String("pool", "", "assign this Pool id")
	cmd.Flags().Bool("clear-zone", false, "clear the Zone assignment")
	cmd.Flags().Bool("clear-pool", false, "clear the Pool assignment")
	return cmd
}

// placementBodyFromFlags builds the placement request from flags. A present id
// assigns the grouping; a --clear-* flag sends an explicit null to clear it; an
// untouched field is omitted so it is left unchanged, matching the contract.
func placementBodyFromFlags(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	if zone, _ := cmd.Flags().GetString("zone"); zone != "" {
		body["zoneId"] = zone
	}
	if clear, _ := cmd.Flags().GetBool("clear-zone"); clear {
		body["zoneId"] = nil
	}
	if pool, _ := cmd.Flags().GetString("pool"); pool != "" {
		body["poolId"] = pool
	}
	if clear, _ := cmd.Flags().GetBool("clear-pool"); clear {
		body["poolId"] = nil
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("nothing to change: pass --file, or one of --zone/--pool/--clear-zone/--clear-pool")
	}
	return body, nil
}
