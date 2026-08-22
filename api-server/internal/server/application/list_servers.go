package application

import (
	"context"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

type ListServersInput struct {
	SiteID            string
	IntegrationID     string
	ProvisioningState string
	ClusterID         string
	Keyword           string
	IncludeAbsent     bool
	Page              pagination.Page
}

// HealthResolver fills in the health axis, which is owned by the metrics store and
// never persisted.
//
// It is an interface here so that the server context does not depend on the monitoring
// context, and so that a deployment with no metrics integration simply has no resolver
// and returns servers with a null health axis.
type HealthResolver interface {
	// ResolveHealth returns health by server ID. Servers it has no answer for are
	// omitted rather than reported as down.
	ResolveHealth(ctx context.Context, serverIDs []string) (map[string]*serverdomain.HealthStatus, error)
}

type ListServersUseCase struct {
	servers serverdomain.ServerRepository
	health  HealthResolver
}

func NewListServersUseCase(servers serverdomain.ServerRepository, health HealthResolver) *ListServersUseCase {
	return &ListServersUseCase{servers: servers, health: health}
}

func (uc *ListServersUseCase) Execute(ctx context.Context, input ListServersInput) (pagination.Result[ServerItem], error) {
	var empty pagination.Result[ServerItem]

	result, err := uc.servers.List(ctx, serverdomain.ListFilter{
		SiteID:            input.SiteID,
		IntegrationID:     input.IntegrationID,
		ProvisioningState: input.ProvisioningState,
		ClusterID:         input.ClusterID,
		Keyword:           input.Keyword,
		IncludeAbsent:     input.IncludeAbsent,
		Offset:            input.Page.Offset(),
		Limit:             input.Page.PageSize,
	})
	if err != nil {
		return empty, err
	}

	uc.attachHealth(ctx, result.Servers)

	items := make([]ServerItem, 0, len(result.Servers))
	for _, s := range result.Servers {
		items = append(items, ToServerItem(s))
	}

	return pagination.NewResult(items, result.Total, input.Page), nil
}

// attachHealth is best-effort on purpose. An unreachable metrics store leaves the
// health axis null, which reads as "not known" — it must never make a listing fail or
// make servers look unhealthy.
func (uc *ListServersUseCase) attachHealth(ctx context.Context, servers []*serverdomain.Server) {
	if uc.health == nil || len(servers) == 0 {
		return
	}

	ids := make([]string, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.ID)
	}

	health, err := uc.health.ResolveHealth(ctx, ids)
	if err != nil {
		return
	}
	for _, s := range servers {
		if h, ok := health[s.ID]; ok {
			s.Health = h
		}
	}
}
