// Package dockerengine is the Docker Engine API adapter of the Docker Host Explorer
// (docs/decisions/043-docker-host-management.md).
//
// It implements softwaredomain.DockerEngineClient over plain HTTP against the listener the Docker
// CE playbook opens on a Server, mapping the Engine's JSON into domain view types and its errors
// into *softwaredomain.DockerEngineError. It deliberately depends on no Docker SDK: the explorer
// uses a small, stable subset of the Engine API, and unversioned paths let each host's Engine serve
// its own current API version.
//
// This package must hold no swallow policy — eligibility, request validation, and the Server Lock
// live in the software application layer — and it persists nothing.
package dockerengine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

const (
	// requestTimeout bounds every Engine call except a pull. It covers the Engine's 10-second
	// stop/restart grace with room to spare and matches servers-docker.md.
	requestTimeout = 60 * time.Second
	// pullTimeout bounds a synchronous image pull (servers-docker.md). GPU images (ROCm, CUDA) run to
	// tens of gigabytes; it stays under the production proxy's 1h read timeout so the API reports the
	// timeout rather than the proxy. The Engine aborts the pull when this request ends.
	pullTimeout = 55 * time.Minute
	// dialTimeout keeps an unreachable host from holding a request for the whole requestTimeout.
	dialTimeout = 5 * time.Second
	// stopGraceSeconds is the time the Engine waits for a container to exit before killing it.
	stopGraceSeconds = 10
	// maxErrorBodyBytes caps how much of an Engine error body is read into a message.
	maxErrorBodyBytes = 4 << 10
	// maxJSONBodyBytes caps a list response; a busy host's container list is far smaller.
	maxJSONBodyBytes = 32 << 20
	// maxLogBytes bounds a log snapshot on top of the tailLines cap, so one pathologically long line
	// cannot stream unbounded memory into the response (servers-docker.md: 4 MiB).
	maxLogBytes = 4 << 20
)

// Factory builds per-Server Engine clients that share one HTTP transport.
//
// Sharing the transport lets requests to the same host reuse a keep-alive connection while
// clients stay cheap and per-request. The transport never uses an environment proxy: Engine hosts
// are on the datacenter network api-server already reaches directly, and a corporate HTTP proxy
// must not see (or break) unauthenticated Engine traffic. Safe for concurrent use.
type Factory struct {
	httpClient *http.Client
}

var _ softwaredomain.DockerEngineClientFactory = (*Factory)(nil)

// NewFactory builds the Engine client factory with its shared transport. Per-call deadlines come
// from contexts (see requestTimeout and pullTimeout), so the http.Client itself has no Timeout —
// a whole-request timeout would also cut off a legitimately long pull stream.
func NewFactory() *Factory {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: requestTimeout,
	}
	return &Factory{httpClient: &http.Client{Transport: transport}}
}

// For returns a client for the Engine API at address:port. It performs no I/O; an unreachable host
// surfaces on the first call as a DockerEngineUnavailable error.
func (f *Factory) For(address string, port int) (softwaredomain.DockerEngineClient, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("docker engine address is empty")
	}
	// JoinHostPort brackets IPv6 literals so an IPv6 primary address forms a valid URL.
	host := net.JoinHostPort(address, strconv.Itoa(port))
	return &Client{baseURL: "http://" + host, endpoint: "tcp://" + host, httpClient: f.httpClient}, nil
}

// Client is a live client for one host's Docker Engine API. It is created per request by Factory.
type Client struct {
	baseURL    string
	endpoint   string
	httpClient *http.Client
}

var _ softwaredomain.DockerEngineClient = (*Client)(nil)

// ---- transport ----

// do sends one request and returns the open response; the caller owns closing the body. A transport
// failure becomes a DockerEngineUnavailable error naming the endpoint.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	return c.doWithHeaders(ctx, method, path, query, body, nil)
}

// registryAuthHeader encodes a registry credential the way the Engine reads X-Registry-Auth: the
// JSON auth configuration, base64url-encoded.
func registryAuthHeader(auth *softwaredomain.DockerRegistryAuth) (string, error) {
	raw, err := json.Marshal(map[string]string{
		"username": auth.Username, "password": auth.Password, "serveraddress": auth.ServerAddress,
	})
	if err != nil {
		return "", fmt.Errorf("encode registry auth: %w", err)
	}
	return base64.URLEncoding.EncodeToString(raw), nil
}

