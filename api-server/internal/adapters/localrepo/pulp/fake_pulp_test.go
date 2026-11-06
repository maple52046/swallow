package pulp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// fakePassword is the admin password the fake Pulp accepts.
const fakePassword = "s3cret-admin-password"

// fakeToken is the bearer token the fake registry issues anonymously.
const fakeToken = "anonymous-pull-token"

// fakeTimeFormat is how the fake writes timestamps: Pulp's microsecond form, fixed width so that
// ordering by a timestamp's text orders by time.
const fakeTimeFormat = "2006-01-02T15:04:05.000000Z07:00"

// fakeCollections lists the collections the fake Pulp serves and how a POST to each behaves.
var fakeCollections = map[string]string{
	debRemotes:                            "create",
	debRepositories:                       "create",
	debDistributions:                      "task",
	debPublications:                       "publish",
	debPackages:                           "",
	pullThroughRemotes:                    "create",
	pullThroughDistributions:              "task",
	containerRepositories:                 "",
	"/remotes/container/container/":       "",
	"/distributions/container/container/": "",
	containerTags:                         "",
	containerBlobs:                        "",
	pythonRemotes:                         "create",
	pythonRepositories:                    "create",
	pythonDistributions:                   "task",
	pythonPackages:                        "",
	fileRepositories:                      "create",
	filePublications:                      "publish",
	fileDistributions:                     "task",
	fileContent:                           "download",
	"/artifacts/":                         "",
	"/signing-services/":                  "",
}

// contentTypes is the pulp_type of the content collections, which repository versions summarize.
var contentTypes = map[string]string{
	debPackages:    debContentType,
	pythonPackages: pythonContentType,
	containerTags:  "container.tag",
	fileContent:    "file.file",
}

// fakeDownload is what the fake Pulp fetches for a file_url.
type fakeDownload struct {
	sha256 string
	size   int64
}

// fakeManifest is a manifest the fake registry serves.
type fakeManifest struct {
	mediaType string
	body      string
}

// fakePulp is an in-memory Pulp REST API and container registry for the adapter's tests. It
// implements the calls the adapter makes with Pulp's shapes: objects with hrefs, filters, pages,
// tasks that run at once (or wait, with holdTasks, until release), repository versions, and the
// registry's token challenge. The registry stores a blob, as Pulp's pull-through does, once it
// served it whole: its blob content gets an artifact. Every GET under the API must carry fields=,
// which keeps the adapter's requests small.
type fakePulp struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	now      time.Time
	nextID   int
	objects  map[string]object
	order    map[string][]string
	tasks    []*fakeTask
	versions map[string][]string
	requests []fakeRequest

	holdTasks bool
	// normalizeURLs makes updates store a url without its trailing slash, a form the adapter does
	// not send.
	normalizeURLs bool
	syncError     map[string]string
	upstream      map[string][]object
	downloads     map[string]fakeDownload
	manifests     map[string]fakeManifest
	blobs         map[string]string
	blobReads     map[string]int
	// manifestFailures and blobFailures make the registry answer a manifest (keyed like manifests)
	// or a blob (by digest) with that HTTP status instead.
	manifestFailures map[string]int
	blobFailures     map[string]int
	// blobGate, when set, holds every authorized blob request until it is closed or the request
	// ends, with the lock released, as a slow upstream would.
	blobGate chan struct{}
}

type fakeRequest struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
	cid    string
	// authorized is set for a registry request carrying the anonymous token.
	authorized bool
}

type fakeTask struct {
	obj      object
	reserved []string
	run      func() ([]string, string)
}

