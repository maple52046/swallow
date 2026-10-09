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
// decisions 047, 049, and 055): the Boot ISO the Server boots first. The method follows the
// Server's Power Configuration — redfish (the BMC mounts it) or libvirt (the Hypervisor puts it on
// the virtual machine's CD-ROM). Enabling names a Boot ISO built for the Server's own provisioner
// (`swallow provisioning boot-isos`); the API derives the rest, so no command takes a URL or path.
func serversBootMediaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boot-media",
		Short: "Read, enable, or disable a Server's Boot Media (iPXE ISO boot through its BMC or hypervisor)",
	}

	get := &cobra.Command{
		Use:   "get <serverId>",
		Short: "Read the Boot Media method, setting, Boot ISO, and the Redfish or libvirt capability",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := newQuery().setBool(cmd, "live", "live").build()
			return getJSON(cmd, fmt.Sprintf("servers/%s/boot-media", args[0]), q)
		},
	}
	get.Flags().Bool("live", false, "also read the BMC's or hypervisor's live state (takes seconds)")

	enable := &cobra.Command{
		Use:   "enable <serverId> --iso <isoId>",
		Short: "Enable Boot Media with a Boot ISO: preflight (attach it, direct the boot), then save",
		Long: "Enable runs the preflight against the Server's real BMC, or for a libvirt virtual machine its\n" +
			"hypervisor, and saves the setting only when the Boot ISO is attached and the next boots start\n" +
			"from it. The Boot ISO must be built for the Server's own provisioner. A libvirt virtual machine\n" +
			"needs its virsh Power Configuration to name a deployed swallow Server as the hypervisor; the\n" +
			"ISO is uploaded there, so no Boot Media base URL is needed. Enabling an enabled Server\n" +
			"re-applies it, or switches to another Boot ISO. It can take several minutes; without\n" +
			"--request-timeout the client waits up to 8 minutes. Every inspection and OS deployment\n" +
			"re-applies it.",
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
		Short: "Disable Boot Media; the BMC or hypervisor is asked to eject the ISO (best effort)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "PUT", fmt.Sprintf("servers/%s/boot-media", args[0]), nil, map[string]any{"enabled": false})
		},
	}

	cmd.AddCommand(get, enable, disable)
	return cmd
}

// serversRedfishProbeCmd re-probes a Server's Boot Media method now: its BMC for Redfish, or a
// libvirt virtual machine's hypervisor. An unreachable or unsupported BMC or hypervisor is a
// successful probe; the result says which.
func serversRedfishProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "redfish-probe <serverId>",
		Short: "Re-probe the Server's Boot Media method (BMC Redfish capability, or libvirt hypervisor)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("servers/%s/redfish/probe", args[0]), nil, nil)
		},
	}
}
