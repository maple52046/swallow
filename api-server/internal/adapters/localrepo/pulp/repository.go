package pulp

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// resyncInterval is how old the last successful sync of a deb source may be before it is synced
// again. An on_demand sync fetches only the upstream's metadata, so a daily sync keeps security
// updates visible to hosts at little cost.
const resyncInterval = 24 * time.Hour

// retryInterval is how long a failed Pulp task (a sync, a file download, a create) stands before the
// adapter asks for it again. It keeps a source whose upstream is down from being retried, and a file
// whose download does not match its checksum from being downloaded again, on every reconcile, while
// still recovering from a transient failure without a person.
const retryInterval = 10 * time.Minute

// Bounds of every download made for the adapter: Pulp's, through the remotes the adapter ensures and
// the file downloads it asks for, and the adapter's own image downloads through Pulp's registry.
// Pulp applies a remote's total_timeout to each download and, while it is unset, falls back to its
// HTTP client's default of 300 seconds (0 does not disable it), so on a slow uplink every package,
// layer, or file that takes longer was cut and fetched again from the start, forever.
const (
	// downloadTimeout bounds one download as a whole (Pulp's total_timeout): a multi-gigabyte layer
	// or file may take hours on a slow uplink, but a download must end.
	downloadTimeout = 6 * time.Hour
	// downloadStall ends a download that received no byte for this long (Pulp's sock_read_timeout):
	// the upstream stopped answering, and holding the connection open helps nobody.
	downloadStall = 5 * time.Minute
)

// downloadTimeouts are downloadTimeout and downloadStall as the fields of a Pulp remote, or of the
// downloader_config of a file download: seconds, which Pulp stores as floats.
func downloadTimeouts() map[string]any {
	return map[string]any{"total_timeout": downloadTimeout.Seconds(), "sock_read_timeout": downloadStall.Seconds()}
}

// withDownloadTimeouts adds downloadTimeouts to the fields a remote must have.
func withDownloadTimeouts(fields map[string]any) map[string]any {
	maps.Copy(fields, downloadTimeouts())
	return fields
}

// Repository is the Local Repository of one Site served by one Pulp instance. It implements
// localrepodomain.Repository and holds its connection settings and the process's image downloads,
// which it shares with every Repository of its factory; it is safe for concurrent use. Every other
// state it reports is read from Pulp on the call.
type Repository struct {
	api *client
	// contentOrigin is the origin hosts read content from (the Integration's contentOrigin setting),
	// without a trailing slash; it differs from the API endpoint when swallow reaches Pulp on an
	// internal network.
	contentOrigin string
	// registry pulls image manifests and blobs through contentOrigin. It has no overall timeout,
	// because a layer may take long on a slow uplink; the image download bounds each pull itself.
	registry *http.Client
	// downloads are this process's image downloads, shared by the factory's Repositories.
	downloads *imageDownloads
	now       func() time.Time
}

var _ localrepodomain.Repository = (*Repository)(nil)