// doWithHeaders is do with extra request headers (the per-pull registry credential).
func (c *Client) doWithHeaders(ctx context.Context, method, path string, query url.Values, body any, headers http.Header) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode docker request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("build docker request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.unreachable(err)
	}
	return resp, nil
}

// call performs a bounded request and decodes a JSON body into out (nil discards the body). Any
// 2xx status succeeds, as does a status listed in alsoOK (the Engine answers 304 when a container
// is already in the requested state). Every other status becomes a DockerEngineError.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any, alsoOK ...int) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	resp, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !succeeded(resp.StatusCode, alsoOK) {
		return engineError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBodyBytes)).Decode(out); err != nil {
		return &softwaredomain.DockerEngineError{
			Kind: softwaredomain.DockerEngineUnavailable, Detail: "The Docker Engine returned an unreadable response", Err: err,
		}
	}
	return nil
}

func succeeded(status int, alsoOK []int) bool {
	if status >= 200 && status <= 299 {
		return true
	}
	for _, ok := range alsoOK {
		if status == ok {
			return true
		}
	}
	return false
}

// unreachable classifies a transport failure, keeping the low-level cause (for example "connection
// refused" or a timeout) in the message because it is what tells an operator whether the listener
// is closed or the host is down.
func (c *Client) unreachable(err error) error {
	cause := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		cause = urlErr.Err
	}
	reason := cause.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timed out"
	}
	return &softwaredomain.DockerEngineError{
		Kind:   softwaredomain.DockerEngineUnavailable,
		Detail: fmt.Sprintf("Could not reach the Docker Engine API at %s (%s)", c.endpoint, reason),
		Err:    err,
	}
}

// engineError maps an Engine error response. The Engine reports {"message": "..."}; its reason is
// kept verbatim because it is the actionable part ("No such image", "port is already allocated").
// 403 is the Engine's answer for protected objects (predefined networks), so it is a conflict.
func engineError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	message := strings.TrimSpace(string(raw))
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Message != "" {
		message = body.Message
	}
	if message == "" {
		message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineNotFound, Detail: message}
	case resp.StatusCode == http.StatusConflict, resp.StatusCode == http.StatusForbidden:
		return &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineConflict, Detail: message}
	case resp.StatusCode >= http.StatusInternalServerError:
		return &softwaredomain.DockerEngineError{
			Kind: softwaredomain.DockerEngineUnavailable, Detail: "The Docker Engine reported an error: " + message,
		}
	default:
		return &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineRejected, Detail: message}
	}
}

// objectPath escapes an Engine object id or name for a path segment (image ids contain ':').
func objectPath(prefix, id, suffix string) string {
	return prefix + "/" + url.PathEscape(id) + suffix
}

