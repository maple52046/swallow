package command

import "github.com/spf13/cobra"

// newInfrastructureCommand groups the swallow-owned Zone and Pool records
// (infrastructure.md). Both have the same list/get/create/update/delete routes, so
// both subtrees are generated from one builder; a Zone belongs to one Site while a
// Pool belongs to the whole installation, and a Pool also has members and sudo
// defaults (pool-access.md). Server placement lives under `swallow servers
// placement` because it is addressed by serverId.
func newInfrastructureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infrastructure",
		Short: "Manage Zones and Pools",
	}
	pools := groupingCmd("pools", "Pool", false)
	pools.AddCommand(poolMembersCmd(), poolAccessPolicyCmd())
	cmd.AddCommand(
		groupingCmd("zones", "Zone", true),
		pools,
	)
	return cmd
}

// groupingCmd builds the list/get/create/update/delete subtree for a Zone or
// Pool. resource is the path segment ("zones"/"pools"); label is the singular
// noun used in help text. siteScoped adds the list's Site filter and names siteId
// in the create body, which only a Zone has: the Pool list takes no query
// parameters (infrastructure.md).
func groupingCmd(resource, label string, siteScoped bool) *cobra.Command {
	base := "infrastructure/" + resource
	cmd := &cobra.Command{
		Use:   resource,
		Short: "Manage " + label + " records",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List every " + label,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !siteScoped {
				return getJSON(cmd, base, nil)
			}
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			return getJSON(cmd, base, q)
		},
	}
	if siteScoped {
		list.Short = "List " + label + " records, optionally filtered by Site"
		list.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	}
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
	if siteScoped {
		addFileFlag(create, label+" definition (siteId, name, description)")
	} else {
		addFileFlag(create, label+" definition (name, description)")
	}
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
