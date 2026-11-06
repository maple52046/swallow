package pulp

import (
	"context"
	"net/url"
	"path"
	"strconv"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// maxTagsPerImage bounds the tags read for one image row; a pull-through repository holds the tags
// hosts pulled, a handful.
const maxTagsPerImage = "100"

// partition is one source's share of a content query: its name and a function that reads a page
// of its items with Pulp's count of all its matching items. A limit of 0 asks only for the count.
type partition struct {
	source string
	fetch  func(ctx context.Context, offset, limit int) ([]localrepodomain.ContentItem, int, error)
}

// pageAcross reads the page [offset, offset+limit) of the partitions laid end to end, in order, with
// one request per partition; a partition after the page is read only for its count. Total is the
// sum of the counts.
func pageAcross(ctx context.Context, partitions []partition, offset, limit int) (localrepodomain.ContentPage, error) {
	var out localrepodomain.ContentPage
	skip := offset
	for _, p := range partitions {
		need := max(limit-len(out.Items), 0)
		items, count, err := p.fetch(ctx, skip, need)
		if err != nil {
			return localrepodomain.ContentPage{}, err
		}
		out.Items = append(out.Items, items[:min(len(items), need)]...)
		out.Total += count
		skip = max(skip-count, 0)
	}
	return out, nil
}

// pageQuery is the paging part of a Pulp list query. Pulp has no count-only list, so a limit of 0
// reads one item, which the caller drops.
func pageQuery(offset, limit int, ordering, fields string) url.Values {
	return url.Values{
		"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(max(limit, 1))},
		"ordering": {ordering}, "fields": {fields},
	}
}

// repositoryPartitions lists the repositories of collection (one per source of the format, named
// after it), only the one named source when it is set, as partitions whose items fetch reads from
// the repository's newest version.
func (r *Repository) repositoryPartitions(
	ctx context.Context,
	collection, source string,
	fetch func(ctx context.Context, source, version string, offset, limit int) ([]localrepodomain.ContentItem, int, error),
) ([]partition, error) {
	query := url.Values{"limit": {listLimit}, "ordering": {"name"}, "fields": {"name,latest_version_href"}}
	if source != "" {
		query.Set("name", source)
	}
	repos, err := r.api.list(ctx, collection, query)
	if err != nil {
		return nil, err
	}
	partitions := make([]partition, 0, len(repos.Results))
	for _, repo := range repos.Results {
		name, version := repo.str("name"), repo.str("latest_version_href")
		if version == "" {
			continue
		}
		partitions = append(partitions, partition{source: name, fetch: func(ctx context.Context, offset, limit int) ([]localrepodomain.ContentItem, int, error) {
			return fetch(ctx, name, version, offset, limit)
		}})
	}
	return partitions, nil
}

// browseDeb pages through the packages of the deb sources, matching search against the package
// name case-insensitively. A deb package's download size is not known before a host fetches it (the
// sync is on demand), so SizeBytes stays 0.
func (r *Repository) browseDeb(ctx context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	partitions, err := r.repositoryPartitions(ctx, debRepositories, query.Source,
		func(ctx context.Context, source, version string, offset, limit int) ([]localrepodomain.ContentItem, int, error) {
			q := pageQuery(offset, limit, "package,version", "package,version,architecture,relative_path")
			q.Set("repository_version", version)
			if query.Search != "" {
				q.Set("package__icontains", query.Search)
			}
			found, err := r.api.list(ctx, debPackages, q)
			if err != nil {
				return nil, 0, err
			}
			items := make([]localrepodomain.ContentItem, 0, len(found.Results))
			for _, pkg := range found.Results {
				items = append(items, debPackageItem(source, pkg))
			}
			return items, found.Count, nil
		})
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	return pageAcross(ctx, partitions, query.Offset, query.Limit)
}

// browsePython pages through the package files pulled through the python sources. pulp_python
// matches a name only case-sensitively, so search is passed as it is.
func (r *Repository) browsePython(ctx context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	partitions, err := r.repositoryPartitions(ctx, pythonRepositories, query.Source,
		func(ctx context.Context, source, version string, offset, limit int) ([]localrepodomain.ContentItem, int, error) {
			q := pageQuery(offset, limit, "name,version", "name,version,filename,size")
			q.Set("repository_version", version)
			if query.Search != "" {
				q.Set("name__contains", query.Search)
			}
			found, err := r.api.list(ctx, pythonPackages, q)
			if err != nil {
				return nil, 0, err
			}
			items := make([]localrepodomain.ContentItem, 0, len(found.Results))
			for _, pkg := range found.Results {
				items = append(items, localrepodomain.ContentItem{
					Name: pkg.str("name"), Version: pkg.str("version"), Source: source,
					Path: pkg.str("filename"), SizeBytes: pkg.number("size"),
				})
			}
			return items, found.Count, nil
		})
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	return pageAcross(ctx, partitions, query.Offset, query.Limit)
}

// browseOCI pages through the images pulled through the oci sources, one item per image
// repository: Name is the image path at its registry, Path the path hosts pull it at beneath the
// registry (<host>/<image>), Version its cached tags, comma-separated. search matches the path
// case-insensitively.
func (r *Repository) browseOCI(ctx context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	hosts, err := r.pullThroughHosts(ctx)
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	partitions := make([]partition, 0, len(hosts))
	for _, host := range hosts {
		if query.Source != "" && host != query.Source {
			continue
		}
		partitions = append(partitions, partition{source: host, fetch: func(ctx context.Context, offset, limit int) ([]localrepodomain.ContentItem, int, error) {
			q := pageQuery(offset, limit, "name", "name,latest_version_href")
			q.Set("name__startswith", host+"/")
			if query.Search != "" {
				q.Set("name__icontains", query.Search)
			}
			found, err := r.api.list(ctx, containerRepositories, q)
			if err != nil || limit == 0 {
				return nil, found.Count, err
			}
			items := make([]localrepodomain.ContentItem, 0, len(found.Results))
			for _, repo := range found.Results {
				tags, err := r.imageTags(ctx, repo.str("latest_version_href"))
				if err != nil {
					return nil, 0, err
				}
				items = append(items, localrepodomain.ContentItem{
					Name: strings.TrimPrefix(repo.str("name"), host+"/"), Version: strings.Join(tags, ", "),
					Source: host, Path: repo.str("name"),
				})
			}
			return items, found.Count, nil
		}})
	}
	return pageAcross(ctx, partitions, query.Offset, query.Limit)
}

// imageTags lists the tag names in a container repository version.
func (r *Repository) imageTags(ctx context.Context, version string) ([]string, error) {
	if version == "" {
		return nil, nil
	}
	found, err := r.api.list(ctx, containerTags, url.Values{
		"repository_version": {version}, "limit": {maxTagsPerImage}, "ordering": {"name"}, "fields": {"name"},
	})
	if err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(found.Results))
	for _, tag := range found.Results {
		tags = append(tags, tag.str("name"))
	}
	return tags, nil
}

