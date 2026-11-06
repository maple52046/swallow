package pulp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// object is a Pulp object as the API returns it. It is decoded generically because the adapter
// reads a few fields of many object types and compares them with the values it wants, which one
// struct per type would only repeat.
type object map[string]any

// str returns a string field, "" when it is null, missing, or of another type: Pulp writes an unset
// relation (a distribution's publication) as null.
func (o object) str(key string) string {
	value, _ := o[key].(string)
	return value
}

func (o object) href() string { return o.str("pulp_href") }

// list returns a list-valued field (a pull-through distribution's distributions, a task's created
// resources), skipping values that are not strings.
func (o object) list(key string) []string {
	values, _ := o[key].([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// labels returns a string map field such as pulp_labels, empty when it is unset.
func (o object) labels(key string) map[string]string {
	values, _ := o[key].(map[string]any)
	out := make(map[string]string, len(values))
	for name, value := range values {
		if text, ok := value.(string); ok {
			out[name] = text
		}
	}
	return out
}

// timestamp parses an RFC 3339 field such as pulp_last_updated, or returns nil when it is unset or
// malformed.
func (o object) timestamp(key string) *time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, o.str(key))
	if err != nil {
		return nil
	}
	return &parsed
}

// number returns an integer field such as an artifact's size; JSON numbers decode as float64, and a
// field Pulp serializes as a string is parsed. It is 0 when unset or malformed.
func (o object) number(key string) int64 {
	switch value := o[key].(type) {
	case float64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	default:
		return 0
	}
}

// page is one page of a Pulp list: Count matches the filter across all pages.
type page struct {
	Count   int      `json:"count"`
	Results []object `json:"results"`
}

// list reads one page of collection with query (filters, limit, offset, fields).
func (c *client) list(ctx context.Context, collection string, query url.Values) (page, error) {
	var out page
	if err := c.get(ctx, api(collection), query, &out); err != nil {
		return page{}, err
	}
	return out, nil
}

// find returns the object of collection named name with only fields, or nil when there is none.
// Pulp names are unique per object kind, so one page of one is the whole answer.
func (c *client) find(ctx context.Context, collection, name string, fields []string) (object, error) {
	query := url.Values{"name": {name}, "limit": {"1"}, "fields": {strings.Join(fields, ",")}}
	found, err := c.list(ctx, collection, query)
	if err != nil {
		return nil, err
	}
	if len(found.Results) == 0 {
		return nil, nil
	}
	return found.Results[0], nil
}

// words is a list Pulp stores as one space-separated string (a deb remote's distributions,
// components, and architectures). It is sent joined, as null when empty, and compared as a set,
// because Pulp keeps the string as it was written.
type words []string

// MarshalJSON writes the space-separated form Pulp expects.
func (w words) MarshalJSON() ([]byte, error) {
	if len(w) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(strings.Join(w, " "))
}

// sameValue reports whether actual, a field as Pulp returned it, already is want.
func sameValue(actual, want any) bool {
	switch typed := want.(type) {
	case nil:
		return actual == nil || actual == ""
	case string:
		text, _ := actual.(string)
		return text == typed
	case bool:
		flag, ok := actual.(bool)
		return ok && flag == typed
	case float64:
		// Pulp returns a float field with a fraction (21600.0), which must match the 21600 sent; an
		// unset one is null and differs.
		number, ok := actual.(float64)
		return ok && number == typed
	case words:
		text, _ := actual.(string)
		have := strings.Fields(text)
		wanted := slices.Clone([]string(typed))
		slices.Sort(have)
		slices.Sort(wanted)
		return slices.Equal(slices.Compact(have), slices.Compact(wanted))
	default:
		encodedActual, _ := json.Marshal(actual)
		encodedWant, _ := json.Marshal(want)
		return string(encodedActual) == string(encodedWant)
	}
}

// objectSpec is the Pulp object one source needs: its kind (collection), its name, the field values
// it must have, the values only sent when creating it (a pull-through distribution's base path
// cannot be changed), and the other fields the caller reads from it.
type objectSpec struct {
	collection string
	name       string
	want       map[string]any
	createOnly map[string]any
	read       []string
	// asyncCreate is set for kinds Pulp creates in a task (distributions) rather than answering
	// the create with the object (remotes, repositories).
	asyncCreate bool
}

// fields is what a lookup of the object asks Pulp for: what the adapter compares and reads.
func (s objectSpec) fields() []string {
	fields := []string{"pulp_href", "name"}
	for key := range s.want {
		fields = append(fields, key)
	}
	fields = append(fields, s.read...)
	slices.Sort(fields)
	return slices.Compact(fields)
}

// step is what the adapter found or did about one change Pulp had to make: ready when nothing is
// in flight, pending or syncing while Pulp's task waits or runs, failed (with Message, Pulp's
// reason) when the task failed less than retryInterval ago. Changed is set when this call or a task
// still in flight creates or updates the object.
type step struct {
	State   localrepodomain.SyncState
	Message string
	Changed bool
}

var stepReady = step{State: localrepodomain.SyncReady}

// severity orders states so that combining steps reports the one that matters most to a reader.
func severity(state localrepodomain.SyncState) int {
	switch state {
	case localrepodomain.SyncFailed:
		return 3
	case localrepodomain.SyncSyncing:
		return 2
	case localrepodomain.SyncPending:
		return 1
	default:
		return 0
	}
}

// worst combines steps: the most severe state with its message, changed when any changed.
func worst(steps ...step) step {
	out := stepReady
	for _, s := range steps {
		if severity(s.State) > severity(out.State) {
			out.State, out.Message = s.State, s.Message
		}
		out.Changed = out.Changed || s.Changed
	}
	return out
}

// ensure converges the object s describes: it creates the object when Pulp has none of that name,
// and updates the fields of s.want that differ otherwise. It returns the object as Pulp reported it
// before any update (its href does not change), or nil while an asynchronous create has not
// finished, and the step that tells whether a task for it is still in flight or failed.
//
// Nothing is remembered between calls: an update or create already dispatched is recognized by the
// correlation id of its request (see dispatch), so calling ensure again while Pulp's worker is busy
// does not queue the same change twice.
func (r *Repository) ensure(ctx context.Context, s objectSpec) (object, step, error) {
	found, err := r.api.find(ctx, s.collection, s.name, s.fields())
	if err != nil {
		return nil, step{}, err
	}
	if found == nil {
		body := map[string]any{"name": s.name}
		for key, value := range s.want {
			body[key] = value
		}
		for key, value := range s.createOnly {
			body[key] = value
		}
		if s.asyncCreate {
			created, err := r.dispatch(ctx, correlationID("create", s.collection, s.name, canonical(body)), http.MethodPost, api(s.collection), body)
			created.Changed = true
			return nil, created, err
		}
		var created object
		if _, err := r.api.send(ctx, http.MethodPost, api(s.collection), body, "", &created); err != nil {
			return nil, step{}, err
		}
		return created, step{State: localrepodomain.SyncReady, Changed: true}, nil
	}
	updated, err := r.update(ctx, found, s.want)
	return found, updated, err
}

// update sends Pulp the fields of want that found, an object as Pulp reported it, does not have yet,
// and reports the step of that update; an object that already has them is ready. Like ensure, it
// remembers nothing: the update's correlation id finds a task already dispatched for the same
// change.
func (r *Repository) update(ctx context.Context, found object, want map[string]any) (step, error) {
	diff := map[string]any{}
	for key, value := range want {
		if !sameValue(found[key], value) {
			diff[key] = value
		}
	}
	if len(diff) == 0 {
		return stepReady, nil
	}
	updated, err := r.dispatch(ctx, correlationID("update", found.href(), canonical(diff)), http.MethodPatch, found.href(), diff)
	// Only an update Pulp runs (or still runs) changes the object; an update Pulp answered as a
	// no-op, or one recently applied, does not, so it must not set off a sync.
	updated.Changed = updated.State == localrepodomain.SyncPending || updated.State == localrepodomain.SyncSyncing
	return updated, err
}

// canonical is a stable text of body for a correlation id: encoding/json sorts map keys.
func canonical(body any) string {
	encoded, _ := json.Marshal(body)
	return string(encoded)
}

// task is a Pulp task record: a change Pulp runs in its worker, the only place its outcome is kept.
type task struct {
	Href       string     `json:"pulp_href"`
	Name       string     `json:"name"`
	State      string     `json:"state"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Error      struct {
		Description string `json:"description"`
	} `json:"error"`
	CreatedResources []string `json:"created_resources"`
}

// taskFields is what the adapter reads of a task; the rest (progress reports, tracebacks) is large.
const taskFields = "pulp_href,name,state,started_at,finished_at,error,created_resources"

// Pulp's task states. waiting, running, and canceling are in flight; skipped, completed, failed,
// and canceled are final.
const (
	taskWaiting   = "waiting"
	taskRunning   = "running"
	taskCanceling = "canceling"
	taskCompleted = "completed"
	taskFailed    = "failed"
)

// incompleteStates is the state__in filter of tasks still in flight.
const incompleteStates = taskWaiting + "," + taskRunning + "," + taskCanceling

func (t *task) incomplete() bool {
	return t.State == taskWaiting || t.State == taskRunning || t.State == taskCanceling
}

// syncState is how an in-flight task reads to the core: queued is pending, started is syncing.
func (t *task) syncState() localrepodomain.SyncState {
	if t.State == taskWaiting {
		return localrepodomain.SyncPending
	}
	return localrepodomain.SyncSyncing
}

// failedRecently reports whether the task failed less than retryInterval before now, so its
// outcome stands instead of being retried. A canceled or skipped task never stands: nothing says
// the change itself cannot be made.
func (t *task) failedRecently(now time.Time) bool {
	return t.State == taskFailed && t.FinishedAt != nil && now.Sub(*t.FinishedAt) < retryInterval
}

// reason is the task's failure for an item's message, with any credential the error text carries
// removed by the caller's redaction.
func (t *task) reason() string {
	if t.Error.Description != "" {
		return t.Error.Description
	}
	return "Pulp reported the task as " + t.State + " without a reason."
}

// latestTask returns the newest task matching filter, or nil when none does.
func (c *client) latestTask(ctx context.Context, filter url.Values) (*task, error) {
	query := url.Values{"limit": {"1"}, "ordering": {"-pulp_created"}, "fields": {taskFields}}
	for key, values := range filter {
		query[key] = values
	}
	var out struct {
		Results []task `json:"results"`
	}
	if err := c.get(ctx, api("/tasks/"), query, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	return &out.Results[0], nil
}

// dispatch asks Pulp for a change it runs as a task (method on href with body), unless the newest
// task recorded under cid is still in flight, or failed less than retryInterval ago, in which case
// that task's state is the answer. The request carries cid as its correlation id, which is how a
// later call finds the task without the adapter remembering it.
//
// An update (PATCH) whose task completed less than retryInterval ago is not sent again either: the
// object still differing then means Pulp stores the value in another form than the adapter compares,
// and sending it on every call would only churn. A synchronous answer (an update Pulp found to change
// nothing) is ready.
func (r *Repository) dispatch(ctx context.Context, cid, method, href string, body any) (step, error) {
	last, err := r.api.latestTask(ctx, url.Values{"logging_cid": {cid}})
	if err != nil {
		return step{}, err
	}
	if last != nil {
		now := r.now()
		switch {
		case last.incomplete():
			return step{State: last.syncState()}, nil
		case last.failedRecently(now):
			return step{State: localrepodomain.SyncFailed, Message: last.reason()}, nil
		case method == http.MethodPatch && last.State == taskCompleted && last.FinishedAt != nil &&
			now.Sub(*last.FinishedAt) < retryInterval:
			return stepReady, nil
		}
	}
	var answer struct {
		Task string `json:"task"`
	}
	if _, err := r.api.send(ctx, method, href, body, cid, &answer); err != nil {
		return step{}, err
	}
	if answer.Task == "" {
		return stepReady, nil
	}
	return step{State: localrepodomain.SyncPending}, nil
}

// correlationID derives the correlation id of a request from parts that identify the change, so the
// same change sent again carries the same id. Pulp records the id on the task only when it parses as
// a UUID, so the digest is shaped as a version 4 UUID in Pulp's own 32-digit form.
func correlationID(parts ...string) string {
	sum := sha256.Sum256([]byte("swallow\x00" + strings.Join(parts, "\x00")))
	id := sum[:16]
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return hex.EncodeToString(id)
}

// publicationSpec is how a repository kind that serves publications is published: its publication
// collection, the fields of a publication request besides the repository, and whether an existing
// publication was made the way the adapter publishes now (signed by the right service).
type publicationSpec struct {
	collection string
	body       map[string]any
	current    func(publication object) bool
}

// ensurePublished makes sure the newest publication of repo publishes its newest version as s
// describes, because the distribution serves only the newest publication. Repositories the adapter
// creates publish every new version on their own, so this asks for a publication only for a
// repository that was changed without one (created or synced before automatic publishing was set,
// or by hand), and never while a task holds the repository.
func (r *Repository) ensurePublished(ctx context.Context, s publicationSpec, repo object) (step, error) {
	repoHref := repo.href()
	newest, err := r.api.list(ctx, s.collection, url.Values{
		"repository": {repoHref}, "ordering": {"-pulp_created"}, "limit": {"1"},
		"fields": {"pulp_href,repository_version,signing_service"},
	})
	if err != nil {
		return step{}, err
	}
	if len(newest.Results) > 0 && s.current(newest.Results[0]) &&
		!versionBefore(newest.Results[0].str("repository_version"), repo.str("latest_version_href")) {
		return stepReady, nil
	}
	busy, err := r.api.latestTask(ctx, url.Values{"reserved_resources": {repoHref}, "state__in": {incompleteStates}})
	if err != nil {
		return step{}, err
	}
	if busy != nil {
		return step{State: busy.syncState()}, nil
	}
	lastPublish, err := r.api.latestTask(ctx, url.Values{"reserved_resources": {repoHref}, "name__contains": {"publish"}})
	if err != nil {
		return step{}, err
	}
	if lastPublish != nil && lastPublish.failedRecently(r.now()) {
		return step{State: localrepodomain.SyncFailed, Message: lastPublish.reason()}, nil
	}
	body := map[string]any{"repository": repoHref}
	for key, value := range s.body {
		body[key] = value
	}
	if _, err := r.api.send(ctx, http.MethodPost, api(s.collection), body, "", nil); err != nil {
		return step{}, err
	}
	return step{State: localrepodomain.SyncPending}, nil
}

// versionBefore reports whether the repository version at published is older than the one at
// latest. Version hrefs end in their number (".../versions/7/"); comparing numbers rather than
// hrefs keeps a repository read a moment before a sync finished from looking unpublished.
func versionBefore(published, latest string) bool {
	if published == latest {
		return false
	}
	publishedNumber, okPublished := versionNumber(published)
	latestNumber, okLatest := versionNumber(latest)
	if !okPublished || !okLatest {
		return true
	}
	return publishedNumber < latestNumber
}

func versionNumber(href string) (int, bool) {
	_, number, found := strings.Cut(strings.TrimSuffix(href, "/"), "/versions/")
	if !found {
		return 0, false
	}
	parsed, err := strconv.Atoi(number)
	return parsed, err == nil
}

// contentCount returns how many items of contentType (a Pulp content type such as "deb.package")
// the repository version at versionHref holds, or 0 when there is no version yet.
func (c *client) contentCount(ctx context.Context, versionHref, contentType string) (int, error) {
	if versionHref == "" {
		return 0, nil
	}
	var version struct {
		ContentSummary struct {
			Present map[string]struct {
				Count int `json:"count"`
			} `json:"present"`
		} `json:"content_summary"`
	}
	if err := c.get(ctx, versionHref, url.Values{"fields": {"content_summary"}}, &version); err != nil {
		return 0, err
	}
	return version.ContentSummary.Present[contentType].Count, nil
}
