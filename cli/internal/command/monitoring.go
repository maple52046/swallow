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
		Short: "Read current server metrics, usage history, and metric names",
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

	cmd.AddCommand(monitoringMetricsRangeCmd())

	cmd.AddCommand(&cobra.Command{
		Use:   "names",
		Short: "List the metric names a client may request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "monitoring/metrics/names", nil)
		},
	})

	return cmd
}

// monitoringMetricsRangeCmd reads usage history (server-metrics.md, "Usage History"). Exactly
// one scope flag is required; the CLI checks only that, and leaves window, span, mode, and
// scope-size rules to the server so the contract stays the single source of truth.
func monitoringMetricsRangeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "range",
		Short: "Read usage history for Servers, a Pool, a Zone, or a Platform",
		Long: "Read bounded time series of the named metrics. Choose exactly one scope: " +
			"--server (repeatable, max 200), --pool, --zone, or --platform. The server picks the " +
			"step (about 180 points). Aggregate mode (default) returns one series per metric for " +
			"the whole scope; --mode perServer returns one series per Server (max 50 Servers).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			servers, _ := cmd.Flags().GetStringSlice("server")
			pool, _ := cmd.Flags().GetString("pool")
			zone, _ := cmd.Flags().GetString("zone")
			platform, _ := cmd.Flags().GetString("platform")
			scopes := 0
			for _, present := range []bool{len(servers) > 0, pool != "", zone != "", platform != ""} {
				if present {
					scopes++
				}
			}
			if scopes != 1 {
				return fmt.Errorf("exactly one of --server, --pool, --zone, or --platform is required")
			}

			metrics, _ := cmd.Flags().GetStringSlice("metric")
			window, _ := cmd.Flags().GetString("window")
			start, _ := cmd.Flags().GetString("start")
			end, _ := cmd.Flags().GetString("end")
			mode, _ := cmd.Flags().GetString("mode")
			q := newQuery().
				set("serverIds", strings.Join(servers, ",")).
				set("poolId", pool).
				set("zoneId", zone).
				set("platformId", platform).
				set("metrics", strings.Join(metrics, ",")).
				set("window", window).
				set("start", start).
				set("end", end).
				set("mode", mode)
			return getJSON(cmd, "monitoring/metrics/range", q.build())
		},
	}
	cmd.Flags().StringSlice("server", nil, "Server id in the scope (repeatable, max 200)")
	cmd.Flags().String("pool", "", "chart the Servers currently in this Pool id")
	cmd.Flags().String("zone", "", "chart the Servers currently in this Zone id")
	cmd.Flags().String("platform", "", "chart the Servers that are members of this Platform id")
	cmd.Flags().StringSlice("metric", nil, "metric name to evaluate (repeatable; defaults to all)")
	cmd.Flags().String("window", "", "1h, 6h, 24h, or 7d ending now (server default 1h)")
	cmd.Flags().String("start", "", "explicit range start, ISO 8601 (use with --end instead of --window)")
	cmd.Flags().String("end", "", "explicit range end, ISO 8601")
	cmd.Flags().String("mode", "", "aggregate (default) or perServer")
	return cmd
}
