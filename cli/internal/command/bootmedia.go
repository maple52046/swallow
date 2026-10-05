package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// bootMediaPreflightTimeout is the client-side deadline for enabling Boot Media when the operator
// did not set --request-timeout. The preflight drives a real BMC (mounting an HTTP ISO and letting
// it settle takes several minutes; the API allows six), so the global 60-second default would
// abandon a request the server is still completing.
const bootMediaPreflightTimeout = 8 * time.Minute

// serversBootMediaCmd groups one Server's Boot Media (server-detail-actions.md "Boot Media",
// decisions 047 and 049): the Boot ISO the BMC mounts and boots first. Enabling names a Boot ISO
// built for the Server's own provisioner (`swallow provisioning boot-isos`); the API derives its
// URL, so no command takes one.
func serversBootMediaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boot-media",
		Short: "Read, enable, or disable a Server's Boot Media (Redfish iPXE ISO boot)",
	}

	get := &cobra.Command{
		Use:   "get <serverId>",
		Short: "Read the Boot Media setting, its Boot ISO, and the Redfish capability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := newQuery().setBool(cmd, "live", "live").build()
			return getJSON(cmd, fmt.Sprintf("servers/%s/boot-media", args[0]), q)
		},
	}
	get.Flags().Bool("live", false, "also read the BMC's live state (takes seconds)")

	enable := &cobra.Command{
		Use:   "enable <serverId> --iso <isoId>",
		Short: "Enable Boot Media with a Boot ISO: preflight on the BMC (mount it, direct the boot), then save",
		Long: "Enable runs the preflight against the Server's real BMC and saves the setting only when the BMC\n" +
			"mounted the Boot ISO and directs the next boots at it. The Boot ISO must be built for the\n" +
			"Server's own provisioner. Enabling an enabled Server re-applies it, or switches to another\n" +
			"Boot ISO after ejecting the previous one. It can take several minutes; without\n" +
			"--request-timeout the client waits up to 8 minutes. Every OS deployment re-applies it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			iso, _ := cmd.Flags().GetString("iso")
			body := map[string]any{"enabled": true, "isoId": strings.TrimSpace(iso)}
			return sendJSONWithin(cmd, "PUT", fmt.Sprintf("servers/%s/boot-media", args[0]), body, bootMediaPreflightTimeout)
		},
	}
	// No backticks in the usage text: pflag would render the quoted words as the value's name.
	enable.Flags().String("iso", "", "Boot ISO id, from: swallow provisioning boot-isos list")
	_ = enable.MarkFlagRequired("iso")

	disable := &cobra.Command{
		Use:   "disable <serverId>",
		Short: "Disable Boot Media; the BMC is asked to eject the ISO (best effort)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "PUT", fmt.Sprintf("servers/%s/boot-media", args[0]), nil, map[string]any{"enabled": false})
		},
	}

	cmd.AddCommand(get, enable, disable)
	return cmd
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
