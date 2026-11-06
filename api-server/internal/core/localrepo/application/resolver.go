package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// HostVarName is the trusted extra var that points a playbook's hosts at their Site's Local
// Repository (the playbooks marked usesLocalRepository read it). Its value's keys are the contract
// between swallow and the shared role that configures a host's package sources.
const HostVarName = "swallow_local_repository"

// Resolver resolves, at automation acceptance, where a Site's hosts fetch their artifacts. It reads
// only the store (the status the controller recorded) and never calls the provider, so accepting a
// deployment does not depend on the provider answering.
type Resolver struct {
	integrations sitedomain.IntegrationRepository
	records      localrepodomain.Store
	modules      []localrepodomain.ModuleDeclaration
}

// NewResolver wires the resolver to the Integrations, the records, and the compiled modules'
// declarations. Every dependency is required.
func NewResolver(integrations sitedomain.IntegrationRepository, records localrepodomain.Store, modules []localrepodomain.ModuleDeclaration) *Resolver {
	return &Resolver{integrations: integrations, records: records, modules: modules}
}

// HostVars returns the trusted extra vars a playbook that fetches from the Local Repository needs,
// {HostVarName: value}, where value is:
//
//	deb:              [{name, uri, suites, components}], one per effective deb source
//	deb_signing_key:  the armored key the deb metadata is signed with, "" when unsigned
//	registry:         the registry's "host:port"
//	registry_mirrors: {upstream registry host: mirror URL}
//	python_index:     the Python simple index URL
//	files:            the URL prefix a file's path is appended to
//
// Every key is present; a kind the repository does not serve is empty. It returns
// ErrLocalRepositoryUnavailable, wrapped with what to register, fix, enable, or wait for, while the
// Site has no enabled repository Integration, the controller's last pass was stopped by the
// Integration or its provider (the realized condition's reason and message), or an effective
// source, file, or declared image is not ready; store failures pass through.
func (r *Resolver) HostVars(ctx context.Context, siteID string) (map[string]any, error) {
	integration, err := repositoryIntegration(ctx, r.integrations, siteID)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		return nil, fmt.Errorf("%w: the site has no repository integration; register one and add its sources",
			localrepodomain.ErrLocalRepositoryUnavailable)
	}
	record, err := r.records.Get(ctx, siteID)
	if errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		record, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	view := buildView(siteID, integration, record, Effective(r.modules, specSources(record)))
	if !view.Ready {
		return nil, fmt.Errorf("%w: %s", localrepodomain.ErrLocalRepositoryUnavailable, view.NotReadyMessage())
	}
	return map[string]any{HostVarName: hostVarValue(view)}, nil
}

// hostVarValue shapes a ready view's endpoints. The deb list keeps only the effective deb sources,
// in name order, so a host is never pointed at a source swallow does not manage.
func hostVarValue(view View) map[string]any {
	endpoints := view.Endpoints
	wanted := map[string]bool{}
	for _, source := range view.Sources {
		if source.Format == localrepodomain.FormatDeb {
			wanted[source.Name] = true
		}
	}
	deb := []any{}
	served := slices.Clone(endpoints.Deb)
	slices.SortFunc(served, func(a, b localrepodomain.DebSource) int { return strings.Compare(a.Name, b.Name) })
	for _, source := range served {
		if !wanted[source.Name] {
			continue
		}
		deb = append(deb, map[string]any{
			"name": source.Name, "uri": source.URI,
			"suites": nonNil(source.Suites), "components": nonNil(source.Components),
		})
	}
	mirrors := map[string]any{}
	for host, mirror := range endpoints.RegistryMirrors {
		mirrors[host] = mirror
	}
	return map[string]any{
		"deb":              deb,
		"deb_signing_key":  endpoints.DebSigningKey,
		"registry":         endpoints.Registry,
		"registry_mirrors": mirrors,
		"python_index":     endpoints.PythonIndex,
		"files":            endpoints.Files,
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return slices.Clone(values)
}
