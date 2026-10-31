package dockerengine

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// newTestClient points a Client at an httptest Engine.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}
	port, _ := strconv.Atoi(portText)
	client, err := NewFactory().For(host, port)
	if err != nil {
		t.Fatalf("For(%s, %d) error = %v", host, port, err)
	}
	return client.(*Client)
}

func engineErrorKind(t *testing.T, err error) softwaredomain.DockerEngineErrorKind {
	t.Helper()
	var engineErr *softwaredomain.DockerEngineError
	if !errors.As(err, &engineErr) {
		t.Fatalf("error = %v (%T), want *DockerEngineError", err, err)
	}
	return engineErr.Kind
}

func TestEngineStatusesMapToErrorKinds(t *testing.T) {
	tests := []struct {
		status int
		want   softwaredomain.DockerEngineErrorKind
	}{
		{status: http.StatusNotFound, want: softwaredomain.DockerEngineNotFound},
		{status: http.StatusConflict, want: softwaredomain.DockerEngineConflict},
		{status: http.StatusForbidden, want: softwaredomain.DockerEngineConflict},
		{status: http.StatusBadRequest, want: softwaredomain.DockerEngineRejected},
		{status: http.StatusInternalServerError, want: softwaredomain.DockerEngineUnavailable},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"message":"engine says no"}`)
			}))
			err := client.RemoveVolume(context.Background(), "data", false)
			if got := engineErrorKind(t, err); got != tc.want {
				t.Errorf("RemoveVolume with Engine HTTP %d kind = %q, want %q", tc.status, got, tc.want)
			}
			if !strings.Contains(err.Error(), "engine says no") {
				t.Errorf("error %q does not carry the Engine's message", err)
			}
		})
	}
}

func TestUnreachableEngineIsUnavailable(t *testing.T) {
	// Reserve a port and close it so the dial is refused.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	client, _ := NewFactory().For("127.0.0.1", port)

	_, err = client.ListImages(context.Background())
	if got := engineErrorKind(t, err); got != softwaredomain.DockerEngineUnavailable {
		t.Fatalf("ListImages against a closed port kind = %q, want unavailable", got)
	}
	if !strings.Contains(err.Error(), "tcp://127.0.0.1:"+strconv.Itoa(port)) {
		t.Errorf("error %q does not name the endpoint", err)
	}
}

func TestStartAndStopTreatNotModifiedAsSuccess(t *testing.T) {
	var paths []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusNotModified)
	}))
	if err := client.StartContainer(context.Background(), "web"); err != nil {
		t.Errorf("StartContainer on a running container error = %v, want nil", err)
	}
	if err := client.StopContainer(context.Background(), "web"); err != nil {
		t.Errorf("StopContainer on a stopped container error = %v, want nil", err)
	}
	want := []string{"/containers/web/start?", "/containers/web/stop?t=10"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("Engine paths = %v, want %v", paths, want)
	}
}

func TestPullImageReportsMidStreamFailure(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fromImage") != "nginx" || r.URL.Query().Get("tag") != "1.27" {
			t.Errorf("pull query = %q, want fromImage=nginx tag=1.27", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"status":"Pulling from library/nginx","id":"1.27"}`+"\n")
		_, _ = io.WriteString(w, `{"errorDetail":{"message":"no space left on device"},"error":"no space left on device"}`+"\n")
	}))
	_, err := client.PullImage(context.Background(), "nginx", "1.27", nil)
	if got := engineErrorKind(t, err); got != softwaredomain.DockerEngineUnavailable {
		t.Fatalf("PullImage with a mid-stream error kind = %q, want unavailable", got)
	}
	if !strings.Contains(err.Error(), "no space left on device") {
		t.Errorf("error %q does not carry the stream's reason", err)
	}
}

