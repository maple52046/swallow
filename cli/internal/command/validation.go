package command

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/maple52046/swallow/cli/internal/client"
)

// defaultRunPollInterval is how often `runs start --wait` and `runs watch` read the run: a run
// moves at the pace of its Tasks, so a few seconds keeps the CLI responsive without load.
const defaultRunPollInterval = 5 * time.Second

// newValidationCommand groups the Validation surface (validation.md, decision 081): the suite
// catalog and user-defined suites, Validation Runs, and Validation Schedules. Every route requires
// `admin`.
func newValidationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validation",
		Short: "Run Validation Suites on Servers and Platforms, and read their results",
	}
	cmd.AddCommand(validationSuitesCmd(), validationRunsCmd(), validationSchedulesCmd())
	return cmd
}

// validationSuitesCmd covers the catalog. Module suites are read-only; a user-defined suite is a
// script whose exit code is its one check's verdict, and deleting one retires it.
func validationSuitesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "suites", Short: "List Validation Suites and manage script suites"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List the suites the modules declare and the script suites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "validation/suites", newQuery().setBool(cmd, "includeRetired", "include-retired").build())
		},
	}
	list.Flags().Bool("include-retired", false, "also list retired script suites")

	get := &cobra.Command{
		Use:   "get <suiteId>",
		Short: "Get one suite with its checks and parameter schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "validation/suites/"+args[0], nil)
		},
	}

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a script suite (from flags or a JSON/YAML body)",
		Long: "Creates a user-defined Validation Suite: its script runs on each target Server, and its exit code is the verdict " +
			"(0 passed, any other code failed, past the timeout an error). The script is stored and returned as written: " +
			"do not put secrets in it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := userSuiteBody(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "validation/suites", nil, body)
		},
	}
	addUserSuiteFlags(create)

	update := &cobra.Command{
		Use:   "update <suiteId>",
		Short: "Replace a script suite; a new script, account, timeout, or impact makes a new revision",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := userSuiteBody(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", "validation/suites/"+args[0], nil, body)
		},
	}
	addUserSuiteFlags(update)

	retire := &cobra.Command{
		Use:   "retire <suiteId>",
		Short: "Retire a script suite; its past runs stay readable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", "validation/suites/"+args[0], nil, nil)
		},
	}

	cmd.AddCommand(list, get, create, update, retire)
	return cmd
}

// addUserSuiteFlags registers the user suite fields. They are the contract's whole request body,
// so simple flags express it; --file passes a body through instead.
func addUserSuiteFlags(cmd *cobra.Command) {
	addFileFlag(cmd, "suite body (name, description, script, runAs, timeoutSeconds, impact) instead of the flags")
	cmd.Flags().String("name", "", "suite name")
	cmd.Flags().String("description", "", "what the suite checks")
	cmd.Flags().String("script-file", "", "path to the script, or - for stdin")
	cmd.Flags().String("run-as", "", "account the script runs as: automation-user (default) or root")
	cmd.Flags().Int("timeout", 0, "seconds one run of the script may take on a Server (default 600, at most 3600)")
	cmd.Flags().String("impact", "", "observe, workload (default), or disruptive")
}

// userSuiteBody returns the --file body when one is given, else the body the flags describe. An
// unset optional flag is left out, so the API applies its default.
func userSuiteBody(cmd *cobra.Command) (any, error) {
	if body, ok, err := readBodyFile(cmd); ok || err != nil {
		return body, err
	}
	name, _ := cmd.Flags().GetString("name")
	scriptPath, _ := cmd.Flags().GetString("script-file")
	if strings.TrimSpace(name) == "" || scriptPath == "" {
		return nil, errors.New("pass --name and --script-file, or a body with --file")
	}
	script, err := readFileOrStdin(scriptPath)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"name": name, "script": string(script)}
	for flag, key := range map[string]string{"description": "description", "run-as": "runAs", "impact": "impact"} {
		if value, _ := cmd.Flags().GetString(flag); value != "" {
			body[key] = value
		}
	}
	if cmd.Flags().Changed("timeout") {
		timeout, _ := cmd.Flags().GetInt("timeout")
		body["timeoutSeconds"] = timeout
	}
	return body, nil
}

