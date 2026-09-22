// Package command builds the Cobra command tree for the `swallow` operator CLI.
// Each file registers one resource group (auth, servers, provisioning, and so
// on) that maps onto the api-server Active HTTP contract.
//
// Component boundary: these commands are a conformist consumer of the
// api-server published contract. They translate operator input into contract
// requests through the client package and render responses through the output
// package. They do not import api-server internals and do not define API
// behavior; where a request body is complex, a command accepts a JSON/YAML file
// so payload fields track the contract rather than a hand-copied flag set.
package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/maple52046/swallow/cli/internal/client"
	"github.com/maple52046/swallow/cli/internal/config"
	"github.com/maple52046/swallow/cli/internal/output"
)

// globalFlags holds the values bound to the root command's persistent flags.
// They are resolved against the stored profile and environment in
// PersistentPreRunE; a set flag always overrides the profile for that
// invocation without rewriting the file.
type globalFlags struct {
	configPath   string
	endpoint     string
	token        string
	site         string
	machineToken string
	outputFormat string
	insecure     bool
	timeout      time.Duration
}

// runtime is the resolved per-invocation state shared by command handlers: the
// effective profile, the chosen output format, and the profile path (so `login`
// and `logout` can persist changes). It is populated once in PersistentPreRunE.
type runtime struct {
	cfg    *config.Config
	format output.Format
	path   string
}

// gf and rt are process-global because Cobra handlers are functions without a
// shared receiver. The CLI runs one command per process, so a single instance
// of each is the simplest correct design.
var (
	gf globalFlags
	rt runtime
)

// NewRootCommand assembles the full command tree. main calls Execute on the
// returned command. Global flags are persistent so every subcommand inherits
// the connection profile and output selection.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "swallow",
		Short: "Operator CLI for the Swallow GPU datacenter platform",
		Long: "swallow is the operator command-line client for the Swallow platform. " +
			"It talks to the api-server HTTP API and covers the full active surface: " +
			"auth, sites, integrations, servers, provisioning, infrastructure, platforms, " +
			"workflows, monitoring, and discovery.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// PersistentPreRunE resolves the profile before any handler runs. It is
		// skipped for pure help so `swallow --help` works without a profile.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return resolveRuntime()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&gf.configPath, "config", "", "path to the config file (default: $SWALLOW_CONFIG or <user-config-dir>/swallow/config.yaml)")
	pf.StringVar(&gf.endpoint, "endpoint", "", "api-server base URL, e.g. https://swallow.example (overrides the stored profile)")
	pf.StringVar(&gf.token, "token", "", "access token to use for this invocation (overrides the stored profile)")
	pf.StringVar(&gf.site, "site", "", "default Site scope applied to commands that accept siteId")
	pf.StringVar(&gf.machineToken, "machine-token", "", "machine bearer token for discovery endpoints")
	pf.StringVarP(&gf.outputFormat, "output", "o", "table", "output format: table, json, or yaml")
	pf.BoolVar(&gf.insecure, "insecure", false, "skip TLS certificate verification (lab endpoints only)")
	pf.DurationVar(&gf.timeout, "request-timeout", 60*time.Second, "per-request timeout; 0 disables the client-side deadline")

	root.AddCommand(
		newLoginCommand(),
		newLogoutCommand(),
		newAuthCommand(),
		newOverviewCommand(),
		newSitesCommand(),
		newIntegrationsCommand(),
		newServersCommand(),
		newProvisioningCommand(),
		newInfrastructureCommand(),
		newPlatformsCommand(),
		newWorkflowsCommand(),
		newMonitoringCommand(),
		newDiscoveryCommand(),
	)
	return root
}

// resolveRuntime loads the profile file, overlays SWALLOW_* env and then the
// global flags, and parses the output format. It records everything on rt for
// the handlers. A missing profile file is tolerated so first-run `login` works.
func resolveRuntime() error {
	path := gf.configPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	// Flag overlay: only a changed flag overrides the resolved profile value, so
	// an unset flag never blanks a stored field.
	if gf.endpoint != "" {
		cfg.Endpoint = gf.endpoint
	}
	if gf.token != "" {
		cfg.Token = gf.token
	}
	if gf.site != "" {
		cfg.Site = gf.site
	}
	if gf.machineToken != "" {
		cfg.MachineToken = gf.machineToken
	}
	if gf.insecure {
		cfg.InsecureSkipTLS = true
	}

	format, err := output.ParseFormat(gf.outputFormat)
	if err != nil {
		return err
	}

	rt = runtime{cfg: cfg, format: format, path: path}
	return nil
}

// newClient builds an api-server client from the resolved profile. It is the
// single construction point so credential and TLS handling stay consistent
// across commands.
func newClient() (*client.Client, error) {
	return client.New(client.Options{
		Endpoint:     rt.cfg.Endpoint,
		Token:        rt.cfg.Token,
		MachineToken: rt.cfg.MachineToken,
		InsecureTLS:  rt.cfg.InsecureSkipTLS,
		Timeout:      gf.timeout,
	})
}

// printResult renders a decoded payload using the resolved output format to
// stdout. It is the standard way handlers emit results.
func printResult(v any) error {
	return output.New(os.Stdout, rt.format).Print(v)
}

// ctx returns the command context, which carries cancellation from signals.
func ctx(cmd *cobra.Command) context.Context {
	return cmd.Context()
}

// addFileFlag registers the shared -f/--file flag used by commands that submit a
// request body. The value is a path to a JSON or YAML document, or "-" for stdin.
func addFileFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().StringP("file", "f", "", usage)
}

// readBodyFile reads the -f/--file document into a generic value suitable for
// JSON submission. It returns (nil, false, nil) when the flag was not set so a
// caller can decide whether the body is required. YAML is a JSON superset, so a
// JSON file parses too.
func readBodyFile(cmd *cobra.Command) (any, bool, error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return nil, false, nil
	}
	data, err := readFileOrStdin(path)
	if err != nil {
		return nil, false, err
	}
	var body any
	if err := yaml.Unmarshal(data, &body); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", displayPath(path), err)
	}
	return body, true, nil
}

// requireBodyFile is readBodyFile for endpoints whose body is mandatory; it
// returns a clear error naming the flag when nothing was provided.
func requireBodyFile(cmd *cobra.Command) (any, error) {
	body, ok, err := readBodyFile(cmd)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("this command requires a request body: pass --file <path> (or --file - for stdin)")
	}
	return body, nil
}

// readFileOrStdin reads the named file, or all of stdin when path is "-".
func readFileOrStdin(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func displayPath(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}

// defaultSite returns the effective siteId for a command flag: the explicit
// per-command value when set, otherwise the profile's default Site. It lets the
// global --site or a stored default flow into siteId query parameters without
// each command re-reading the profile.
func defaultSite(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	return rt.cfg.Site
}
