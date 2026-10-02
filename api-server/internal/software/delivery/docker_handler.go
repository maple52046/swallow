package delivery

import (
	"errors"
	"log"
	"net/url"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// This file is the Docker Host Explorer HTTP surface, contracted in
// docs/development/api-contracts/api-server/servers-docker.md. Handlers parse the request, call the
// DockerExplorerUseCase (which owns eligibility, validation, and the lock), and shape the contract's
// wire DTOs. Nothing here is persisted and no handler talks to the Engine directly.

const (
	defaultContainerLogTailLines = 200
	maxContainerLogTailLines     = 2000
)

// DockerHandler serves /servers/{id}/docker. Routes are mounted on the admin-only /servers group,
// so every handler runs after authentication and the admin check.
type DockerHandler struct {
	explorer *softwareapp.DockerExplorerUseCase
}

// NewDockerHandler wires the Docker Host Explorer use case to HTTP.
func NewDockerHandler(explorer *softwareapp.DockerExplorerUseCase) *DockerHandler {
	return &DockerHandler{explorer: explorer}
}

// pathParam returns a decoded path parameter. Fiber hands parameters back still percent-encoded, and
// clients must encode Engine ids (image ids contain ':'), so decoding here keeps the id the Engine
// sees identical to the one the list returned. An undecodable value is passed through unchanged
// and fails at the Engine as not found.
func pathParam(c *fiber.Ctx, name string) string {
	raw := c.Params(name)
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

func queryFlag(c *fiber.Ctx, name string) bool {
	return c.Query(name) == "true"
}

func invalidBody(c *fiber.Ctx) error {
	return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
}

func success(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true})
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

// ---- summary ----

type dockerSummaryResponse struct {
	Endpoint          string `json:"endpoint"`
	ServerVersion     string `json:"serverVersion"`
	APIVersion        string `json:"apiVersion"`
	OperatingSystem   string `json:"operatingSystem"`
	OSType            string `json:"osType"`
	Architecture      string `json:"architecture"`
	KernelVersion     string `json:"kernelVersion"`
	StorageDriver     string `json:"storageDriver"`
	CPUs              int    `json:"cpus"`
	MemoryBytes       int64  `json:"memoryBytes"`
	Containers        int    `json:"containers"`
	ContainersRunning int    `json:"containersRunning"`
	ContainersPaused  int    `json:"containersPaused"`
	ContainersStopped int    `json:"containersStopped"`
	Images            int    `json:"images"`
}

// Summary returns the live Engine header.
func (h *DockerHandler) Summary(c *fiber.Ctx) error {
	summary, err := h.explorer.Summary(c.Context(), c.Params("id"))
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.JSON(dockerSummaryResponse{
		Endpoint: summary.Endpoint, ServerVersion: summary.ServerVersion, APIVersion: summary.APIVersion,
		OperatingSystem: summary.OperatingSystem, OSType: summary.OSType, Architecture: summary.Architecture,
		KernelVersion: summary.KernelVersion, StorageDriver: summary.StorageDriver, CPUs: summary.CPUs,
		MemoryBytes: summary.MemoryBytes, Containers: summary.Containers, ContainersRunning: summary.ContainersRunning,
		ContainersPaused: summary.ContainersPaused, ContainersStopped: summary.ContainersStopped, Images: summary.Images,
	})
}

// ---- images ----

type dockerImageResponse struct {
	ID          string   `json:"id"`
	RepoTags    []string `json:"repoTags"`
	RepoDigests []string `json:"repoDigests"`
	SizeBytes   int64    `json:"sizeBytes"`
	CreatedAt   string   `json:"createdAt"`
	Dangling    bool     `json:"dangling"`
}

// ListImages returns the host's images, newest first.
func (h *DockerHandler) ListImages(c *fiber.Ctx) error {
	images, err := h.explorer.ListImages(c.Context(), c.Params("id"))
	if err != nil {
		return respondDockerError(c, err)
	}
	items := make([]dockerImageResponse, 0, len(images))
	for _, image := range images {
		items = append(items, dockerImageResponse{
			ID: image.ID, RepoTags: stringsOrEmpty(image.RepoTags), RepoDigests: stringsOrEmpty(image.RepoDigests),
			SizeBytes: image.SizeBytes, CreatedAt: formatTime(image.CreatedAt), Dangling: image.Dangling,
		})
	}
	return c.JSON(fiber.Map{"items": items})
}

type pullImageRequest struct {
	Reference string `json:"reference"`
}

// PullImage pulls one image reference synchronously (bounded by the use case's client).
func (h *DockerHandler) PullImage(c *fiber.Ctx) error {
	var req pullImageRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	// fasthttp cancels c.Context() only on server shutdown, not when the client disconnects, so a pull
	// whose caller stops waiting (a closed browser tab) still completes within the pull bound.
	result, err := h.explorer.PullImage(c.Context(), c.Params("id"), req.Reference)
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.JSON(fiber.Map{
		"reference": result.Reference, "status": result.Status,
		"registry": result.Registry, "authenticated": result.Authenticated,
	})
}

// RemoveImage removes one image; ?force=true also removes multi-tagged images.
func (h *DockerHandler) RemoveImage(c *fiber.Ctx) error {
	if err := h.explorer.RemoveImage(c.Context(), c.Params("id"), pathParam(c, "imageId"), queryFlag(c, "force")); err != nil {
		return respondDockerError(c, err)
	}
	return success(c)
}

// ---- containers ----

type dockerPortResponse struct {
	IP          string `json:"ip"`
	PrivatePort int    `json:"privatePort"`
	// PublicPort is null for an exposed but unpublished port.
	PublicPort *int   `json:"publicPort"`
	Protocol   string `json:"protocol"`
}

type dockerMountResponse struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

type dockerContainerResponse struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Image     string                `json:"image"`
	ImageID   string                `json:"imageId"`
	Command   string                `json:"command"`
	State     string                `json:"state"`
	Status    string                `json:"status"`
	CreatedAt string                `json:"createdAt"`
	Ports     []dockerPortResponse  `json:"ports"`
	Networks  []string              `json:"networks"`
	Mounts    []dockerMountResponse `json:"mounts"`
}

