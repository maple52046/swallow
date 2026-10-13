package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// MonitoringLink is an operator-registered link from a Platform to the runtime's own
// monitoring user interface — for example the Grafana that Omnistat brings for a Slurm
// Platform (glossary: Monitoring Link).
//
// It is swallow-owned display data and nothing more: swallow never connects to, authenticates
// against, or health-checks the URL, and a link is not a metrics Integration. swallow's usage
// charts keep reading the Site metrics backend whether or not a Platform has links.
type MonitoringLink struct {
	// Name is the label an operator clicks; unique within the Platform, case-insensitively.
	Name string
	// URL is an absolute http or https address with a host.
	URL string
}

// Limits on a Platform's Monitoring Links (platforms.md). They keep the list a handful of
// shortcuts rather than a bookmark store, and bound what one PATCH can write.
const (
	MaxMonitoringLinks       = 10
	MaxMonitoringLinkNameLen = 64
	MaxMonitoringLinkURLLen  = 2048
)

// ErrInvalidMonitoringLink marks a Monitoring Link list that breaks one of the invariants
// below. The wrapped message names the offending link so an operator can fix it.
var ErrInvalidMonitoringLink = errors.New("invalid monitoring link")

// NormalizeMonitoringLinks validates a replacement list and returns it trimmed.
//
// The list is all-or-nothing: any violation returns ErrInvalidMonitoringLink and no links, so a
// PATCH never stores a partially valid list. A nil or empty input yields an empty, non-nil list,
// which is how a Platform without links is represented.
func NormalizeMonitoringLinks(links []MonitoringLink) ([]MonitoringLink, error) {
	if len(links) > MaxMonitoringLinks {
		return nil, fmt.Errorf("%w: at most %d links, got %d", ErrInvalidMonitoringLink, MaxMonitoringLinks, len(links))
	}
	normalized := make([]MonitoringLink, 0, len(links))
	seen := make(map[string]bool, len(links))
	for i, link := range links {
		name := strings.TrimSpace(link.Name)
		rawURL := strings.TrimSpace(link.URL)
		switch {
		case name == "":
			return nil, fmt.Errorf("%w: link %d has no name", ErrInvalidMonitoringLink, i+1)
		case utf8.RuneCountInString(name) > MaxMonitoringLinkNameLen:
			return nil, fmt.Errorf("%w: name %q is longer than %d characters", ErrInvalidMonitoringLink, name, MaxMonitoringLinkNameLen)
		case seen[strings.ToLower(name)]:
			return nil, fmt.Errorf("%w: name %q is used more than once", ErrInvalidMonitoringLink, name)
		case len(rawURL) > MaxMonitoringLinkURLLen:
			return nil, fmt.Errorf("%w: the URL of %q is longer than %d characters", ErrInvalidMonitoringLink, name, MaxMonitoringLinkURLLen)
		case !isAbsoluteWebURL(rawURL):
			return nil, fmt.Errorf("%w: the URL of %q must be an absolute http or https URL", ErrInvalidMonitoringLink, name)
		}
		seen[strings.ToLower(name)] = true
		normalized = append(normalized, MonitoringLink{Name: name, URL: rawURL})
	}
	return normalized, nil
}

// isAbsoluteWebURL accepts only http(s) URLs with a host. Other schemes (javascript:, file:)
// are refused because the dashboard renders the URL as a clickable link.
func isAbsoluteWebURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
