package delivery

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

type ProvisioningHandler struct {
	deployments     *application.DeployServersUseCase
	templates       *application.DeploymentTemplateService
	targetPreflight *application.DeploymentTargetPreflightService
	networks        *application.NetworkConfigurationService
	tasks           *application.ProvisioningTaskService
	refresh         *application.RefreshServerUseCase
	images          *application.ListOSImagesUseCase
	reconcile       *application.ReconcileUseCase
	detail          *application.GetProvisionerDetailUseCase
	events          *application.GetProviderEventsUseCase
	actions         *application.MachineActionsUseCase
	deleteServer    *application.DeleteServerUseCase
	deleteImage     *application.DeleteOSImageUseCase
	imageUploads    *application.OSImageUploadService
	imageMetadata   *application.SetOSImageMetadataUseCase
	serverTagsList  *application.ListServerTagsUseCase
	serverTagsEdit  *application.EditServerTagsUseCase
	serverTagDelete *application.DeleteServerTagUseCase
	serverTagRename *application.RenameServerTagUseCase
	durable         application.DurableOperationLauncher
	inspections     application.HardwareInspectionLauncher
	enrollment      *application.HostEnrollmentUseCase
	power           *application.PowerConfigurationUseCase
	vmEnrollment    application.VirtualMachineEnrollmentLauncher
	httpProxy       *application.HTTPProxyUseCase
	subnetDNS       *application.SubnetDNSUseCase
}

func NewProvisioningHandler(
	deployments *application.DeployServersUseCase,
	targetPreflight *application.DeploymentTargetPreflightService,
	templates *application.DeploymentTemplateService,
	networks *application.NetworkConfigurationService,
	tasks *application.ProvisioningTaskService,
	refresh *application.RefreshServerUseCase,
	images *application.ListOSImagesUseCase,
	reconcile *application.ReconcileUseCase,
	detail *application.GetProvisionerDetailUseCase,
	events *application.GetProviderEventsUseCase,
	actions *application.MachineActionsUseCase,
	deleteServer *application.DeleteServerUseCase,
	deleteImage *application.DeleteOSImageUseCase,
	imageUploads *application.OSImageUploadService,
	imageMetadata *application.SetOSImageMetadataUseCase,
	serverTagsList *application.ListServerTagsUseCase,
	serverTagsEdit *application.EditServerTagsUseCase,
	serverTagDelete *application.DeleteServerTagUseCase,
	serverTagRename *application.RenameServerTagUseCase,
	durable ...application.DurableOperationLauncher,
) *ProvisioningHandler {
	handler := &ProvisioningHandler{
		deployments:     deployments,
		templates:       templates,
		targetPreflight: targetPreflight,
		networks:        networks,
		tasks:           tasks,
		refresh:         refresh,
		images:          images,
		reconcile:       reconcile,
		detail:          detail,
		events:          events,
		actions:         actions,
		deleteServer:    deleteServer,
		deleteImage:     deleteImage,
		imageUploads:    imageUploads,
		imageMetadata:   imageMetadata,
		serverTagsList:  serverTagsList,
		serverTagsEdit:  serverTagsEdit,
		serverTagDelete: serverTagDelete,
		serverTagRename: serverTagRename,
	}
	if len(durable) > 0 {
		handler.durable = durable[0]
	}
	return handler
}