func containerResponse(container softwaredomain.DockerContainer) dockerContainerResponse {
	ports := make([]dockerPortResponse, 0, len(container.Ports))
	for _, port := range container.Ports {
		var public *int
		if port.PublicPort > 0 {
			value := port.PublicPort
			public = &value
		}
		ports = append(ports, dockerPortResponse{IP: port.IP, PrivatePort: port.PrivatePort, PublicPort: public, Protocol: port.Protocol})
	}
	mounts := make([]dockerMountResponse, 0, len(container.Mounts))
	for _, mount := range container.Mounts {
		mounts = append(mounts, dockerMountResponse{
			Type: mount.Type, Source: mount.Source, Destination: mount.Destination, ReadOnly: mount.ReadOnly,
		})
	}
	return dockerContainerResponse{
		ID: container.ID, Name: container.Name, Image: container.Image, ImageID: container.ImageID,
		Command: container.Command, State: container.State, Status: container.Status,
		CreatedAt: formatTime(container.CreatedAt), Ports: ports, Networks: stringsOrEmpty(container.Networks), Mounts: mounts,
	}
}

// ListContainers returns every container on the host by name.
func (h *DockerHandler) ListContainers(c *fiber.Ctx) error {
	containers, err := h.explorer.ListContainers(c.Context(), c.Params("id"))
	if err != nil {
		return respondDockerError(c, err)
	}
	items := make([]dockerContainerResponse, 0, len(containers))
	for _, container := range containers {
		items = append(items, containerResponse(container))
	}
	return c.JSON(fiber.Map{"items": items})
}

