package application

import (
	"context"
	"fmt"
	"slices"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ServerTagsItem is one Server's spec after a tag edit, deletion, or rename: its generation and the
// complete set of manual tags it must carry. The provisioner converges to it asynchronously; the
// Server's conditions report when it has (server-tags.md).
type ServerTagsItem struct {
	ServerID   string   `json:"serverId"`
	Generation int64    `json:"generation"`
	Tags       []string `json:"tags"`
}

// EditServerTagsInput is a tag edit applied to one or more Servers. Add and Remove are the diff the
// editor computed: only the tags whose tri-state changed are sent, so tags left untouched on each
// Server are preserved. A name in both Add and Remove is contradictory and refused.
type EditServerTagsInput struct {
	ServerIDs []string
	Add       []string
	Remove    []string
}

// EditServerTagsUseCase writes a tag edit into the spec of one or more Servers (decision 071). For
// manual tags it never calls the provisioner: the Server controller converges each Server's manual
// tags to its spec. The spec part is atomic across Servers — every Server's spec changes, or none
// does — and each write is conditional on the generation read in the same transaction; a Server
// whose manual tags the edit leaves as they were is not written, so its generation stays.
//
// A Server that has no spec yet is refused with ErrSpecNotAdopted: its controller adopts the first
// observation within moments, and an edit before that could not keep the fields adoption would
// take. An automatic tag (one the provisioner computes or maintains) is never added to a spec. A
// removable one in Remove is taken off the listed Servers that carry it by the provisioner itself,
// after the transaction: that is an action, not desired state, and the provisioner may apply it
// again later. Tag editing is metadata, not a host change, so it is not gated on Server Lock; a
// locked Server simply holds convergence until it is unlocked.
type EditServerTagsUseCase struct {
	servers    serverdomain.ServerRepository
	specs      serverdomain.ServerConvergenceStore
	providers  provisioningdomain.ProviderFactory
	transactor store.Transactor
}

// NewEditServerTagsUseCase wires the Server repository and its desired-state store, the provider
// factory (to recognize automatic tags and to remove one), and the store transaction boundary.
func NewEditServerTagsUseCase(
	transactor store.Transactor,
	servers serverdomain.ServerRepository,
	specs serverdomain.ServerConvergenceStore,
	providers provisioningdomain.ProviderFactory,
) *EditServerTagsUseCase {
	return &EditServerTagsUseCase{servers: servers, specs: specs, providers: providers, transactor: transactor}
}

// Execute validates the diff, refuses an automatic tag in Add and a non-removable one in Remove,
// writes every Server's new spec in one transaction, removes the automatic tags in Remove through
// the provisioner, and returns each Server's spec in the input order. ErrServerNotFound for an
// unknown Server; ErrSpecNotAdopted for one not adopted yet; ErrSpecConflict when a Server's spec
// changed during the edit; ErrInvalidTag for an invalid, contradictory, or refused name. A provider
// error while removing an automatic tag is returned after the specs are written; repeating the
// edit finishes it, since the removal is an ensure.
func (uc *EditServerTagsUseCase) Execute(ctx context.Context, input EditServerTagsInput) ([]ServerTagsItem, error) {
	add, remove, err := normalizeTagDiff(input.Add, input.Remove)
	if err != nil {
		return nil, err
	}
	if len(input.ServerIDs) == 0 {
		return nil, provisioningdomain.ErrInvalidTag
	}
	order := make([]string, 0, len(input.ServerIDs))
	for _, id := range input.ServerIDs {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}

	// Classifying tags reads each provisioner's catalog, so it happens before the transaction,
	// which must not do provider I/O.
	servers := make([]*serverdomain.Server, 0, len(order))
	for _, id := range order {
		server, err := uc.servers.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if server.Generation == 0 {
			return nil, serverdomain.ErrSpecNotAdopted
		}
		servers = append(servers, server)
	}
	groups := uc.groupByProvisioner(ctx, servers)
	automaticRemove, err := refuseAutomaticTags(groups, add, remove)
	if err != nil {
		return nil, err
	}

	items := make([]ServerTagsItem, 0, len(order))
	err = uc.transactor.InTransaction(ctx, func(ctx context.Context) error {
		items = items[:0]
		for _, id := range order {
			server, err := uc.servers.FindByID(ctx, id)
			if err != nil {
				return err
			}
			if server.Generation == 0 {
				return serverdomain.ErrSpecNotAdopted
			}
			tags := groups[server.Source.IntegrationID].tags
			next := manualSpecTags(tags, server, applyTagDiff(server.Spec.Tags, add, remove))
			if slices.Equal(next, server.Spec.Tags) {
				items = append(items, ServerTagsItem{ServerID: id, Generation: server.Generation, Tags: wire.Strings(next)})
				continue
			}
			spec := server.Spec
			spec.Tags = next
			generation, err := uc.specs.UpdateSpec(ctx, id, server.Generation, spec)
			if err != nil {
				return err
			}
			items = append(items, ServerTagsItem{ServerID: id, Generation: generation, Tags: wire.Strings(next)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := removeAutomaticTags(ctx, groups, automaticRemove); err != nil {
		return nil, err
	}
	return items, nil
}

// provisionerGroup is the listed Servers of one provisioner with that provisioner's tags; tags is
// nil when its catalog could not be read.
type provisionerGroup struct {
	tags    *provisionerTags
	servers []*serverdomain.Server
}

// groupByProvisioner groups the Servers by provisioner Integration and reads each one's catalog.
// The read is best-effort: when it fails the Servers' observed automatic tags still classify the
// common case, and a provider call the edit then needs fails on its own.
func (uc *EditServerTagsUseCase) groupByProvisioner(ctx context.Context, servers []*serverdomain.Server) map[string]*provisionerGroup {
	groups := map[string]*provisionerGroup{}
	for _, server := range servers {
		integrationID := server.Source.IntegrationID
		group, ok := groups[integrationID]
		if !ok {
			group = &provisionerGroup{}
			// A failed read leaves tags nil, which classifies from observations only.
			group.tags, _ = loadProvisionerTags(ctx, uc.providers, integrationID)
			groups[integrationID] = group
		}
		group.servers = append(group.servers, server)
	}
	return groups
}

// refuseAutomaticTags rejects an automatic tag in add, which swallow never assigns, and a
// non-removable one in remove, which only deleting the tag can take away. It returns the automatic
// tags in remove, which the provisioner must take off the machines instead of the spec.
func refuseAutomaticTags(groups map[string]*provisionerGroup, add, remove []string) ([]string, error) {
	for _, name := range add {
		for _, group := range groups {
			if automatic, _ := tagKind(group.tags, group.servers, name); automatic {
				return nil, fmt.Errorf("%w: %q is an automatic tag the provisioner applies itself", provisioningdomain.ErrInvalidTag, name)
			}
		}
	}
	var automaticRemove []string
	for _, name := range remove {
		for _, group := range groups {
			automatic, removable := tagKind(group.tags, group.servers, name)
			if !automatic {
				continue
			}
			if !removable {
				return nil, fmt.Errorf("%w: %q is computed by the provisioner from a rule; delete the tag instead", provisioningdomain.ErrInvalidTag, name)
			}
			if !slices.Contains(automaticRemove, name) {
				automaticRemove = append(automaticRemove, name)
			}
		}
	}
	return automaticRemove, nil
}

// removeAutomaticTags takes each tag in names off the listed machines that carry it, one provider
// call per tag and provisioner, wherever that provisioner treats the tag as automatic; where it is
// manual, the spec change already removed it. A provisioner whose catalog could not be read cannot
// be called safely, so the removal fails as unavailable and repeating the edit retries it.
func removeAutomaticTags(ctx context.Context, groups map[string]*provisionerGroup, names []string) error {
	for _, name := range names {
		for _, group := range groups {
			if automatic, _ := tagKind(group.tags, group.servers, name); !automatic {
				continue
			}
			var machines []string
			for _, server := range group.servers {
				if slices.Contains(server.Observed.Tags, name) && server.Source.ProviderMachineID != "" {
					machines = append(machines, server.Source.ProviderMachineID)
				}
			}
			if len(machines) == 0 {
				continue
			}
			if group.tags == nil {
				return &provisioningdomain.ProviderError{
					Kind:   provisioningdomain.ProviderErrorUnavailable,
					Detail: "The provisioner's tags could not be read, so the automatic tag was not removed; try again.",
				}
			}
			if group.tags.controller == nil {
				continue
			}
			if err := group.tags.controller.RemoveTag(ctx, name, machines); err != nil {
				return err
			}
		}
	}
	return nil
}
