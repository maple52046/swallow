package domain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// This file defines the Docker Host Explorer: live view types, request specs and their invariants,
// and the client port for managing the Docker Engine on a Server where swallow installed Docker CE
// with the Engine API enabled (docs/decisions/043-docker-host-management.md).
//
// Nothing here is persisted. Images, containers, volumes, and networks are Docker Engine facts read
// from, or written to, the host's Engine API at request time (ADR 001). The only swallow-owned input
// is the Docker CE Software Assignment, which decides eligibility; see DockerAPIEnabled.

// DockerEngineSummary is the small live header of one host's Docker Engine.
type DockerEngineSummary struct {
	// Endpoint is the Engine API address swallow dialled (tcp://host:port), for display.
	Endpoint          string
	ServerVersion     string
	APIVersion        string
	OperatingSystem   string
	OSType            string
	Architecture      string
	KernelVersion     string
	StorageDriver     string
	CPUs              int
	MemoryBytes       int64
	Containers        int
	ContainersRunning int
	ContainersPaused  int
	ContainersStopped int
	Images            int
}

// DockerImage is one image in the host's local image store.
type DockerImage struct {
	ID string
	// RepoTags excludes Docker's "<none>:<none>" placeholder, so an untagged image has none.
	RepoTags    []string
	RepoDigests []string
	SizeBytes   int64
	CreatedAt   time.Time
	// Dangling is true when no tag references the image any more.
	Dangling bool
}

// DockerImagePullResult reports a completed pull: the canonical reference pulled, the Engine's
// final progress message, the registry the reference resolved to, and whether a stored Registry
// Credential was sent.
type DockerImagePullResult struct {
	Reference     string
	Status        string
	Registry      string
	Authenticated bool
}

// DockerContainer is one container, running or not.
type DockerContainer struct {
	ID      string
	Name    string
	Image   string
	ImageID string
	Command string
	// State is the Engine's own value (created, restarting, running, removing, paused, exited,
	// dead), passed through unchanged; an unknown future value must not be treated as a failure.
	State string
	// Status is the Engine's human-readable summary, for example "Up 2 hours".
	Status    string
	CreatedAt time.Time
	Ports     []DockerPort
	Networks  []string
	Mounts    []DockerMount
}

// DockerPort is one exposed container port. PublicPort is 0 when the port is not published.
type DockerPort struct {
	IP          string
	PrivatePort int
	PublicPort  int
	Protocol    string
}

// DockerMount is one container mount. Source is the volume name for a volume mount and the host
// path for a bind mount.
type DockerMount struct {
	Type        string
	Source      string
	Destination string
	ReadOnly    bool
}

// DockerVolume is one Engine volume. CreatedAt is nil when the Engine does not report it.
type DockerVolume struct {
	Name       string
	Driver     string
	Mountpoint string
	Scope      string
	CreatedAt  *time.Time
	Labels     map[string]string
}

// DockerNetwork is one Engine network. Predefined marks the Engine's own bridge/host/none networks,
// which cannot be removed.
type DockerNetwork struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Attachable bool
	Predefined bool
	Subnets    []DockerSubnet
	CreatedAt  *time.Time
}

// DockerSubnet is one IPAM pool of a network. Gateway is empty when none is configured.
type DockerSubnet struct {
	Subnet  string
	Gateway string
}

// DockerContainerSpec is the accepted intent for creating a container. Validate enforces the
// contract rules before any Engine call; the Engine remains the authority on everything else (for
// example whether the image exists locally or the name is free).
type DockerContainerSpec struct {
	Name          string
	Image         string
	Command       []string
	Env           []string
	Ports         []DockerPortBinding
	Volumes       []DockerVolumeBinding
	Network       string
	RestartPolicy string
	// Start starts the container after a successful create.
	Start bool
}

// DockerPortBinding publishes ContainerPort/Protocol on HostIP:HostPort. HostPort 0 lets the Engine
// choose a free port; an empty Protocol means tcp.
type DockerPortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
}

// DockerVolumeBinding mounts Source at Target. Source is a volume name (created on demand) or an
// absolute host path (a bind mount).
type DockerVolumeBinding struct {
	Source   string
	Target   string
	ReadOnly bool
}

