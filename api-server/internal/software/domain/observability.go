package domain

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Prometheus and Grafana Managed Software (decision 063). Each installs either as a pinned
// container (Docker CE or Podman) or natively from the vendor's official releases, listening on the
// Server's management address. Their ports are owned here and passed to the playbooks as trusted
// variables so a composing platform (Omnistat) and the playbooks cannot disagree.
const (
	// PrometheusPort is where Prometheus listens on the Server's address.
	PrometheusPort = 9090
	// GrafanaPort is where Grafana listens on the Server's address.
	GrafanaPort = 3000
	// DefaultPrometheusVersion and DefaultGrafanaVersion are the release versions an install pins
	// when the spec names none; the recorded spec always carries the version that was applied. The
	// same version selects the container image tag and the native release.
	DefaultPrometheusVersion = "3.15.0"
	DefaultGrafanaVersion    = "12.4.12"
	// DefaultPrometheusScrapeJob labels scraped series when the spec names no job.
	DefaultPrometheusScrapeJob = "swallow"
	// MaxPrometheusScrapeTargets bounds one install's static targets; a larger fleet belongs in a
	// discovery-based setup, not a per-Server software spec.
	MaxPrometheusScrapeTargets = 512
)

// Spec keys of the Prometheus and Grafana kinds. They are published language of the software
// contract (software.md), so renaming one is a breaking change.
const (
	// SpecInstallMethod selects the InstallMethod; omitted means the catalog's first (container).
	SpecInstallMethod = "installMethod"
	// SpecContainerRuntime is the runtime the container method runs on. A standalone install
	// resolves it from the Server's installed runtime and records it; a composing platform sets it.
	SpecContainerRuntime = "containerRuntime"
	// SpecScrapeJob is the Prometheus job name of the static targets.
	SpecScrapeJob = "scrapeJob"
	// SpecScrapeTargets lists host:port endpoints Prometheus scrapes.
	SpecScrapeTargets = "scrapeTargets"
	// SpecScrapeServerIDs lists Swallow Servers Prometheus scrapes on SpecScrapePort. Their
	// addresses are resolved at every run from the Server's current primary address, so a
	// redeploy that changes an address converges on the next run instead of scraping a stale IP.
	SpecScrapeServerIDs = "scrapeServerIds"
	// SpecScrapePort is the port of every SpecScrapeServerIDs Server.
	SpecScrapePort = "scrapePort"
	// SpecPrometheusURL is the Prometheus Grafana's datasource reads; empty means the Prometheus
	// on the same Server.
	SpecPrometheusURL = "prometheusUrl"
	// SpecDashboards names the Swallow-shipped dashboard bundles Grafana provisions.
	SpecDashboards = "dashboards"
)

// GrafanaDashboardOmnistat is the bundle of Omnistat's RMS dashboards (global, node, job) pinned
// to the Omnistat release Swallow installs.
const GrafanaDashboardOmnistat = "omnistat"

// grafanaDashboardBundles lists the dashboard bundles a release ships.
var grafanaDashboardBundles = []string{GrafanaDashboardOmnistat}

// releaseVersionPattern is a plain major.minor.patch release. It keeps a version usable both as a
// container image tag and in an official download URL or package pin, and keeps it from smuggling
// a registry, path, or digest.
var releaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// jobNamePattern is Prometheus's label-value-safe job name subset Swallow accepts.
var jobNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

// NormalizeReleaseVersion trims a version and drops one leading "v", so "v3.15.0" and "3.15.0"
// name the same release.
func NormalizeReleaseVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// ValidateReleaseVersion checks a normalized Prometheus or Grafana release version.
func ValidateReleaseVersion(version string) error {
	if !releaseVersionPattern.MatchString(version) {
		return fmt.Errorf("%w: spec.%s %q must be a release version such as 3.15.0", ErrSpecInvalid, SpecVersion, version)
	}
	return nil
}

// ValidateScrapeJob checks a Prometheus job name.
func ValidateScrapeJob(job string) error {
	if !jobNamePattern.MatchString(job) {
		return fmt.Errorf("%w: spec.%s %q must start with a letter or underscore and use letters, digits, '_' or '-' (at most 64)", ErrSpecInvalid, SpecScrapeJob, job)
	}
	return nil
}

// ValidateScrapeTarget checks one host:port scrape target. The host may be a name or an IP
// address (IPv6 in brackets); the port must be 1-65535. Schemes and paths are refused because
// Prometheus static targets are bare endpoints.
func ValidateScrapeTarget(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || strings.ContainsAny(host, "/ \t") {
		return fmt.Errorf("%w: spec.%s entry %q must be host:port", ErrSpecInvalid, SpecScrapeTargets, target)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%w: spec.%s entry %q has an invalid port", ErrSpecInvalid, SpecScrapeTargets, target)
	}
	return nil
}

// ValidatePrometheusURL checks an absolute http or https URL for Grafana's datasource.
func ValidatePrometheusURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: spec.%s %q must be an absolute http or https URL", ErrSpecInvalid, SpecPrometheusURL, raw)
	}
	return nil
}

// ValidateDashboardBundle checks that a release ships the named Grafana dashboard bundle.
func ValidateDashboardBundle(name string) error {
	for _, bundle := range grafanaDashboardBundles {
		if bundle == name {
			return nil
		}
	}
	return fmt.Errorf("%w: spec.%s %q is not a shipped dashboard bundle (%s)", ErrSpecInvalid, SpecDashboards, name, strings.Join(grafanaDashboardBundles, ", "))
}
