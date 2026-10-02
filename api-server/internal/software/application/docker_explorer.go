package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// DockerExplorerUseCase is the Docker Host Explorer: live management of the Docker Engine on a
// Server where swallow installed Docker CE with the Engine API enabled (decision 043).
//
// It owns the explorer's rules so no route can skip them:
//
//   - Eligibility, from swallow-owned facts only: the Server exists (else ErrServerNotFound), is
//     deployed with a primary address (else ErrDockerHostUnavailable), has an installed Docker CE
//     assignment (else ErrDockerNotInstalled) whose spec records enableApi=true (else
//     ErrDockerAPIDisabled). Hosts are never probed for an API enabled outside swallow.
//   - Request validation (domain spec rules, ErrInvalidDockerRequest) before any I/O.
//   - The Server Lock for every write (ADR 013): reads stay available on a locked Server, writes
//     fail with the guard's ServerLockedError / ServerLockUnavailableError.
//   - Registry Credentials for pulls (decision 044): the stored credential matching the reference's
//     registry is sent with that one pull, never persisted on the host.
//
// It performs no writes to swallow state: Docker objects are Engine facts (ADR 001), every call is
// a synchronous Engine request, and no Workflow is created. Engine failures surface unchanged as
// *softwaredomain.DockerEngineError for delivery to map.
type DockerExplorerUseCase struct {
	assignments softwaredomain.AssignmentRepository
	servers     serverdomain.ServerRepository
	clients     softwaredomain.DockerEngineClientFactory
	guard       serverdomain.MutationGuard
	credentials softwaredomain.RegistryCredentialRepository
}

// NewDockerExplorerUseCase wires the assignment and server repositories (eligibility), the Engine
// client factory, and the provider-backed lock guard (writes).
func NewDockerExplorerUseCase(
	assignments softwaredomain.AssignmentRepository,
	servers serverdomain.ServerRepository,
	clients softwaredomain.DockerEngineClientFactory,
	guard serverdomain.MutationGuard,
) *DockerExplorerUseCase {
	return &DockerExplorerUseCase{assignments: assignments, servers: servers, clients: clients, guard: guard}
}

// Summary reads the Engine header.
func (uc *DockerExplorerUseCase) Summary(ctx context.Context, serverID string) (*softwaredomain.DockerEngineSummary, error) {
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.Summary(ctx)
}

// ListImages lists the host's local images.
func (uc *DockerExplorerUseCase) ListImages(ctx context.Context, serverID string) ([]softwaredomain.DockerImage, error) {
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.ListImages(ctx)
}

// AttachRegistryCredentials wires the sealed Registry Credential store used by PullImage. Without
// it every pull is anonymous.
func (uc *DockerExplorerUseCase) AttachRegistryCredentials(credentials softwaredomain.RegistryCredentialRepository) {
	uc.credentials = credentials
}

