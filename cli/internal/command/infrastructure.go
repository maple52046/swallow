package command

import "github.com/spf13/cobra"

// newInfrastructureCommand groups the swallow-owned Zone and Pool records
// (infrastructure.md). Zones and Pools share an identical shape and behavior, so
// both subtrees are generated from one builder. Server placement lives under
// `swallow servers placement` because it is addressed by serverId.
func newInfrastructureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infrastructure",
		Short: "Manage Zones and Pools",
	}
	cmd.AddCommand(
		groupingCmd("zones", "Zone"),
		groupingCmd("pools", "Pool"),
	)
	return cmd
}

// groupingCmd builds the list/get/create/update/delete subtree for a Zone or
// Pool. resource is the path segment ("zones"/"pools"); label is the singular
// noun used in help text.
func groupingCmd(resource, label string) *cobra.Command {
	base := "infrastructure/" + resource
	cmd := &cobra.Command{
		Use:   resource,
		Short: "Manage " + label + " records",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List " + label + " records, optionally filtered by Site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			return getJSON(cmd, base, q)
		},
	}
	list.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <id>",
		Short: "Get one " + label,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, base+"/"+args[0], nil)
		},
	})

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a " + label + " from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", base, nil, body)
		},
	}
	addFileFlag(create, label+" definition (siteId, name, description)")
	cmd.AddCommand(create)

	update := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a " + label + " from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", base+"/"+args[0], nil, body)
		},
	}
	addFileFlag(update, "changed "+label+" fields (name, description)")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a " + label,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", base+"/"+args[0], nil, nil)
		},
	})

	return cmd
}
