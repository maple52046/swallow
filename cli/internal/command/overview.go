package command

import "github.com/spf13/cobra"

// newOverviewCommand implements `swallow overview` (overview.md): a site-scoped
// operational summary. With no --site it returns the swallow-wide view.
func newOverviewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overview",
		Short: "Show the site-scoped operational summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			return getJSON(cmd, "overview", q)
		},
	}
	cmd.Flags().String("site-id", "", "scope the overview to one Site (defaults to the global --site)")
	return cmd
}