// RefreshServer reads the current provider state for one machine and advances its
// provisioning projection and observed addresses. It is used for bounded follow-up
// after asynchronous actions such as Release; full inventory reconciliation remains separate.
func (h *ProvisioningHandler) RefreshServer(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	item, err := h.refresh.Execute(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// DeleteServer removes the backing provisioner Machine before deleting the Server
// projection. It is synchronous because returning success before both writes complete
// would let the dashboard hide a Server that reconciliation can recreate.
func (h *ProvisioningHandler) DeleteServer(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	if err := h.deleteServer.Execute(c.Context(), id); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ProvisionerDetail proxies the provisioner for one machine's full detail, plus the
// capabilities that tell a client which actions to offer. Read live, so it reflects the
// provisioner exactly and swallow keeps no schema for it.
func (h *ProvisioningHandler) ProvisionerDetail(c *fiber.Ctx) error {
	// The optional live BMC section can contain a user-requested password. Keep the
	// response out of browser and intermediary caches even though the route is admin-only.
	c.Set(fiber.HeaderCacheControl, "no-store")

	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	item, err := h.detail.Execute(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// serverAction runs one lifecycle action addressed by server ID and returns the resulting
// provisioning snapshot with 202: the provisioner has accepted the request, and the
// reconciler tracks it from there. The shared shape keeps each action's handler to its
// intent.
func (h *ProvisioningHandler) serverAction(
	c *fiber.Ctx,
	run func(ctx context.Context, id string) (*application.ProvisioningStateItem, error),
) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := run(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

func (h *ProvisioningHandler) PowerOn(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.PowerOn)
}

func (h *ProvisioningHandler) PowerOff(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.PowerOff)
}

// PowerState reads the live BMC power state. It is a GET because it changes nothing.
func (h *ProvisioningHandler) PowerState(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := h.actions.QueryPower(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// powerConfigurationRequest is the PUT /servers/{id}/power-configuration body. Password is a
// pointer so an absent key (keep the stored password for an unchanged driver) is told apart from
// "" (clear it); JSON null counts as absent.
type powerConfigurationRequest struct {
	Driver   string  `json:"driver"`
	Address  string  `json:"address"`
	PowerID  string  `json:"powerId"`
	Username string  `json:"username"`
	Password *string `json:"password"`
}

// GetPowerConfiguration serves GET /servers/{id}/power-configuration (server-detail-actions.md):
// the Server's Power Configuration read live from its provisioner, without the password. The
// response is not cached because it reflects live provisioner state a user is about to edit.
func (h *ProvisioningHandler) GetPowerConfiguration(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.power == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Power configuration is unavailable."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := h.power.Get(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// SetPowerConfiguration serves PUT /servers/{id}/power-configuration: it writes the user's
// Power Configuration through to the provisioner, keeps it only when the provisioner reads the
// power through it, and answers 200 with the configuration read back and that power state.
// The request carries a write-only password; nothing here logs the body, and the shared error
// mapper logs only the provider's client-safe detail.
func (h *ProvisioningHandler) SetPowerConfiguration(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.power == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Power configuration is unavailable."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	var req powerConfigurationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.Driver) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "driver is required."))
	}
	item, err := h.power.Set(c.Context(), id, application.SetPowerConfigurationInput{
		Driver: req.Driver, Address: req.Address, PowerID: req.PowerID,
		Username: req.Username, Password: req.Password,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// httpProxyRequest is the Provisioner HTTP Proxy write body (provisioner-http-proxy.md).
type httpProxyRequest struct {
	Mode      string `json:"mode"`
	HTTPProxy string `json:"httpProxy"`
}

// GetHTTPProxy serves GET /provisioning/integrations/{id}/http-proxy (provisioner-http-proxy.md):
// the Integration's Provisioner HTTP Proxy read live from its provisioner. The response is not
// cached because it reflects live provisioner state a user is about to edit.
func (h *ProvisioningHandler) GetHTTPProxy(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.httpProxy == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "HTTP proxy settings are unavailable."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := h.httpProxy.Get(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// SetHTTPProxy serves PUT /provisioning/integrations/{id}/http-proxy: it writes the user's
// Provisioner HTTP Proxy through to the provisioner and answers 200 with the configuration read
// back. Field validation beyond a present mode belongs to the use case, so the API and any later
// client share one set of rules.
func (h *ProvisioningHandler) SetHTTPProxy(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.httpProxy == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "HTTP proxy settings are unavailable."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	var req httpProxyRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.Mode) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "mode is required."))
	}
	item, err := h.httpProxy.Set(c.Context(), id, application.SetHTTPProxyInput{Mode: req.Mode, HTTPProxy: req.HTTPProxy})
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// Inspect serves POST /servers/{id}/inspect (server-detail-actions.md): it starts, or resumes
// after attention, the Server's inspect-hardware Workflow and responds 202 with the stored
// provisioning snapshot plus workflowId and resumed. Inspection only runs as that Workflow.
func (h *ProvisioningHandler) Inspect(c *fiber.Ctx) error {
	if h.inspections == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Hardware inspection is not configured."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	requestedBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		requestedBy = principal.Username
	}
	accepted, err := h.inspections.LaunchInspection(c.Context(), application.InspectionRequest{
		ServerID: id, Origin: provisioningdomain.InspectionOriginRequested,
		RequestedBy: requestedBy, RequestID: c.GetRespHeader(fiber.HeaderXRequestID),
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(accepted)
}

// enrollVirtualMachinesRequest is the virtual-machine enrollment body (server-enrollment.md).
type enrollVirtualMachinesRequest struct {
	IntegrationID      string   `json:"integrationId"`
	HypervisorServerID string   `json:"hypervisorServerId"`
	Domains            []string `json:"domains"`
	BootISOID          string   `json:"bootIsoId"`
	Account            string   `json:"account"`
	PowerOffRunning    bool     `json:"powerOffRunning"`
}

// EnrollVirtualMachines serves POST /provisioning/virtual-machine-enrollments
// (server-enrollment.md, decision 055): it starts an enroll-virtual-machines Workflow and responds
// 202 with its id.
func (h *ProvisioningHandler) EnrollVirtualMachines(c *fiber.Ctx) error {
	if h.vmEnrollment == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Virtual machine enrollment is unavailable."))
	}
	var req enrollVirtualMachinesRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	requestedBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		requestedBy = principal.Username
	}
	accepted, err := h.vmEnrollment.LaunchVirtualMachineEnrollment(c.Context(), application.VirtualMachineEnrollmentRequest{
		IntegrationID: req.IntegrationID, HypervisorServerID: req.HypervisorServerID, Domains: req.Domains,
		BootISOID: req.BootISOID, Account: req.Account, PowerOffRunning: req.PowerOffRunning,
		RequestedBy: requestedBy, RequestID: c.GetRespHeader(fiber.HeaderXRequestID),
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(accepted)
}

// enrollBundleRequest is the optional enroll-bundle body: the address the host reaches swallow at.
type enrollBundleRequest struct {
	SwallowURL string `json:"swallowUrl"`
}

// EnrollBundle serves POST /provisioning/integrations/{id}/enroll-bundle (server-enrollment.md).
// The body is optional; without swallowUrl the command uses the address this request came in on.
// The response carries the provisioner's credential for the host to register itself, so it is
// never cached; the route is admin-only and nothing here logs the response.
func (h *ProvisioningHandler) EnrollBundle(c *fiber.Ctx) error {
	if h.enrollment == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Server enrollment is unavailable."))
	}
	var req enrollBundleRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
		}
	}
	if strings.TrimSpace(req.SwallowURL) == "" {
		req.SwallowURL = c.BaseURL()
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	bundle, err := h.enrollment.Bundle(c.Context(), c.Params("id"), req.SwallowURL)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(bundle)
}

func (h *ProvisioningHandler) Test(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Test)
}

func (h *ProvisioningHandler) Abort(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Abort)
}

func (h *ProvisioningHandler) OverrideFailedTesting(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.OverrideFailedTesting)
}

func (h *ProvisioningHandler) Lock(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Lock)
}

func (h *ProvisioningHandler) Unlock(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Unlock)
}

func (h *ProvisioningHandler) MarkBroken(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.MarkBroken)
}

func (h *ProvisioningHandler) MarkFixed(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.MarkFixed)
}

