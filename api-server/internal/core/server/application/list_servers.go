package application

import (
	"context"
	"fmt"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

// ListServersInput narrows a Server listing. PoolIDs with RestrictPools limits it to the Servers
// placed in those Pools: the Servers a User that is not an admin may read (decision 082).
type ListServersInput struct {
	PoolIDs           []string
	RestrictPools     bool
	SiteID            string
	IntegrationID     string
	ProvisioningState string
	PlatformID        string
	Keyword           string
	IncludeAbsent     bool
	Page              pagination.Page
}

// HealthResolver classifies the health axis, which is never persisted: swallow's monitoring
// answer for each Server (servers-list.md, Health).
//
// It is an interface here so that the server context depends on neither the monitoring context
// nor the contexts that own the monitoring policy's inputs (Software Assignments, Site automation);
// the composition root supplies the implementation.
type HealthResolver interface {
	// ResolveHealth returns the classified health of every Server in servers, keyed by Server ID,
	// so a caller sets each one from the map. A metrics store that is missing or cannot be read is
	// not an error: the Servers it would have answered for are unknown. An error means the
	// records the classification needs could not be read; the caller fails its read rather than
	// show a health nobody worked out.
	ResolveHealth(ctx context.Context, servers []*serverdomain.Server) (map[string]serverdomain.HealthStatus, error)
}

// ListServersUseCase lists the Server projection with its health classified (servers-list.md).
// Filtering and pagination are the repository's; health is resolved for the page only.
type ListServersUseCase struct {
	servers serverdomain.ServerRepository
	health  HealthResolver
}

// NewListServersUseCase builds the list over the repository. health is required: every listed
// Server carries a classified health axis.
func NewListServersUseCase(servers serverdomain.ServerRepository, health HealthResolver) *ListServersUseCase {
	return &ListServersUseCase{servers: servers, health: health}
}

// Execute returns one page of Servers. A repository or classification failure fails the listing;
// a metrics outage only leaves health unknown (HealthResolver).
func (uc *ListServersUseCase) Execute(ctx context.Context, input ListServersInput) (pagination.Result[ServerItem], error) {
	var empty pagination.Result[ServerItem]

	result, err := uc.servers.List(ctx, serverdomain.ListFilter{
		PoolIDs:           input.PoolIDs,
		RestrictPools:     input.RestrictPools,
		SiteID:            input.SiteID,
		IntegrationID:     input.IntegrationID,
		ProvisioningState: input.ProvisioningState,
		PlatformID:        input.PlatformID,
		Keyword:           input.Keyword,
		IncludeAbsent:     input.IncludeAbsent,
		Offset:            input.Page.Offset(),
		Limit:             input.Page.PageSize,
	})
	if err != nil {
		return empty, err
	}

	if err := attachHealth(ctx, uc.health, result.Servers); err != nil {
		return empty, err
	}

	items := make([]ServerItem, 0, len(result.Servers))
	for _, s := range result.Servers {
		items = append(items, ToServerItem(s))
	}

	return pagination.NewResult(items, result.Total, input.Page), nil
}

// attachHealth sets the classified health on every Server, in one resolver call for the whole
// batch. A Server the resolver left out of its answer keeps a nil Health, which the API would show
// as null; the resolver's contract is to answer for every Server it is given.
func attachHealth(ctx context.Context, resolver HealthResolver, servers []*serverdomain.Server) error {
	if len(servers) == 0 {
		return nil
	}
	health, err := resolver.ResolveHealth(ctx, servers)
	if err != nil {
		return fmt.Errorf("classify server health: %w", err)
	}
	for _, s := range servers {
		if h, ok := health[s.ID]; ok {
			s.Health = &h
		}
	}
	return nil
}
