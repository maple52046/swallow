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

// The values of the var's "source" key, the Site's mode at acceptance: every artifact from the Local
// Repository (offline mode), every artifact from the upstreams, or each from where cache mode
// decided. The first two are also the values of its "apt" key, where apt fetches from.
const (
	artifactsFromLocalRepository = "local_repository"
	artifactsFromUpstream        = "upstream"
	artifactsFromCache           = "cache"
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
//	source:           "local_repository", "upstream", or "cache"
//	apt:              where apt fetches from, "local_repository" or "upstream"
//	deb:              [{name, uri, suites, components, signing_key_url, signing_key_fingerprint}], by name
//	deb_signing_key:  the armored key the Local Repository signs its deb metadata with while apt
//	                  fetches from it, "" otherwise
//	registry:         the Local Repository's registry "host:port" while an image or mirror goes
//	                  through it, "" otherwise
//	registry_mirrors: {upstream registry host: mirror URL} for the hosts that go through it
//	python_index:     the Local Repository's Python simple index URL while Python packages come
//	                  from it, "" otherwise
//	files:            {declared path: the URL to fetch it from}
//	images:           {declared reference: the reference to pull}
//
// The spec's mode decides. In offline mode the Site gets "local_repository": every effective deb
// source at the provider's address and every file and image through the provider. In cache mode it
// gets "cache": what the recorded status says the provider serves goes through it, the rest comes
// from the modules' declarations (see cacheValue). A Site in either mode whose Local Repository is
// not ready (View.Ready) is ErrLocalRepositoryUnavailable, wrapped with what to enable, fix, or wait
// for; neither mode falls back to the upstreams for what the Site chose to fetch from the Local
// Repository. A Site in upstream mode, one without a repository Integration included, gets
// "upstream": the modules' own declarations, so the user's sources play no part. Every key is
// present and a kind nothing serves is empty. Store failures pass through.
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
	if record == nil || !record.Spec.Mode.Converges() {
		return map[string]any{ArtifactsVarName: upstreamValue(Effective(r.modules, nil))}, nil
	}
	view := buildView(siteID, integration, record, Effective(r.modules, record.Spec.Sources))
	if !view.Ready {
		return nil, fmt.Errorf("%w: %s", localrepodomain.ErrLocalRepositoryUnavailable, view.NotReadyMessage())
	}
	if view.Spec.Mode == localrepodomain.ModeCache {
		return map[string]any{ArtifactsVarName: cacheValue(view, Effective(r.modules, nil))}, nil
	}
	return map[string]any{ArtifactsVarName: localRepositoryValue(view)}, nil
}

// localRepositoryValue shapes a ready offline view's endpoints: every artifact through the Local
// Repository.
func localRepositoryValue(view View) map[string]any {
	endpoints := view.Endpoints
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
		"apt":              artifactsFromLocalRepository,
		"deb":              localDebSources(view),
		"deb_signing_key":  endpoints.DebSigningKey,
		"registry":         endpoints.Registry,
		"registry_mirrors": mirrors,
		"python_index":     endpoints.PythonIndex,
		"files":            files,
		"images":           images,
	}
}

// localDebSources lists the effective deb sources at the provider's address, in name order. Only the
// effective ones are kept, so a host is never pointed at a source swallow does not manage; they are
// all signed with the Local Repository's own key, so none carries a key of its own.
func localDebSources(view View) []any {
	wanted := map[string]bool{}
	for _, source := range view.Sources {
		if source.Format == localrepodomain.FormatDeb {
			wanted[source.Name] = true
		}
	}
	deb := []any{}
	served := slices.Clone(view.Endpoints.Deb)
	slices.SortFunc(served, func(a, b localrepodomain.DebSource) int { return strings.Compare(a.Name, b.Name) })
	for _, source := range served {
		if !wanted[source.Name] {
			continue
		}
		deb = append(deb, debEntry(source.Name, source.URI, source.Suites, source.Components, localrepodomain.SigningKey{}))
	}
	return deb
}

// cacheValue shapes a ready cache view: each artifact goes through the Local Repository when the
// recorded status says the provider serves it, and otherwise comes from what the modules declare
// (upstream, the modules' declarations alone, as upstreamValue uses them).
//
//   - apt as a whole: from the Local Repository when there is an effective deb source and every one
//     is serving; otherwise from the upstreams, keeping the host's own OS sources. It is not mixed
//     per source: a user source has no signing key to verify its upstream with, and the host's own
//     sources may be disabled only once the Site's OS archive is served.
//   - Python packages: from the Local Repository's index while a python source is serving.
//   - images: through the Local Repository while the oci source of their registry host is serving,
//     whether or not the image was fetched in advance (the provider pulls it through); the mirrors
//     list only those hosts, and registry is set only while something goes through it.
//   - files: from the Local Repository once it holds them (ready), otherwise from their URLs; a host
//     verifies the declared sha256 either way.
func cacheValue(view View, upstream EffectiveSpec) map[string]any {
	endpoints := view.Endpoints
	value := upstreamValue(upstream)
	value["source"] = artifactsFromCache

	debSources, debServing, pythonServing := 0, 0, false
	ociServing := map[string]bool{}
	for _, source := range view.Sources {
		switch source.Format {
		case localrepodomain.FormatDeb:
			debSources++
			if source.Serving {
				debServing++
			}
		case localrepodomain.FormatPython:
			pythonServing = pythonServing || source.Serving
		case localrepodomain.FormatOCI:
			ociServing[source.Name] = source.Serving
		}
	}
	if debSources > 0 && debServing == debSources {
		value["apt"], value["deb"], value["deb_signing_key"] = artifactsFromLocalRepository, localDebSources(view), endpoints.DebSigningKey
	}
	if pythonServing && endpoints.PythonIndex != "" {
		value["python_index"] = endpoints.PythonIndex
	}

	mirrors := map[string]any{}
	for host, mirror := range endpoints.RegistryMirrors {
		if ociServing[host] {
			mirrors[host] = mirror
		}
	}
	images := map[string]any{}
	throughRegistry := len(mirrors) > 0
	for _, image := range view.Images {
		images[image.Reference] = image.Reference
		if host, ok := RegistryHost(image.Reference); ok && ociServing[host] && endpoints.Registry != "" {
			images[image.Reference] = endpoints.Registry + "/" + image.Reference
			throughRegistry = true
		}
	}
	value["registry_mirrors"], value["images"] = mirrors, images
	if throughRegistry {
		value["registry"] = endpoints.Registry
	}

	files := map[string]any{}
	for _, file := range view.Files {
		files[file.Path] = file.URL
		if file.State == localrepodomain.SyncReady && endpoints.Files != "" {
			files[file.Path] = endpoints.Files + file.Path
		}
	}
	value["files"] = files
	return value
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
		"apt":              artifactsFromUpstream,
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
