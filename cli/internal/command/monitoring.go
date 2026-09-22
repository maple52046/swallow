package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newMonitoringCommand groups the alerts and metrics reads (monitoring-alerts.md,
// server-metrics.md). All routes are admin-only.
func newMonitoringCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitoring",
		Short: "Read alerts and server metrics",
	}
	cmd.AddCommand(monitoringAlertsCmd(), monitoringMetricsCmd())
	return cmd
}

func monitoringAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "List alerts and acknowledge them",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List firing/resolved alerts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			server, _ := cmd.Flags().GetString("server")
			severity, _ := cmd.Flags().GetString("severity")
			state, _ := cmd.Flags().GetString("state")
			q := newQuery().
				set("siteId", defaultSite(site)).
				set("serverId", server).
				set("severity", severity).
				set("state", state).
				build()
			return getJSON(cmd, "monitoring/alerts", q)
		},
	}
	list.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	list.Flags().String("server", "", "filter by correlated Server id")
	list.Flags().String("severity", "", "filter by severity")
	list.Flags().String("state", "", "filter by state")
	cmd.AddCommand(list)

	ack := &cobra.Command{
		Use:   "acknowledge <fingerprint>",
		Short: "Create a silence for an alert from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			site, _ := cmd.Flags().GetString("site-id")
			q := newQuery().set("siteId", defaultSite(site)).build()
			return sendJSON(cmd, "POST", fmt.Sprintf("monitoring/alerts/%s/acknowledge", args[0]), q, body)
		},
	}
	addFileFlag(ack, "acknowledge body (matchers, optional duration, comment)")
	ack.Flags().String("site-id", "", "scope the silence to one Site (defaults to the global --site)")
	cmd.AddCommand(ack)

	return cmd
}

func monitoringMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Read current server metrics and list metric names",
	}

	get := &cobra.Command{
		Use:   "get",
		Short: "Read current metric values for one or more Servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			servers, _ := cmd.Flags().GetStringSlice("server")
			if len(servers) == 0 {
				return fmt.Errorf("at least one --server is required")
			}
			metrics, _ := cmd.Flags().GetStringSlice("metric")
			q := newQuery().set("serverIds", strings.Join(servers, ","))
			if len(metrics) > 0 {
				q.set("metrics", strings.Join(metrics, ","))
			}
			return getJSON(cmd, "monitoring/metrics", q.build())
		},
	}
	get.Flags().StringSlice("server", nil, "Server id to query (repeatable, max 200)")
	get.Flags().StringSlice("metric", nil, "metric name to evaluate (repeatable; defaults to all)")
	cmd.AddCommand(get)

	cmd.AddCommand(&cobra.Command{
		Use:   "names",
		Short: "List the metric names a client may request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "monitoring/metrics/names", nil)
		},
	})

	return cmd
}
