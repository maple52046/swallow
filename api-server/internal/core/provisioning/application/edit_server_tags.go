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

// ServerTagsItem is one Server's spec after a tag edit: its new generation and the complete set of
// manual tags it must carry. The provisioner converges to it asynchronously; the Server's
// conditions report when it has (server-tags.md).
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

// EditServerTagsUseCase writes a tag edit into the spec of one or more Servers (decision 071). It
// never calls the provisioner to change a tag: the Server controller converges each Server's
// manual tags to its spec. The edit is atomic across Servers — every Server's spec changes, or none
// does — and each write is conditional on the generation read in the same transaction.
//
// A Server that has no spec yet is refused with ErrSpecNotAdopted: its controller adopts the first
// observation within moments, and an edit before that could not keep the fields adoption would
// take. An automatic tag (computed by the provisioner) can be neither added nor removed. Tag editing is metadata, not a host change, so it
// is not gated on Server Lock; a locked Server simply holds convergence until it is unlocked.
type EditServerTagsUseCase struct {
	servers    serverdomain.ServerRepository
	specs      serverdomain.ServerConvergenceStore
	providers  provisioningdomain.ProviderFactory
	transactor store.Transactor
}

// NewEditServerTagsUseCase wires the Server repository and its desired-state store, the provider
// factory (read only, to recognize automatic tags), and the store transaction boundary.
func NewEditServerTagsUseCase(
	transactor store.Transactor,
	servers serverdomain.ServerRepository,
	specs serverdomain.ServerConvergenceStore,
	providers provisioningdomain.ProviderFactory,
) *EditServerTagsUseCase {
	return &EditServerTagsUseCase{servers: servers, specs: specs, providers: providers, transactor: transactor}
}

// Execute validates the diff, refuses automatic tag names, writes every Server's new spec in one
// transaction, and returns each Server's spec in the input order. ErrServerNotFound for an unknown
// Server; ErrSpecNotAdopted for one not adopted yet; ErrSpecConflict when a Server's spec changed
// during the edit; ErrInvalidTag for an invalid, contradictory, or automatic name.
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

	// Recognizing automatic tags reads the provisioner's catalog, so it happens before the
	// transaction, which must not do provider I/O.
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
	if err := uc.refuseAutomaticTags(ctx, servers, append(slices.Clone(add), remove...)); err != nil {
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
			next := applyTagDiff(server.Spec.Tags, add, remove)
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
	return items, nil
}

// refuseAutomaticTags rejects a name the provisioner computes itself: on any of the Servers, or in
// the catalog of a tagging provisioner. The catalog read is best-effort; when the provisioner is
// unreachable the observed automatic tags still guard the common case, and the controller reports a
// rejection if the provisioner refuses the change.
func (uc *EditServerTagsUseCase) refuseAutomaticTags(ctx context.Context, servers []*serverdomain.Server, names []string) error {
	automatic := map[string]bool{}
	integrations := map[string]bool{}
	for _, server := range servers {
		for _, tag := range server.Observed.AutomaticTags {
			automatic[tag] = true
		}
		integrations[server.Source.IntegrationID] = true
	}
	for integrationID := range integrations {
		provider, err := uc.providers.For(ctx, integrationID)
		if err != nil {
			continue
		}
		controller, ok := provider.(provisioningdomain.MachineTagController)
		if !ok || !provider.Capabilities().Tagging {
			continue
		}
		catalog, err := controller.ListTags(ctx)
		if err != nil {
			continue
		}
		for _, tag := range catalog {
			if !tag.Editable {
				automatic[tag.Name] = true
			}
		}
	}
	for _, name := range names {
		if automatic[name] {
			return fmt.Errorf("%w: %q is an automatic tag computed by the provisioner", provisioningdomain.ErrInvalidTag, name)
		}
	}
	return nil
}