func boolQuery(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// parseEngineTime reads the Engine's RFC 3339 timestamps. The Engine reports the zero time as
// "0001-01-01T00:00:00Z" for objects that never had one; that and an empty string become nil.
func parseEngineTime(value string) *time.Time {
	if value == "" || strings.HasPrefix(value, "0001-") {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

// ---- summary ----

// Summary combines /info (counts, host facts) and /version (the negotiated API version).
func (c *Client) Summary(ctx context.Context) (*softwaredomain.DockerEngineSummary, error) {
	var info struct {
		ServerVersion     string `json:"ServerVersion"`
		OperatingSystem   string `json:"OperatingSystem"`
		OSType            string `json:"OSType"`
		Architecture      string `json:"Architecture"`
		KernelVersion     string `json:"KernelVersion"`
		Driver            string `json:"Driver"`
		NCPU              int    `json:"NCPU"`
		MemTotal          int64  `json:"MemTotal"`
		Containers        int    `json:"Containers"`
		ContainersRunning int    `json:"ContainersRunning"`
		ContainersPaused  int    `json:"ContainersPaused"`
		ContainersStopped int    `json:"ContainersStopped"`
		Images            int    `json:"Images"`
	}
	if err := c.call(ctx, http.MethodGet, "/info", nil, nil, &info); err != nil {
		return nil, err
	}
	var version struct {
		APIVersion string `json:"ApiVersion"`
	}
	if err := c.call(ctx, http.MethodGet, "/version", nil, nil, &version); err != nil {
		return nil, err
	}
	return &softwaredomain.DockerEngineSummary{
		Endpoint: c.endpoint, ServerVersion: info.ServerVersion, APIVersion: version.APIVersion,
		OperatingSystem: info.OperatingSystem, OSType: info.OSType, Architecture: info.Architecture,
		KernelVersion: info.KernelVersion, StorageDriver: info.Driver, CPUs: info.NCPU, MemoryBytes: info.MemTotal,
		Containers: info.Containers, ContainersRunning: info.ContainersRunning,
		ContainersPaused: info.ContainersPaused, ContainersStopped: info.ContainersStopped, Images: info.Images,
	}, nil
}

// ---- images ----

// ListImages lists local images newest first. Docker's "<none>" placeholders are dropped so an
// untagged image reads as dangling instead of carrying a fake tag.
func (c *Client) ListImages(ctx context.Context) ([]softwaredomain.DockerImage, error) {
	var items []struct {
		ID          string   `json:"Id"`
		RepoTags    []string `json:"RepoTags"`
		RepoDigests []string `json:"RepoDigests"`
		Created     int64    `json:"Created"`
		Size        int64    `json:"Size"`
	}
	if err := c.call(ctx, http.MethodGet, "/images/json", nil, nil, &items); err != nil {
		return nil, err
	}
	images := make([]softwaredomain.DockerImage, 0, len(items))
	for _, item := range items {
		tags := withoutPlaceholders(item.RepoTags)
		images = append(images, softwaredomain.DockerImage{
			ID: item.ID, RepoTags: tags, RepoDigests: withoutPlaceholders(item.RepoDigests),
			SizeBytes: item.Size, CreatedAt: time.Unix(item.Created, 0).UTC(), Dangling: len(tags) == 0,
		})
	}
	sort.SliceStable(images, func(i, j int) bool {
		if !images[i].CreatedAt.Equal(images[j].CreatedAt) {
			return images[i].CreatedAt.After(images[j].CreatedAt)
		}
		return images[i].ID < images[j].ID
	})
	return images, nil
}

func withoutPlaceholders(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, "<none>") {
			continue
		}
		out = append(out, value)
	}
	return out
}

// PullImage pulls name at tagOrDigest and consumes the Engine's progress stream to the end.
//
// The Engine answers 200 as soon as the pull starts and reports a later failure inside the stream,
// so success is only declared after the stream ends without an error message. The stream is decoded
// incrementally and never buffered, so a long pull costs no memory beyond one message. A non-nil
// auth travels only in the X-Registry-Auth header of this request; the Engine's error messages never
// echo it, so passing them through cannot leak the password.
func (c *Client) PullImage(ctx context.Context, name, tagOrDigest string, auth *softwaredomain.DockerRegistryAuth) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	query := url.Values{"fromImage": {name}, "tag": {tagOrDigest}}
	var headers http.Header
	if auth != nil {
		encoded, err := registryAuthHeader(auth)
		if err != nil {
			return "", err
		}
		headers = http.Header{"X-Registry-Auth": {encoded}}
	}
	resp, err := c.doWithHeaders(ctx, http.MethodPost, "/images/create", query, nil, headers)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if !succeeded(resp.StatusCode, nil) {
		return "", engineError(resp)
	}

	decoder := json.NewDecoder(resp.Body)
	last := ""
	for {
		var message struct {
			Status      string `json:"status"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		err := decoder.Decode(&message)
		if errors.Is(err, io.EOF) {
			return last, nil
		}
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", &softwaredomain.DockerEngineError{
					Kind:   softwaredomain.DockerEngineUnavailable,
					Detail: fmt.Sprintf("The image pull did not finish within %s", pullTimeout),
					Err:    ctx.Err(),
				}
			}
			return "", &softwaredomain.DockerEngineError{
				Kind: softwaredomain.DockerEngineUnavailable, Detail: "The image pull stream ended unexpectedly", Err: err,
			}
		}
		if reason := firstNonEmpty(message.ErrorDetail.Message, message.Error); reason != "" {
			return "", &softwaredomain.DockerEngineError{
				Kind: softwaredomain.DockerEngineUnavailable, Detail: "The Docker Engine could not pull the image: " + reason,
			}
		}
		if message.Status != "" {
			last = message.Status
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// RemoveImage removes one image by id.
func (c *Client) RemoveImage(ctx context.Context, id string, force bool) error {
	query := url.Values{"force": {boolQuery(force)}}
	return c.call(ctx, http.MethodDelete, objectPath("/images", id, ""), query, nil, nil)
}

// ---- containers ----

// ListContainers lists every container (all=1), ordered by name.
func (c *Client) ListContainers(ctx context.Context) ([]softwaredomain.DockerContainer, error) {
	var items []struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		Image   string   `json:"Image"`
		ImageID string   `json:"ImageID"`
		Command string   `json:"Command"`
		Created int64    `json:"Created"`
		State   string   `json:"State"`
		Status  string   `json:"Status"`
		Ports   []struct {
			IP          string `json:"IP"`
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
		NetworkSettings struct {
			Networks map[string]json.RawMessage `json:"Networks"`
		} `json:"NetworkSettings"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
	}
	if err := c.call(ctx, http.MethodGet, "/containers/json", url.Values{"all": {"1"}}, nil, &items); err != nil {
		return nil, err
	}
	containers := make([]softwaredomain.DockerContainer, 0, len(items))
	for _, item := range items {
		container := softwaredomain.DockerContainer{
			ID: item.ID, Image: item.Image, ImageID: item.ImageID, Command: item.Command,
			State: item.State, Status: item.Status, CreatedAt: time.Unix(item.Created, 0).UTC(),
		}
		if len(item.Names) > 0 {
			// The Engine prefixes names with "/" (a legacy of container links).
			container.Name = strings.TrimPrefix(item.Names[0], "/")
		}
		for _, port := range item.Ports {
			container.Ports = append(container.Ports, softwaredomain.DockerPort{
				IP: port.IP, PrivatePort: port.PrivatePort, PublicPort: port.PublicPort, Protocol: port.Type,
			})
		}
		for network := range item.NetworkSettings.Networks {
			container.Networks = append(container.Networks, network)
		}
		sort.Strings(container.Networks)
		for _, mount := range item.Mounts {
			source := mount.Source
			if mount.Type == "volume" && mount.Name != "" {
				source = mount.Name
			}
			container.Mounts = append(container.Mounts, softwaredomain.DockerMount{
				Type: mount.Type, Source: source, Destination: mount.Destination, ReadOnly: !mount.RW,
			})
		}
		containers = append(containers, container)
	}
	sort.SliceStable(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return containers, nil
}

// CreateContainer translates the validated spec into the Engine's create body, then starts the
// container when asked. Binds carry both named volumes and host paths (source:target[:ro]); a
// HostPort of 0 is sent as "" so the Engine picks a free port.
func (c *Client) CreateContainer(ctx context.Context, spec softwaredomain.DockerContainerSpec) (*softwaredomain.DockerContainerCreated, error) {
	exposed := map[string]struct{}{}
	bindings := map[string][]map[string]string{}
	for _, port := range spec.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		key := fmt.Sprintf("%d/%s", port.ContainerPort, protocol)
		exposed[key] = struct{}{}
		hostPort := ""
		if port.HostPort > 0 {
			hostPort = strconv.Itoa(port.HostPort)
		}
		bindings[key] = append(bindings[key], map[string]string{"HostIp": port.HostIP, "HostPort": hostPort})
	}
	binds := make([]string, 0, len(spec.Volumes))
	for _, volume := range spec.Volumes {
		bind := volume.Source + ":" + volume.Target
		if volume.ReadOnly {
			bind += ":ro"
		}
		binds = append(binds, bind)
	}
	restartPolicy := spec.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "no"
	}
	hostConfig := map[string]any{
		"PortBindings":  bindings,
		"Binds":         binds,
		"RestartPolicy": map[string]any{"Name": restartPolicy},
	}
	if spec.Network != "" {
		hostConfig["NetworkMode"] = spec.Network
	}
	body := map[string]any{
		"Image":        spec.Image,
		"Env":          spec.Env,
		"ExposedPorts": exposed,
		"HostConfig":   hostConfig,
	}
	if len(spec.Command) > 0 {
		body["Cmd"] = spec.Command
	}
	var query url.Values
	if spec.Name != "" {
		query = url.Values{"name": {spec.Name}}
	}

	var created struct {
		ID       string   `json:"Id"`
		Warnings []string `json:"Warnings"`
	}
	if err := c.call(ctx, http.MethodPost, "/containers/create", query, body, &created); err != nil {
		return nil, err
	}
	result := &softwaredomain.DockerContainerCreated{ID: created.ID, Warnings: created.Warnings}
	if !spec.Start {
		return result, nil
	}
	if err := c.StartContainer(ctx, created.ID); err != nil {
		var engineErr *softwaredomain.DockerEngineError
		if errors.As(err, &engineErr) {
			return result, &softwaredomain.DockerEngineError{
				Kind: engineErr.Kind, Detail: "The container was created but could not be started: " + engineErr.Detail, Err: engineErr.Err,
			}
		}
		return result, err
	}
	result.Started = true
	return result, nil
}

