package command

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// bootISOBuildTimeout is the client-side deadline for building a Boot ISO when the operator did
// not set --request-timeout. The API packages the ISO synchronously and bounds that at two
// minutes; a build normally takes seconds, but the global 60-second default must not abandon one
// the server is still finishing.
const bootISOBuildTimeout = 3 * time.Minute

// provisioningBootISOsCmd groups Boot ISOs (boot-isos.md, decision 049): iPXE boot ISOs swallow
// builds per provisioner Integration from its fixed template, which take a lease from the site's
// own DHCP and chain to that provisioner's MAAS rack. A Server's Boot Media mounts one of its own
// provisioner's ISOs (`swallow servers boot-media enable --iso`). There is no script input: the
// script is always the template filled with the rack address.
func provisioningBootISOsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boot-isos",
		Short: "Build, list, and delete iPXE Boot ISOs for Boot Media",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List Boot ISOs and whether this installation can build them",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			integration, _ := cmd.Flags().GetString("integration")
			q := newQuery().set("siteId", defaultSite(site)).set("integrationId", integration).build()
			return getJSON(cmd, "provisioning/boot-isos", q)
		},
	}
	list.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	list.Flags().String("integration", "", "filter by provisioner integration id")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <isoId>",
		Short: "Get one Boot ISO, including its rendered iPXE script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "provisioning/boot-isos/"+args[0], nil)
		},
	})

	create := &cobra.Command{
		Use:   "create",
		Short: "Build a Boot ISO that chains to a provisioner's MAAS rack",
		Long: "Create renders swallow's iPXE template for the MAAS rack (DHCP from the site network, then\n" +
			"chain http://<rack>:<port or 5248>/ipxe.cfg) and packages a BIOS and UEFI ISO. The rack is an\n" +
			"IPv4 address or hostname, optionally with :port; swallow neither resolves nor contacts it.\n" +
			"Servers booting the ISO must have Secure Boot off (iPXE is unsigned).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			integration, _ := cmd.Flags().GetString("integration")
			rack, _ := cmd.Flags().GetString("rack")
			body := map[string]any{
				"name":          strings.TrimSpace(name),
				"integrationId": strings.TrimSpace(integration),
				"rackAddress":   strings.TrimSpace(rack),
			}
			return sendJSONWithin(cmd, "POST", "provisioning/boot-isos", body, bootISOBuildTimeout)
		},
	}
	create.Flags().String("name", "", "a name for the ISO, unique per provisioner")
	create.Flags().String("integration", "", "provisioner integration id the ISO chains to")
	create.Flags().String("rack", "", "MAAS rack address: IPv4 or hostname, optionally :port (default port 5248)")
	_ = create.MarkFlagRequired("name")
	_ = create.MarkFlagRequired("integration")
	_ = create.MarkFlagRequired("rack")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <isoId>",
		Short: "Delete a Boot ISO no Server's enabled Boot Media uses",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", "provisioning/boot-isos/"+args[0], nil, nil)
		},
	})
	return cmd
}

// sendJSONWithin sends a JSON request whose server-side work can outlast the global default
// timeout, and prints the response. When the operator left --request-timeout at its default, the
// client deadline is raised to atLeast; an explicit --request-timeout is always honoured.
func sendJSONWithin(cmd *cobra.Command, method, path string, body any, atLeast time.Duration) error {
	timeout := gf.timeout
	if !cmd.Flags().Changed("request-timeout") && timeout > 0 && timeout < atLeast {
		timeout = atLeast
	}
	c, err := client.New(clientOptions(timeout))
	if err != nil {
		return err
	}
	var out any
	request := client.Request{Method: method, Path: path, Body: body, Auth: client.AuthBearer}
	if err := c.JSON(ctx(cmd), request, &out); err != nil {
		return err
	}
	return printResult(out)
}
