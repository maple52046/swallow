package pulp

import (
	"context"
	"net/url"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// listLimit is the page size of the lists the adapter reads whole (distributions, remotes,
// repositories of one kind). A Site's sources number in the tens, so one page holds them all.
const listLimit = "1000"

// contentPathPrefix is where Pulp's content app serves distributions beneath the content origin
// (pulpcore's CONTENT_PATH_PREFIX default).
const contentPathPrefix = "/pulp/content/"

// Endpoints reports the addresses hosts read the repository's content at, from what Pulp serves
// now: every deb distribution the adapter created (base path deb/<name>) with its remote's suites
// and components, the deb signing service's public key, the registry (the content origin's
// host:port) with one mirror per pull-through distribution, the python index of the python
// distribution (the one at base path "pypi" when there are several), and the file distribution's
// prefix. It lists a source whether or not its first sync has finished; the caller combines it with
// the sources' states. Every address is beneath the content origin, never the API endpoint.
func (r *Repository) Endpoints(ctx context.Context) (localrepodomain.HostEndpoints, error) {
	var endpoints localrepodomain.HostEndpoints

	signing, err := r.signingService(ctx, "public_key")
	if err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	if signing != nil {
		endpoints.DebSigningKey = signing.str("public_key")
	}

	debs, err := r.api.list(ctx, debDistributions, url.Values{
		"limit": {listLimit}, "ordering": {"name"}, "fields": {"name,base_path"},
	})
	if err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	if len(debs.Results) > 0 {
		remotes, err := r.api.list(ctx, debRemotes, url.Values{"limit": {listLimit}, "fields": {"name,distributions,components"}})
		if err != nil {
			return localrepodomain.HostEndpoints{}, err
		}
		byName := make(map[string]object, len(remotes.Results))
		for _, remote := range remotes.Results {
			byName[remote.str("name")] = remote
		}
		for _, distribution := range debs.Results {
			name := distribution.str("name")
			remote, ok := byName[name]
			if !ok || distribution.str("base_path") != debBasePath(name) {
				continue
			}
			endpoints.Deb = append(endpoints.Deb, localrepodomain.DebSource{
				Name:       name,
				URI:        r.contentOrigin + contentPathPrefix + debBasePath(name) + "/",
				Suites:     strings.Fields(remote.str("distributions")),
				Components: strings.Fields(remote.str("components")),
			})
		}
	}

	hosts, err := r.pullThroughHosts(ctx)
	if err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	if len(hosts) > 0 {
		endpoints.Registry = registryHost(r.contentOrigin)
		endpoints.RegistryMirrors = make(map[string]string, len(hosts))
		for _, host := range hosts {
			endpoints.RegistryMirrors[host] = r.contentOrigin + "/v2/" + host
		}
	}

	indexes, err := r.api.list(ctx, pythonDistributions, url.Values{
		"limit": {listLimit}, "ordering": {"name"}, "fields": {"name,base_path"},
	})
	if err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	if basePath := pythonIndexPath(indexes.Results); basePath != "" {
		endpoints.PythonIndex = r.contentOrigin + "/pypi/" + basePath + "/simple/"
	}

	files, err := r.api.find(ctx, fileDistributions, filesName, []string{"base_path"})
	if err != nil {
		return localrepodomain.HostEndpoints{}, err
	}
	if files != nil && files.str("base_path") != "" {
		endpoints.Files = r.contentOrigin + contentPathPrefix + files.str("base_path") + "/"
	}
	return endpoints, nil
}

// pythonIndexPath picks the base path hosts use as their one python index: "pypi" when a
// distribution serves it, otherwise the first by name.
func pythonIndexPath(distributions []object) string {
	for _, distribution := range distributions {
		if distribution.str("base_path") == defaultPythonBasePath {
			return defaultPythonBasePath
		}
	}
	if len(distributions) > 0 {
		return distributions[0].str("base_path")
	}
	return ""
}

// defaultPythonBasePath is the base path of the python source the modules' requirements use.
const defaultPythonBasePath = "pypi"

// registryHost is the host[:port] of the content origin, the registry name hosts prefix image
// references with; the registry API is served at its root.
func registryHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return parsed.Host
}
