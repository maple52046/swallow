package application

import (
	"context"
	"fmt"
	"slices"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// siteTagWriter is what the Site-wide tag use cases share: finding the Site's provisioner and
// Servers, and rewriting the Servers' specs in one transaction before the provisioner is called.
type siteTagWriter struct {
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
	servers      serverdomain.ServerRepository
	specs        serverdomain.ServerConvergenceStore
	transactor   store.Transactor
}

// scopeFor loads the Site's tag scope and checks that the tag can be acted on: the Site knows it
// (ErrTagNotFound otherwise, also for a Site with no enabled provisioner) and no Server not adopted
// yet carries it (ErrSpecNotAdopted). A catalog read failure is returned before anything is written.
func (w *siteTagWriter) scopeFor(ctx context.Context, siteID, name string) (*siteTagScope, error) {
	scope, err := loadSiteTagScope(ctx, w.integrations, w.providers, w.servers, siteID)
	if err != nil {
		return nil, err
	}
	if scope == nil || !scope.knows(name) {
		return nil, fmt.Errorf("%w: %q", provisioningdomain.ErrTagNotFound, name)
	}
	if err := scope.refuseUnadopted(name); err != nil {
		return nil, err
	}
	return scope, nil
}

// rewriteSpecs applies change to the spec tags of every adopted Server of the scope whose spec names
// name, in one transaction, and returns each rewritten Server's spec in the scope's order. Each
// Server is read again inside the transaction and written conditionally on that generation, so a
// concurrent spec write fails the whole rewrite with ErrSpecConflict and nothing is changed. A
// Server's automatic spec tags are dropped as it is written (manualSpecTags). The transaction does
// no provider I/O; callers call the provisioner after it commits.
func (w *siteTagWriter) rewriteSpecs(
	ctx context.Context,
	scope *siteTagScope,
	name string,
	change func([]string) []string,
) ([]ServerTagsItem, error) {
	var candidates []string
	for _, server := range scope.servers {
		if server.Generation > 0 && slices.Contains(server.Spec.Tags, name) {
			candidates = append(candidates, server.ID)
		}
	}
	items := make([]ServerTagsItem, 0, len(candidates))
	if len(candidates) == 0 {
		return items, nil
	}
	err := w.transactor.InTransaction(ctx, func(ctx context.Context) error {
		items = items[:0]
		for _, id := range candidates {
			server, err := w.servers.FindByID(ctx, id)
			if err != nil {
				return err
			}
			next := manualSpecTags(scope.tags, server, change(server.Spec.Tags))
			if slices.Equal(next, server.Spec.Tags) {
				continue
			}
			spec := server.Spec
			spec.Tags = next
			generation, err := w.specs.UpdateSpec(ctx, id, server.Generation, spec)
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

// DeleteServerTagUseCase deletes a tag across a Site (server-tags.md, Delete a tag). A manual tag
// is removed from every spec of the Site's provisioner in one transaction, then deleted from the
// provisioner; an automatic tag is deleted from the provisioner only, as a one-off action the
// provisioner may undo by applying the tag again. Writing the specs first keeps the Server
// controller, converging a spec it read before, from re-creating the tag. The provisioner call is
// an ensure, so a request repeated after a provider failure finishes the deletion. Not gated on
// Server Lock: tags are metadata.
type DeleteServerTagUseCase struct {
	writer siteTagWriter
}

// NewDeleteServerTagUseCase wires the integration repository (to find the Site's provisioner), the
// provider factory, the Server repository and its desired-state store, and the transaction
// boundary.
func NewDeleteServerTagUseCase(
	transactor store.Transactor,
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
	servers serverdomain.ServerRepository,
	specs serverdomain.ServerConvergenceStore,
) *DeleteServerTagUseCase {
	return &DeleteServerTagUseCase{writer: siteTagWriter{
		integrations: integrations, providers: providers, servers: servers, specs: specs, transactor: transactor,
	}}
}

// Execute deletes the tag rawName from siteID and returns each Server whose spec changed.
// ErrInvalidTag for an invalid name; ErrTagNotFound when the Site does not know the tag;
// ErrSpecNotAdopted when a Server not adopted yet carries it; ErrSpecConflict when a spec changed
// during the write. A provider error reading the catalog is returned before any write; one deleting
// the tag is returned after the specs are written.
func (uc *DeleteServerTagUseCase) Execute(ctx context.Context, siteID, rawName string) ([]ServerTagsItem, error) {
	name, err := provisioningdomain.NormalizeTagName(rawName)
	if err != nil {
		return nil, err
	}
	scope, err := uc.writer.scopeFor(ctx, siteID, name)
	if err != nil {
		return nil, err
	}
	items, err := uc.writer.rewriteSpecs(ctx, scope, name, func(tags []string) []string {
		return applyTagDiff(tags, nil, []string{name})
	})
	if err != nil {
		return nil, err
	}
	if scope.tags.controller != nil {
		if err := scope.tags.controller.DeleteTag(ctx, name); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// RenameServerTagUseCase renames a manual tag across a Site (server-tags.md, Rename a tag). Every
// spec of the Site's provisioner that names the tag gets the new name in one transaction; then the
// provisioner renames the tag in place, so machines keep it without a gap. Renaming onto an existing
// manual tag merges the two: the provisioner deletes the old tag and the Server controller assigns
// the existing one where the specs now want it. An automatic tag is neither renamed nor a rename
// target. Not gated on Server Lock.
type RenameServerTagUseCase struct {
	writer siteTagWriter
}

// NewRenameServerTagUseCase wires the same dependencies as NewDeleteServerTagUseCase.
func NewRenameServerTagUseCase(
	transactor store.Transactor,
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
	servers serverdomain.ServerRepository,
	specs serverdomain.ServerConvergenceStore,
) *RenameServerTagUseCase {
	return &RenameServerTagUseCase{writer: siteTagWriter{
		integrations: integrations, providers: providers, servers: servers, specs: specs, transactor: transactor,
	}}
}

// Execute renames the tag rawFrom of siteID to rawTo and returns each Server whose spec changed.
// ErrInvalidTag for an invalid or unchanged name, or an automatic tag on either side; the other
// errors are those of DeleteServerTagUseCase.Execute.
func (uc *RenameServerTagUseCase) Execute(ctx context.Context, siteID, rawFrom, rawTo string) ([]ServerTagsItem, error) {
	from, err := provisioningdomain.NormalizeTagName(rawFrom)
	if err != nil {
		return nil, err
	}
	to, err := provisioningdomain.NormalizeTagName(rawTo)
	if err != nil {
		return nil, err
	}
	if from == to {
		return nil, fmt.Errorf("%w: the new name is the current name", provisioningdomain.ErrInvalidTag)
	}
	scope, err := uc.writer.scopeFor(ctx, siteID, from)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{from, to} {
		if automatic, _ := scope.kind(name); automatic {
			return nil, fmt.Errorf("%w: %q is an automatic tag the provisioner applies itself", provisioningdomain.ErrInvalidTag, name)
		}
	}
	items, err := uc.writer.rewriteSpecs(ctx, scope, from, func(tags []string) []string {
		renamed := make([]string, 0, len(tags))
		for _, tag := range tags {
			if tag == from {
				tag = to
			}
			if !slices.Contains(renamed, tag) {
				renamed = append(renamed, tag)
			}
		}
		return renamed
	})
	if err != nil {
		return nil, err
	}
	if scope.tags.controller != nil {
		if err := scope.tags.controller.RenameTag(ctx, from, to); err != nil {
			return nil, err
		}
	}
	return items, nil
}
