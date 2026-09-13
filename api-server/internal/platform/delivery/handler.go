package delivery

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type PlatformHandler struct {
	platforms    *application.PlatformService
	uninstall    *application.UninstallService
	membership   *application.MembershipSyncUseCase
	deploy       *application.DeployService
	slurmCluster *application.GetSlurmClusterUseCase
	requirements *application.DeploymentRequirementService
}

func NewPlatformHandler(
	platforms *application.PlatformService,
	membership *application.MembershipSyncUseCase,
	deploy *application.DeployService,
	uninstall *application.UninstallService,
	slurmCluster *application.GetSlurmClusterUseCase,
	requirements *application.DeploymentRequirementService,
) *PlatformHandler {
	return &PlatformHandler{
		platforms: platforms, membership: membership, deploy: deploy,
		uninstall: uninstall, slurmCluster: slurmCluster, requirements: requirements,
	}
}

// acceptedOperationResponse keeps the one-release clusterId alias at the HTTP boundary.
type acceptedOperationResponse struct {
	PlatformID  string `json:"platformId"`
	ClusterID   string `json:"clusterId"`
	OperationID string `json:"operationId"`
}

// membershipReportResponse keeps the former clusterId field out of the canonical
// application DTO while preserving the published compatibility projection.
type membershipReportResponse struct {
	PlatformID   string   `json:"platformId"`
	ClusterID    string   `json:"clusterId"`
	PlatformName string   `json:"platformName"`
	Members      int      `json:"members"`
	Matched      int      `json:"matched"`
	Cleared      int      `json:"cleared"`
	Unmatched    []string `json:"unmatched"`
	Error        *string  `json:"error"`
}

func membershipResponse(report application.MembershipReport) membershipReportResponse {
	return membershipReportResponse{
		PlatformID: report.PlatformID, ClusterID: report.PlatformID,
		PlatformName: report.PlatformName, Members: report.Members,
		Matched: report.Matched, Cleared: report.Cleared,
		Unmatched: report.Unmatched, Error: report.Error,
	}
}

type createPlatformRequest struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IntegrationID string `json:"integrationId"`
	GPUStackOwner string `json:"gpuStackOwner"`
	ExporterOwner string `json:"exporterOwner"`
}