// DockerContainerCreated reports a created container and whether it was started.
type DockerContainerCreated struct {
	ID       string
	Warnings []string
	Started  bool
}

// DockerVolumeSpec is the accepted intent for creating a volume. An empty Name lets the Engine
// generate one; an empty Driver means local.
type DockerVolumeSpec struct {
	Name   string
	Driver string
	Labels map[string]string
}

// DockerNetworkSpec is the accepted intent for creating a network. An empty Driver means bridge.
type DockerNetworkSpec struct {
	Name       string
	Driver     string
	Internal   bool
	Attachable bool
	Subnet     string
	Gateway    string
}

// dockerObjectName is the Engine's own rule for container, volume, and network names, checked
// up front so a typo is a clean validation error rather than an Engine round trip.
var dockerObjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// predefinedDockerNetworks are the networks every Engine creates for itself.
var predefinedDockerNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

// IsPredefinedDockerNetwork reports whether a network name (or the id a caller passed) names one of
// the Engine's own networks, which the explorer refuses to remove before contacting the Engine.
func IsPredefinedDockerNetwork(name string) bool {
	return predefinedDockerNetworks[name]
}

// dockerRestartPolicies are the restart policies the contract accepts; empty means "no".
var dockerRestartPolicies = map[string]bool{"": true, "no": true, "always": true, "unless-stopped": true, "on-failure": true}

// dockerProtocols are the port protocols the Engine supports; empty means tcp.
var dockerProtocols = map[string]bool{"": true, "tcp": true, "udp": true, "sctp": true}

// Validate checks the container spec against the contract rules. Every failure wraps
// ErrInvalidDockerRequest so delivery maps it to 400.
func (s DockerContainerSpec) Validate() error {
	if strings.TrimSpace(s.Image) == "" {
		return fmt.Errorf("%w: image is required", ErrInvalidDockerRequest)
	}
	if s.Name != "" && !dockerObjectName.MatchString(s.Name) {
		return fmt.Errorf("%w: container name %q must match [a-zA-Z0-9][a-zA-Z0-9_.-]*", ErrInvalidDockerRequest, s.Name)
	}
	for _, entry := range s.Env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: environment entry %q must be KEY=value", ErrInvalidDockerRequest, entry)
		}
	}
	for _, port := range s.Ports {
		if port.ContainerPort < 1 || port.ContainerPort > 65535 {
			return fmt.Errorf("%w: containerPort %d must be between 1 and 65535", ErrInvalidDockerRequest, port.ContainerPort)
		}
		if port.HostPort < 0 || port.HostPort > 65535 {
			return fmt.Errorf("%w: hostPort %d must be between 0 and 65535", ErrInvalidDockerRequest, port.HostPort)
		}
		if !dockerProtocols[port.Protocol] {
			return fmt.Errorf("%w: protocol %q must be tcp, udp, or sctp", ErrInvalidDockerRequest, port.Protocol)
		}
		if port.HostIP != "" && net.ParseIP(port.HostIP) == nil {
			return fmt.Errorf("%w: hostIp %q is not an IP address", ErrInvalidDockerRequest, port.HostIP)
		}
	}
	for _, volume := range s.Volumes {
		if !strings.HasPrefix(volume.Target, "/") {
			return fmt.Errorf("%w: volume target %q must be an absolute path", ErrInvalidDockerRequest, volume.Target)
		}
		if !strings.HasPrefix(volume.Source, "/") && !dockerObjectName.MatchString(volume.Source) {
			return fmt.Errorf("%w: volume source %q must be a volume name or an absolute host path", ErrInvalidDockerRequest, volume.Source)
		}
		// The Engine's bind syntax is source:target[:ro]; a colon inside either side would be
		// parsed as a different mount, so it is refused rather than silently misinterpreted.
		if strings.Contains(volume.Source, ":") || strings.Contains(volume.Target, ":") {
			return fmt.Errorf("%w: volume paths must not contain ':'", ErrInvalidDockerRequest)
		}
	}
	if !dockerRestartPolicies[s.RestartPolicy] {
		return fmt.Errorf("%w: restartPolicy %q must be no, always, unless-stopped, or on-failure", ErrInvalidDockerRequest, s.RestartPolicy)
	}
	return nil
}

