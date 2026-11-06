package pulp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// Pulp's collections for files (pulp_file, part of pulpcore). Every File lives in one repository
// and distribution named filesName, served at <content origin>/pulp/content/files/<path>.
const (
	fileRepositories  = "/repositories/file/file/"
	filePublications  = "/publications/file/file/"
	fileDistributions = "/distributions/file/file/"
	fileContent       = "/content/file/files/"
	filesName         = "files"
)

// filePublication publishes the files repository; any publication of its newest version serves.
var filePublication = publicationSpec{
	collection: filePublications,
	current:    func(object) bool { return true },
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// EnsureFile serves file from Pulp's file repository and reports its state; it never waits for a
// download.
//
// The repository publishes every new version automatically, and file distributions only serve
// publications, so the distribution names the repository and serves its newest publication; a file
// is ready only once that publication holds it (a repository filled before automatic publishing was
// set is published once). A
// file is fetched by asking Pulp to download its URL into the repository; Pulp cannot be told the
// checksum beforehand, so the adapter compares the downloaded sha256 with the declared one, removes
// a mismatching file from the repository, and reports it failed, fetching it again after
// retryInterval. Content Pulp already holds with the declared path and checksum is added instead of
// downloaded again. Pulp holds the repository exclusively for each download, so only one file
// downloads at a time and the others report pending meanwhile.
func (r *Repository) EnsureFile(ctx context.Context, file localrepodomain.File) (localrepodomain.FileStatus, error) {
	file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
	if err := validateFile(file); err != nil {
		return localrepodomain.FileStatus{}, err
	}
	repo, repoStep, err := r.ensure(ctx, objectSpec{
		collection: fileRepositories,
		name:       filesName,
		want:       map[string]any{"autopublish": true},
		read:       []string{"latest_version_href"},
	})
	if err != nil {
		return localrepodomain.FileStatus{}, err
	}
	_, distributionStep, err := r.ensure(ctx, objectSpec{
		collection:  fileDistributions,
		name:        filesName,
		want:        map[string]any{"base_path": filesName, "repository": repo.href(), "publication": nil},
		asyncCreate: true,
	})
	if err != nil {
		return localrepodomain.FileStatus{}, err
	}
	fetch, size, err := r.fetchFile(ctx, repo, file)
	if err != nil {
		return localrepodomain.FileStatus{}, err
	}
	combined := worst(repoStep, distributionStep, fetch)
	return localrepodomain.FileStatus{
		Path:      file.Path,
		State:     combined.State,
		Message:   redact.Text(combined.Message),
		SizeBytes: size,
	}, nil
}

// fetchFile decides the step of one file from what the repository's newest version holds at its
// path and from the newest task its download request was correlated with, and returns the file's
// size once it is served.
func (r *Repository) fetchFile(ctx context.Context, repo object, file localrepodomain.File) (step, int64, error) {
	cid := correlationID("file", file.Path, file.URL, file.SHA256)
	last, err := r.api.latestTask(ctx, url.Values{"logging_cid": {cid}})
	if err != nil {
		return step{}, 0, err
	}
	held, err := r.fileAt(ctx, url.Values{"repository_version": {repo.str("latest_version_href")}, "relative_path": {file.Path}})
	if err != nil {
		return step{}, 0, err
	}
	if held != nil {
		heldSHA := strings.ToLower(held.str("sha256"))
		if heldSHA == file.SHA256 {
			published, err := r.ensurePublished(ctx, filePublication, repo)
			if err != nil {
				return step{}, 0, err
			}
			size, err := r.artifactSize(ctx, held.str("artifact"))
			return published, size, err
		}
		message := fmt.Sprintf("Pulp held %s with sha256 %s, not the declared %s; it is removed from the repository and fetched again.",
			file.Path, heldSHA, file.SHA256)
		if last != nil && last.State == taskCompleted && slices.Contains(last.CreatedResources, held.href()) {
			message = fmt.Sprintf("The file downloaded from %s has sha256 %s, not the declared %s; it is removed from the repository and fetched again after %s.",
				file.URL, heldSHA, file.SHA256, retryInterval)
		}
		removal, err := r.dispatch(ctx, correlationID("remove", repo.href(), held.href()), http.MethodPost, repo.href()+"modify/",
			map[string]any{"remove_content_units": []string{held.href()}})
		if err != nil {
			return step{}, 0, err
		}
		if removal.State == localrepodomain.SyncFailed {
			message += " Removing it failed: " + removal.Message
		}
		return step{State: localrepodomain.SyncFailed, Message: message}, 0, nil
	}

	now := r.now()
	downloadedRecently := false
	if last != nil {
		switch {
		case last.incomplete():
			return step{State: last.syncState()}, 0, nil
		case last.failedRecently(now):
			return step{State: localrepodomain.SyncFailed, Message: last.reason()}, 0, nil
		case last.State == taskCompleted && last.FinishedAt != nil && now.Sub(*last.FinishedAt) < retryInterval:
			downloadedRecently = true
		}
	}

	known, err := r.fileAt(ctx, url.Values{"relative_path": {file.Path}, "sha256": {file.SHA256}})
	if err != nil {
		return step{}, 0, err
	}
	if known != nil {
		added, err := r.dispatch(ctx, correlationID("add", repo.href(), known.href()), http.MethodPost, repo.href()+"modify/",
			map[string]any{"add_content_units": []string{known.href()}})
		return added, 0, err
	}
	if downloadedRecently {
		// The download finished, and what it fetched was removed above for not matching.
		return step{State: localrepodomain.SyncFailed, Message: fmt.Sprintf(
			"The file downloaded from %s did not match the declared sha256 %s; it is fetched again after %s.",
			file.URL, file.SHA256, retryInterval)}, 0, nil
	}

	busy, err := r.api.latestTask(ctx, url.Values{"reserved_resources": {repo.href()}, "state__in": {incompleteStates}})
	if err != nil {
		return step{}, 0, err
	}
	if busy != nil {
		return step{State: localrepodomain.SyncPending}, 0, nil
	}
	// A download by file_url gets none of a remote's settings; without its own downloader_config it
	// would be cut at Pulp's default total timeout.
	if _, err := r.api.send(ctx, http.MethodPost, api(fileContent), map[string]any{
		"file_url": file.URL, "relative_path": file.Path, "repository": repo.href(), "downloader_config": downloadTimeouts(),
	}, cid, nil); err != nil {
		return step{}, 0, err
	}
	return step{State: localrepodomain.SyncPending}, 0, nil
}

// fileAt returns the first file content matching filter, with the fields the adapter compares.
func (r *Repository) fileAt(ctx context.Context, filter url.Values) (object, error) {
	query := url.Values{"limit": {"1"}, "fields": {"pulp_href,relative_path,sha256,artifact"}}
	for key, values := range filter {
		query[key] = values
	}
	found, err := r.api.list(ctx, fileContent, query)
	if err != nil {
		return nil, err
	}
	if len(found.Results) == 0 {
		return nil, nil
	}
	return found.Results[0], nil
}

// artifactSize returns the size in bytes of the artifact at href, or 0 when there is none.
func (r *Repository) artifactSize(ctx context.Context, href string) (int64, error) {
	if href == "" {
		return 0, nil
	}
	var artifact object
	if err := r.api.get(ctx, href, url.Values{"fields": {"size"}}, &artifact); err != nil {
		return 0, err
	}
	return artifact.number("size"), nil
}

// validateFile refuses a file Pulp could not serve at its path or verify: the path must be relative
// without "." or ".." segments, the URL http or https, and the checksum 64 lower-case hex digits.
func validateFile(file localrepodomain.File) error {
	path := file.Path
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.ContainsAny(path, "\\ \t\r\n") {
		return rejected("ensure file", fmt.Sprintf("The file path %q must be a relative path without spaces.", path))
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return rejected("ensure file", fmt.Sprintf("The file path %q must not hold empty, '.', or '..' segments.", path))
		}
	}
	// A query is allowed: download URLs of release assets often carry one.
	parsed, err := url.Parse(file.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return rejected("ensure file", fmt.Sprintf("The URL of file %s must be an http or https URL without a user or password.", path))
	}
	if !sha256Hex.MatchString(file.SHA256) {
		return rejected("ensure file", fmt.Sprintf("The sha256 of file %s must be 64 hexadecimal digits.", path))
	}
	return nil
}
