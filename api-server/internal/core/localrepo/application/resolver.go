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

// ArtifactsVarName is the trusted extra var that tells a playbook's hosts where to fetch the
// artifacts their modules declare (the playbooks the release manifest marks fetchesArtifacts read
// it). Its value's keys are the contract between swallow and the shared role that configures a
// host's package sources (contract local-repository.md, Deployments), so a key is never renamed
// or dropped without that role.
const ArtifactsVarName = "swallow_artifacts"

// The values of the var's "source" key: where the hosts fetch every artifact from.
const (
	artifactsFromLocalRepository = "local_repository"
	artifactsFromUpstream        = "upstream"
)

// Resolver resolves, at automation acceptance, where a Site's hosts fetch their artifacts. It reads
// only the store (the spec and the status the controller recorded) and never calls the provider, so
// accepting a deployment does not depend on the provider answering.
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

// ArtifactVars returns the trusted extra vars a playbook that fetches artifacts needs,
// {ArtifactsVarName: value}, where value is:
//
//	source:           "local_repository" or "upstream"
//	deb:              [{name, uri, suites, components, signing_key_url, signing_key_fingerprint}], by name
//	deb_signing_key:  the armored key the Local Repository signs its deb metadata with, "" otherwise
//	registry:         the Local Repository's registry "host:port", "" otherwise
//	registry_mirrors: {upstream registry host: mirror URL}, {} from the upstreams
//	python_index:     the Local Repository's Python simple index URL, "" otherwise
//	files:            {declared path: the URL to fetch it from}
//	images:           {declared reference: the reference to pull}
//
// A Site whose Local Repository serves deployments (its spec's ServeDeployments) gets
// "local_repository": every effective deb source at the provider's address and every file and image
// through the provider. Such a Site that is not ready is ErrLocalRepositoryUnavailable, wrapped with
// what to enable, fix, or wait for (a disabled Integration, the realized reason that stopped the
// controller's last pass, or the sources, files, and images not ready yet); it never falls back to
// the upstreams. Any other Site, one without a repository Integration included, gets "upstream":
// the modules' own declarations, so the user's sources play no part. Every key is present and a
// kind nothing serves is empty. Store failures pass through.
func (r *Resolver) ArtifactVars(ctx context.Context, siteID string) (map[string]any, error) {
	integration, err := repositoryIntegration(ctx, r.integrations, siteID)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		return map[string]any{ArtifactsVarName: upstreamValue(Effective(r.modules, nil))}, nil
	}
	record, err := r.records.Get(ctx, siteID)
	if errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		record, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Spec.ServeDeployments {
		return map[string]any{ArtifactsVarName: upstreamValue(Effective(r.modules, nil))}, nil
	}
	view := buildView(siteID, integration, record, Effective(r.modules, record.Spec.Sources))
	if !view.Ready {
		return nil, fmt.Errorf("%w: %s", localrepodomain.ErrLocalRepositoryUnavailable, view.NotReadyMessage())
	}
	return map[string]any{ArtifactsVarName: localRepositoryValue(view)}, nil
}

// localRepositoryValue shapes a ready view's endpoints. The deb list keeps only the effective deb
// sources, in name order, so a host is never pointed at a source swallow does not manage; they are
// all signed with the Local Repository's own key, so none carries a key of its own.
func localRepositoryValue(view View) map[string]any {
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
		deb = append(deb, debEntry(source.Name, source.URI, source.Suites, source.Components, localrepodomain.SigningKey{}))
	}
	mirrors := map[string]any{}
	for host, mirror := range endpoints.RegistryMirrors {
		mirrors[host] = mirror
	}
	files := map[string]any{}
	for _, file := range view.Files {
		files[file.Path] = endpoints.Files + file.Path
	}
	images := map[string]any{}
	for _, image := range view.Images {
		images[image.Reference] = endpoints.Registry + "/" + image.Reference
	}
	return map[string]any{
		"source":           artifactsFromLocalRepository,
		"deb":              deb,
		"deb_signing_key":  endpoints.DebSigningKey,
		"registry":         endpoints.Registry,
		"registry_mirrors": mirrors,
		"python_index":     endpoints.PythonIndex,
		"files":            files,
		"images":           images,
	}
}

// upstreamValue points the hosts at what the modules declare: their deb sources at the declared URL
// with the declared signing key, each file at its URL (the role verifies the declared sha256), and
// each image at its own reference. The host keeps its OS package sources, so no OS source is
// listed, and pip and the container runtimes keep their defaults.
func upstreamValue(effective EffectiveSpec) map[string]any {
	deb := []any{}
	for _, source := range effective.Sources {
		if source.Format == localrepodomain.FormatDeb {
			deb = append(deb, debEntry(source.Name, source.URL, source.Suites, source.Components, source.SigningKey))
		}
	}
	files := map[string]any{}
	for _, file := range effective.Files {
		files[file.Path] = file.URL
	}
	images := map[string]any{}
	for _, image := range effective.Images {
		images[image.Reference] = image.Reference
	}
	return map[string]any{
		"source":           artifactsFromUpstream,
		"deb":              deb,
		"deb_signing_key":  "",
		"registry":         "",
		"registry_mirrors": map[string]any{},
		"python_index":     "",
		"files":            files,
		"images":           images,
	}
}

// debEntry is one apt source of the var's deb list; key is zero for a source the Local Repository
// signs.
func debEntry(name, uri string, suites, components []string, key localrepodomain.SigningKey) map[string]any {
	return map[string]any{
		"name": name, "uri": uri, "suites": nonNil(suites), "components": nonNil(components),
		"signing_key_url": key.URL, "signing_key_fingerprint": key.Fingerprint,
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return slices.Clone(values)
}
