package application

import (
	"context"
	"slices"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// provisionerTags is one provisioner's tag controller and catalog, which together tell its manual
// tags, owned by the Server specs, from its automatic tags, which it computes or maintains itself
// (decision 071). controller is nil when the provisioner cannot set tags: it then reports no
// automatic tags and there is nothing to call.
type provisionerTags struct {
	controller provisioningdomain.MachineTagController
	catalog    []provisioningdomain.MachineTag
	byName     map[string]provisioningdomain.MachineTag
}

// loadProvisionerTags builds the provider of integrationID and reads its tag catalog. Building the
// provider or reading the catalog can fail (an unreachable provisioner, a missing credential); the
// error is returned for the caller to decide whether a classification is required (a deletion) or
// best-effort (an edit).
func loadProvisionerTags(
	ctx context.Context,
	providers provisioningdomain.ProviderFactory,
	integrationID string,
) (*provisionerTags, error) {
	provider, err := providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.MachineTagController)
	if !ok || !provider.Capabilities().Tagging {
		return &provisionerTags{}, nil
	}
	catalog, err := controller.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]provisioningdomain.MachineTag, len(catalog))
	for _, tag := range catalog {
		byName[tag.Name] = tag
	}
	return &provisionerTags{controller: controller, catalog: catalog, byName: byName}, nil
}

// tagKind reports whether name is an automatic tag for these Servers of one provisioner, and if so
// whether swallow may remove it from a machine. The catalog decides when it holds the name; tags
// is nil when the catalog could not be read. Otherwise a name is automatic only when one of the
// Servers observes it so, which happens when the provisioner applied it after the catalog was read
// or the catalog is unavailable; such a tag is treated as removable and the provisioner has the
// last word.
func tagKind(tags *provisionerTags, servers []*serverdomain.Server, name string) (automatic, removable bool) {
	if tags != nil {
		if tag, ok := tags.byName[name]; ok {
			return !tag.Editable, tag.Removable
		}
	}
	for _, server := range servers {
		if slices.Contains(server.Observed.AutomaticTags, name) {
			return true, true
		}
	}
	return false, true
}

// manualSpecTags drops from a Server's spec tags every tag that is automatic for it. A spec can
// still name one when the Server was adopted before the provisioner adapter recognized the tag as
// automatic; the server-tags contract has every write of that Server's spec drop it, and the
// Server controller never converges it.
func manualSpecTags(tags *provisionerTags, server *serverdomain.Server, specTags []string) []string {
	manual := make([]string, 0, len(specTags))
	for _, name := range specTags {
		if automatic, _ := tagKind(tags, []*serverdomain.Server{server}, name); !automatic {
			manual = append(manual, name)
		}
	}
	return manual
}

// siteTagScope is what the Site-wide tag routes act on: the Site's provisioner, its tags, and every
// Server of that provisioner, absent ones included because their specs still name tags. A Site has
// one provisioner (glossary: OS Provisioning Provider), so the first enabled one is used, as the
// rest of the provisioning surface does.
type siteTagScope struct {
	tags    *provisionerTags
	servers []*serverdomain.Server
}

// loadSiteTagScope resolves siteID's provisioner, reads its tag catalog, and lists its Servers. It
// returns nil and no error when the Site has no enabled provisioner (including an empty or unknown
// siteID). Reading the catalog must succeed: the Site-wide routes cannot tell manual from
// automatic tags without it.
func loadSiteTagScope(
	ctx context.Context,
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
	servers serverdomain.ServerRepository,
	siteID string,
) (*siteTagScope, error) {
	siteID = strings.TrimSpace(siteID)
	if siteID == "" {
		return nil, nil
	}
	found, err := integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID:      siteID,
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil || len(found) == 0 {
		return nil, err
	}
	integrationID := found[0].ID
	tags, err := loadProvisionerTags(ctx, providers, integrationID)
	if err != nil {
		return nil, err
	}
	result, err := servers.List(ctx, serverdomain.ListFilter{IntegrationID: integrationID, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	return &siteTagScope{tags: tags, servers: result.Servers}, nil
}

// kind reports whether name is an automatic tag of the Site, and whether it is removable.
func (s *siteTagScope) kind(name string) (automatic, removable bool) {
	return tagKind(s.tags, s.servers, name)
}

// knows reports whether the Site has the tag at all: in its provisioner's catalog, in a Server's
// spec, or on a machine as observed.
func (s *siteTagScope) knows(name string) bool {
	if _, ok := s.tags.byName[name]; ok {
		return true
	}
	for _, server := range s.servers {
		if slices.Contains(server.Spec.Tags, name) || slices.Contains(server.Observed.Tags, name) {
			return true
		}
	}
	return false
}

// refuseUnadopted returns ErrSpecNotAdopted when a Server not adopted yet carries the tag. Its
// controller adopts the observed manual tags as its spec within moments; a deletion or rename
// written before that would be undone by the adoption, so the request is refused as a retryable
// conflict instead.
func (s *siteTagScope) refuseUnadopted(name string) error {
	for _, server := range s.servers {
		if server.Generation == 0 && slices.Contains(server.Observed.Tags, name) {
			return serverdomain.ErrSpecNotAdopted
		}
	}
	return nil
}
