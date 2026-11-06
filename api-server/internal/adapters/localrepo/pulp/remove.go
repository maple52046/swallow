package pulp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// removeWait bounds how long RemoveSource waits for Pulp to finish its deletions. Deletions run as
// tasks that wait for any task still holding the object; past this bound RemoveSource reports the
// removal unavailable, so its caller tries again rather than believing the source gone.
const removeWait = time.Minute

// Bounds of the backoff between two reads of a task RemoveSource waits for.
const (
	taskPollFirst = 200 * time.Millisecond
	taskPollMax   = 2 * time.Second
)

// removeObjects deletes the objects named name in collections, in order (distribution first, so
// hosts stop being served before the content goes), cancelling the tasks that still hold a
// repository first so its deletion does not wait for a sync. It waits for the deletions.
func (r *Repository) removeObjects(ctx context.Context, name string, collections ...string) error {
	var pending []string
	for _, collection := range collections {
		found, err := r.api.find(ctx, collection, name, []string{"pulp_href"})
		if err != nil {
			return err
		}
		if found == nil {
			continue
		}
		deletion, err := r.deleteObject(ctx, found.href(), strings.HasPrefix(collection, "/repositories/"))
		if err != nil {
			return err
		}
		pending = append(pending, deletion...)
	}
	return r.waitTasks(ctx, pending)
}

// removeOCI deletes an oci source: the distribution, repository, and remote Pulp created for each
// image pulled through it (they would keep serving and pulling those images), then the pull-through
// distribution and remote.
func (r *Repository) removeOCI(ctx context.Context, name string) error {
	var pending []string
	distribution, err := r.api.find(ctx, pullThroughDistributions, name, []string{"pulp_href", "distributions"})
	if err != nil {
		return err
	}
	if distribution != nil {
		for _, childHref := range distribution.list("distributions") {
			var child object
			if err := r.api.get(ctx, childHref, url.Values{"fields": {"pulp_href,repository,remote"}}, &child); err != nil {
				if isStatus(err, http.StatusNotFound) {
					continue
				}
				return err
			}
			for _, href := range []string{child.href(), child.str("repository"), child.str("remote")} {
				if href == "" {
					continue
				}
				deletion, err := r.deleteObject(ctx, href, href == child.str("repository"))
				if err != nil {
					return err
				}
				pending = append(pending, deletion...)
			}
		}
		deletion, err := r.deleteObject(ctx, distribution.href(), false)
		if err != nil {
			return err
		}
		pending = append(pending, deletion...)
	}
	remote, err := r.api.find(ctx, pullThroughRemotes, name, []string{"pulp_href"})
	if err != nil {
		return err
	}
	if remote != nil {
		deletion, err := r.deleteObject(ctx, remote.href(), false)
		if err != nil {
			return err
		}
		pending = append(pending, deletion...)
	}
	return r.waitTasks(ctx, pending)
}

// deleteObject asks Pulp to delete href and returns the deletion task, none when the object is
// already gone. With cancelHeld it first cancels the tasks still holding the object.
func (r *Repository) deleteObject(ctx context.Context, href string, cancelHeld bool) ([]string, error) {
	if cancelHeld {
		if err := r.cancelTasks(ctx, href); err != nil {
			return nil, err
		}
	}
	var answer struct {
		Task string `json:"task"`
	}
	if _, err := r.api.send(ctx, http.MethodDelete, href, nil, "", &answer); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if answer.Task == "" {
		return nil, nil
	}
	return []string{answer.Task}, nil
}

// cancelTasks cancels every task waiting for or running on the resource at href.
func (r *Repository) cancelTasks(ctx context.Context, href string) error {
	held, err := r.api.list(ctx, "/tasks/", url.Values{
		"reserved_resources": {href}, "state__in": {taskWaiting + "," + taskRunning}, "limit": {listLimit}, "fields": {"pulp_href"},
	})
	if err != nil {
		return err
	}
	for _, t := range held.Results {
		if _, err := r.api.send(ctx, http.MethodPatch, t.href(), map[string]any{"state": "canceled"}, "", nil); err != nil {
			// 404 and 409 mean the task finished on its own between the list and the cancel.
			if isStatus(err, http.StatusNotFound, http.StatusConflict) {
				continue
			}
			return err
		}
	}
	return nil
}

// waitTasks waits, with a bounded exponential backoff, until every task in hrefs has finished. A
// failed task is a rejection carrying Pulp's reason; tasks still running after removeWait are an
// unavailable error. The caller's ctx ends the wait early with its own error.
func (r *Repository) waitTasks(ctx context.Context, hrefs []string) error {
	if len(hrefs) == 0 {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, removeWait)
	defer cancel()
	for _, href := range hrefs {
		delay := taskPollFirst
		for {
			var t task
			err := r.api.get(waitCtx, href, url.Values{"fields": {taskFields}}, &t)
			if err != nil {
				if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
					return stillRemoving(href)
				}
				return err
			}
			if !t.incomplete() {
				if t.State == taskFailed {
					return &localrepodomain.ProviderError{
						Kind:      localrepodomain.ProviderErrorRejected,
						Detail:    redact.Text("Pulp could not remove the source: " + t.reason()),
						Operation: "wait for " + strings.TrimPrefix(href, apiPath),
						Endpoint:  redact.URL(r.api.base),
					}
				}
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-waitCtx.Done():
				return stillRemoving(href)
			case <-time.After(delay):
			}
			delay = min(delay*2, taskPollMax)
		}
	}
	return nil
}

// stillRemoving is the error of a removal whose deletion task at href had not finished in time.
func stillRemoving(href string) error {
	return &localrepodomain.ProviderError{
		Kind:      localrepodomain.ProviderErrorUnavailable,
		Detail:    "Pulp is still removing the source; the removal is checked again later.",
		Operation: "wait for " + strings.TrimPrefix(href, apiPath),
	}
}
