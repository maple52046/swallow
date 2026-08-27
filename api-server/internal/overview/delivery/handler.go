// Package delivery exposes the provider-owned operator overview over HTTP.
package delivery

import (
	"errors"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"

	overviewapp "github.com/maple52046/swallow/internal/overview/application"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// Handler maps the overview application result onto the published HTTP contract.
type Handler struct {
	service *overviewapp.Service
}

// NewHandler creates the thin HTTP adapter for the overview query.
func NewHandler(service *overviewapp.Service) *Handler {
	return &Handler{service: service}
}

// Get validates the optional Site scope through the use case and preserves the common
// error envelope for all fatal failures.
func (h *Handler) Get(c *fiber.Ctx) error {
	result, err := h.service.Execute(c.Context(), c.Query("siteId"))
	if errors.Is(err, overviewapp.ErrSiteNotFound) {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))
	}
	if err != nil {
		log.Printf("overview: %v", err)
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	return c.JSON(toResponse(result))
}

type response struct {
	GeneratedAt  string               `json:"generatedAt"`
	Scope        scopeResponse        `json:"scope"`
	Inventory    inventoryResponse    `json:"inventory"`
	Integrations integrationsResponse `json:"integrations"`
	Clusters     clustersResponse     `json:"clusters"`
	Operations   operationsResponse   `json:"operations"`
	Monitoring   monitoringResponse   `json:"monitoring"`
}

type scopeResponse struct {
	SiteID *string `json:"siteId"`
}

type inventoryResponse struct {
	Sites      int            `json:"sites"`
	Servers    int            `json:"servers"`
	Absent     int            `json:"absent"`
	Deployed   int            `json:"deployed"`
	Clustered  int            `json:"clustered"`
	GPUDevices int            `json:"gpuDevices"`
	Health     healthResponse `json:"health"`
}

type healthResponse struct {
	Up      int `json:"up"`
	Down    int `json:"down"`
	Unknown int `json:"unknown"`
}

type integrationsResponse struct {
	Total   int                   `json:"total"`
	Failing int                   `json:"failing"`
	Items   []integrationResponse `json:"items"`
}

type integrationResponse struct {
	ID              string  `json:"id"`
	SiteID          string  `json:"siteId"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	ProviderKind    string  `json:"providerKind"`
	Enabled         bool    `json:"enabled"`
	LastSucceededAt *string `json:"lastSucceededAt"`
	LastError       *string `json:"lastError"`
}

type clustersResponse struct {
	Total            int `json:"total"`
	Unreachable      int `json:"unreachable"`
	UnmatchedMembers int `json:"unmatchedMembers"`
}

type operationsResponse struct {
	Active            int                 `json:"active"`
	FailedLast24Hours int                 `json:"failedLast24Hours"`
	Recent            []operationResponse `json:"recent"`
}

type operationResponse struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"`
	Intent          string            `json:"intent"`
	SiteID          string            `json:"siteId"`
	ClusterID       *string           `json:"clusterId"`
	TargetServerIDs []string          `json:"targetServerIds"`
	RetryOfID       *string           `json:"retryOfOperationId"`
	Execution       executionResponse `json:"execution"`
	RequestedBy     string            `json:"requestedBy"`
	RequestedAt     string            `json:"requestedAt"`
	UpdatedAt       string            `json:"updatedAt"`
}