type createContainerRequest struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command []string `json:"command"`
	Env     []string `json:"env"`
	Ports   []struct {
		ContainerPort int    `json:"containerPort"`
		HostPort      int    `json:"hostPort"`
		Protocol      string `json:"protocol"`
		HostIP        string `json:"hostIp"`
	} `json:"ports"`
	Volumes []struct {
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"readOnly"`
	} `json:"volumes"`
	Network       string `json:"network"`
	RestartPolicy string `json:"restartPolicy"`
	// Start is a pointer so an omitted field takes the contract default (true).
	Start *bool `json:"start"`
}

// CreateContainer creates (and by default starts) a container and returns 201 with its id. When
// the start fails after a successful create, the start error is returned (the container remains).
func (h *DockerHandler) CreateContainer(c *fiber.Ctx) error {
	var req createContainerRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	spec := softwaredomain.DockerContainerSpec{
		Name: req.Name, Image: req.Image, Command: req.Command, Env: req.Env,
		Network: req.Network, RestartPolicy: req.RestartPolicy, Start: req.Start == nil || *req.Start,
	}
	for _, port := range req.Ports {
		spec.Ports = append(spec.Ports, softwaredomain.DockerPortBinding{
			HostIP: port.HostIP, HostPort: port.HostPort, ContainerPort: port.ContainerPort, Protocol: port.Protocol,
		})
	}
	for _, volume := range req.Volumes {
		spec.Volumes = append(spec.Volumes, softwaredomain.DockerVolumeBinding{
			Source: volume.Source, Target: volume.Target, ReadOnly: volume.ReadOnly,
		})
	}
	created, err := h.explorer.CreateContainer(c.Context(), c.Params("id"), spec)
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id": created.ID, "warnings": stringsOrEmpty(created.Warnings), "started": created.Started,
	})
}

// StartContainer starts a container; one already running is a success.
func (h *DockerHandler) StartContainer(c *fiber.Ctx) error {
	return h.actOnContainer(c, softwareapp.ContainerStart)
}

// StopContainer stops a container after the Engine's grace period.
func (h *DockerHandler) StopContainer(c *fiber.Ctx) error {
	return h.actOnContainer(c, softwareapp.ContainerStop)
}

// RestartContainer restarts a container.
func (h *DockerHandler) RestartContainer(c *fiber.Ctx) error {
	return h.actOnContainer(c, softwareapp.ContainerRestart)
}

func (h *DockerHandler) actOnContainer(c *fiber.Ctx, action softwareapp.ContainerAction) error {
	if err := h.explorer.ActOnContainer(c.Context(), c.Params("id"), pathParam(c, "containerId"), action); err != nil {
		return respondDockerError(c, err)
	}
	return success(c)
}

// ContainerLogs returns a bounded log snapshot. tailLines defaults to 200 and is capped at 2000; a
// non-positive or non-integer value is a validation error.
func (h *DockerHandler) ContainerLogs(c *fiber.Ctx) error {
	tailLines := defaultContainerLogTailLines
	if raw := c.Query("tailLines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "tailLines must be a positive integer."))
		}
		tailLines = parsed
	}
	if tailLines > maxContainerLogTailLines {
		tailLines = maxContainerLogTailLines
	}
	logs, err := h.explorer.ContainerLogs(c.Context(), c.Params("id"), pathParam(c, "containerId"), tailLines)
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.JSON(fiber.Map{"logs": logs})
}

// RemoveContainer removes a container; ?force=true kills a running one, ?removeVolumes=true also
// removes its anonymous volumes.
func (h *DockerHandler) RemoveContainer(c *fiber.Ctx) error {
	err := h.explorer.RemoveContainer(c.Context(), c.Params("id"), pathParam(c, "containerId"),
		queryFlag(c, "force"), queryFlag(c, "removeVolumes"))
	if err != nil {
		return respondDockerError(c, err)
	}
	return success(c)
}

// ---- volumes ----

type dockerVolumeResponse struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Scope      string            `json:"scope"`
	CreatedAt  *string           `json:"createdAt"`
	Labels     map[string]string `json:"labels"`
}

func volumeResponse(volume softwaredomain.DockerVolume) dockerVolumeResponse {
	labels := volume.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return dockerVolumeResponse{
		Name: volume.Name, Driver: volume.Driver, Mountpoint: volume.Mountpoint, Scope: volume.Scope,
		CreatedAt: formatOptionalTime(volume.CreatedAt), Labels: labels,
	}
}

// ListVolumes returns the host's volumes by name.
func (h *DockerHandler) ListVolumes(c *fiber.Ctx) error {
	volumes, err := h.explorer.ListVolumes(c.Context(), c.Params("id"))
	if err != nil {
		return respondDockerError(c, err)
	}
	items := make([]dockerVolumeResponse, 0, len(volumes))
	for _, volume := range volumes {
		items = append(items, volumeResponse(volume))
	}
	return c.JSON(fiber.Map{"items": items})
}

type createVolumeRequest struct {
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels"`
}

// CreateVolume creates a volume and returns it (201).
func (h *DockerHandler) CreateVolume(c *fiber.Ctx) error {
	var req createVolumeRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	volume, err := h.explorer.CreateVolume(c.Context(), c.Params("id"), softwaredomain.DockerVolumeSpec{
		Name: req.Name, Driver: req.Driver, Labels: req.Labels,
	})
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(volumeResponse(*volume))
}

// RemoveVolume removes a volume; ?force=true is passed to the Engine.
func (h *DockerHandler) RemoveVolume(c *fiber.Ctx) error {
	if err := h.explorer.RemoveVolume(c.Context(), c.Params("id"), pathParam(c, "volumeName"), queryFlag(c, "force")); err != nil {
		return respondDockerError(c, err)
	}
	return success(c)
}

// ---- networks ----

type dockerSubnetResponse struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}