func TestPullImageReturnsFinalStatus(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"Pulling from library/hello-world"}`+"\n")
		_, _ = io.WriteString(w, `{"status":"Status: Downloaded newer image for hello-world:latest"}`+"\n")
	}))
	status, err := client.PullImage(context.Background(), "hello-world", "latest", nil)
	if err != nil {
		t.Fatalf("PullImage error = %v", err)
	}
	if status != "Status: Downloaded newer image for hello-world:latest" {
		t.Errorf("status = %q, want the final progress message", status)
	}
}

// The Engine reads X-Registry-Auth as base64url JSON with these exact keys; an anonymous pull must
// not send the header at all.
func TestPullImageSendsRegistryAuthOnlyWhenGiven(t *testing.T) {
	var headers []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("X-Registry-Auth"))
		_, _ = io.WriteString(w, `{"status":"Status: Downloaded newer image"}`+"\n")
	}))
	auth := &softwaredomain.DockerRegistryAuth{Username: "robot$ci", Password: "p@ss/word+", ServerAddress: "harbor.lab.local"}
	if _, err := client.PullImage(context.Background(), "harbor.lab.local/team/app", "1.4", auth); err != nil {
		t.Fatalf("PullImage with auth error = %v", err)
	}
	if _, err := client.PullImage(context.Background(), "nginx", "1.27", nil); err != nil {
		t.Fatalf("PullImage anonymous error = %v", err)
	}
	if len(headers) != 2 || headers[1] != "" {
		t.Fatalf("X-Registry-Auth headers = %q, want one credential then none", headers)
	}
	raw, err := base64.URLEncoding.DecodeString(headers[0])
	if err != nil {
		t.Fatalf("X-Registry-Auth %q is not base64url: %v", headers[0], err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("X-Registry-Auth payload %q is not JSON: %v", raw, err)
	}
	want := map[string]string{"username": "robot$ci", "password": "p@ss/word+", "serveraddress": "harbor.lab.local"}
	for key, value := range want {
		if decoded[key] != value {
			t.Errorf("X-Registry-Auth %s = %q, want %q", key, decoded[key], value)
		}
	}
}

func TestListImagesDropsPlaceholdersAndSortsNewestFirst(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[
			{"Id":"sha256:old","RepoTags":["nginx:1.26"],"RepoDigests":["nginx@sha256:a"],"Created":100,"Size":10},
			{"Id":"sha256:new","RepoTags":["<none>:<none>"],"RepoDigests":["<none>@<none>"],"Created":200,"Size":20}
		]`)
	}))
	images, err := client.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages error = %v", err)
	}
	if len(images) != 2 || images[0].ID != "sha256:new" {
		t.Fatalf("ListImages = %+v, want newest (sha256:new) first", images)
	}
	if !images[0].Dangling || len(images[0].RepoTags) != 0 || len(images[0].RepoDigests) != 0 {
		t.Errorf("untagged image = %+v, want dangling with no placeholder tags", images[0])
	}
	if images[1].Dangling || images[1].RepoTags[0] != "nginx:1.26" {
		t.Errorf("tagged image = %+v, want nginx:1.26 not dangling", images[1])
	}
}

// muxFrame builds one multiplexed log frame as the Engine sends it without a TTY.
func muxFrame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	return append(header, payload...)
}

func TestContainerLogsDemultiplexesWithoutTTY(t *testing.T) {
	tests := []struct {
		name string
		tty  bool
		body []byte
		want string
	}{
		{
			name: "multiplexed stdout and stderr",
			tty:  false,
			body: append(muxFrame(1, "out line\n"), muxFrame(2, "err line\n")...),
			want: "out line\nerr line\n",
		},
		{name: "raw tty stream", tty: true, body: []byte("tty line\n"), want: "tty line\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/containers/web/json":
					_ = json.NewEncoder(w).Encode(map[string]any{"Config": map[string]any{"Tty": tc.tty}})
				case "/containers/web/logs":
					if r.URL.Query().Get("tail") != "50" {
						t.Errorf("logs tail = %q, want 50", r.URL.Query().Get("tail"))
					}
					_, _ = w.Write(tc.body)
				default:
					t.Errorf("unexpected Engine path %s", r.URL.Path)
				}
			}))
			logs, err := client.ContainerLogs(context.Background(), "web", 50)
			if err != nil {
				t.Fatalf("ContainerLogs error = %v", err)
			}
			if logs != tc.want {
				t.Errorf("ContainerLogs = %q, want %q", logs, tc.want)
			}
		})
	}
}