func newFakePulp(t *testing.T) *fakePulp {
	t.Helper()
	f := &fakePulp{
		t:         t,
		now:       time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC),
		objects:   map[string]object{},
		order:     map[string][]string{},
		versions:  map[string][]string{},
		syncError: map[string]string{},
		upstream:  map[string][]object{},
		downloads: map[string]fakeDownload{},
		manifests: map[string]fakeManifest{},
		blobs:     map[string]string{},
		blobReads: map[string]int{},

		manifestFailures: map[string]int{},
		blobFailures:     map[string]int{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// newRepository returns a Repository against the fake whose clock is the fake's, with the image
// downloads of a process of its own.
func (f *fakePulp) newRepository(t *testing.T) *Repository {
	t.Helper()
	return f.newRepositorySharing(t, newImageDownloads())
}

// newRepositorySharing returns a Repository like newRepository whose image downloads are downloads,
// as the Repositories of one factory share them.
func (f *fakePulp) newRepositorySharing(t *testing.T, downloads *imageDownloads) *Repository {
	t.Helper()
	api, err := newClient(f.server.URL, fakePassword, f.server.Client())
	if err != nil {
		t.Fatalf("newClient(%q) error = %v", f.server.URL, err)
	}
	return &Repository{
		api:           api,
		contentOrigin: f.server.URL,
		registry:      f.server.Client(),
		downloads:     downloads,
		now:           f.clock,
	}
}

// holdBlobs makes the registry hold every blob request until the returned release is called (or the
// test ends), so a download stays in flight.
func (f *fakePulp) holdBlobs(t *testing.T) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	f.mu.Lock()
	f.blobGate = gate
	f.mu.Unlock()
	// Cleanups run last-registered first, so this releases held requests before the server closes.
	t.Cleanup(release)
	return release
}

// blobRequestCount counts the authorized blob requests the registry received for digest, held ones
// included.
func (f *fakePulp) blobRequestCount(digest string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, req := range f.requests {
		if req.method == http.MethodGet && strings.HasSuffix(req.path, "/blobs/"+digest) && req.authorized {
			count++
		}
	}
	return count
}

func (f *fakePulp) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakePulp) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// setDownload sets what the fake fetches for fileURL.
func (f *fakePulp) setDownload(fileURL string, download fakeDownload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads[fileURL] = download
}

// setSyncError makes the syncs of the repository named name fail with failure ("" to succeed).
func (f *fakePulp) setSyncError(name, failure string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncError[name] = failure
}

func (f *fakePulp) setHold(hold bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdTasks = hold
}

// seed stores an object in collection as if Pulp had it, and returns its href.
func (f *fakePulp) seed(collection string, fields map[string]any) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.store(collection, fields)
}

func (f *fakePulp) store(collection string, fields map[string]any) string {
	f.nextID++
	href := api(collection) + fmt.Sprintf("%04d/", f.nextID)
	// Creation times are unique, as Pulp's microsecond timestamps are, so ordering by them is stable.
	created := f.now.Add(time.Duration(f.nextID) * time.Microsecond).Format(fakeTimeFormat)
	obj := object{"pulp_href": href, "pulp_created": created, "pulp_last_updated": f.now.Format(fakeTimeFormat)}
	for key, value := range fields {
		obj[key] = value
	}
	if pulpType, ok := contentTypes[collection]; ok {
		obj["pulp_type"] = pulpType
	}
	if strings.HasPrefix(collection, "/repositories/") {
		version := href + "versions/0/"
		obj["latest_version_href"] = version
		f.versions[version] = nil
	}
	f.objects[href] = obj
	f.order[collection] = append(f.order[collection], href)
	return href
}

// object returns a copy of the object at href, nil when there is none.
func (f *fakePulp) object(href string) object {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[href]
	if !ok {
		return nil
	}
	return copyObject(obj)
}

// named returns a copy of the object of collection named name, nil when there is none.
func (f *fakePulp) named(collection, name string) object {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, href := range f.order[collection] {
		if obj, ok := f.objects[href]; ok && obj.str("name") == name {
			return copyObject(obj)
		}
	}
	return nil
}

// latestContents returns the content hrefs of the newest version of the repository at repoHref.
func (f *fakePulp) latestContents(repoHref string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.versions[f.objects[repoHref].str("latest_version_href")])
}

// addVersion makes a new version of the repository at repoHref holding contents and returns it.
func (f *fakePulp) addVersion(repoHref string, contents []string) string {
	repo := f.objects[repoHref]
	next := 1
	if latest := repo.str("latest_version_href"); latest != "" {
		number, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(latest, repoHref+"versions/"), "/"))
		next = number + 1
	}
	version := repoHref + "versions/" + strconv.Itoa(next) + "/"
	f.versions[version] = slices.Clone(contents)
	repo["latest_version_href"] = version
	if repo["autopublish"] == true {
		f.store(publicationCollection(repoHref), map[string]any{
			"repository": repoHref, "repository_version": version, "signing_service": repo["signing_service"],
		})
	}
	return version
}