// PullImage pulls one reference (latest when untagged) and returns the canonical reference, the
// Engine's final status, the resolved registry, and whether a stored credential was sent. The
// reference is validated before the eligibility and lock checks reach the provider, so a malformed
// request costs no I/O. A credential-store failure fails the pull instead of silently falling back
// to an anonymous pull that would report a misleading "access denied".
func (uc *DockerExplorerUseCase) PullImage(ctx context.Context, serverID, reference string) (*softwaredomain.DockerImagePullResult, error) {
	name, tagOrDigest, canonical, err := softwaredomain.ParseImageReference(reference)
	if err != nil {
		return nil, err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	registry := softwaredomain.ImageRegistry(name)
	auth, err := uc.registryAuth(ctx, registry)
	if err != nil {
		return nil, err
	}
	status, err := client.PullImage(ctx, name, tagOrDigest, auth)
	if err != nil {
		return nil, withPullAuthNote(err, registry, auth)
	}
	return &softwaredomain.DockerImagePullResult{
		Reference: canonical, Status: status, Registry: registry, Authenticated: auth != nil,
	}, nil
}

// withPullAuthNote appends to an Engine pull failure whether the pull was anonymous or signed in, and
// to which registry. A registry's "pull access denied" reads the same either way, and the operator
// cannot otherwise tell that no saved credential matched the reference.
func withPullAuthNote(err error, registry string, auth *softwaredomain.DockerRegistryAuth) error {
	var engineErr *softwaredomain.DockerEngineError
	if !errors.As(err, &engineErr) {
		return err
	}
	note := fmt.Sprintf(" (pulled anonymously: no registry credential is saved for %s)", registry)
	if auth != nil {
		note = fmt.Sprintf(" (signed in to %s as %s)", registry, auth.Username)
	}
	return &softwaredomain.DockerEngineError{Kind: engineErr.Kind, Detail: engineErr.Detail + note, Err: engineErr.Err}
}

// registryAuth returns the stored credential for a registry, or nil for an anonymous pull.
func (uc *DockerExplorerUseCase) registryAuth(ctx context.Context, registry string) (*softwaredomain.DockerRegistryAuth, error) {
	if uc.credentials == nil {
		return nil, nil
	}
	credential, password, err := uc.credentials.FindAuth(ctx, registry)
	if errors.Is(err, softwaredomain.ErrRegistryCredentialNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry credential for %s: %w", registry, err)
	}
	return &softwaredomain.DockerRegistryAuth{
		Username: credential.Username, Password: password, ServerAddress: softwaredomain.RegistryServerAddress(registry),
	}, nil
}

// RemoveImage removes one image by id; force also removes multi-tagged or stopped-container images.
func (uc *DockerExplorerUseCase) RemoveImage(ctx context.Context, serverID, imageID string, force bool) error {
	if err := requireIdentifier("image id", imageID); err != nil {
		return err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return err
	}
	return client.RemoveImage(ctx, imageID, force)
}

// ListContainers lists every container, running or not.
func (uc *DockerExplorerUseCase) ListContainers(ctx context.Context, serverID string) ([]softwaredomain.DockerContainer, error) {
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.ListContainers(ctx)
}

// CreateContainer validates the spec and creates (and optionally starts) a container. A start
// failure after a successful create is returned as the error with the created id still reported,
// matching DockerEngineClient.CreateContainer.
func (uc *DockerExplorerUseCase) CreateContainer(ctx context.Context, serverID string, spec softwaredomain.DockerContainerSpec) (*softwaredomain.DockerContainerCreated, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.CreateContainer(ctx, spec)
}

// ContainerAction is a lifecycle verb the explorer applies to an existing container. The values
// match the contract's route segments (start|stop|restart).
type ContainerAction string

const (
	// ContainerStart starts a created or stopped container; already running is success.
	ContainerStart ContainerAction = "start"
	// ContainerStop stops a running container, killing it after the Engine's 10-second grace.
	ContainerStop ContainerAction = "stop"
	// ContainerRestart stops (with the same grace) and starts a container.
	ContainerRestart ContainerAction = "restart"
)

// ActOnContainer starts, stops, or restarts one container. An unknown action is a validation error.
func (uc *DockerExplorerUseCase) ActOnContainer(ctx context.Context, serverID, containerID string, action ContainerAction) error {
	if err := requireIdentifier("container id", containerID); err != nil {
		return err
	}
	if action != ContainerStart && action != ContainerStop && action != ContainerRestart {
		return fmt.Errorf("%w: unknown container action %q", softwaredomain.ErrInvalidDockerRequest, action)
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return err
	}
	switch action {
	case ContainerStart:
		return client.StartContainer(ctx, containerID)
	case ContainerStop:
		return client.StopContainer(ctx, containerID)
	default:
		return client.RestartContainer(ctx, containerID)
	}
}

// RemoveContainer removes one container; force kills a running one first, removeVolumes also
// removes its anonymous volumes.
func (uc *DockerExplorerUseCase) RemoveContainer(ctx context.Context, serverID, containerID string, force, removeVolumes bool) error {
	if err := requireIdentifier("container id", containerID); err != nil {
		return err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return err
	}
	return client.RemoveContainer(ctx, containerID, force, removeVolumes)
}

// ContainerLogs reads a bounded log snapshot. tailLines must already be clamped by the caller to
// the contract's range; a non-positive value is a validation error.
func (uc *DockerExplorerUseCase) ContainerLogs(ctx context.Context, serverID, containerID string, tailLines int) (string, error) {
	if err := requireIdentifier("container id", containerID); err != nil {
		return "", err
	}
	if tailLines <= 0 {
		return "", fmt.Errorf("%w: tailLines must be positive", softwaredomain.ErrInvalidDockerRequest)
	}
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return "", err
	}
	return client.ContainerLogs(ctx, containerID, tailLines)
}

