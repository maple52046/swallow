package command

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// bootMediaPreflightTimeout is the client-side deadline for enabling Boot Media when the operator
// did not set --request-timeout. The preflight drives a real BMC (mounting an HTTP ISO and letting
// it settle takes several minutes; the API allows six), so the global 60-second default would
// abandon a request the server is still completing.
const bootMediaPreflightTimeout = 8 * time.Minute

// serversBootMediaCmd groups one Server's Boot Media (server-detail-actions.md "Boot Media",
// decision 047): the swallow-served iPXE ISO the BMC mounts and boots first. The ISO URL is fixed
// by the installation, so no command takes one.
func serversBootMediaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boot-media",
		Short: "Read, enable, or disable a Server's Boot Media (Redfish iPXE ISO boot)",
	}

	get := &cobra.Command{
		Use:   "get <serverId>",
		Short: "Read the Boot Media setting, Redfish capability, and installation ISO",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := newQuery().setBool(cmd, "live", "live").build()
			return getJSON(cmd, fmt.Sprintf("servers/%s/boot-media", args[0]), q)
		},
	}
	get.Flags().Bool("live", false, "also read the BMC's live state (takes seconds)")

	enable := &cobra.Command{
		Use:   "enable <serverId>",
		Short: "Enable Boot Media: preflight on the BMC (mount the ISO, direct the boot), then save",
		Long: "Enable runs the preflight against the Server's real BMC and saves the setting only when the BMC\n" +
			"mounted the ISO and directs the next boots at it. It can take several minutes; without\n" +
			"--request-timeout the client waits up to 8 minutes. Every OS deployment re-applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setBootMedia(cmd, args[0], true)
		},
	}

	disable := &cobra.Command{
		Use:   "disable <serverId>",
		Short: "Disable Boot Media; the BMC is asked to eject the ISO (best effort)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setBootMedia(cmd, args[0], false)
		},
	}

	cmd.AddCommand(get, enable, disable)
	return cmd
}

// setBootMedia sends PUT /servers/{id}/boot-media. For an enable whose operator left
// --request-timeout at its default, the client deadline is raised to the preflight's.
func setBootMedia(cmd *cobra.Command, serverID string, enabled bool) error {
	timeout := gf.timeout
	if enabled && !cmd.Flags().Changed("request-timeout") && timeout > 0 && timeout < bootMediaPreflightTimeout {
		timeout = bootMediaPreflightTimeout
	}
	c, err := client.New(clientOptions(timeout))
	if err != nil {
		return err
	}
	var out any
	request := client.Request{
		Method: "PUT", Path: fmt.Sprintf("servers/%s/boot-media", serverID),
		Body: map[string]any{"enabled": enabled}, Auth: client.AuthBearer,
	}
	if err := c.JSON(ctx(cmd), request, &out); err != nil {
		return err
	}
	return printResult(out)
}

// serversRedfishProbeCmd re-probes a Server's BMC for Redfish capability now. An unreachable or
// unsupported BMC is a successful probe; the result says which.
func serversRedfishProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "redfish-probe <serverId>",
		Short: "Re-probe the Server's BMC for Redfish Boot Media capability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("servers/%s/redfish/probe", args[0]), nil, nil)
		},
	}
}