func publicationCollection(repoHref string) string {
	if strings.HasPrefix(repoHref, api(debRepositories)) {
		return debPublications
	}
	return filePublications
}

// addTask records a task and runs it at once unless tasks are held. run returns the created
// resources, or an error description to fail the task with.
func (f *fakePulp) addTask(name, cid string, reserved []string, run func() ([]string, string)) string {
	f.nextID++
	href := api("/tasks/") + fmt.Sprintf("%04d/", f.nextID)
	task := &fakeTask{
		obj: object{
			"pulp_href": href, "name": name, "state": taskWaiting, "logging_cid": cid,
			"pulp_created": f.now.Format(fakeTimeFormat),
		},
		reserved: reserved,
		run:      run,
	}
	f.tasks = append(f.tasks, task)
	f.objects[href] = task.obj
	if !f.holdTasks {
		f.finish(task)
	}
	return href
}

func (f *fakePulp) finish(task *fakeTask) {
	if task.obj.str("state") != taskWaiting && task.obj.str("state") != taskRunning {
		return
	}
	task.obj["started_at"] = f.now.Format(fakeTimeFormat)
	created, failure := task.run()
	task.obj["finished_at"] = f.now.Format(fakeTimeFormat)
	if failure != "" {
		task.obj["state"] = taskFailed
		task.obj["error"] = map[string]any{"description": failure, "traceback": "Traceback (most recent call last): ..."}
		return
	}
	task.obj["state"] = taskCompleted
	createdAny := make([]any, 0, len(created))
	for _, href := range created {
		createdAny = append(createdAny, href)
	}
	task.obj["created_resources"] = createdAny
}

// release runs every held task in order and stops holding new ones.
func (f *fakePulp) release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.holdTasks = false
	for _, task := range f.tasks {
		f.finish(task)
	}
}

// startRunning moves every waiting task to running.
func (f *fakePulp) startRunning() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.obj.str("state") == taskWaiting {
			task.obj["state"] = taskRunning
			task.obj["started_at"] = f.now.Format(fakeTimeFormat)
		}
	}
}

// requestsTo returns the recorded requests with method whose path starts with prefix.
func (f *fakePulp) requestsTo(method, prefix string) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, req := range f.requests {
		if req.method == method && strings.HasPrefix(req.path, prefix) {
			out = append(out, req)
		}
	}
	return out
}

func (f *fakePulp) blobReadCount(digest string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blobReads[digest]
}

func (f *fakePulp) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var body map[string]any
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				f.t.Errorf("%s %s: body is not a JSON object: %v", r.Method, r.URL.Path, err)
			}
		}
	}
	f.requests = append(f.requests, fakeRequest{
		method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body, cid: r.Header.Get(correlationHeader),
		authorized: r.Header.Get("Authorization") == "Bearer "+fakeToken,
	})

	switch {
	case r.URL.Path == "/token/":
		writeJSON(w, http.StatusOK, map[string]any{"token": fakeToken})
		return
	case strings.HasPrefix(r.URL.Path, "/v2/"):
		f.serveRegistry(w, r)
		return
	}

	user, password, ok := r.BasicAuth()
	if !ok || user != adminUser || password != fakePassword {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "Invalid username/password."})
		return
	}
	if r.Method == http.MethodGet && r.URL.Query().Get("fields") == "" {
		f.t.Errorf("GET %s?%s carries no fields= parameter", r.URL.Path, r.URL.RawQuery)
	}
	f.serveAPI(w, r, body)
}