// validationRunsCmd covers Validation Runs: list, read, start, cancel, and follow one to its
// verdict.
func validationRunsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "runs", Short: "Start, follow, and read Validation Runs"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List runs, newest first, without their results",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := newQuery()
			applyPagination(cmd, q)
			for flag, key := range map[string]string{
				"suite": "suiteId", "server": "serverId", "platform": "platformId", "schedule": "scheduleId",
				"status": "status", "verdict": "verdict", "trigger": "trigger",
			} {
				value, _ := cmd.Flags().GetString(flag)
				q.set(key, value)
			}
			q.setBool(cmd, "active", "active")
			return getJSON(cmd, "validation/runs", q.build())
		},
	}
	addPagination(list)
	list.Flags().String("suite", "", "filter by suite id")
	list.Flags().String("server", "", "filter by a Server among the targets or members")
	list.Flags().String("platform", "", "filter by Platform id")
	list.Flags().String("schedule", "", "filter by the schedule that started the run")
	list.Flags().String("status", "", "filter by status: pending, running, completed, or canceled")
	list.Flags().String("verdict", "", "filter by verdict: passed, failed, or error")
	list.Flags().String("trigger", "", "filter by what started the run: manual, schedule, or deployment")
	list.Flags().Bool("active", false, "only runs that have not ended")

	get := &cobra.Command{
		Use:   "get <runId>",
		Short: "Get one run with every check's verdict on every target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "validation/runs/"+args[0], nil)
		},
	}

	start := &cobra.Command{
		Use:   "start",
		Short: "Start a run of a suite on Servers or a Platform",
		Long: "Starts a report run. A servers suite takes --server (repeat or comma-separate), a platform suite takes " +
			"--platform. --param key=value sets a run parameter; the value is read as YAML, so 64 is a number and " +
			"[a,b] a list. A disruptive suite needs --confirm. With --wait the command follows the run until it ends " +
			"and exits non-zero unless its verdict is passed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := runStartBody(cmd)
			if err != nil {
				return err
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			var run map[string]any
			if err := c.JSON(ctx(cmd), client.Request{Method: "POST", Path: "validation/runs", Body: body, Auth: client.AuthBearer}, &run); err != nil {
				return err
			}
			if wait, _ := cmd.Flags().GetBool("wait"); !wait {
				return printResult(run)
			}
			id, _ := run["id"].(string)
			return followRun(cmd, c, id)
		},
	}
	addFileFlag(start, "run body (suiteId, serverIds or platformId, parameters, confirm) instead of the flags")
	start.Flags().String("suite", "", "suite id")
	start.Flags().StringSlice("server", nil, "target Server id of a servers suite (repeat or comma-separate)")
	start.Flags().String("platform", "", "target Platform id of a platform suite")
	start.Flags().StringArray("param", nil, "run parameter as key=value (repeat)")
	start.Flags().Bool("confirm", false, "acknowledge a disruptive suite's impact")
	addFollowFlags(start, true)

	watch := &cobra.Command{
		Use:   "watch <runId>",
		Short: "Follow a run until it ends; exits non-zero unless its verdict is passed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			return followRun(cmd, c, args[0])
		},
	}
	addFollowFlags(watch, false)

	cancel := &cobra.Command{
		Use:   "cancel <runId>",
		Short: "Cancel a report run; the results recorded so far are kept",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("validation/runs/%s/cancel", args[0]), nil, nil)
		},
	}

	cmd.AddCommand(list, get, start, watch, cancel)
	return cmd
}

// addFollowFlags registers --interval, and --wait on start, which only follows the run on request.
func addFollowFlags(cmd *cobra.Command, withWait bool) {
	if withWait {
		cmd.Flags().Bool("wait", false, "follow the run until it ends; exit non-zero unless its verdict is passed")
	}
	cmd.Flags().Duration("interval", defaultRunPollInterval, "how often to read the run while following it")
}

// runStartBody returns the --file body when one is given, else the body the flags describe.
func runStartBody(cmd *cobra.Command) (any, error) {
	if body, ok, err := readBodyFile(cmd); ok || err != nil {
		return body, err
	}
	suite, _ := cmd.Flags().GetString("suite")
	if strings.TrimSpace(suite) == "" {
		return nil, errors.New("pass --suite, or a body with --file")
	}
	body := map[string]any{"suiteId": suite}
	if servers, _ := cmd.Flags().GetStringSlice("server"); len(servers) > 0 {
		body["serverIds"] = servers
	}
	if platform, _ := cmd.Flags().GetString("platform"); platform != "" {
		body["platformId"] = platform
	}
	params, _ := cmd.Flags().GetStringArray("param")
	if len(params) > 0 {
		parameters, err := parseRunParameters(params)
		if err != nil {
			return nil, err
		}
		body["parameters"] = parameters
	}
	if confirm, _ := cmd.Flags().GetBool("confirm"); confirm {
		body["confirm"] = true
	}
	return body, nil
}

