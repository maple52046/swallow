package app

import (
	"context"

	discoveryapp "github.com/maple52046/swallow/internal/discovery/application"
)

// executionInventoryAdapter keeps the operation context on the discovery use case port.
type executionInventoryAdapter struct {
	discovery *discoveryapp.DiscoveryUseCase
}

func (a executionInventoryAdapter) Inventory(ctx context.Context, siteID string) (map[string]any, error) {
	inventory, err := a.discovery.AnsibleInventory(ctx, discoveryapp.DiscoveryInput{SiteID: siteID})
	if err != nil {
		return nil, err
	}
	return map[string]any(inventory), nil
}
