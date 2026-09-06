// Package application builds the operator overview without owning any of the facts it summarizes.
package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	recentOperationLimit = 8
	firingAlertLimit     = 10
)

var (
	// ErrSiteNotFound means an explicit overview scope does not identify a Site.
	ErrSiteNotFound = errors.New("overview site not found")
	// ErrMonitoringUnavailable marks an optional monitoring-provider failure that the
	// overview can safely expose as partial state instead of failing durable inventory.
	ErrMonitoringUnavailable = errors.New("overview monitoring unavailable")
)

// Clock supplies the snapshot time used for the generated timestamp and rolling failure
// window. Implementations must return UTC-compatible instants and be safe for concurrent use.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production overview clock. It owns no timers or resources.
type SystemClock struct{}

// Now returns the current UTC instant.
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

// Source is the read boundary for the contexts summarized by Overview.
//
// Implementations translate context-owned repositories into these deliberately small
// projections. A missing optional alert provider must wrap ErrMonitoringUnavailable;
// durable repository failures remain ordinary errors and fail the query.
type Source interface {
	ListSites(ctx context.Context) ([]Site, error)
	ListIntegrations(ctx context.Context, siteID string) ([]Integration, error)
	ListServers(ctx context.Context, siteID string) ([]Server, error)
	ListPlatforms(ctx context.Context, siteID string) ([]Platform, error)
	ListOperations(ctx context.Context, siteID string) ([]Operation, error)
	ListFiringAlerts(ctx context.Context, siteID string) ([]Alert, error)
}

// Site carries only the identity needed to validate an optional overview scope.
type Site struct {
	ID string
}

// Integration carries sync freshness without exposing its write-only credential.
type Integration struct {
	ID              string
	SiteID          string
	Name            string
	Kind            string
	ProviderKind    string
	Enabled         bool
	LastSucceededAt *time.Time
	LastError       string
}

// Server carries the independent inventory facts used by platform-level counts.
type Server struct {
	Absent            bool
	ProvisioningState string
	PlatformID        string
	GPUDevices        int
	HealthState       string
}

// Platform carries reachability and membership-correlation gaps.
type Platform struct {
	IntegrationID string
	MemberCount   int
	MatchedCount  int
}

// Operation is the public, secret-free summary used by the overview's recent activity.
type Operation struct {
	ID                 string
	Kind               string
	Intent             string
	SiteID             string
	PlatformID         string
	TargetServerIDs    []string
	RetryOfOperationID string
	RunID              string
	Playbook           string
	Status             string
	StatusReason       string
	StartedAt          *time.Time
	FinishedAt         *time.Time
	RequestedBy        string
	RequestedAt        time.Time
	UpdatedAt          time.Time
}

// Alert is a secret-free firing alert correlated to optional Swallow resource IDs.
type Alert struct {
	Fingerprint string
	Name        string
	Severity    string
	State       string
	Summary     string
	Description string
	Labels      map[string]string
	StartsAt    *time.Time
	ServerID    string
	SiteID      string
	PlatformID  string
}

// InventorySummary separates the independent health axis from lifecycle counts.
type InventorySummary struct {
	Sites      int
	Servers    int
	Absent     int
	Deployed   int
	Platformed int
	GPUDevices int
	Health     HealthSummary
}

// HealthSummary counts observed up/down states and treats every missing answer as unknown.
type HealthSummary struct {
	Up      int
	Down    int
	Unknown int
}

// IntegrationSummary retains all scoped integrations so freshness is inspectable.
type IntegrationSummary struct {
	Total   int
	Failing int
	Items   []Integration
}

// PlatformSummary reports registration reachability and the human-actionable join gap.
type PlatformSummary struct {
	Total            int
	Unreachable      int
	UnmatchedMembers int
}

// OperationSummary reports activity and a bounded newest-first history.
type OperationSummary struct {
	Active            int
	FailedLast24Hours int
	Recent            []Operation
}

// EmbeddedError follows the shared error-code vocabulary inside an otherwise successful
// partial overview response.
type EmbeddedError struct {
	Code    string
	Message string
}

// FiringSummary contains a bounded severity-first alert list and headline counts.
type FiringSummary struct {
	Critical int
	Warning  int
	Items    []Alert
}

// MonitoringSummary makes optional-provider failure explicit without pretending that no
// alerts means the same thing as an unavailable alert source.
type MonitoringSummary struct {
	Available bool
	Error     *EmbeddedError
	Firing    FiringSummary
}

// Result is one coherent, point-in-time overview read model.
type Result struct {
	GeneratedAt  time.Time
	SiteID       string
	Inventory    InventorySummary
	Integrations IntegrationSummary
	Platforms    PlatformSummary
	Operations   OperationSummary
	Monitoring   MonitoringSummary
}

// Service coordinates the overview query across narrow read projections. It never writes
// domain state and never persists its derived counts.
type Service struct {
	source Source
	clock  Clock
}

// NewService creates an overview service with explicit source and clock boundaries.
func NewService(source Source, clock Clock) *Service {
	return &Service{source: source, clock: clock}
}

