package pulp

import (
	"context"
	"net/url"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// Pulp's collections for oci sources (the pulp_container plugin). A source is a pull-through
// remote and distribution; Pulp creates a container repository, remote, and distribution for every
// image path a client pulls through it, each named after that path ("docker.io/grafana/grafana").
const (
	pullThroughRemotes       = "/remotes/container/pull-through/"
	pullThroughDistributions = "/distributions/container/pull-through/"
	containerRemotes         = "/remotes/container/container/"
	containerRepositories    = "/repositories/container/container/"
	containerTags            = "/content/container/tags/"
	containerBlobs           = "/content/container/blobs/"
)

// pullThroughPathLabel is the label pulp_container keeps a pull-through distribution's registry
// path in; its base_path holds a generated value so that it does not overlap the distributions
// created beneath it.
const pullThroughPathLabel = "pulp_container.pull_through"

// registryAliases maps the host a registry is named by in image references to the host its
// registry API is served from. Docker Hub is the case that matters: references say docker.io, its
// API answers at registry-1.docker.io.
var registryAliases = map[string]string{
	"docker.io":       "registry-1.docker.io",
	"index.docker.io": "registry-1.docker.io",
}

// upstreamRegistryURL is the URL the pull-through remote of an oci source fetches from: the
// source's URL, or https://<name> when it has none, with a known alias host replaced.
func upstreamRegistryURL(source localrepodomain.Source) (string, error) {
	raw := strings.TrimSpace(source.URL)
	if raw == "" {
		raw = "https://" + source.Name
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	normalized, err := normalizeOrigin(raw, "oci source URL")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	if alias, ok := registryAliases[strings.ToLower(parsed.Host)]; ok {
		parsed.Host = alias
	}
	return parsed.String(), nil
}

// ensureOCI converges the pull-through remote and distribution of an oci source, whose name is the
// registry host hosts mirror (the distribution's registry path, so images read as
// <registry>/<host>/<path>), and the download timeouts of the remotes Pulp created beneath it. It is
// ready once the distribution exists and no update of those remotes is in flight; ContentCount is
// how many images were pulled through it so far. It is serving once the distribution exists and
// neither it nor the pull-through remote is being changed: the timeouts of the image remotes do not
// keep a pull from working.
func (r *Repository) ensureOCI(ctx context.Context, source localrepodomain.Source) (localrepodomain.SourceStatus, error) {
	upstream, err := upstreamRegistryURL(source)
	if err != nil {
		return localrepodomain.SourceStatus{}, rejected("ensure source", "The oci repository source URL must be an http or https registry URL without a user or password.")
	}
	remote, remoteStep, err := r.ensure(ctx, objectSpec{
		collection: pullThroughRemotes,
		name:       source.Name,
		want:       withDownloadTimeouts(map[string]any{"url": upstream}),
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	imageRemotesStep, err := r.ensureImageRemotes(ctx, source.Name)
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	distribution, distributionStep, err := r.ensure(ctx, objectSpec{
		collection:  pullThroughDistributions,
		name:        source.Name,
		want:        map[string]any{"remote": remote.href()},
		createOnly:  map[string]any{"base_path": source.Name},
		read:        []string{"distributions"},
		asyncCreate: true,
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	status := sourceStatus(source, worst(remoteStep, imageRemotesStep, distributionStep))
	status.Serving = pullingThrough(distribution, remoteStep, distributionStep)
	if distribution != nil {
		status.ContentCount = len(distribution.list("distributions"))
	}
	return status, nil
}

// ensureImageRemotes gives downloadTimeouts to the remote Pulp created for each image pulled through
// the oci source named name (named "<name>/<path>"), and reports the combined step of the updates.
// Pulp copies the pull-through remote's settings into an image's remote only when it creates it and
// fetches the image's blobs through that remote from then on, so a remote created before the
// pull-through remote had the timeouts would keep cutting long downloads at Pulp's default.
func (r *Repository) ensureImageRemotes(ctx context.Context, name string) (step, error) {
	found, err := r.api.list(ctx, containerRemotes, url.Values{
		"name__startswith": {name + "/"}, "limit": {listLimit}, "fields": {"pulp_href,name,total_timeout,sock_read_timeout"},
	})
	if err != nil {
		return step{}, err
	}
	combined := stepReady
	for _, remote := range found.Results {
		updated, err := r.update(ctx, remote, downloadTimeouts())
		if err != nil {
			return step{}, err
		}
		combined = worst(combined, updated)
	}
	return combined, nil
}

// registryPath is the path a pull-through distribution serves at: its label when pulp_container
// moved it there, its base path otherwise (distributions created before that move).
func registryPath(distribution object) string {
	if path := distribution.labels("pulp_labels")[pullThroughPathLabel]; path != "" {
		return path
	}
	return distribution.str("base_path")
}

// pullThroughHosts lists the registry paths of every pull-through distribution, which are the
// registry hosts of the oci sources.
func (r *Repository) pullThroughHosts(ctx context.Context) ([]string, error) {
	found, err := r.api.list(ctx, pullThroughDistributions, url.Values{
		"limit": {listLimit}, "ordering": {"name"}, "fields": {"name,base_path,pulp_labels"},
	})
	if err != nil {
		return nil, err
	}
	hosts := make([]string, 0, len(found.Results))
	for _, distribution := range found.Results {
		if path := registryPath(distribution); path != "" {
			hosts = append(hosts, path)
		}
	}
	return hosts, nil
}