// browseFiles pages through the files repository's newest version, matching search against the
// path case-insensitively. Files belong to no source, so a query for a source finds nothing.
func (r *Repository) browseFiles(ctx context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	if query.Source != "" {
		return localrepodomain.ContentPage{}, nil
	}
	repo, err := r.api.find(ctx, fileRepositories, filesName, []string{"latest_version_href"})
	if err != nil || repo == nil || repo.str("latest_version_href") == "" {
		return localrepodomain.ContentPage{}, err
	}
	q := pageQuery(query.Offset, query.Limit, "relative_path", "relative_path,artifact")
	q.Set("repository_version", repo.str("latest_version_href"))
	if query.Search != "" {
		q.Set("relative_path__icontains", query.Search)
	}
	found, err := r.api.list(ctx, fileContent, q)
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	sizes, err := r.artifactSizes(ctx, found.Results)
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	out := localrepodomain.ContentPage{Total: found.Count, Items: make([]localrepodomain.ContentItem, 0, len(found.Results))}
	for _, file := range found.Results {
		relative := file.str("relative_path")
		out.Items = append(out.Items, localrepodomain.ContentItem{
			Name: path.Base(relative), Path: relative, SizeBytes: sizes[file.str("artifact")],
		})
	}
	return out, nil
}

// artifactSizes reads the sizes of the artifacts of contents in one request, keyed by artifact href.
func (r *Repository) artifactSizes(ctx context.Context, contents []object) (map[string]int64, error) {
	hrefs := make([]string, 0, len(contents))
	for _, content := range contents {
		if href := content.str("artifact"); href != "" {
			hrefs = append(hrefs, href)
		}
	}
	if len(hrefs) == 0 {
		return nil, nil
	}
	found, err := r.api.list(ctx, "/artifacts/", url.Values{
		"pulp_href__in": {strings.Join(hrefs, ",")}, "limit": {strconv.Itoa(len(hrefs))}, "fields": {"pulp_href,size"},
	})
	if err != nil {
		return nil, err
	}
	sizes := make(map[string]int64, len(found.Results))
	for _, artifact := range found.Results {
		sizes[artifact.href()] = artifact.number("size")
	}
	return sizes, nil
}