// ListVolumes lists the host's volumes.
func (uc *DockerExplorerUseCase) ListVolumes(ctx context.Context, serverID string) ([]softwaredomain.DockerVolume, error) {
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.ListVolumes(ctx)
}

// CreateVolume validates the spec and creates a volume.
func (uc *DockerExplorerUseCase) CreateVolume(ctx context.Context, serverID string, spec softwaredomain.DockerVolumeSpec) (*softwaredomain.DockerVolume, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.CreateVolume(ctx, spec)
}

// RemoveVolume removes one volume; force removes it even when the Engine reports it in use by a
// stopped container.
func (uc *DockerExplorerUseCase) RemoveVolume(ctx context.Context, serverID, name string, force bool) error {
	if err := requireIdentifier("volume name", name); err != nil {
		return err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return err
	}
	return client.RemoveVolume(ctx, name, force)
}

// ListNetworks lists the host's networks.
func (uc *DockerExplorerUseCase) ListNetworks(ctx context.Context, serverID string) ([]softwaredomain.DockerNetwork, error) {
	client, err := uc.reader(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.ListNetworks(ctx)
}

// CreateNetwork validates the spec and creates a network.
func (uc *DockerExplorerUseCase) CreateNetwork(ctx context.Context, serverID string, spec softwaredomain.DockerNetworkSpec) (*softwaredomain.DockerNetwork, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return client.CreateNetwork(ctx, spec)
}

// RemoveNetwork removes one network by id or name. A predefined network name is refused before any
// I/O; a predefined network addressed by id is refused by the Engine and mapped to a conflict.
func (uc *DockerExplorerUseCase) RemoveNetwork(ctx context.Context, serverID, networkID string) error {
	if err := requireIdentifier("network id", networkID); err != nil {
		return err
	}
	if softwaredomain.IsPredefinedDockerNetwork(networkID) {
		return fmt.Errorf("%w: %s", softwaredomain.ErrPredefinedDockerNetwork, networkID)
	}
	client, err := uc.writer(ctx, serverID)
	if err != nil {
		return err
	}
	return client.RemoveNetwork(ctx, networkID)
}

// reader resolves an eligible Engine client for a read.
func (uc *DockerExplorerUseCase) reader(ctx context.Context, serverID string) (softwaredomain.DockerEngineClient, error) {
	return uc.resolve(ctx, serverID)
}

// writer resolves an eligible Engine client and then requires the Server to be unlocked. The lock
// is checked after eligibility so an ineligible Server reports why it is ineligible rather than
// costing a live provisioner read.
func (uc *DockerExplorerUseCase) writer(ctx context.Context, serverID string) (softwaredomain.DockerEngineClient, error) {
	client, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if uc.guard != nil {
		if err := uc.guard.RequireUnlocked(ctx, []string{serverID}); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// resolve enforces eligibility and builds the client for the Server's primary address.
func (uc *DockerExplorerUseCase) resolve(ctx context.Context, serverID string) (softwaredomain.DockerEngineClient, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" {
		return nil, fmt.Errorf("%w: %s", softwaredomain.ErrDockerHostUnavailable, server.DisplayName())
	}

	assignment, err := uc.assignments.FindByServerAndKind(ctx, serverID, softwaredomain.KindDockerCE)
	if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		return nil, fmt.Errorf("%w: %s", softwaredomain.ErrDockerNotInstalled, server.DisplayName())
	}
	if err != nil {
		return nil, err
	}
	if assignment.State != softwaredomain.StateInstalled {
		return nil, fmt.Errorf("%w: %s (assignment is %s)", softwaredomain.ErrDockerNotInstalled, server.DisplayName(), assignment.State)
	}
	if !softwaredomain.DockerAPIEnabled(assignment.Spec) {
		return nil, fmt.Errorf("%w: %s", softwaredomain.ErrDockerAPIDisabled, server.DisplayName())
	}

	address := server.PrimaryAddress()
	if address == "" {
		return nil, fmt.Errorf("%w: %s has no known address", softwaredomain.ErrDockerHostUnavailable, server.DisplayName())
	}
	return uc.clients.For(address, softwaredomain.DockerEngineAPIPort)
}

// requireIdentifier rejects an empty path identifier before any I/O.
func requireIdentifier(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", softwaredomain.ErrInvalidDockerRequest, label)
	}
	return nil
}