func TestCreateContainerMapsSpecAndKeepsIDWhenStartFails(t *testing.T) {
	var createBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/create":
			if r.URL.Query().Get("name") != "web" {
				t.Errorf("create name = %q, want web", r.URL.Query().Get("name"))
			}
			_ = json.NewDecoder(r.Body).Decode(&createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"Id":"abc123","Warnings":[]}`)
		case "/containers/abc123/start":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"port is already allocated"}`)
		default:
			t.Errorf("unexpected Engine path %s", r.URL.Path)
		}
	}))
	created, err := client.CreateContainer(context.Background(), softwaredomain.DockerContainerSpec{
		Name: "web", Image: "nginx:1.27", Env: []string{"A=1"},
		Ports:   []softwaredomain.DockerPortBinding{{ContainerPort: 80, HostPort: 8080}},
		Volumes: []softwaredomain.DockerVolumeBinding{{Source: "web-data", Target: "/data", ReadOnly: true}},
		Network: "app-net", RestartPolicy: "unless-stopped", Start: true,
	})
	if created == nil || created.ID != "abc123" || created.Started {
		t.Fatalf("CreateContainer result = %+v, want id abc123 not started", created)
	}
	if !strings.Contains(err.Error(), "created but could not be started") || !strings.Contains(err.Error(), "port is already allocated") {
		t.Errorf("start failure error = %q, want it to say the container exists and why it did not start", err)
	}

	hostConfig, _ := createBody["HostConfig"].(map[string]any)
	bindings, _ := hostConfig["PortBindings"].(map[string]any)
	if _, ok := bindings["80/tcp"]; !ok {
		t.Errorf("PortBindings = %v, want 80/tcp (protocol defaults to tcp)", bindings)
	}
	if binds, _ := hostConfig["Binds"].([]any); len(binds) != 1 || binds[0] != "web-data:/data:ro" {
		t.Errorf("Binds = %v, want [web-data:/data:ro]", hostConfig["Binds"])
	}
	if policy, _ := hostConfig["RestartPolicy"].(map[string]any); policy["Name"] != "unless-stopped" {
		t.Errorf("RestartPolicy = %v, want unless-stopped", hostConfig["RestartPolicy"])
	}
	if hostConfig["NetworkMode"] != "app-net" {
		t.Errorf("NetworkMode = %v, want app-net", hostConfig["NetworkMode"])
	}
}

// Image ids contain ':' and must reach the Engine as one path segment.
func TestRemoveImageEscapesID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/images/sha256:abc" && r.URL.EscapedPath() != "/images/sha256%3Aabc" {
			t.Errorf("remove path = %q, want /images/sha256:abc as one segment", r.URL.EscapedPath())
		}
		if r.URL.Query().Get("force") != "1" {
			t.Errorf("force = %q, want 1", r.URL.Query().Get("force"))
		}
		_, _ = io.WriteString(w, `[{"Deleted":"sha256:abc"}]`)
	}))
	if err := client.RemoveImage(context.Background(), "sha256:abc", true); err != nil {
		t.Fatalf("RemoveImage error = %v", err)
	}
}

func TestCreateNetworkReadsBackAndMarksPredefined(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/networks/create":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["Driver"] != "bridge" || body["IPAM"] == nil {
				t.Errorf("create body = %v, want default bridge driver and an IPAM pool", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"Id":"net1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/networks/net1":
			_, _ = io.WriteString(w, `{"Id":"net1","Name":"app-net","Driver":"bridge","Scope":"local",
				"Created":"2026-10-02T03:08:00.123456789Z","IPAM":{"Config":[{"Subnet":"172.20.0.0/16","Gateway":"172.20.0.1"}]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/networks":
			_, _ = io.WriteString(w, `[{"Id":"b","Name":"bridge","Driver":"bridge"},{"Id":"a","Name":"app-net","Driver":"bridge"}]`)
		default:
			t.Errorf("unexpected Engine call %s %s", r.Method, r.URL.Path)
		}
	}))
	network, err := client.CreateNetwork(context.Background(), softwaredomain.DockerNetworkSpec{
		Name: "app-net", Subnet: "172.20.0.0/16", Gateway: "172.20.0.1",
	})
	if err != nil {
		t.Fatalf("CreateNetwork error = %v", err)
	}
	if network.ID != "net1" || len(network.Subnets) != 1 || network.Subnets[0].Gateway != "172.20.0.1" || network.CreatedAt == nil {
		t.Errorf("CreateNetwork = %+v, want the read-back network with its subnet and creation time", network)
	}

	networks, err := client.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListNetworks error = %v", err)
	}
	if len(networks) != 2 || networks[0].Name != "app-net" || networks[0].Predefined || !networks[1].Predefined {
		t.Errorf("ListNetworks = %+v, want app-net then predefined bridge", networks)
	}
}