// Validate checks the volume spec: an explicit name must follow the Engine's naming rule.
func (s DockerVolumeSpec) Validate() error {
	if s.Name != "" && !dockerObjectName.MatchString(s.Name) {
		return fmt.Errorf("%w: volume name %q must match [a-zA-Z0-9][a-zA-Z0-9_.-]*", ErrInvalidDockerRequest, s.Name)
	}
	return nil
}

// Validate checks the network spec: a valid, non-predefined name, a CIDR subnet, and a gateway
// that is an IP address and only appears with a subnet.
func (s DockerNetworkSpec) Validate() error {
	if !dockerObjectName.MatchString(s.Name) {
		return fmt.Errorf("%w: network name %q is required and must match [a-zA-Z0-9][a-zA-Z0-9_.-]*", ErrInvalidDockerRequest, s.Name)
	}
	if IsPredefinedDockerNetwork(s.Name) {
		return fmt.Errorf("%w: %q is a predefined network name", ErrInvalidDockerRequest, s.Name)
	}
	if s.Subnet != "" {
		if _, _, err := net.ParseCIDR(s.Subnet); err != nil {
			return fmt.Errorf("%w: subnet %q is not a CIDR", ErrInvalidDockerRequest, s.Subnet)
		}
	}
	if s.Gateway != "" {
		if s.Subnet == "" {
			return fmt.Errorf("%w: gateway requires subnet", ErrInvalidDockerRequest)
		}
		if net.ParseIP(s.Gateway) == nil {
			return fmt.Errorf("%w: gateway %q is not an IP address", ErrInvalidDockerRequest, s.Gateway)
		}
	}
	return nil
}

// ParseImageReference splits an image reference into the name and the tag-or-digest the Engine's
// pull call expects, and returns the canonical reference string.
//
// A reference without a tag or digest resolves to "latest". This matters for safety, not just
// convenience: the Engine pulls every tag of a repository when the tag is empty. A colon only
// separates a tag when it follows the last slash, so a registry port ("host:5000/app") is not
// mistaken for a tag.
func ParseImageReference(reference string) (name, tagOrDigest, canonical string, err error) {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.ContainsAny(reference, " \t\r\n") {
		return "", "", "", fmt.Errorf("%w: image reference %q is required and must not contain whitespace", ErrInvalidDockerRequest, reference)
	}
	if at := strings.Index(reference, "@"); at >= 0 {
		name, digest := reference[:at], reference[at+1:]
		if name == "" || digest == "" {
			return "", "", "", fmt.Errorf("%w: image reference %q has an empty name or digest", ErrInvalidDockerRequest, reference)
		}
		return name, digest, name + "@" + digest, nil
	}
	if colon := strings.LastIndex(reference, ":"); colon > strings.LastIndex(reference, "/") {
		name, tag := reference[:colon], reference[colon+1:]
		if name == "" || tag == "" {
			return "", "", "", fmt.Errorf("%w: image reference %q has an empty name or tag", ErrInvalidDockerRequest, reference)
		}
		return name, tag, name + ":" + tag, nil
	}
	return reference, "latest", reference + ":latest", nil
}