// parseRunParameters turns key=value pairs into the run's parameters, reading each value as YAML
// so a number, a boolean, or a [list] reaches the API typed; the suite's schema is checked there.
func parseRunParameters(pairs []string) (map[string]any, error) {
	parameters := map[string]any{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--param %q: want key=value", pair)
		}
		var typed any
		if err := yaml.Unmarshal([]byte(value), &typed); err != nil {
			return nil, fmt.Errorf("--param %s: %w", key, err)
		}
		if typed == nil {
			typed = value
		}
		parameters[strings.TrimSpace(key)] = typed
	}
	return parameters, nil
}

// followRun reads run id every --interval until it is no longer pending or running, writing a line
// to stderr whenever its status or counts change, then prints the run (with its results) to
// stdout. It returns an error, and so a non-zero exit, unless the run completed with verdict
// passed; an interrupt (the command context) stops it.
func followRun(cmd *cobra.Command, c *client.Client, id string) error {
	interval, _ := cmd.Flags().GetDuration("interval")
	if interval <= 0 {
		interval = defaultRunPollInterval
	}
	last := ""
	for {
		var run map[string]any
		if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "validation/runs/" + id, Auth: client.AuthBearer}, &run); err != nil {
			return err
		}
		status, _ := run["status"].(string)
		progress := fmt.Sprintf("%s: %s", status, runCounts(run))
		if progress != last {
			fmt.Fprintf(cmd.ErrOrStderr(), "run %s %s\n", id, progress)
			last = progress
		}
		if status != "pending" && status != "running" {
			if err := printResult(run); err != nil {
				return err
			}
			verdict, _ := run["verdict"].(string)
			if status == "completed" && verdict == "passed" {
				return nil
			}
			if status == "canceled" {
				return fmt.Errorf("validation run %s was canceled", id)
			}
			return fmt.Errorf("validation run %s ended %s", id, verdict)
		}
		select {
		case <-ctx(cmd).Done():
			return ctx(cmd).Err()
		case <-time.After(interval):
		}
	}
}

// runCounts renders a run's verdict counts, naming only those that occurred.
func runCounts(run map[string]any) string {
	counts, _ := run["counts"].(map[string]any)
	var parts []string
	for _, verdict := range []string{"passed", "failed", "error", "skipped"} {
		if n, _ := counts[verdict].(float64); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", int(n), verdict))
		}
	}
	if len(parts) == 0 {
		return "no results yet"
	}
	return strings.Join(parts, ", ")
}

// validationSchedulesCmd covers Validation Schedules. A schedule's spec (suite, targets,
// parameters, trigger) is passed as a JSON/YAML body so it tracks the contract.
func validationSchedulesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "schedules", Short: "Manage the schedules that start Validation Runs"}

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a schedule from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "validation/schedules", nil, body)
		},
	}
	addFileFlag(create, "schedule body (name, suiteId, serverIds or platformId, parameters, trigger, enabled)")

	update := &cobra.Command{
		Use:   "update <scheduleId>",
		Short: "Replace a schedule's spec; with generation in the body it applies only at that generation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PUT", "validation/schedules/"+args[0], nil, body)
		},
	}
	addFileFlag(update, "schedule body, optionally with the generation it replaces")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List schedules with when they run and their latest run",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return getJSON(cmd, "validation/schedules", nil)
			},
		},
		&cobra.Command{
			Use:   "get <scheduleId>",
			Short: "Get one schedule's spec and status",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return getJSON(cmd, "validation/schedules/"+args[0], nil)
			},
		},
		create,
		update,
		&cobra.Command{
			Use:   "delete <scheduleId>",
			Short: "Delete a schedule; its runs stay",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return sendNoContent(cmd, "DELETE", "validation/schedules/"+args[0], nil, nil)
			},
		},
	)
	return cmd
}