// StartContainer starts a container; 304 (already running) is success.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodPost, objectPath("/containers", id, "/start"), nil, nil, nil, http.StatusNotModified)
}

// StopContainer stops a container after the grace period; 304 (already stopped) is success.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	query := url.Values{"t": {strconv.Itoa(stopGraceSeconds)}}
	return c.call(ctx, http.MethodPost, objectPath("/containers", id, "/stop"), query, nil, nil, http.StatusNotModified)
}

// RestartContainer restarts a container with the same grace period as stop.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	query := url.Values{"t": {strconv.Itoa(stopGraceSeconds)}}
	return c.call(ctx, http.MethodPost, objectPath("/containers", id, "/restart"), query, nil, nil)
}

// RemoveContainer removes a container; v=1 also removes its anonymous volumes.
func (c *Client) RemoveContainer(ctx context.Context, id string, force, removeVolumes bool) error {
	query := url.Values{"force": {boolQuery(force)}, "v": {boolQuery(removeVolumes)}}
	return c.call(ctx, http.MethodDelete, objectPath("/containers", id, ""), query, nil, nil)
}

// ContainerLogs returns the last tailLines lines of stdout and stderr.
//
// Without a TTY the Engine multiplexes both streams into frames with an 8-byte header; with a TTY it
// sends raw bytes. The container is inspected first so the right decoding is chosen rather than
// guessed from the payload. The read is capped at maxLogBytes; a frame cut by the cap is kept up
// to the cut.
func (c *Client) ContainerLogs(ctx context.Context, id string, tailLines int) (string, error) {
	var inspect struct {
		Config struct {
			Tty bool `json:"Tty"`
		} `json:"Config"`
	}
	if err := c.call(ctx, http.MethodGet, objectPath("/containers", id, "/json"), nil, nil, &inspect); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	query := url.Values{"stdout": {"1"}, "stderr": {"1"}, "tail": {strconv.Itoa(tailLines)}}
	resp, err := c.do(ctx, http.MethodGet, objectPath("/containers", id, "/logs"), query, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if !succeeded(resp.StatusCode, nil) {
		return "", engineError(resp)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes))
	if err != nil {
		return "", c.unreachable(err)
	}
	if inspect.Config.Tty {
		return string(raw), nil
	}
	return demultiplex(raw), nil
}

