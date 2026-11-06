package pulp

import (
	"context"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// Pulp's collections for python sources (the pulp_python plugin).
const (
	pythonRemotes       = "/remotes/python/python/"
	pythonRepositories  = "/repositories/python/python/"
	pythonDistributions = "/distributions/python/pypi/"
	pythonPackages      = "/content/python/packages/"
	pythonContentType   = "python.python"
)

// defaultPythonIndex is the upstream a python source without a URL caches.
const defaultPythonIndex = "https://pypi.org/"

// ensurePython converges the remote, repository, and distribution of a python source. The
// distribution names both the repository and the remote, which makes it a pull-through index:
// Pulp fetches a package from the upstream the first time a host asks for it and adds it to the
// repository. The index is served at /pypi/<name>/simple/ and is ready once the distribution
// exists; ContentCount is how many package files were pulled through so far.
func (r *Repository) ensurePython(ctx context.Context, source localrepodomain.Source) (localrepodomain.SourceStatus, error) {
	upstream := strings.TrimSpace(source.URL)
	if upstream == "" {
		upstream = defaultPythonIndex
	}
	if _, err := normalizeOrigin(upstream, "python source URL"); err != nil {
		return localrepodomain.SourceStatus{}, rejected("ensure source", "The python repository source URL must be an http or https URL without a user or password.")
	}
	if !strings.HasSuffix(upstream, "/") {
		upstream += "/"
	}

	remote, remoteStep, err := r.ensure(ctx, objectSpec{
		collection: pythonRemotes,
		name:       source.Name,
		want:       withDownloadTimeouts(map[string]any{"url": upstream, "policy": "on_demand"}),
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	repo, repoStep, err := r.ensure(ctx, objectSpec{
		collection: pythonRepositories,
		name:       source.Name,
		read:       []string{"latest_version_href"},
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	_, distributionStep, err := r.ensure(ctx, objectSpec{
		collection:  pythonDistributions,
		name:        source.Name,
		want:        map[string]any{"base_path": source.Name, "repository": repo.href(), "remote": remote.href()},
		asyncCreate: true,
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	count, err := r.api.contentCount(ctx, repo.str("latest_version_href"), pythonContentType)
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	status := sourceStatus(source, worst(remoteStep, repoStep, distributionStep))
	status.ContentCount = count
	return status, nil
}