// EnsureSource serves source from Pulp: it creates or updates the Pulp objects that mirror or
// cache it, starts a sync when one is new, changed, due, or retried, and reports the source's state.
// It never waits for a sync.
//
// A deb source is a remote that syncs on demand into a signed repository Pulp publishes on every new
// version, distributed at deb/<name>; it is ready once a sync completed and the newest publication,
// signed by the deb signing service, holds the newest version, and it is synced again when its last
// successful sync is older than a day or its remote changed. An oci source (named after the
// registry host) is a pull-through cache, ready once its distribution exists. A python source is a
// pull-through index distributed at <name>, ready the same way. rpm sources are not served yet and
// are rejected. Every remote a source has carries downloadTimeouts, and an existing remote without
// them is updated; for an oci source that includes the remote Pulp created for each image pulled
// through it.
//
// Every Pulp object of a source is named after it, and Pulp names are unique per object type across
// plugins, so two sources of different formats cannot share a name, and no source can be named
// "files" (the file repository). Pulp refuses such a source: a rejected error, or a failed state
// when the refusal comes from one of Pulp's tasks.
//
// A failed sync is reported as failed with Pulp's reason and retried after retryInterval. Errors are
// for calls that could not be made: Pulp unreachable or refusing the credential, Pulp refusing the
// request (a URL it cannot use), or a malformed source.
func (r *Repository) EnsureSource(ctx context.Context, source localrepodomain.Source) (localrepodomain.SourceStatus, error) {
	if !validName.MatchString(source.Name) {
		return localrepodomain.SourceStatus{}, rejected("ensure source",
			fmt.Sprintf("The repository source name %q must start with a letter or digit and hold only letters, digits, '.', '_', and '-'.", source.Name))
	}
	switch source.Format {
	case localrepodomain.FormatDeb:
		return r.ensureDeb(ctx, source)
	case localrepodomain.FormatOCI:
		return r.ensureOCI(ctx, source)
	case localrepodomain.FormatPython:
		return r.ensurePython(ctx, source)
	case localrepodomain.FormatRPM:
		return localrepodomain.SourceStatus{}, rejected("ensure source", "The Pulp repository does not serve rpm sources yet.")
	default:
		return localrepodomain.SourceStatus{}, rejected("ensure source", fmt.Sprintf("Repository sources of format %q cannot be served by Pulp.", source.Format))
	}
}

// RemoveSource deletes the Pulp objects that serve the source named name, cancelling the tasks
// still running on them, and waits (bounded by removeWait) until Pulp has deleted them. For an oci
// source this includes the repository, remote, and distribution Pulp created for every image pulled
// through it, which would otherwise keep serving and pulling those images. A source that does not
// exist is not an error; a file is not a source, so FormatFile is rejected.
func (r *Repository) RemoveSource(ctx context.Context, name string, format localrepodomain.Format) error {
	switch format {
	case localrepodomain.FormatDeb:
		return r.removeObjects(ctx, name, debDistributions, debRepositories, debRemotes)
	case localrepodomain.FormatPython:
		return r.removeObjects(ctx, name, pythonDistributions, pythonRepositories, pythonRemotes)
	case localrepodomain.FormatOCI:
		return r.removeOCI(ctx, name)
	case localrepodomain.FormatRPM:
		// Nothing can exist: EnsureSource never created an rpm source.
		return nil
	default:
		return rejected("remove source", fmt.Sprintf("Repository sources of format %q are not served by Pulp.", format))
	}
}

// Browse reads one page of what the repository serves of query.Format, newest Pulp state, filtered
// by source name and by a case-insensitive name search where Pulp supports one (Python package names
// match case-sensitively). Items carry what the format knows: deb packages their name, version,
// architecture, and pool path; Python packages their name, version, file name, and size; images
// their repository path and cached tags (Version, comma-separated); files their path and size.
// Total counts every item matching the query.
func (r *Repository) Browse(ctx context.Context, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	if query.Limit <= 0 || query.Offset < 0 {
		return localrepodomain.ContentPage{}, rejected("browse", "A content page needs a positive limit and a non-negative offset.")
	}
	switch query.Format {
	case localrepodomain.FormatDeb:
		return r.browseDeb(ctx, query)
	case localrepodomain.FormatOCI:
		return r.browseOCI(ctx, query)
	case localrepodomain.FormatPython:
		return r.browsePython(ctx, query)
	case localrepodomain.FormatFile:
		return r.browseFiles(ctx, query)
	case localrepodomain.FormatRPM:
		return localrepodomain.ContentPage{}, rejected("browse", "The Pulp repository does not serve rpm sources yet.")
	default:
		return localrepodomain.ContentPage{}, rejected("browse", fmt.Sprintf("Content of format %q is not served by Pulp.", query.Format))
	}
}

// validName is what a source name may be. It becomes a Pulp object name and part of a base path
// hosts read, so it holds no path separator or space.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// sourceStatus turns the combined step of a source into its status. Messages come from Pulp's task
// errors, which may quote an upstream URL, so they are redacted.
func sourceStatus(source localrepodomain.Source, s step) localrepodomain.SourceStatus {
	return localrepodomain.SourceStatus{
		Name:    source.Name,
		Format:  source.Format,
		State:   s.State,
		Message: redact.Text(s.Message),
	}
}