// Execute builds a site-scoped snapshot. Monitoring-provider failure is returned inside
// Result; every other source failure aborts so partial durable inventory is never mistaken
// for a complete swallow-wide view.
func (s *Service) Execute(ctx context.Context, siteID string) (Result, error) {
	var result Result
	generatedAt := s.clock.Now().UTC()
	sites, err := s.source.ListSites(ctx)
	if err != nil {
		return result, err
	}
	if siteID != "" && !containsSite(sites, siteID) {
		return result, ErrSiteNotFound
	}

	integrations, err := s.source.ListIntegrations(ctx, siteID)
	if err != nil {
		return result, err
	}
	servers, err := s.source.ListServers(ctx, siteID)
	if err != nil {
		return result, err
	}
	platforms, err := s.source.ListPlatforms(ctx, siteID)
	if err != nil {
		return result, err
	}
	operations, err := s.source.ListOperations(ctx, siteID)
	if err != nil {
		return result, err
	}

	result = Result{
		GeneratedAt:  generatedAt,
		SiteID:       siteID,
		Inventory:    summarizeInventory(sites, servers, siteID),
		Integrations: summarizeIntegrations(integrations),
		Platforms:    summarizePlatforms(platforms),
		Operations:   summarizeOperations(operations, generatedAt),
		Monitoring:   MonitoringSummary{Available: true, Firing: FiringSummary{Items: []Alert{}}},
	}

	alerts, err := s.source.ListFiringAlerts(ctx, siteID)
	if errors.Is(err, ErrMonitoringUnavailable) {
		result.Monitoring = MonitoringSummary{
			Available: false,
			Error: &EmbeddedError{
				Code:    "provider_unavailable",
				Message: "Monitoring provider is unavailable.",
			},
			Firing: FiringSummary{Items: []Alert{}},
		}
		return result, nil
	}
	if err != nil {
		return Result{}, err
	}
	result.Monitoring.Firing = summarizeAlerts(alerts)
	return result, nil
}

func containsSite(sites []Site, siteID string) bool {
	for _, site := range sites {
		if site.ID == siteID {
			return true
		}
	}
	return false
}

func summarizeInventory(sites []Site, servers []Server, siteID string) InventorySummary {
	summary := InventorySummary{Sites: len(sites)}
	if siteID != "" {
		summary.Sites = 1
	}
	for _, server := range servers {
		summary.Servers++
		if server.Absent {
			summary.Absent++
		}
		if server.ProvisioningState == "deployed" {
			summary.Deployed++
		}
		if server.PlatformID != "" {
			summary.Platformed++
		}
		summary.GPUDevices += server.GPUDevices
		switch strings.ToLower(server.HealthState) {
		case "up":
			summary.Health.Up++
		case "down":
			summary.Health.Down++
		default:
			summary.Health.Unknown++
		}
	}
	return summary
}

func summarizeIntegrations(integrations []Integration) IntegrationSummary {
	summary := IntegrationSummary{Total: len(integrations), Items: integrations}
	for _, integration := range integrations {
		if integration.LastError != "" {
			summary.Failing++
		}
	}
	return summary
}

func summarizePlatforms(platforms []Platform) PlatformSummary {
	summary := PlatformSummary{Total: len(platforms)}
	for _, platform := range platforms {
		if platform.IntegrationID == "" {
			summary.Unreachable++
		}
		if platform.MemberCount > platform.MatchedCount {
			summary.UnmatchedMembers += platform.MemberCount - platform.MatchedCount
		}
	}
	return summary
}

func summarizeOperations(operations []Operation, generatedAt time.Time) OperationSummary {
	sorted := append([]Operation(nil), operations...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].RequestedAt.After(sorted[j].RequestedAt)
	})
	summary := OperationSummary{}
	cutoff := generatedAt.Add(-24 * time.Hour)
	for _, operation := range sorted {
		switch operation.Status {
		case "pending", "running":
			summary.Active++
		case "failed":
			if !operation.RequestedAt.Before(cutoff) {
				summary.FailedLast24Hours++
			}
		}
	}
	if len(sorted) > recentOperationLimit {
		sorted = sorted[:recentOperationLimit]
	}
	summary.Recent = sorted
	return summary
}

func summarizeAlerts(alerts []Alert) FiringSummary {
	firing := make([]Alert, 0, len(alerts))
	for _, alert := range alerts {
		if alert.State != "" && !strings.EqualFold(alert.State, "firing") {
			continue
		}
		switch strings.ToLower(alert.Severity) {
		case "critical":
			firing = append(firing, alert)
		case "warning":
			firing = append(firing, alert)
		default:
			firing = append(firing, alert)
		}
	}
	sort.SliceStable(firing, func(i, j int) bool {
		left, right := severityRank(firing[i].Severity), severityRank(firing[j].Severity)
		if left != right {
			return left < right
		}
		return timeValue(firing[i].StartsAt).After(timeValue(firing[j].StartsAt))
	})

	summary := FiringSummary{}
	for _, alert := range firing {
		switch strings.ToLower(alert.Severity) {
		case "critical":
			summary.Critical++
		case "warning":
			summary.Warning++
		}
	}
	if len(firing) > firingAlertLimit {
		firing = firing[:firingAlertLimit]
	}
	summary.Items = firing
	return summary
}

func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