type executionResponse struct {
	RunID        string  `json:"runId"`
	Playbook     string  `json:"playbook"`
	Status       string  `json:"status"`
	StatusReason *string `json:"statusReason"`
	StartedAt    *string `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
}

type monitoringResponse struct {
	Available bool           `json:"available"`
	Error     *embeddedError `json:"error"`
	Firing    firingResponse `json:"firing"`
}

type embeddedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type firingResponse struct {
	Critical int             `json:"critical"`
	Warning  int             `json:"warning"`
	Items    []alertResponse `json:"items"`
}

type alertResponse struct {
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name"`
	Severity    string            `json:"severity"`
	State       string            `json:"state"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	StartsAt    *string           `json:"startsAt"`
	ServerID    *string           `json:"serverId"`
	SiteID      *string           `json:"siteId"`
	ClusterID   *string           `json:"clusterId"`
}

func toResponse(result overviewapp.Result) response {
	integrations := make([]integrationResponse, len(result.Integrations.Items))
	for i, item := range result.Integrations.Items {
		integrations[i] = integrationResponse{
			ID: item.ID, SiteID: item.SiteID, Name: item.Name, Kind: item.Kind,
			ProviderKind: item.ProviderKind, Enabled: item.Enabled,
			LastSucceededAt: timeString(item.LastSucceededAt), LastError: optionalString(item.LastError),
		}
	}
	operations := make([]operationResponse, len(result.Operations.Recent))
	for i, item := range result.Operations.Recent {
		operations[i] = operationResponse{
			ID: item.ID, Kind: item.Kind, Intent: item.Intent, SiteID: item.SiteID,
			ClusterID:       optionalString(item.ClusterID),
			TargetServerIDs: append([]string(nil), item.TargetServerIDs...),
			RetryOfID:       optionalString(item.RetryOfOperationID),
			Execution: executionResponse{
				RunID: item.RunID, Playbook: item.Playbook, Status: item.Status,
				StatusReason: optionalString(item.StatusReason),
				StartedAt:    timeString(item.StartedAt), FinishedAt: timeString(item.FinishedAt),
			},
			RequestedBy: item.RequestedBy, RequestedAt: item.RequestedAt.UTC().Format(time.RFC3339),
			UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	alerts := make([]alertResponse, len(result.Monitoring.Firing.Items))
	for i, item := range result.Monitoring.Firing.Items {
		alerts[i] = alertResponse{
			Fingerprint: item.Fingerprint, Name: item.Name, Severity: item.Severity,
			State: item.State, Summary: item.Summary, Description: item.Description,
			Labels: item.Labels, StartsAt: timeString(item.StartsAt),
			ServerID: optionalString(item.ServerID), SiteID: optionalString(item.SiteID),
			ClusterID: optionalString(item.ClusterID),
		}
	}
	var monitoringError *embeddedError
	if result.Monitoring.Error != nil {
		monitoringError = &embeddedError{
			Code:    result.Monitoring.Error.Code,
			Message: result.Monitoring.Error.Message,
		}
	}
	return response{
		GeneratedAt: result.GeneratedAt.UTC().Format(time.RFC3339),
		Scope:       scopeResponse{SiteID: optionalString(result.SiteID)},
		Inventory: inventoryResponse{
			Sites: result.Inventory.Sites, Servers: result.Inventory.Servers,
			Absent: result.Inventory.Absent, Deployed: result.Inventory.Deployed,
			Clustered: result.Inventory.Clustered, GPUDevices: result.Inventory.GPUDevices,
			Health: healthResponse{
				Up: result.Inventory.Health.Up, Down: result.Inventory.Health.Down,
				Unknown: result.Inventory.Health.Unknown,
			},
		},
		Integrations: integrationsResponse{
			Total: result.Integrations.Total, Failing: result.Integrations.Failing,
			Items: integrations,
		},
		Clusters: clustersResponse{
			Total: result.Clusters.Total, Unreachable: result.Clusters.Unreachable,
			UnmatchedMembers: result.Clusters.UnmatchedMembers,
		},
		Operations: operationsResponse{
			Active:            result.Operations.Active,
			FailedLast24Hours: result.Operations.FailedLast24Hours,
			Recent:            operations,
		},
		Monitoring: monitoringResponse{
			Available: result.Monitoring.Available, Error: monitoringError,
			Firing: firingResponse{
				Critical: result.Monitoring.Firing.Critical,
				Warning:  result.Monitoring.Firing.Warning, Items: alerts,
			},
		},
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func timeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
