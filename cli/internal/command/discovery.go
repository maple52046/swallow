package command

import (
	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newDiscoveryCommand groups the machine-authenticated discovery endpoints
// (discovery-prometheus.md). These accept a machine token or an admin JWT, so
// the client uses AuthMachine: it presents the configured --machine-token when
// set, otherwise falls back to the interactive session token.
func newDiscoveryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discovery",
		Short: "Read machine-facing service discovery",
	}

	prom := &cobra.Command{
		Use:   "prometheus",
		Short: "Read the Prometheus http_sd target list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			tag, _ := cmd.Flags().GetString("tag")
			state, _ := cmd.Flags().GetString("provisioning-state")
			q := newQuery().
				set("siteId", defaultSite(site)).
				set("tag", tag).
				set("provisioningState", state).
				setInt(cmd, "port", "port").
				build()

			c, err := newClient()
			if err != nil {
				return err
			}
			var out any
			if err := c.JSON(ctx(cmd), client.Request{
				Method: "GET",
				Path:   "discovery/prometheus",
				Query:  q,
				Auth:   client.AuthMachine,
			}, &out); err != nil {
				return err
			}
			return printResult(out)
		},
	}
	prom.Flags().Int("port", 9100, "exporter port to place in each target")
	prom.Flags().String("tag", "", "restrict to Servers carrying this provisioner tag")
	prom.Flags().String("site-id", "", "restrict to one Site (defaults to the global --site)")
	prom.Flags().String("provisioning-state", "", "provisioning state filter (use 'all' to remove it)")
	cmd.AddCommand(prom)

	return cmd
}
