package pulp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// Pulp's collections for deb sources (the pulp_deb plugin's APT objects).
const (
	debRemotes       = "/remotes/deb/apt/"
	debRepositories  = "/repositories/deb/apt/"
	debPublications  = "/publications/deb/apt/"
	debDistributions = "/distributions/deb/apt/"
	debPackages      = "/content/deb/packages/"
	debContentType   = "deb.package"
)

// debSigningService is the name of the Pulp signing service that signs every deb publication's
// Release file. The installation registers it with the key whose public half hosts trust
// (HostEndpoints.DebSigningKey), so hosts verify the metadata instead of trusting it blindly.
const debSigningService = "swallow-deb"

// debBasePath is where the content app serves the deb source named name: hosts read it at
// <content origin>/pulp/content/deb/<name>/.
func debBasePath(name string) string {
	return "deb/" + name
}

// ensureDeb converges the remote, repository, and distribution of a deb source and reports its
// sync state (see EnsureSource).
//
// The repository publishes automatically: Pulp creates a signed, structured publication of every
// new version inside the sync task, and the distribution names the repository, so it serves the
// repository's newest publication without the adapter moving it after each sync. A publication is
// created explicitly only when the newest one is missing or was signed by another signing service
// (a repository created before automatic publishing, or whose signing changed).
func (r *Repository) ensureDeb(ctx context.Context, source localrepodomain.Source) (localrepodomain.SourceStatus, error) {
	if len(source.Suites) == 0 {
		return localrepodomain.SourceStatus{}, rejected("ensure source", "A deb repository source needs at least one suite.")
	}
	if _, err := normalizeOrigin(source.URL, "deb source URL"); err != nil {
		return localrepodomain.SourceStatus{}, rejected("ensure source", "The deb repository source URL must be an http or https URL without a user or password.")
	}

	signing, err := r.signingService(ctx, "pulp_href")
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	if signing == nil {
		return localrepodomain.SourceStatus{}, rejected("ensure source",
			"Pulp has no signing service named "+debSigningService+", which signs the deb metadata hosts verify; register it in the Pulp installation.")
	}

	remote, remoteStep, err := r.ensure(ctx, objectSpec{
		collection: debRemotes,
		name:       source.Name,
		want: withDownloadTimeouts(map[string]any{
			"url":           source.URL,
			"distributions": words(source.Suites),
			"components":    words(source.Components),
			"architectures": words(source.Architectures),
			"policy":        "on_demand",
		}),
		read: []string{"pulp_last_updated"},
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	repo, repoStep, err := r.ensure(ctx, objectSpec{
		collection: debRepositories,
		name:       source.Name,
		want:       map[string]any{"signing_service": signing.href(), "autopublish": true},
		read:       []string{"latest_version_href"},
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	_, distributionStep, err := r.ensure(ctx, objectSpec{
		collection:  debDistributions,
		name:        source.Name,
		want:        map[string]any{"base_path": debBasePath(source.Name), "repository": repo.href(), "publication": nil},
		asyncCreate: true,
	})
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}

	syncStep, lastSynced, err := r.debSync(ctx, remote, repo, signing.href(), remoteStep.Changed)
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}
	count, err := r.api.contentCount(ctx, repo.str("latest_version_href"), debContentType)
	if err != nil {
		return localrepodomain.SourceStatus{}, err
	}

	status := sourceStatus(source, worst(remoteStep, repoStep, distributionStep, syncStep))
	status.LastSyncedAt = lastSynced
	status.ContentCount = count
	return status, nil
}

// debSync decides the sync and publication step of a deb source from Pulp's task records and
// returns it with the time of the last successful sync. remoteChanged is set when this call (or an
// update still in flight) changed the remote, which needs a sync even when the last one is recent;
// signingHref is the signing service the newest publication must be signed by.
//
// Any task still holding the repository (a sync, a publication, an update) makes the source
// pending or syncing and starts nothing, so repeated calls never queue a second sync. A periodic
// resync therefore reports syncing while the previous publication is still served; LastSyncedAt
// tells a reader that content is available meanwhile.
func (r *Repository) debSync(ctx context.Context, remote, repo object, signingHref string, remoteChanged bool) (step, *time.Time, error) {
	repoHref := repo.href()
	busy, err := r.api.latestTask(ctx, url.Values{"reserved_resources": {repoHref}, "state__in": {incompleteStates}})
	if err != nil {
		return step{}, nil, err
	}
	syncFilter := url.Values{"reserved_resources": {repoHref}, "name__contains": {"synchronize"}}
	lastSync, err := r.api.latestTask(ctx, syncFilter)
	if err != nil {
		return step{}, nil, err
	}
	lastOK := lastSync
	if lastSync != nil && lastSync.State != taskCompleted {
		okFilter := url.Values{"state": {taskCompleted}}
		for key, values := range syncFilter {
			okFilter[key] = values
		}
		if lastOK, err = r.api.latestTask(ctx, okFilter); err != nil {
			return step{}, nil, err
		}
	}
	var lastSynced *time.Time
	if lastOK != nil {
		lastSynced = lastOK.FinishedAt
	}

	if busy != nil {
		return step{State: busy.syncState()}, lastSynced, nil
	}

	now := r.now()
	needSync := remoteChanged
	switch {
	case lastSync == nil:
		needSync = true
	case lastSync.failedRecently(now):
		return step{State: localrepodomain.SyncFailed, Message: lastSync.reason()}, lastSynced, nil
	case lastSync.State != taskCompleted:
		// Failed long enough ago, canceled, or skipped: try again.
		needSync = true
	case lastSync.FinishedAt == nil || now.Sub(*lastSync.FinishedAt) > resyncInterval:
		needSync = true
	case remoteUpdatedAfter(remote, lastSync):
		// The remote changed after the last sync started, for example by an update a call sent
		// before it failed to start the sync.
		needSync = true
	}
	if needSync {
		// A mirror sync replaces the repository's content with the upstream's. An additive one keeps
		// the previous Release and Packages indices beside the new ones at the same paths, which Pulp
		// refuses as duplicated paths once the upstream's metadata has changed (every resync of an
		// archive that publishes updates).
		if _, err := r.api.send(ctx, http.MethodPost, repoHref+"sync/",
			map[string]any{"remote": remote.href(), "mirror": true}, "", nil); err != nil {
			return step{}, nil, err
		}
		return step{State: localrepodomain.SyncPending}, lastSynced, nil
	}

	published, err := r.ensurePublished(ctx, publicationSpec{
		collection: debPublications,
		body:       map[string]any{"structured": true, "simple": false, "signing_service": signingHref},
		current: func(publication object) bool {
			return publication.str("signing_service") == signingHref
		},
	}, repo)
	return published, lastSynced, err
}

// remoteUpdatedAfter reports whether the remote was last updated after the sync started.
func remoteUpdatedAfter(remote object, sync *task) bool {
	updated := remote.timestamp("pulp_last_updated")
	return updated != nil && sync.StartedAt != nil && updated.After(*sync.StartedAt)
}

// signingService returns Pulp's deb signing service with fields, or nil when it is not registered.
func (r *Repository) signingService(ctx context.Context, fields ...string) (object, error) {
	return r.api.find(ctx, "/signing-services/", debSigningService, fields)
}

// debPackageItem maps a Pulp deb package to a content item of the source it was synced from.
func debPackageItem(source string, pkg object) localrepodomain.ContentItem {
	return localrepodomain.ContentItem{
		Name:         pkg.str("package"),
		Version:      pkg.str("version"),
		Architecture: pkg.str("architecture"),
		Source:       source,
		Path:         strings.TrimPrefix(pkg.str("relative_path"), "/"),
	}
}