// demultiplex concatenates the payloads of the Engine's stdout/stderr frames in arrival order.
// Each frame is [stream byte, 0, 0, 0, uint32 big-endian size] followed by size bytes.
func demultiplex(raw []byte) string {
	var out strings.Builder
	for len(raw) >= 8 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		out.Write(raw[:size])
		raw = raw[size:]
	}
	return out.String()
}

// ---- volumes ----

type volumeJSON struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Scope      string            `json:"Scope"`
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels"`
}

func (v volumeJSON) toVolume() softwaredomain.DockerVolume {
	return softwaredomain.DockerVolume{
		Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Scope: v.Scope,
		CreatedAt: parseEngineTime(v.CreatedAt), Labels: v.Labels,
	}
}

// ListVolumes lists volumes by name.
func (c *Client) ListVolumes(ctx context.Context) ([]softwaredomain.DockerVolume, error) {
	var listing struct {
		Volumes []volumeJSON `json:"Volumes"`
	}
	if err := c.call(ctx, http.MethodGet, "/volumes", nil, nil, &listing); err != nil {
		return nil, err
	}
	volumes := make([]softwaredomain.DockerVolume, 0, len(listing.Volumes))
	for _, item := range listing.Volumes {
		volumes = append(volumes, item.toVolume())
	}
	sort.SliceStable(volumes, func(i, j int) bool { return volumes[i].Name < volumes[j].Name })
	return volumes, nil
}