func (h *PlatformHandler) Create(c *fiber.Ctx) error {
	var req createPlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	item, err := h.platforms.Create(c.Context(), application.CreatePlatformInput{
		SiteID:        req.SiteID,
		Name:          req.Name,
		Type:          req.Type,
		IntegrationID: req.IntegrationID,
		GPUStackOwner: req.GPUStackOwner,
		ExporterOwner: req.ExporterOwner,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// roleAssignmentRequest keeps workload co-location additive: an omitted JSON boolean maps
// to false and preserves existing dedicated control-plane requests.
type roleAssignmentRequest struct {
	ServerID     string `json:"serverId"`
	Role         string `json:"role"`
	RunWorkloads bool   `json:"runWorkloads"`
}

// deployPlatformRequest is the stable wire shape; zero VIP fields mean omitted and the
// application decides whether the inferred topology requires them.
type machinePreparationRequest struct {
	Mode       string                            `json:"mode"`
	TemplateID string                            `json:"templateId"`
	Settings   machinePreparationSettingsRequest `json:"settings"`
	UserData   machinePreparationUserDataRequest `json:"userData"`
	Network    *machinePreparationNetworkRequest `json:"network"`
}

type machinePreparationSettingsRequest struct {
	ImageID   string `json:"imageId"`
	Ephemeral *bool  `json:"ephemeral"`
}

type machinePreparationUserDataRequest struct {
	Mode  string `json:"mode"`
	Value string `json:"value"`
}

type machinePreparationNetworkRequest struct {
	Mode           string                                `json:"mode"`
	SubnetID       string                                `json:"subnetId"`
	DefaultGateway bool                                  `json:"defaultGateway"`
	Assignments    []machinePreparationAssignmentRequest `json:"assignments"`
}

type machinePreparationAssignmentRequest struct {
	ServerID    string `json:"serverId"`
	InterfaceID string `json:"interfaceId"`
	SubnetID    string `json:"subnetId"`
	IPAddress   string `json:"ipAddress"`
}

func machinePreparationFromRequest(request machinePreparationRequest) platformdomain.MachinePreparation {
	preparation := platformdomain.MachinePreparation{
		Mode: platformdomain.MachinePreparationMode(request.Mode), TemplateID: request.TemplateID,
		ImageID: request.Settings.ImageID, Ephemeral: request.Settings.Ephemeral,
		UserDataMode: request.UserData.Mode, UserData: request.UserData.Value,
	}
	if request.Network != nil {
		preparation.NetworkMode = request.Network.Mode
		preparation.SubnetID = request.Network.SubnetID
		preparation.DefaultGateway = request.Network.DefaultGateway
		preparation.Assignments = make([]platformdomain.MachineNetworkAssignment, len(request.Network.Assignments))
		for index, assignment := range request.Network.Assignments {
			preparation.Assignments[index] = platformdomain.MachineNetworkAssignment{
				ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID,
				SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
			}
		}
	}
	return preparation
}

// slurmSpecFromRequest maps the optional Slurm deploy body into domain intent. A nil request
// yields a zero spec, which the application rejects for a slurm deployment (no node
// assignments) while remaining harmless for a kubernetes one.
func slurmSpecFromRequest(request *slurmDeployRequest) platformdomain.SlurmDeploymentSpec {
	if request == nil {
		return platformdomain.SlurmDeploymentSpec{}
	}
	assignments := make([]platformdomain.SlurmNodeAssignment, len(request.NodeAssignments))
	for index, assignment := range request.NodeAssignments {
		assignments[index] = platformdomain.SlurmNodeAssignment{
			ServerID: assignment.ServerID, Controller: assignment.Controller,
			Compute: assignment.Compute, Login: assignment.Login,
		}
	}
	return platformdomain.SlurmDeploymentSpec{
		ClusterName:       request.ClusterName,
		APIVersion:        request.APIVersion,
		StateSaveLocation: request.StateSaveLocation,
		NodeAssignments:   assignments,
		WorkloadStorage:   slurmWorkloadStorageFromRequest(request.WorkloadStorage),
	}
}

// slurmWorkloadStorageFromRequest maps the optional workload-storage body into domain intent.
// A nil request means no shared workload filesystem (Enabled stays false); a present object
// enables it, and the application validates the mode-specific fields.
func slurmWorkloadStorageFromRequest(request *slurmWorkloadStorageReq) platformdomain.SlurmWorkloadStorageSpec {
	if request == nil {
		return platformdomain.SlurmWorkloadStorageSpec{}
	}
	spec := platformdomain.SlurmWorkloadStorageSpec{
		Enabled:   true,
		Mode:      platformdomain.SlurmWorkloadStorageMode(request.Mode),
		Type:      platformdomain.SlurmStorageType(request.Type),
		MountPath: request.MountPath,
	}
	if request.NFS != nil {
		spec.NFSURL = request.NFS.URL
		spec.NFSMountOptions = request.NFS.MountOptions
	}
	return spec
}

type deployPlatformRequest struct {
	SiteID        string `json:"siteId"`
	Name          string `json:"name"`
	GPUStackOwner string `json:"gpuStackOwner"`
	// Type selects the platform to build: "kubernetes" (default when omitted) or "slurm".
	// The Kubernetes fields below are read for kubernetes; Slurm is read for slurm.
	Type               string                    `json:"type"`
	K0sVersion         string                    `json:"k0sVersion"`
	PodCIDR            string                    `json:"podCidr"`
	ServiceCIDR        string                    `json:"serviceCidr"`
	APIVIP             string                    `json:"apiVip"`
	APIVIPPrefix       int                       `json:"apiVipPrefix"`
	RoleAssignments    []roleAssignmentRequest   `json:"roleAssignments"`
	Slurm              *slurmDeployRequest       `json:"slurm"`
	MachinePreparation machinePreparationRequest `json:"machinePreparation"`
}

// slurmDeployRequest is the Slurm-specific deploy body. Node roles are per-role flags because
// a Server may run slurmctld, slurmd, both, or be a login (submission) host. Empty optional
// fields let the application default them (clusterName from the platform name; a
// controller-local stateSaveLocation for a single controller). workloadStorage is optional;
// omitting it means no shared workload filesystem.
type slurmDeployRequest struct {
	ClusterName       string                   `json:"clusterName"`
	APIVersion        string                   `json:"apiVersion"`
	StateSaveLocation string                   `json:"stateSaveLocation"`
	NodeAssignments   []slurmNodeAssignReq     `json:"nodeAssignments"`
	WorkloadStorage   *slurmWorkloadStorageReq `json:"workloadStorage"`
}

// slurmNodeAssignReq assigns Slurm roles to one Server; an omitted flag is false. Login marks
// a submission/client host that runs no cluster daemon.
type slurmNodeAssignReq struct {
	ServerID   string `json:"serverId"`
	Controller bool   `json:"controller"`
	Compute    bool   `json:"compute"`
	Login      bool   `json:"login"`
}

// slurmWorkloadStorageReq is the optional shared workload filesystem. mode is self-hosted (the
// login node exports NFS) or external (an operator NFS url); type is nfs; mountPath defaults to
// a non-overlapping path. nfs.url is required for external mode.
type slurmWorkloadStorageReq struct {
	Mode      string       `json:"mode"`
	Type      string       `json:"type"`
	MountPath string       `json:"mountPath"`
	NFS       *slurmNFSReq `json:"nfs"`
}

// slurmNFSReq carries the NFS source for external workload storage.
type slurmNFSReq struct {
	URL          string `json:"url"`
	MountOptions string `json:"mountOptions"`
}

// Deploy maps transport data into deployment intent; topology and network rules stay in
// the application use case so every delivery shares one policy.
func (h *PlatformHandler) Deploy(c *fiber.Ctx) error {
	var req deployPlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	assignments := make([]platformdomain.RoleAssignment, len(req.RoleAssignments))
	for i, assignment := range req.RoleAssignments {
		assignments[i] = platformdomain.RoleAssignment{
			ServerID:     assignment.ServerID,
			Role:         platformdomain.NodeRole(assignment.Role),
			RunWorkloads: assignment.RunWorkloads,
		}
	}

	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}

	result, err := h.deploy.Deploy(c.Context(), application.DeployPlatformInput{
		SiteID:        req.SiteID,
		Name:          req.Name,
		Type:          platformdomain.PlatformType(req.Type),
		GPUStackOwner: req.GPUStackOwner,
		Spec: platformdomain.DeploymentSpec{
			K0sVersion:      req.K0sVersion,
			PodCIDR:         req.PodCIDR,
			ServiceCIDR:     req.ServiceCIDR,
			APIVIP:          req.APIVIP,
			APIVIPPrefix:    req.APIVIPPrefix,
			RoleAssignments: assignments,
		},
		SlurmSpec:   slurmSpecFromRequest(req.Slurm),
		RequestedBy: requestedBy, RequestCorrelation: c.GetRespHeader(fiber.HeaderXRequestID),
		MachinePreparation: machinePreparationFromRequest(req.MachinePreparation),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(acceptedOperationResponse{
		PlatformID: result.PlatformID, ClusterID: result.PlatformID,
		OperationID: result.OperationID,
	})
}

func (h *PlatformHandler) List(c *fiber.Ctx) error {
	items, err := h.platforms.List(c.Context(), c.Query("siteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

func (h *PlatformHandler) Get(c *fiber.Ctx) error {
	item, err := h.platforms.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

type updatePlatformRequest struct {
	Name          *string `json:"name"`
	IntegrationID *string `json:"integrationId"`
	GPUStackOwner *string `json:"gpuStackOwner"`
	ExporterOwner *string `json:"exporterOwner"`
}

func (h *PlatformHandler) Update(c *fiber.Ctx) error {
	var req updatePlatformRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	item, err := h.platforms.Update(c.Context(), c.Params("id"), application.UpdatePlatformInput{
		Name:          req.Name,
		IntegrationID: req.IntegrationID,
		GPUStackOwner: req.GPUStackOwner,
		ExporterOwner: req.ExporterOwner,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// Uninstall starts removal of a Swallow-deployed k0s platform from its original targets.
// uninstallPlatformRequest is the optional uninstall body. An absent body keeps the
// default of removing k0s only; releaseServers additionally returns each member server to
// the provider, with releaseOptions mirroring the standalone Release action.
type uninstallPlatformRequest struct {
	ReleaseServers bool `json:"releaseServers"`
	ReleaseOptions struct {
		Erase           bool `json:"erase"`
		SecureErase     bool `json:"secureErase"`
		QuickErase      bool `json:"quickErase"`
		UnbindStaticIPs bool `json:"unbindStaticIps"`
	} `json:"releaseOptions"`
}

func (h *PlatformHandler) Uninstall(c *fiber.Ctx) error {
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	var req uninstallPlatformRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
		}
	}
	result, err := h.uninstall.Uninstall(c.Context(), application.UninstallPlatformInput{
		PlatformID: c.Params("id"), RequestedBy: requestedBy,
		ReleaseServers: req.ReleaseServers,
		ReleaseOptions: platformdomain.ServerReleaseOptions{
			Erase:           req.ReleaseOptions.Erase,
			SecureErase:     req.ReleaseOptions.SecureErase,
			QuickErase:      req.ReleaseOptions.QuickErase,
			UnbindStaticIPs: req.ReleaseOptions.UnbindStaticIPs,
		},
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(acceptedOperationResponse{
		PlatformID: result.PlatformID, ClusterID: result.PlatformID,
		OperationID: result.OperationID,
	})
}

func (h *PlatformHandler) Delete(c *fiber.Ctx) error {
	if err := h.platforms.Delete(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// SyncMembership reads the platform's membership now instead of waiting for the interval.
// The report names members swallow could not match to a server, which is the part that
// needs a human.
func (h *PlatformHandler) SyncMembership(c *fiber.Ctx) error {
	report, err := h.membership.Execute(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(membershipResponse(*report))
}

// slurmClusterResponse is the wire shape of a Slurm platform's live cluster state. It is
// Slurm-native on purpose (controllers, partitions, node scheduler state) and separate from
// the generic member list and the deployment intent; it carries no monitoring health.
type slurmClusterResponse struct {
	Controllers []slurmControllerResponse `json:"controllers"`
	Partitions  []slurmPartitionResponse  `json:"partitions"`
	Nodes       []slurmNodeResponse       `json:"nodes"`
}

type slurmControllerResponse struct {
	Hostname string `json:"hostname"`
	Primary  bool   `json:"primary"`
	Status   string `json:"status"`
}

type slurmPartitionResponse struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	NodeSpec   string `json:"nodeSpec"`
	TotalNodes int    `json:"totalNodes"`
}

type slurmNodeResponse struct {
	Name          string   `json:"name"`
	State         string   `json:"state"`
	Cpus          int      `json:"cpus"`
	RealMemoryMiB int64    `json:"realMemoryMiB"`
	Gres          string   `json:"gres"`
	Partitions    []string `json:"partitions"`
	Address       string   `json:"address"`
}

func slurmClusterBody(state *platformdomain.SlurmClusterState) slurmClusterResponse {
	body := slurmClusterResponse{
		Controllers: make([]slurmControllerResponse, 0, len(state.Controllers)),
		Partitions:  make([]slurmPartitionResponse, 0, len(state.Partitions)),
		Nodes:       make([]slurmNodeResponse, 0, len(state.Nodes)),
	}
	for _, controller := range state.Controllers {
		body.Controllers = append(body.Controllers, slurmControllerResponse{
			Hostname: controller.Hostname, Primary: controller.Primary, Status: controller.Status,
		})
	}
	for _, partition := range state.Partitions {
		body.Partitions = append(body.Partitions, slurmPartitionResponse{
			Name: partition.Name, State: partition.State,
			NodeSpec: partition.NodeSpec, TotalNodes: partition.TotalNodes,
		})
	}
	for _, node := range state.Nodes {
		partitions := node.Partitions
		if partitions == nil {
			partitions = []string{}
		}
		body.Nodes = append(body.Nodes, slurmNodeResponse{
			Name: node.Name, State: node.State, Cpus: node.CPUs,
			RealMemoryMiB: node.RealMemoryMiB, Gres: node.Gres,
			Partitions: partitions, Address: node.Address,
		})
	}
	return body
}

// GetSlurmCluster returns a Slurm platform's live cluster state read on demand from
// slurmrestd. It is a Slurm-only read: a non-Slurm platform, or one whose slurmrestd
// integration is not recorded, returns a validation error the dashboard treats as "no live
// view yet" and degrades from.
func (h *PlatformHandler) GetSlurmCluster(c *fiber.Ctx) error {
	if h.slurmCluster == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Slurm cluster state is unavailable."))
	}
	state, err := h.slurmCluster.Execute(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(slurmClusterBody(state))
}

func (h *PlatformHandler) SyncAllMembership(c *fiber.Ctx) error {
	reports, err := h.membership.ExecuteAll(c.Context())
	if err != nil {
		return respondError(c, err)
	}
	responses := make([]membershipReportResponse, len(reports))
	for index, report := range reports {
		responses[index] = membershipResponse(report)
	}
	return c.JSON(responses)
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, platformdomain.ErrPlatformNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Platform not found."))

	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, platformdomain.ErrPlatformNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A platform with this name already exists at this site."))

	case errors.Is(err, platformdomain.ErrPlatformNotDeployManaged),
		errors.Is(err, platformdomain.ErrPlatformAlreadyUninstalled),
		errors.Is(err, platformdomain.ErrPlatformUninstallConflict),
		errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, operationdomain.ErrPolicyConflict),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrAutomationConfigNotFound),
		errors.Is(err, operationdomain.ErrAutomationDisabled),
		errors.Is(err, operationdomain.ErrAutomationCredentialMissing),
		errors.Is(err, operationdomain.ErrPlaybookNotAllowed),
		errors.Is(err, operationapp.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))

	case errors.Is(err, application.ErrInvalidPlatform),
		errors.Is(err, platformdomain.ErrInvalidDeploymentRequirement),
		errors.Is(err, platformdomain.ErrInvalidDeployment),
		errors.Is(err, serverdomain.ErrServerNotFound),
		errors.Is(err, platformdomain.ErrUnsupportedPlatformType):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, platformdomain.ErrNoPlatformIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"This platform has no integration configured, so its membership cannot be read."))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The platform integration has no credential configured."))
	case errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			err.Error()))
	}

	var readerErr *platformdomain.ReaderError
	if errors.As(err, &readerErr) {
		if readerErr.Kind == platformdomain.ReaderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, readerErr.Detail))
		}
		log.Printf("platform api %s: %v", readerErr.Kind, readerErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, readerErr.Detail))
	}

	log.Printf("platform: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