type dockerNetworkResponse struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Driver     string                 `json:"driver"`
	Scope      string                 `json:"scope"`
	Internal   bool                   `json:"internal"`
	Attachable bool                   `json:"attachable"`
	Predefined bool                   `json:"predefined"`
	Subnets    []dockerSubnetResponse `json:"subnets"`
	CreatedAt  *string                `json:"createdAt"`
}

func networkResponse(network softwaredomain.DockerNetwork) dockerNetworkResponse {
	subnets := make([]dockerSubnetResponse, 0, len(network.Subnets))
	for _, subnet := range network.Subnets {
		subnets = append(subnets, dockerSubnetResponse{Subnet: subnet.Subnet, Gateway: subnet.Gateway})
	}
	return dockerNetworkResponse{
		ID: network.ID, Name: network.Name, Driver: network.Driver, Scope: network.Scope,
		Internal: network.Internal, Attachable: network.Attachable, Predefined: network.Predefined,
		Subnets: subnets, CreatedAt: formatOptionalTime(network.CreatedAt),
	}
}

// ListNetworks returns the host's networks by name.
func (h *DockerHandler) ListNetworks(c *fiber.Ctx) error {
	networks, err := h.explorer.ListNetworks(c.Context(), c.Params("id"))
	if err != nil {
		return respondDockerError(c, err)
	}
	items := make([]dockerNetworkResponse, 0, len(networks))
	for _, network := range networks {
		items = append(items, networkResponse(network))
	}
	return c.JSON(fiber.Map{"items": items})
}

type createNetworkRequest struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Internal   bool   `json:"internal"`
	Attachable bool   `json:"attachable"`
	Subnet     string `json:"subnet"`
	Gateway    string `json:"gateway"`
}

// CreateNetwork creates a network and returns it (201).
func (h *DockerHandler) CreateNetwork(c *fiber.Ctx) error {
	var req createNetworkRequest
	if err := c.BodyParser(&req); err != nil {
		return invalidBody(c)
	}
	network, err := h.explorer.CreateNetwork(c.Context(), c.Params("id"), softwaredomain.DockerNetworkSpec{
		Name: req.Name, Driver: req.Driver, Internal: req.Internal, Attachable: req.Attachable,
		Subnet: req.Subnet, Gateway: req.Gateway,
	})
	if err != nil {
		return respondDockerError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(networkResponse(*network))
}

// RemoveNetwork removes a network by id or name; predefined networks are refused.
func (h *DockerHandler) RemoveNetwork(c *fiber.Ctx) error {
	if err := h.explorer.RemoveNetwork(c.Context(), c.Params("id"), pathParam(c, "networkId")); err != nil {
		return respondDockerError(c, err)
	}
	return success(c)
}

// ---- errors ----

// respondDockerError maps explorer errors onto the shared envelope per servers-docker.md:
// eligibility failures are 404 (no Server) or 409 (ineligible), the lock is 409 (or 503 when it
// cannot be confirmed), validation is 400, and Engine failures follow their DockerEngineError kind.
// Eligibility messages are fixed, operator-facing sentences; Engine messages carry the Engine's
// own reason. Unknown errors are logged and returned as 500 without detail.
func respondDockerError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	case errors.Is(err, softwaredomain.ErrDockerHostUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The Docker explorer needs a deployed Server with a known address."))
	case errors.Is(err, softwaredomain.ErrDockerNotInstalled):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"Docker CE is not installed on this Server by swallow."))
	case errors.Is(err, softwaredomain.ErrDockerAPIDisabled):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The Docker Engine API is not enabled for this Server. Enable it on the Docker CE installation first."))
	case errors.Is(err, softwaredomain.ErrPredefinedDockerNetwork):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A predefined Docker network (bridge, host, none) cannot be removed."))
	case errors.Is(err, softwaredomain.ErrInvalidDockerRequest):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}

	var engineErr *softwaredomain.DockerEngineError
	if errors.As(err, &engineErr) {
		switch engineErr.Kind {
		case softwaredomain.DockerEngineNotFound:
			return apierror.Respond(c, apierror.New(apierror.CodeNotFound, engineErr.Detail))
		case softwaredomain.DockerEngineConflict:
			return apierror.Respond(c, apierror.New(apierror.CodeConflict, engineErr.Detail))
		case softwaredomain.DockerEngineRejected:
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, engineErr.Detail))
		default:
			log.Printf("docker explorer: engine unavailable: %v", engineErr)
			return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, engineErr.Detail))
		}
	}

	log.Printf("docker explorer: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
