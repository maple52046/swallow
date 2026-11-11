package application

import (
	"context"
	"slices"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// ServerTagOption is one tag a Site knows, as the editor and the tag manager offer it.
//
// Editable is false for an automatic tag, which the provisioner computes or maintains itself: it is
// real and meaningful (the `amd-gpu` tag still drives Server Type) but swallow never assigns it, so
// the editor never offers to add it. Removable says whether swallow may take the tag off a Server;
// an automatic tag computed from a rule can only be deleted. ServerCount is how many of the
// provisioner's Servers carry the tag: in their spec for a manual tag, as observed for an automatic
// one.
type ServerTagOption struct {
	Name        string `json:"name"`
	Editable    bool   `json:"editable"`
	Removable   bool   `json:"removable"`
	ServerCount int    `json:"serverCount"`
}

// ListServerTagsUseCase lists the tags known for a Site, so the editor can offer existing names and
// the tag manager can show how widely each is used.
//
// The known set is the Site provisioner's tag catalog, with each tag's kind, plus every tag in the
// specs of that provisioner's Servers that the catalog does not hold yet (one just added and not yet
// created there). When the provisioner cannot set tags, it is those spec tags alone, all manual: the
// spec is kept even though the provisioner cannot realize it (the Servers report
// capability_missing). A Site with no enabled provisioner yields an empty list.
type ListServerTagsUseCase struct {
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
	servers      serverdomain.ServerRepository
}

// NewListServerTagsUseCase wires the integration repository (to find a Site's provisioner), the
// provider factory (to read its catalog), and the Server repository whose specs and observations
// count each tag.
func NewListServerTagsUseCase(
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
	servers serverdomain.ServerRepository,
) *ListServerTagsUseCase {
	return &ListServerTagsUseCase{integrations: integrations, providers: providers, servers: servers}
}

// Execute returns the known tags for the Site, catalog tags first in the provisioner's order. An
// empty siteID, or a Site with no enabled provisioner, returns an empty list. A provisioner that
// cannot be reached, or refuses the catalog read, fails the listing with its provider error.
func (uc *ListServerTagsUseCase) Execute(ctx context.Context, siteID string) ([]ServerTagOption, error) {
	scope, err := loadSiteTagScope(ctx, uc.integrations, uc.providers, uc.servers, siteID)
	if err != nil {
		return nil, err
	}
	if scope == nil {
		return []ServerTagOption{}, nil
	}

	options := make([]ServerTagOption, 0, len(scope.tags.catalog))
	index := make(map[string]int, len(scope.tags.catalog))
	for _, tag := range scope.tags.catalog {
		index[tag.Name] = len(options)
		options = append(options, ServerTagOption{Name: tag.Name, Editable: tag.Editable, Removable: tag.Removable})
	}
	for _, server := range scope.servers {
		for _, name := range server.Spec.Tags {
			if automatic, _ := scope.kind(name); automatic {
				continue
			}
			i, ok := index[name]
			if !ok {
				i = len(options)
				index[name] = i
				options = append(options, ServerTagOption{Name: name, Editable: true, Removable: true})
			}
			options[i].ServerCount++
		}
	}
	for i := range options {
		if options[i].Editable {
			continue
		}
		for _, server := range scope.servers {
			if slices.Contains(server.Observed.Tags, options[i].Name) {
				options[i].ServerCount++
			}
		}
	}
	return options, nil
}

// normalizeTagDiff validates and cleans an add/remove tag diff. Each list is normalized (trimmed,
// deduplicated, validated) and a name appearing in both is rejected as contradictory, because
// applying an add and a remove of the same tag in one edit has no coherent result.
func normalizeTagDiff(rawAdd, rawRemove []string) (add, remove []string, err error) {
	add, err = provisioningdomain.NormalizeTagNames(rawAdd)
	if err != nil {
		return nil, nil, err
	}
	remove, err = provisioningdomain.NormalizeTagNames(rawRemove)
	if err != nil {
		return nil, nil, err
	}
	addSet := make(map[string]struct{}, len(add))
	for _, name := range add {
		addSet[name] = struct{}{}
	}
	for _, name := range remove {
		if _, clash := addSet[name]; clash {
			return nil, nil, provisioningdomain.ErrInvalidTag
		}
	}
	return add, remove, nil
}

// applyTagDiff returns current with remove taken out and add put in, preserving order and never
// duplicating. It is how a tag edit computes a Server's new spec tags.
func applyTagDiff(current, add, remove []string) []string {
	removeSet := make(map[string]struct{}, len(remove))
	for _, name := range remove {
		removeSet[name] = struct{}{}
	}
	present := make(map[string]struct{}, len(current))
	result := make([]string, 0, len(current)+len(add))
	for _, name := range current {
		if _, dropped := removeSet[name]; dropped {
			continue
		}
		if _, dup := present[name]; dup {
			continue
		}
		present[name] = struct{}{}
		result = append(result, name)
	}
	for _, name := range add {
		if _, dup := present[name]; dup {
			continue
		}
		present[name] = struct{}{}
		result = append(result, name)
	}
	return result
}