func (h *ProvisioningHandler) RescueMode(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.RescueMode)
}

func (h *ProvisioningHandler) ExitRescueMode(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.ExitRescueMode)
}

// ListImages returns the images one provisioner can deploy. The integration must be
// named: images differ per site, and a merged list would offer images the target site
// cannot deploy.
func (h *ProvisioningHandler) ListImages(c *fiber.Ctx) error {
	integrationID := c.Query("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}

	items, err := h.images.Execute(c.Context(), integrationID)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(items)
}

// DeleteImage removes one provider-owned OS image. The image is selected by the same
// identity the catalog returns — integration, image ID, and architecture — passed as query
// parameters because an image ID contains a slash and cannot be a path segment. Only
// uploaded custom images are removable; the provider refuses anything else.
func (h *ProvisioningHandler) DeleteImage(c *fiber.Ctx) error {
	integrationID := c.Query("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}
	imageID := c.Query("imageId")
	if imageID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "imageId is required."))
	}
	architecture := c.Query("architecture")
	if architecture == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "architecture is required."))
	}

	if err := h.deleteImage.Execute(c.Context(), integrationID, imageID, architecture); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// imageUploadRequest is the JSON body of POST /image-uploads: the upload intent and the size and
// sha256 of the content the caller will stream. The text fields are trimmed and validated by the
// application service, so this type only decodes the wire shape.
type imageUploadRequest struct {
	IntegrationID string `json:"integrationId"`
	Name          string `json:"name"`
	Architecture  string `json:"architecture"`
	Title         string `json:"title"`
	FileType      string `json:"filetype"`
	DefaultUser   string `json:"defaultUser"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
}

// imageUploadResponse is the 201 body of POST /image-uploads: the upload's id and the time it
// stops accepting content.
type imageUploadResponse struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CreateImageUpload records the intent to upload a new provider-owned OS image (decision 027). It
// touches no provider: the intent is validated, including the provisioner's upload capability and
// the adapter's preflight, and stored so the content request needs only the upload's id. Errors
// follow the shared provisioning mapper (a broken rule or a refused intent is a 400, an unknown
// Integration a 404, a size over the installation's limit a 413).
func (h *ProvisioningHandler) CreateImageUpload(c *fiber.Ctx) error {
	var req imageUploadRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	upload, err := h.imageUploads.Create(c.Context(), application.CreateOSImageUploadInput{
		IntegrationID: req.IntegrationID,
		Name:          req.Name,
		Architecture:  req.Architecture,
		Title:         req.Title,
		FileType:      req.FileType,
		DefaultUser:   req.DefaultUser,
		SizeBytes:     req.SizeBytes,
		SHA256:        req.SHA256,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(imageUploadResponse{ID: upload.ID, ExpiresAt: upload.ExpiresAt})
}

// SendImageUploadContent streams the raw content of one OS image upload to its provisioner and
// returns the created image once the provider reports it fully staged.
//
// The app runs fasthttp with StreamRequestBody, so the body is read from the request stream as
// the provider adapter forwards it: it is never spooled to disk, never held whole in memory, and
// never read through c.Body(), which would buffer it. Only a server built without
// StreamRequestBody (a test app) has no body stream; its already-buffered body is read instead.
// fasthttp's request context is not cancelled when the client goes away;
// the application's exact-length read turns a body that ends early into a 400 instead of a
// complete upload. The media type and the presence of Content-Length are checked before the
// upload is claimed, so such a refusal leaves the upload usable; every later failure removes it.
//
// Several refusals answer before the body is read. The connection is therefore always closed
// after the response: otherwise fasthttp would parse the unread body bytes as the next request
// on the keep-alive connection. One connection per upload costs nothing next to its content.
func (h *ProvisioningHandler) SendImageUploadContent(c *fiber.Ctx) error {
	c.Context().SetConnectionClose()
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	if mediaType, _, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType)); err != nil || mediaType != fiber.MIMEOctetStream {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Content-Type must be application/octet-stream."))
	}
	// fasthttp reports -1 for a chunked body and 0 when no Content-Length was sent; neither can
	// equal an upload's positive sizeBytes.
	contentLength := c.Request().Header.ContentLength()
	if contentLength <= 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"Content-Length is required and must equal the upload's sizeBytes; chunked transfer is not accepted."))
	}

	body := c.Context().RequestBodyStream()
	if body == nil {
		body = bytes.NewReader(c.Request().Body())
	}
	item, err := h.imageUploads.SendContent(c.Context(), id, int64(contentLength), body)
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// imageMetadataRequest carries the swallow-owned display overrides, tags, and default user for an
// OS Image metadata. Each field is optional; an empty field clears that value so the image shows
// its provider value (or, for defaultUser, the built-in default). Tags and defaultUser are
// swallow-owned with no provider counterpart; an empty or omitted value clears them.
type imageMetadataRequest struct {
	Name        string   `json:"name"`
	OSSystem    string   `json:"osSystem"`
	Release     string   `json:"release"`
	Tags        []string `json:"tags"`
	DefaultUser string   `json:"defaultUser"`
}

// SetImageMetadata replaces swallow's metadata (name, OS, release, tags, default user) of one OS
// Image, which becomes the effective values a user sees over the provider's. The image is selected
// by the same identity GET /images returns — integration, image ID, and architecture — passed as
// query parameters because an image ID contains a slash and cannot be a path segment, and one
// image name can back several architectures. The metadata needs no provider realization and never
// changes the provider (decision 071).
func (h *ProvisioningHandler) SetImageMetadata(c *fiber.Ctx) error {
	integrationID := c.Query("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}
	imageID := c.Query("imageId")
	if imageID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "imageId is required."))
	}
	architecture := c.Query("architecture")
	if architecture == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "architecture is required."))
	}
	var req imageMetadataRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	if err := h.imageMetadata.Set(c.Context(), integrationID, imageID, architecture, application.OSImageMetadataInput{
		Name: req.Name, OSSystem: req.OSSystem, Release: req.Release, Tags: req.Tags, DefaultUser: req.DefaultUser,
	}); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ClearImageMetadata removes the swallow-owned metadata for one OS Image, reverting it to the
// provider values. It uses the same query-parameter identity as SetImageMetadata and DeleteImage,
// and returns 204 even when no metadata existed, because the requested end state already holds.
func (h *ProvisioningHandler) ClearImageMetadata(c *fiber.Ctx) error {
	integrationID := c.Query("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}
	imageID := c.Query("imageId")
	if imageID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "imageId is required."))
	}
	architecture := c.Query("architecture")
	if architecture == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "architecture is required."))
	}

	if err := h.imageMetadata.Clear(c.Context(), integrationID, imageID, architecture); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListServerTags returns the tags known for a Site with each tag's kind and how many Servers carry
// it, so the tag editor can offer existing names and the tag manager can show them. It is under
// /provisioning rather than /servers to avoid colliding with GET /servers/:id, and scoped to a Site
// because each Site's provisioner holds its own tag catalog; swallow owns the manual tags in the
// Server specs, and the provisioner its automatic ones (docs/decisions/071).
func (h *ProvisioningHandler) ListServerTags(c *fiber.Ctx) error {
	siteID := c.Query("siteId")
	if siteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}

	options, err := h.serverTagsList.Execute(c.Context(), siteID)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(fiber.Map{"tags": options})
}

// editServerTagsRequest is a tri-state tag edit: the tags to add to every listed Server and the tags
// to remove from every listed Server. The editor sends only the tags whose all/some/none state
// changed, so any tag not named here is left untouched on each Server.
type editServerTagsRequest struct {
	ServerIDs []string `json:"serverIds"`
	Add       []string `json:"add"`
	Remove    []string `json:"remove"`
}

// EditServerTags writes a tag edit into the spec of one or more Servers and returns each Server's
// spec (docs/decisions/071); the Server controller converges the provisioner afterwards, and an
// automatic tag in remove is taken off by the provisioner before the response. Editing tags is
// metadata, so it is not gated on Server Lock. A refused tag is a 400, a spec that changed during
// the edit a 409, and an unreachable provisioner a 503, all through the shared error mapper.
func (h *ProvisioningHandler) EditServerTags(c *fiber.Ctx) error {
	var req editServerTagsRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if len(req.ServerIDs) == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "serverIds is required."))
	}
	if len(req.Add) == 0 && len(req.Remove) == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "At least one tag to add or remove is required."))
	}

	items, err := h.serverTagsEdit.Execute(c.Context(), application.EditServerTagsInput{
		ServerIDs: req.ServerIDs,
		Add:       req.Add,
		Remove:    req.Remove,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(fiber.Map{"servers": items})
}

// DeleteServerTag deletes the tag named in the path from the Site in the siteId query: from every
// Server spec that names it, then from the provisioner (server-tags.md, Delete a tag). The response
// lists the Servers whose spec changed. An unknown tag is a 404; a provider failure after the specs
// were written is a 503 the caller resolves by repeating the request.
func (h *ProvisioningHandler) DeleteServerTag(c *fiber.Ctx) error {
	siteID := c.Query("siteId")
	if siteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}
	items, err := h.serverTagDelete.Execute(c.Context(), siteID, c.Params("name"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(fiber.Map{"servers": items})
}

// renameServerTagRequest carries the new name of a tag rename.
type renameServerTagRequest struct {
	Name string `json:"name"`
}

// RenameServerTag renames the manual tag named in the path across the Site in the siteId query,
// merging it into an existing manual tag of the new name (server-tags.md, Rename a tag). Errors
// follow DeleteServerTag; an automatic tag on either side is a 400.
func (h *ProvisioningHandler) RenameServerTag(c *fiber.Ctx) error {
	siteID := c.Query("siteId")
	if siteID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}
	var req renameServerTagRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	items, err := h.serverTagRename.Execute(c.Context(), siteID, c.Params("name"), req.Name)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(fiber.Map{"servers": items})
}

// Reconcile runs a projection pass immediately instead of waiting for the interval.
//
// The report is returned rather than just an acknowledgement, because the interesting
// output is what it refused to do: conflicts need a user, and hiding them behind
// a 202 would leave them unnoticed.
func (h *ProvisioningHandler) Reconcile(c *fiber.Ctx) error {
	integrationID := c.Params("id")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	report, err := h.reconcile.Execute(c.Context(), integrationID)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(report)
}

// ReconcileAll runs a pass over every enabled provisioner.
func (h *ProvisioningHandler) ReconcileAll(c *fiber.Ctx) error {
	reports, err := h.reconcile.ExecuteAll(c.Context())
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(reports)
}

// RespondError maps provisioning failures onto the API error contract. Exported so
// that routes mounted under other resources can share one translation.
func RespondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, provisioningdomain.ErrSSHKeyRegistration):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))

	// The Tasks of a deployment fetch artifacts, so while the Site's Local Repository serves
	// deployments and is not ready the deployment is refused at acceptance; the message names what
	// to enable, fix, or wait for.
	case errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing),
		errors.Is(err, provisioningdomain.ErrNTPServerMissing),
		errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrDeploymentTemplateNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Deployment template not found."))

	case errors.Is(err, provisioningdomain.ErrDeploymentTemplateNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A deployment template with this name already exists for the integration."))

	// A capability the provisioner does not have is a 400 like every other unsupported
	// capability, including inside a deployment batch, which wraps it.
	case errors.Is(err, provisioningdomain.ErrNetworkConfigurationUnsupported):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	// Boot ISOs (contract boot-isos.md). A build failure carries only the packaging tool's
	// summary, which holds no secret, so it is returned as the message.
	case errors.Is(err, provisioningdomain.ErrBootISONotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Boot ISO not found."))
	case errors.Is(err, provisioningdomain.ErrBootISONameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A Boot ISO with this name already exists for the integration."))
	case errors.Is(err, provisioningdomain.ErrBootISOInUse),
		errors.Is(err, provisioningdomain.ErrBootISOBuilderUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, provisioningdomain.ErrInvalidBootISO):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, provisioningdomain.ErrBootISOBuildFailed):
		slog.Error("boot ISO build failed", "requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, err.Error()))

	case errors.Is(err, provisioningdomain.ErrProvisioningTaskNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Provisioning task not found."))

	// OS Image uploads (provisioning.md, OS Image Upload). A content error that surfaced through
	// the provider adapter's read still wraps ErrInvalidOSImageUpload, so it is matched (as a
	// 400, below) before the generic *ProviderError branch.
	case errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "OS image upload not found or expired."))
	case errors.Is(err, provisioningdomain.ErrOSImageUploadInProgress):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "This upload's content is already being sent."))
	case errors.Is(err, provisioningdomain.ErrOSImageUploadTooLarge):
		return apierror.Respond(c, apierror.New(apierror.CodePayloadTooLarge,
			"The image is larger than this installation's upload limit."))

	// Virtual-machine enrollment (server-enrollment.md, decision 055) checks the Hypervisor and the
	// Boot ISO through the server context.
	case errors.Is(err, serverdomain.ErrBootISOUnknown):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Boot ISO not found."))
	case errors.Is(err, serverdomain.ErrBootISOWrongIntegration),
		errors.Is(err, serverdomain.ErrInvalidDefaultUser),
		errors.Is(err, provisioningdomain.ErrInvalidVirtualMachineEnrollment):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, provisioningdomain.ErrVirtualMachineEnrollmentConflict),
		errors.Is(err, serverdomain.ErrHypervisorNotDeployed),
		errors.Is(err, serverdomain.ErrBootMediaNotConfigured):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrProvisioningTaskConflict),
		errors.Is(err, provisioningdomain.ErrServerMutationConflict),
		errors.Is(err, provisioningdomain.ErrPowerConfigurationManaged),
		errors.Is(err, provisioningdomain.ErrInspectionNotAllowed),
		errors.Is(err, serverdomain.ErrServerLocked),
		errors.Is(err, serverdomain.ErrSpecConflict),
		errors.Is(err, serverdomain.ErrSpecNotAdopted),
		errors.Is(err, provisioningdomain.ErrNetworkConfigurationConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrInvalidDeploymentTemplate),
		errors.Is(err, provisioningdomain.ErrInvalidDeploymentBatch),
		errors.Is(err, provisioningdomain.ErrInvalidReleaseRequest),
		errors.Is(err, provisioningdomain.ErrInvalidNetworkConfiguration),
		errors.Is(err, provisioningdomain.ErrInvalidEnrollmentRequest),
		errors.Is(err, provisioningdomain.ErrOSImageMetadataInvalid),
		errors.Is(err, provisioningdomain.ErrInvalidOSImageUpload),
		errors.Is(err, provisioningdomain.ErrInvalidPowerConfiguration),
		errors.Is(err, provisioningdomain.ErrPowerConfigurationUnverified),
		errors.Is(err, provisioningdomain.ErrInvalidHTTPProxy),
		errors.Is(err, provisioningdomain.ErrInvalidSubnetDNS),
		errors.Is(err, provisioningdomain.ErrInvalidTag):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, provisioningdomain.ErrSubnetNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The provisioner has no such subnet."))

	case errors.Is(err, provisioningdomain.ErrTagNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "The Site has no such tag."))

	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))

	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))

	case errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			err.Error()))

	case errors.Is(err, provisioningdomain.ErrMachineNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound,
			"The provisioner no longer has this machine."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner),
		errors.Is(err, provisioningdomain.ErrProviderKindUnsupported):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"This integration has no credential configured."))

	// The durable deploy/release handlers delegate acceptance to the operation
	// WorkflowService, so its client-safe failures reach this mapper. Translate
	// them with the same classification the operation delivery mapper uses instead of
	// letting them fall through to the opaque Internal error. fallback: a busy or
	// locked target, or one not in the required state, is a 409 conflict, and a rejected
	// operation request is a 400. Genuinely internal failures (repository writes) still keep the
	// 500 fallback.
	case errors.Is(err, workflowdomain.ErrTargetsBusy),
		errors.Is(err, workflowdomain.ErrTargetLocked),
		errors.Is(err, workflowdomain.ErrPolicyConflict),
		errors.Is(err, workflowdomain.ErrWorkflowControlConflict),
		errors.Is(err, workflowdomain.ErrTargetStateInvalid):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	// A Platform's Server is released through that Platform's uninstall (decision 067); the
	// message names the Platform and is safe to show as is.
	case errors.Is(err, provisioningdomain.ErrServerClaimedByPlatform):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, workflowapp.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	// Every durable OS deployment carries configure-ntp ansible Tasks (decision 065), so it is
	// accepted only while the Site's automation can run; the user fixes these outside the
	// request, as the site-automation contract says.
	case errors.Is(err, workflowdomain.ErrAutomationConfigNotFound),
		errors.Is(err, workflowdomain.ErrAutomationDisabled),
		errors.Is(err, workflowdomain.ErrAutomationCredentialMissing),
		errors.Is(err, workflowdomain.ErrPlaybookNotAllowed):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}

	var provErr *provisioningdomain.ProviderError
	if errors.As(err, &provErr) {
		// Log only the adapter's client-safe detail. The wrapped provider error can
		// contain internal addresses or values echoed from a request body.
		slog.Warn("provisioning provider request failed",
			"requestId", c.GetRespHeader(fiber.HeaderXRequestID),
			"method", c.Method(),
			"path", c.Path(),
			"resourceId", c.Params("id"),
			"providerErrorKind", provErr.Kind,
			"detail", provErr.Detail,
		)
		// The provider's own wording is preserved: it explains a refusal far better
		// than swallow can, and the adapter keeps credentials out of it.
		if provErr.Kind == provisioningdomain.ProviderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, provErr.Detail))
		}
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, provErr.Detail))
	}

	slog.Error("provisioning request failed",
		"requestId", c.GetRespHeader(fiber.HeaderXRequestID),
		"method", c.Method(),
		"path", c.Path(),
		"resourceId", c.Params("id"),
		"error", err,
	)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}

// ProviderEvents returns the recent machine history retained by the provisioner. It is
// intentionally separate from Operations: synchronous provider actions are not durable
// swallow Operations, while automation runs are.
func (h *ProvisioningHandler) ProviderEvents(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return apierror.Respond(c, apierror.New(
				apierror.CodeValidation,
				"limit must be an integer between 1 and 100.",
			))
		}
		limit = parsed
	}

	item, err := h.events.Execute(c.Context(), id, limit)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// AttachDurableOperations completes composition after the Operation context has been
// constructed. Legacy tests and deployments may omit it while compatibility endpoints live.
func (h *ProvisioningHandler) AttachDurableOperations(launcher application.DurableOperationLauncher) {
	h.durable = launcher
}

// AttachHardwareInspection makes Inspect start or resume an inspect-hardware Workflow (decision
// 053); without it Inspect is refused.
func (h *ProvisioningHandler) AttachHardwareInspection(launcher application.HardwareInspectionLauncher) {
	h.inspections = launcher
}

// AttachHostEnrollment enables the existing-OS enrollment bundle route.
func (h *ProvisioningHandler) AttachHostEnrollment(enrollment *application.HostEnrollmentUseCase) {
	h.enrollment = enrollment
}

// AttachPowerConfiguration enables the Power Configuration routes (decision 054). Without it, as in
// legacy tests, they answer 503 rather than panicking on a nil use case.
func (h *ProvisioningHandler) AttachPowerConfiguration(power *application.PowerConfigurationUseCase) {
	h.power = power
}

// AttachHTTPProxy enables the Provisioner HTTP Proxy routes (decision 071). Without it, as in legacy
// tests, they answer 503 rather than panicking on a nil use case.
func (h *ProvisioningHandler) AttachHTTPProxy(httpProxy *application.HTTPProxyUseCase) {
	h.httpProxy = httpProxy
}

// AttachSubnetDNS enables the Subnet DNS routes (decision 058). Without it, as in legacy tests,
// they answer 503 rather than panicking on a nil use case.
func (h *ProvisioningHandler) AttachSubnetDNS(subnetDNS *application.SubnetDNSUseCase) {
	h.subnetDNS = subnetDNS
}

// AttachVirtualMachineEnrollment enables the virtual-machine enrollment route (decision 055).
// Without it the route answers 503.
func (h *ProvisioningHandler) AttachVirtualMachineEnrollment(launcher application.VirtualMachineEnrollmentLauncher) {
	h.vmEnrollment = launcher
}
