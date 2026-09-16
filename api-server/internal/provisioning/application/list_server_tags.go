package application

import (
	"context"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// ServerTagOption is one known tag offered to the editor, with whether swallow may assign it.
//
// Editable is false for a provider tag computed from a definition (a MAAS automatic tag), which the
// editor shows disabled: it is real and meaningful (the `amd-gpu` tag still drives Server Type) but
// swallow must not try to assign or unassign it.
type ServerTagOption struct {
	Name     string `json:"name"`
	Editable bool   `json:"editable"`
}

// ListServerTagsUseCase lists the tags known for a Site, so the editor can offer existing names and
// disable the ones that are not swallow-editable.
//
// It is capability-first (decision 031): when the Site's provisioner owns tags, the known set is the
// provider's own tag list (with its automatic tags flagged read-only); when it does not, the known
// set is the union of the swallow-owned overlays for that provisioner, all editable. A Site with no
// provisioner yields an empty list rather than an error, because there is simply nothing to offer.
type ListServerTagsUseCase struct {
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
	tagOverlays  provisioningdomain.ServerTagOverlayRepository
}

// NewListServerTagsUseCase wires the integration repository (to find a Site's provisioner), the
// provider factory, and the swallow-owned tag overlay store the fallback reads from.
func NewListServerTagsUseCase(
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
	tagOverlays provisioningdomain.ServerTagOverlayRepository,
) *ListServerTagsUseCase {
	return &ListServerTagsUseCase{
		integrations: integrations,
		providers:    providers,
		tagOverlays:  tagOverlays,
	}
}

// Execute returns the known tags for the Site. An empty siteID, or a Site with no enabled
// provisioner, returns an empty list.
func (uc *ListServerTagsUseCase) Execute(ctx context.Context, siteID string) ([]ServerTagOption, error) {
	siteID = strings.TrimSpace(siteID)
	if siteID == "" {
		return []ServerTagOption{}, nil
	}

	integrations, err := uc.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID:      siteID,
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	if len(integrations) == 0 {
		return []ServerTagOption{}, nil
	}

	// A Site has one provisioner (glossary: MAAS, one per site); use the first enabled one.
	integrationID := integrations[0].ID
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}

	if controller, ok := provider.(provisioningdomain.MachineTagController); ok && provider.Capabilities().Tagging {
		tags, err := controller.ListTags(ctx)
		if err != nil {
			return nil, err
		}
		options := make([]ServerTagOption, 0, len(tags))
		for _, tag := range tags {
			options = append(options, ServerTagOption{Name: tag.Name, Editable: tag.Editable})
		}
		return options, nil
	}

	// Fallback: the known set is every swallow-owned tag for this provisioner, all editable.
	overlays, err := uc.tagOverlays.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	options := make([]ServerTagOption, 0)
	for _, overlay := range overlays {
		for _, name := range overlay.Tags {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			options = append(options, ServerTagOption{Name: name, Editable: true})
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
// duplicating. It is how the swallow-owned fallback computes a Server's new owned tag set.
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
