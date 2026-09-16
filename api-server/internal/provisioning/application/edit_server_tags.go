package application

import (
	"context"
	"errors"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ServerTagsItem is the effective tag set of one Server after an edit, returned so the caller can
// update its rows without a re-read.
type ServerTagsItem struct {
	ServerID string   `json:"serverId"`
	Tags     []string `json:"tags"`
}

// EditServerTagsInput is a tag edit applied to one or more Servers. Add and Remove are the diff the
// editor computed: only the tags whose tri-state changed are sent, so tags left untouched on each
// Server are preserved. A name in both Add and Remove is contradictory and refused.
type EditServerTagsInput struct {
	ServerIDs []string
	Add       []string
	Remove    []string
}

// EditServerTagsUseCase applies a tag edit across Servers, capability-first (decision 031).
//
// It groups the target Servers by their provisioner integration and, per group, branches on the
// provider's Tagging capability: a capable provisioner (MAAS) is driven directly — each added tag is
// ensured then assigned, each removed tag unassigned, in one batch call per tag across the group's
// machines — and each Server is then refreshed from the provider so Observed.Tags reflects the
// change immediately. A provisioner that cannot own tags falls back to the swallow-owned overlay,
// which reconcile merges into Observed.Tags. Tag editing is metadata, not a host state change, so it
// is not gated on Server Lock.
type EditServerTagsUseCase struct {
	servers     serverdomain.ServerRepository
	providers   provisioningdomain.ProviderFactory
	tagOverlays provisioningdomain.ServerTagOverlayRepository
	now         func() time.Time
}

// NewEditServerTagsUseCase wires the Server repository, provider factory, and swallow-owned tag
// overlay store the edit dispatches between.
func NewEditServerTagsUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	tagOverlays provisioningdomain.ServerTagOverlayRepository,
) *EditServerTagsUseCase {
	return &EditServerTagsUseCase{
		servers:     servers,
		providers:   providers,
		tagOverlays: tagOverlays,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Execute validates the diff, loads and groups the target Servers, applies the change per group
// through the capability-first branch, and returns each Server's effective tags in the input order.
func (uc *EditServerTagsUseCase) Execute(ctx context.Context, input EditServerTagsInput) ([]ServerTagsItem, error) {
	add, remove, err := normalizeTagDiff(input.Add, input.Remove)
	if err != nil {
		return nil, err
	}
	if len(input.ServerIDs) == 0 {
		return nil, provisioningdomain.ErrInvalidTag
	}

	// Load each Server once, preserving request order for the response and dropping duplicates so a
	// repeated id does not double-apply to a provider batch.
	order := make([]string, 0, len(input.ServerIDs))
	byID := make(map[string]*serverdomain.Server, len(input.ServerIDs))
	groups := map[string][]*serverdomain.Server{}
	groupOrder := make([]string, 0)
	for _, serverID := range input.ServerIDs {
		if _, seen := byID[serverID]; seen {
			continue
		}
		server, err := uc.servers.FindByID(ctx, serverID)
		if err != nil {
			return nil, err
		}
		order = append(order, serverID)
		byID[serverID] = server
		integrationID := server.Source.IntegrationID
		if _, ok := groups[integrationID]; !ok {
			groupOrder = append(groupOrder, integrationID)
		}
		groups[integrationID] = append(groups[integrationID], server)
	}

	for _, integrationID := range groupOrder {
		if err := uc.applyGroup(ctx, integrationID, groups[integrationID], add, remove); err != nil {
			return nil, err
		}
	}

	items := make([]ServerTagsItem, 0, len(order))
	for _, serverID := range order {
		items = append(items, ServerTagsItem{
			ServerID: serverID,
			Tags:     append([]string(nil), byID[serverID].Observed.Tags...),
		})
	}
	return items, nil
}

// applyGroup applies the diff to one integration's Servers, choosing the provider-driven path when
// the provisioner is tagging-capable and the swallow-owned fallback otherwise.
func (uc *EditServerTagsUseCase) applyGroup(
	ctx context.Context,
	integrationID string,
	servers []*serverdomain.Server,
	add, remove []string,
) error {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return err
	}
	controller, ok := provider.(provisioningdomain.MachineTagController)
	if provider.Capabilities().Tagging && ok {
		return uc.driveProvider(ctx, provider, controller, servers, add, remove)
	}
	return uc.writeOverlays(ctx, integrationID, servers, add, remove)
}

// driveProvider realizes the edit on a tagging-capable provisioner and refreshes each Server.
//
// Assign and unassign are batched: one EnsureTag + AddTag per added name and one RemoveTag per
// removed name, each carrying every machine in the group, because a provider that owns tags globally
// (MAAS) applies one tag to many machines in a single call. After the writes each Server is re-read
// so its Observed.Tags shows the change now rather than waiting for the next reconcile pass.
func (uc *EditServerTagsUseCase) driveProvider(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	controller provisioningdomain.MachineTagController,
	servers []*serverdomain.Server,
	add, remove []string,
) error {
	machineIDs := make([]string, 0, len(servers))
	for _, server := range servers {
		machineIDs = append(machineIDs, server.Source.ProviderMachineID)
	}

	for _, name := range add {
		if err := controller.EnsureTag(ctx, name); err != nil {
			return err
		}
		if err := controller.AddTag(ctx, name, machineIDs); err != nil {
			return err
		}
	}
	for _, name := range remove {
		if err := controller.RemoveTag(ctx, name, machineIDs); err != nil {
			return err
		}
	}

	for _, server := range servers {
		machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
		if err != nil {
			return err
		}
		server.Observed.Tags = append([]string(nil), machine.Tags...)
		server.UpdatedAt = uc.now()
		if err := uc.servers.Upsert(ctx, server); err != nil {
			return err
		}
	}
	return nil
}

// writeOverlays applies the edit as swallow-owned tags for a provisioner that cannot own them. The
// owned set starts from the Server's existing overlay, has the diff applied, and is persisted (or
// deleted when empty). Observed.Tags is set to the new owned set so the response and the live row
// reflect it immediately; reconcile re-derives the same union on its next pass.
func (uc *EditServerTagsUseCase) writeOverlays(
	ctx context.Context,
	integrationID string,
	servers []*serverdomain.Server,
	add, remove []string,
) error {
	for _, server := range servers {
		current, err := uc.currentOwnedTags(ctx, server.ID)
		if err != nil {
			return err
		}
		next := applyTagDiff(current, add, remove)

		if len(next) == 0 {
			if err := uc.tagOverlays.Delete(ctx, server.ID); err != nil {
				return err
			}
		} else {
			if err := uc.tagOverlays.Upsert(ctx, &provisioningdomain.ServerTagOverlay{
				ServerID:      server.ID,
				IntegrationID: integrationID,
				Tags:          next,
				UpdatedAt:     uc.now(),
			}); err != nil {
				return err
			}
		}

		server.Observed.Tags = next
		server.UpdatedAt = uc.now()
		if err := uc.servers.Upsert(ctx, server); err != nil {
			return err
		}
	}
	return nil
}

// currentOwnedTags returns a Server's existing swallow-owned tags, or nil when it has no overlay.
func (uc *EditServerTagsUseCase) currentOwnedTags(ctx context.Context, serverID string) ([]string, error) {
	overlay, err := uc.tagOverlays.Get(ctx, serverID)
	switch {
	case err == nil:
		return overlay.Tags, nil
	case errors.Is(err, provisioningdomain.ErrServerTagOverlayNotFound):
		return nil, nil
	default:
		return nil, err
	}
}