// DockerEngineClient is a live read/write client against one host's Docker Engine API.
//
// Every method calls the Engine at request time and persists nothing in swallow. Implementations
// translate transport failures and Engine error responses into a *DockerEngineError so delivery
// maps them to a stable status, and must bound every call (the context deadline is honoured; an
// implementation may impose a shorter one). A container already in the requested state is success
// for Start/Stop/Restart. Implementations are created per request and need not be safe for reuse
// across Servers.
type DockerEngineClient interface {
	Summary(ctx context.Context) (*DockerEngineSummary, error)

	ListImages(ctx context.Context) ([]DockerImage, error)
	// PullImage pulls name with tagOrDigest (never empty, see ParseImageReference) and returns
	// the Engine's final status; a failure reported mid-stream is an error, not a success. A
	// non-nil auth is sent to the Engine for this request only and must never appear in an error.
	PullImage(ctx context.Context, name, tagOrDigest string, auth *DockerRegistryAuth) (string, error)
	RemoveImage(ctx context.Context, id string, force bool) error

	ListContainers(ctx context.Context) ([]DockerContainer, error)
	// CreateContainer creates (and when spec.Start, starts) a container. When the create succeeds
	// but the start fails, it returns the created id together with the start error so the caller
	// can report that the container exists.
	CreateContainer(ctx context.Context, spec DockerContainerSpec) (*DockerContainerCreated, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string, force, removeVolumes bool) error
	// ContainerLogs returns a bounded snapshot of the last tailLines lines of stdout and stderr.
	ContainerLogs(ctx context.Context, id string, tailLines int) (string, error)

	ListVolumes(ctx context.Context) ([]DockerVolume, error)
	CreateVolume(ctx context.Context, spec DockerVolumeSpec) (*DockerVolume, error)
	RemoveVolume(ctx context.Context, name string, force bool) error

	ListNetworks(ctx context.Context) ([]DockerNetwork, error)
	CreateNetwork(ctx context.Context, spec DockerNetworkSpec) (*DockerNetwork, error)
	RemoveNetwork(ctx context.Context, id string) error
}

// DockerEngineClientFactory builds a client for the Engine API at address:port. The address is the
// Server's observed primary address; the port is DockerEngineAPIPort. It performs no I/O.
type DockerEngineClientFactory interface {
	For(address string, port int) (DockerEngineClient, error)
}

// DockerEngineErrorKind classifies a Docker Engine API failure so delivery can map it without
// knowing HTTP details of the Engine.
type DockerEngineErrorKind string

const (
	// DockerEngineUnavailable: the Engine could not be reached, timed out, or failed internally.
	DockerEngineUnavailable DockerEngineErrorKind = "unavailable"
	// DockerEngineNotFound: the addressed image, container, volume, or network does not exist.
	DockerEngineNotFound DockerEngineErrorKind = "not_found"
	// DockerEngineConflict: the object is in use, its name is taken, or the Engine protects it.
	DockerEngineConflict DockerEngineErrorKind = "conflict"
	// DockerEngineRejected: the Engine rejected the request parameters.
	DockerEngineRejected DockerEngineErrorKind = "rejected"
)

// DockerEngineError is a failure reported by, or while reaching, a host's Docker Engine API. Detail
// is a human-readable message that includes the Engine's own reason and is safe to show operators
// (the Engine API carries no credential in this version).
type DockerEngineError struct {
	Kind   DockerEngineErrorKind
	Detail string
	Err    error
}

// Error returns Detail with the underlying cause appended for logs. Delivery shows only Detail to
// operators, because the cause may carry transport internals (dial addresses, timeouts).
func (e *DockerEngineError) Error() string {
	if e.Err == nil {
		return e.Detail
	}
	return e.Detail + ": " + e.Err.Error()
}

// Unwrap exposes the transport or decode cause so callers can test it with errors.Is/As (for
// example context.DeadlineExceeded).
func (e *DockerEngineError) Unwrap() error { return e.Err }

var (
	// ErrDockerHostUnavailable means the Server is absent, not deployed, or has no known address,
	// so there is no host to dial regardless of what software it carries.
	ErrDockerHostUnavailable = errors.New("server is not a deployed host with a known address")
	// ErrDockerNotInstalled means swallow has no installed Docker CE Software Assignment for the
	// Server; an operator-installed Docker is deliberately not used.
	ErrDockerNotInstalled = errors.New("docker ce is not installed on this server by swallow")
	// ErrDockerAPIDisabled means the Docker CE assignment does not record enableApi=true, including
	// legacy records that predate the variant.
	ErrDockerAPIDisabled = errors.New("the docker engine api is not enabled for this server")
	// ErrInvalidDockerRequest is a request-shape failure the delivery layer maps to 400.
	ErrInvalidDockerRequest = errors.New("invalid docker request")
	// ErrPredefinedDockerNetwork means a caller asked to remove bridge, host, or none.
	ErrPredefinedDockerNetwork = errors.New("a predefined docker network cannot be removed")
)