// CreateVolume creates a volume (driver local by default) and returns the Engine's view of it.
func (c *Client) CreateVolume(ctx context.Context, spec softwaredomain.DockerVolumeSpec) (*softwaredomain.DockerVolume, error) {
	driver := spec.Driver
	if driver == "" {
		driver = "local"
	}
	body := map[string]any{"Name": spec.Name, "Driver": driver, "Labels": spec.Labels}
	var created volumeJSON
	if err := c.call(ctx, http.MethodPost, "/volumes/create", nil, body, &created); err != nil {
		return nil, err
	}
	volume := created.toVolume()
	return &volume, nil
}

// RemoveVolume removes a volume by name.
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	query := url.Values{"force": {boolQuery(force)}}
	return c.call(ctx, http.MethodDelete, objectPath("/volumes", name, ""), query, nil, nil)
}

// ---- networks ----

type networkJSON struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	Created    string `json:"Created"`
	Scope      string `json:"Scope"`
	Driver     string `json:"Driver"`
	Internal   bool   `json:"Internal"`
	Attachable bool   `json:"Attachable"`
	IPAM       struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
}

func (n networkJSON) toNetwork() softwaredomain.DockerNetwork {
	network := softwaredomain.DockerNetwork{
		ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Internal: n.Internal, Attachable: n.Attachable,
		Predefined: softwaredomain.IsPredefinedDockerNetwork(n.Name), CreatedAt: parseEngineTime(n.Created),
	}
	for _, pool := range n.IPAM.Config {
		network.Subnets = append(network.Subnets, softwaredomain.DockerSubnet{Subnet: pool.Subnet, Gateway: pool.Gateway})
	}
	return network
}

// ListNetworks lists networks by name.
func (c *Client) ListNetworks(ctx context.Context) ([]softwaredomain.DockerNetwork, error) {
	var items []networkJSON
	if err := c.call(ctx, http.MethodGet, "/networks", nil, nil, &items); err != nil {
		return nil, err
	}
	networks := make([]softwaredomain.DockerNetwork, 0, len(items))
	for _, item := range items {
		networks = append(networks, item.toNetwork())
	}
	sort.SliceStable(networks, func(i, j int) bool { return networks[i].Name < networks[j].Name })
	return networks, nil
}

// CreateNetwork creates a network (driver bridge by default) and reads it back, because the
// Engine's create response carries only the id.
func (c *Client) CreateNetwork(ctx context.Context, spec softwaredomain.DockerNetworkSpec) (*softwaredomain.DockerNetwork, error) {
	driver := spec.Driver
	if driver == "" {
		driver = "bridge"
	}
	body := map[string]any{"Name": spec.Name, "Driver": driver, "Internal": spec.Internal, "Attachable": spec.Attachable}
	if spec.Subnet != "" {
		pool := map[string]string{"Subnet": spec.Subnet}
		if spec.Gateway != "" {
			pool["Gateway"] = spec.Gateway
		}
		body["IPAM"] = map[string]any{"Config": []map[string]string{pool}}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.call(ctx, http.MethodPost, "/networks/create", nil, body, &created); err != nil {
		return nil, err
	}
	var item networkJSON
	if err := c.call(ctx, http.MethodGet, objectPath("/networks", created.ID, ""), nil, nil, &item); err != nil {
		return nil, err
	}
	network := item.toNetwork()
	return &network, nil
}

// RemoveNetwork removes a network by id or name.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, objectPath("/networks", id, ""), nil, nil, nil)
}