func (f *fakePulp) serveAPI(w http.ResponseWriter, r *http.Request, body map[string]any) {
	path := r.URL.Path
	if path == api("/tasks/") && r.Method == http.MethodGet {
		f.listTasks(w, r.URL.Query())
		return
	}
	for collection, mode := range fakeCollections {
		if path == api(collection) {
			switch r.Method {
			case http.MethodGet:
				f.listObjects(w, collection, r.URL.Query())
			case http.MethodPost:
				f.create(w, collection, mode, body, r.Header.Get(correlationHeader))
			default:
				writeJSON(w, http.StatusMethodNotAllowed, nil)
			}
			return
		}
	}
	if href, ok := strings.CutSuffix(path, "sync/"); ok && r.Method == http.MethodPost {
		f.sync(w, href, body)
		return
	}
	if href, ok := strings.CutSuffix(path, "modify/"); ok && r.Method == http.MethodPost {
		f.modify(w, href, body, r.Header.Get(correlationHeader))
		return
	}
	if versionContents, ok := f.versions[path]; ok && r.Method == http.MethodGet {
		present := map[string]any{}
		for _, href := range versionContents {
			pulpType := f.objects[href].str("pulp_type")
			entry, _ := present[pulpType].(map[string]any)
			if entry == nil {
				entry = map[string]any{"count": 0.0}
				present[pulpType] = entry
			}
			entry["count"] = entry["count"].(float64) + 1
		}
		writeJSON(w, http.StatusOK, map[string]any{"pulp_href": path, "content_summary": map[string]any{"present": present}})
		return
	}
	obj, ok := f.objects[path]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not found."})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, obj)
	case http.MethodPatch:
		if strings.HasPrefix(path, api("/tasks/")) {
			if state := obj.str("state"); state != taskWaiting && state != taskRunning {
				writeJSON(w, http.StatusConflict, map[string]any{"detail": "The task is already finished."})
				return
			}
			obj["state"] = "canceled"
			obj["finished_at"] = f.now.Format(fakeTimeFormat)
			writeJSON(w, http.StatusOK, obj)
			return
		}
		task := f.addTask("pulpcore.app.tasks.base.ageneral_update", r.Header.Get(correlationHeader), []string{path}, func() ([]string, string) {
			for key, value := range body {
				obj[key] = value
				if text, ok := value.(string); ok && key == "url" && f.normalizeURLs {
					obj[key] = strings.TrimSuffix(text, "/")
				}
			}
			obj["pulp_last_updated"] = f.now.Format(fakeTimeFormat)
			return nil, ""
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
	case http.MethodDelete:
		task := f.addTask("pulpcore.app.tasks.base.ageneral_delete", "", []string{path}, func() ([]string, string) {
			delete(f.objects, path)
			return nil, ""
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakePulp) create(w http.ResponseWriter, collection, mode string, body map[string]any, cid string) {
	switch mode {
	case "create":
		if f.nameTaken(collection, body) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"name": []any{"This field must be unique."}})
			return
		}
		href := f.store(collection, body)
		writeJSON(w, http.StatusCreated, f.objects[href])
	case "task":
		task := f.addTask("pulpcore.app.tasks.base.general_create", cid, nil, func() ([]string, string) {
			if f.nameTaken(collection, body) {
				return nil, "name: This field must be unique."
			}
			fields := copyObject(object(body))
			if collection == pullThroughDistributions {
				fields["pulp_labels"] = map[string]any{pullThroughPathLabel: body["base_path"]}
				fields["base_path"] = "0b5e1c9e-generated"
				fields["distributions"] = []any{}
			}
			return []string{f.store(collection, fields)}, ""
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
	case "publish":
		repoHref, _ := body["repository"].(string)
		task := f.addTask("pulp.app.tasks.publishing.publish", cid, []string{repoHref}, func() ([]string, string) {
			repo := f.objects[repoHref]
			return []string{f.store(collection, map[string]any{
				"repository": repoHref, "repository_version": repo.str("latest_version_href"), "signing_service": body["signing_service"],
			})}, ""
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
	case "download":
		repoHref, _ := body["repository"].(string)
		fileURL, _ := body["file_url"].(string)
		relativePath, _ := body["relative_path"].(string)
		task := f.addTask("pulpcore.app.tasks.base.general_create", cid, []string{repoHref}, func() ([]string, string) {
			download, ok := f.downloads[fileURL]
			if !ok {
				return nil, "404, message='Not Found', url='" + fileURL + "'"
			}
			content := f.findContent(relativePath, download.sha256)
			if content == "" {
				artifact := f.store("/artifacts/", map[string]any{"size": float64(download.size), "sha256": download.sha256})
				content = f.store(fileContent, map[string]any{"relative_path": relativePath, "sha256": download.sha256, "artifact": artifact})
			}
			version := f.replaceInVersion(repoHref, content)
			return []string{content, version}, ""
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakePulp) nameTaken(collection string, body map[string]any) bool {
	for _, href := range f.order[collection] {
		if obj, ok := f.objects[href]; ok && obj.str("name") == body["name"] {
			return true
		}
	}
	return false
}

func (f *fakePulp) findContent(relativePath, sha string) string {
	for _, href := range f.order[fileContent] {
		obj := f.objects[href]
		if obj.str("relative_path") == relativePath && obj.str("sha256") == sha {
			return href
		}
	}
	return ""
}

// replaceInVersion adds content to a new version of the file repository, replacing the content at
// the same relative path as pulp_file does.
func (f *fakePulp) replaceInVersion(repoHref, content string) string {
	current := f.versions[f.objects[repoHref].str("latest_version_href")]
	path := f.objects[content].str("relative_path")
	next := make([]string, 0, len(current)+1)
	for _, href := range current {
		if f.objects[href].str("relative_path") != path {
			next = append(next, href)
		}
	}
	return f.addVersion(repoHref, append(next, content))
}

func (f *fakePulp) sync(w http.ResponseWriter, repoHref string, body map[string]any) {
	remoteHref, _ := body["remote"].(string)
	repo, ok := f.objects[repoHref]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not found."})
		return
	}
	task := f.addTask("pulp_deb.app.tasks.synchronizing.synchronize", "", []string{repoHref, remoteHref}, func() ([]string, string) {
		if failure := f.syncError[repo.str("name")]; failure != "" {
			return nil, failure
		}
		remote := f.objects[remoteHref]
		var contents []string
		for _, pkg := range f.upstream[remote.str("url")] {
			contents = append(contents, f.store(debPackages, pkg))
		}
		return []string{f.addVersion(repoHref, contents)}, ""
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
}

func (f *fakePulp) modify(w http.ResponseWriter, repoHref string, body map[string]any, cid string) {
	task := f.addTask("pulpcore.app.tasks.repository.add_and_remove", cid, []string{repoHref}, func() ([]string, string) {
		current := f.versions[f.objects[repoHref].str("latest_version_href")]
		remove := map[string]bool{}
		for _, href := range anyStrings(body["remove_content_units"]) {
			remove[href] = true
		}
		next := []string{}
		for _, href := range current {
			if !remove[href] {
				next = append(next, href)
			}
		}
		next = append(next, anyStrings(body["add_content_units"])...)
		return []string{f.addVersion(repoHref, next)}, ""
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
}

func (f *fakePulp) listObjects(w http.ResponseWriter, collection string, query url.Values) {
	var matched []object
	for _, href := range f.order[collection] {
		obj, ok := f.objects[href]
		if ok && f.matches(obj, query, nil) {
			matched = append(matched, obj)
		}
	}
	if ordering := query.Get("ordering"); ordering != "" {
		keys := strings.Split(ordering, ",")
		sort.SliceStable(matched, func(i, j int) bool {
			for _, key := range keys {
				descending := strings.HasPrefix(key, "-")
				key = strings.TrimPrefix(key, "-")
				a, b := fmt.Sprint(matched[i][key]), fmt.Sprint(matched[j][key])
				if a != b {
					return (a < b) != descending
				}
			}
			return false
		})
	}
	writePage(w, matched, query)
}

func (f *fakePulp) listTasks(w http.ResponseWriter, query url.Values) {
	var matched []object
	for i := len(f.tasks) - 1; i >= 0; i-- {
		task := f.tasks[i]
		if f.matches(task.obj, query, task.reserved) {
			matched = append(matched, task.obj)
		}
	}
	writePage(w, matched, query)
}

// matches applies Pulp's filters the adapter uses to obj; reserved is a task's reserved resources.
func (f *fakePulp) matches(obj object, query url.Values, reserved []string) bool {
	for key, values := range query {
		value := values[0]
		switch key {
		case "limit", "offset", "fields", "ordering":
			continue
		case "repository_version":
			if !slices.Contains(f.versions[value], obj.href()) {
				return false
			}
			continue
		case "reserved_resources":
			if !slices.Contains(reserved, value) {
				return false
			}
			continue
		}
		field, op, _ := strings.Cut(key, "__")
		actual := fmt.Sprint(obj[field])
		if obj[field] == nil {
			actual = ""
		}
		switch op {
		case "":
			if actual != value {
				return false
			}
		case "in":
			if !slices.Contains(strings.Split(value, ","), actual) {
				return false
			}
		case "contains":
			if !strings.Contains(actual, value) {
				return false
			}
		case "icontains":
			if !strings.Contains(strings.ToLower(actual), strings.ToLower(value)) {
				return false
			}
		case "startswith":
			if !strings.HasPrefix(actual, value) {
				return false
			}
		default:
			f.t.Errorf("the fake Pulp does not implement the filter %s", key)
			return false
		}
	}
	return true
}

func (f *fakePulp) serveRegistry(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v2/")
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		repository := rest
		if i := strings.LastIndex(rest, "/manifests/"); i >= 0 {
			repository = rest[:i]
		} else if i := strings.LastIndex(rest, "/blobs/"); i >= 0 {
			repository = rest[:i]
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token/",service="%s",scope="repository:%s:pull"`,
			f.server.URL, strings.TrimPrefix(f.server.URL, "http://"), repository))
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []any{map[string]any{"code": "UNAUTHORIZED"}}})
		return
	}
	if status := f.manifestFailures[rest]; status != 0 {
		writeJSON(w, status, map[string]any{"errors": []any{map[string]any{"code": "UNKNOWN", "message": "upstream unavailable"}}})
		return
	}
	if manifest, ok := f.manifests[rest]; ok {
		w.Header().Set("Content-Type", manifest.mediaType)
		_, _ = io.WriteString(w, manifest.body)
		return
	}
	if i := strings.LastIndex(rest, "/blobs/"); i >= 0 {
		digest := rest[i+len("/blobs/"):]
		if gate := f.blobGate; gate != nil {
			f.mu.Unlock()
			select {
			case <-gate:
			case <-r.Context().Done():
			}
			f.mu.Lock()
		}
		if status := f.blobFailures[digest]; status != 0 {
			writeJSON(w, status, map[string]any{"detail": "upstream unavailable"})
			return
		}
		if blob, ok := f.blobs[digest]; ok {
			f.blobReads[digest]++
			_, _ = io.WriteString(w, blob)
			f.storeBlob(digest, int64(len(blob)))
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"errors": []any{map[string]any{"code": "MANIFEST_UNKNOWN", "message": "manifest unknown"}}})
}

// storeBlob gives the blob content of digest an artifact, creating the content when Pulp did not
// know the blob yet.
func (f *fakePulp) storeBlob(digest string, size int64) {
	artifact := f.store("/artifacts/", map[string]any{"size": float64(size)})
	for _, href := range f.order[containerBlobs] {
		if obj := f.objects[href]; obj.str("digest") == digest {
			obj["artifact"] = artifact
			return
		}
	}
	f.store(containerBlobs, map[string]any{"digest": digest, "artifact": artifact})
}

// seedBlob records the blob content of digest as Pulp holds it: with an artifact when stored, with
// none when Pulp knows it only from the upstream.
func (f *fakePulp) seedBlob(digest string, stored bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stored {
		f.storeBlob(digest, 1)
		return
	}
	f.store(containerBlobs, map[string]any{"digest": digest, "artifact": nil})
}

func writePage(w http.ResponseWriter, matched []object, query url.Values) {
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	results := []object{}
	if offset < len(matched) {
		results = matched[offset:min(offset+limit, len(matched))]
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(matched), "results": results})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func copyObject(obj object) object {
	raw, _ := json.Marshal(obj)
	var out object
	_ = json.Unmarshal(raw, &out)
	return out
}

func anyStrings(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func sha256Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// seedSigningService registers the deb signing service and returns its href.
func (f *fakePulp) seedSigningService() string {
	return f.seed("/signing-services/", map[string]any{
		"name": debSigningService, "public_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----\nfake\n-----END PGP PUBLIC KEY BLOCK-----",
	})
}

// wantState fails the test unless got is want.
func wantState(t *testing.T, what string, got, want localrepodomain.SyncState) {
	t.Helper()
	if got != want {
		t.Fatalf("%s state = %q, want %q", what, got, want)
	}
}
