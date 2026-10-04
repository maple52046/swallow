package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type ProvisioningHandler struct {
	deploy          *application.DeployServerUseCase
	deployments     *application.DeployServersUseCase
	templates       *application.DeploymentTemplateService
	targetPreflight *application.DeploymentTargetPreflightService
	networks        *application.NetworkConfigurationService
	tasks           *application.ProvisioningTaskService
	release         *application.ReleaseServerUseCase
	refresh         *application.RefreshServerUseCase
	images          *application.ListOSImagesUseCase
	reconcile       *application.ReconcileUseCase
	detail          *application.GetProvisionerDetailUseCase
	events          *application.GetProviderEventsUseCase
	actions         *application.MachineActionsUseCase
	deleteServer    *application.DeleteServerUseCase
	deleteImage     *application.DeleteOSImageUseCase
	uploadImage     *application.UploadOSImageUseCase
	imageOverlay    *application.SetOSImageOverlayUseCase
	serverTagsList  *application.ListServerTagsUseCase
	serverTagsEdit  *application.EditServerTagsUseCase
	durable         application.DurableOperationLauncher
}

func NewProvisioningHandler(
	deploy *application.DeployServerUseCase,
	deployments *application.DeployServersUseCase,
	targetPreflight *application.DeploymentTargetPreflightService,
	templates *application.DeploymentTemplateService,
	networks *application.NetworkConfigurationService,
	tasks *application.ProvisioningTaskService,
	release *application.ReleaseServerUseCase,
	refresh *application.RefreshServerUseCase,
	images *application.ListOSImagesUseCase,
	reconcile *application.ReconcileUseCase,
	detail *application.GetProvisionerDetailUseCase,
	events *application.GetProviderEventsUseCase,
	actions *application.MachineActionsUseCase,
	deleteServer *application.DeleteServerUseCase,
	deleteImage *application.DeleteOSImageUseCase,
	uploadImage *application.UploadOSImageUseCase,
	imageOverlay *application.SetOSImageOverlayUseCase,
	serverTagsList *application.ListServerTagsUseCase,
	serverTagsEdit *application.EditServerTagsUseCase,
	durable ...application.DurableOperationLauncher,
) *ProvisioningHandler {
	handler := &ProvisioningHandler{
		deploy:          deploy,
		deployments:     deployments,
		templates:       templates,
		targetPreflight: targetPreflight,
		release:         release,
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
		uploadImage:     uploadImage,
		imageOverlay:    imageOverlay,
		serverTagsList:  serverTagsList,
		serverTagsEdit:  serverTagsEdit,
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

type deployRequest struct {
	OSSystem     string `json:"osSystem"`
	DistroSeries string `json:"distroSeries"`
	UserData     string `json:"userData"`
	Comment      string `json:"comment"`
	// Ephemeral runs the OS from memory and leaves the disks untouched. Refused, not
	// ignored, when the provisioner cannot do it.
	Ephemeral bool `json:"ephemeral"`
	// DeployTarget is the preferred deploy-mode vocabulary ("disk"/"ram"); when present it is
	// authoritative and maps onto Ephemeral. Ephemeral stays accepted as a deprecated alias.
	DeployTarget *string `json:"deployTarget"`
}

type releaseRequest struct {
	Erase           bool   `json:"erase"`
	SecureErase     bool   `json:"secureErase"`
	QuickErase      bool   `json:"quickErase"`
	Comment         string `json:"comment"`
	UnbindStaticIPs bool   `json:"unbindStaticIPs"`
}

// Deploy starts an OS deployment on a server. Responds 202: the provisioner has
// accepted the request, and the reconciler tracks it from there.
func (h *ProvisioningHandler) Deploy(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	var req deployRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	// Required rather than defaulted: letting the provisioner pick an OS makes a
	// deployment a surprise.
	if req.DistroSeries == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "distroSeries is required."))
	}
	ephemeral := req.Ephemeral
	if req.DeployTarget != nil {
		target, valid := provisioningdomain.ParseDeployTarget(*req.DeployTarget)
		if !valid {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`))
		}
		ephemeral = target.Ephemeral()
	}

	item, err := h.deploy.Execute(c.Context(), application.DeployServerInput{
		ServerID:     id,
		OSSystem:     req.OSSystem,
		DistroSeries: req.DistroSeries,
		UserData:     req.UserData,
		Comment:      req.Comment,
		Ephemeral:    ephemeral,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

func (h *ProvisioningHandler) Release(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	var req releaseRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
		}
	}

	item, err := h.release.ExecuteWithOptions(c.Context(), application.ReleaseServerInput{
		ServerID:        id,
		Erase:           req.Erase,
		SecureErase:     req.SecureErase,
		UnbindStaticIPs: req.UnbindStaticIPs,
		RequestID:       c.GetRespHeader(fiber.HeaderXRequestID),
		QuickErase:      req.QuickErase,
		Comment:         req.Comment,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
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
	// The optional live BMC section can contain an operator-requested password. Keep the
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

// Inspect serves POST /servers/{id}/inspect (server-detail-actions.md): it starts the
// provisioner's hardware inspection and responds 202 with the accepted provisioning snapshot.
func (h *ProvisioningHandler) Inspect(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Inspect)
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

// uploadFieldMaxLen bounds each small text field on an image upload so a malformed multipart
// part cannot carry an unbounded string. The file content is bounded by the server body limit,
// not by this.
const uploadFieldMaxLen = 200

// UploadImage creates a new provider-owned OS image on one provisioner from an uploaded file.
//
// This is the only multipart/form-data endpoint on this surface because it carries a potentially
// multi-gigabyte artifact. fasthttp spools the file part to a temporary file, so the file handle
// is seekable and the handler streams from disk rather than holding the artifact in memory: the
// size comes from the multipart header and the sha256 is computed once over the spooled file,
// both of which the provider (MAAS) requires before it will accept the bytes. Swallow keeps no
// copy once the provider accepts it. Whether the uploaded image is a custom image is decided by
// the provider, not by this request. Errors follow the shared provisioning mapper: an unsupported
// provisioner or a provider refusal is a 400, an unknown integration is a 404, and a provider
// transport failure during upload is a 503.
func (h *ProvisioningHandler) UploadImage(c *fiber.Ctx) error {
	integrationID := strings.TrimSpace(c.FormValue("integrationId"))
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "name is required."))
	}
	architecture := strings.TrimSpace(c.FormValue("architecture"))
	if architecture == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "architecture is required."))
	}
	title := strings.TrimSpace(c.FormValue("title"))
	fileType := strings.TrimSpace(c.FormValue("filetype"))
	defaultUser := strings.TrimSpace(c.FormValue("defaultUser"))
	if len(name) > uploadFieldMaxLen || len(architecture) > uploadFieldMaxLen ||
		len(title) > uploadFieldMaxLen || len(fileType) > uploadFieldMaxLen {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "An upload field exceeds the maximum length."))
	}
	// Checked before the artifact is hashed and streamed, so a typo never costs a multi-gigabyte
	// upload that then fails.
	if defaultUser != "" && !provisioningdomain.ValidDefaultUser(defaultUser) {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"defaultUser must be a POSIX login name (lowercase letters, digits, '_' or '-', up to 32 characters)."))
	}

	fileHeader, err := c.FormFile("content")
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "content file is required."))
	}
	if fileHeader.Size <= 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "content file is empty."))
	}

	file, err := fileHeader.Open()
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The uploaded file could not be read."))
	}
	defer file.Close()

	// The sha256 is hashed over the spooled file first, then the handle is rewound so the same
	// bytes stream to the provider. MAAS needs the digest and size up front, and verifies the
	// digest on completion, so hashing here turns a corrupt transfer into an early, clear error.
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return RespondError(c, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return RespondError(c, err)
	}

	item, err := h.uploadImage.Execute(c.Context(), application.UploadOSImageInput{
		IntegrationID: integrationID,
		Name:          name,
		Architecture:  architecture,
		Title:         title,
		FileType:      fileType,
		Size:          fileHeader.Size,
		SHA256:        hex.EncodeToString(sum.Sum(nil)),
		Content:       file,
		DefaultUser:   defaultUser,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// imageOverlayRequest carries the swallow-owned display overrides, tags, and default user for an
// OS Image overlay. Each field is optional; an empty field clears that value so the image shows
// its provider value (or, for defaultUser, the built-in default). Tags and defaultUser are
// swallow-owned with no provider counterpart; an empty or omitted value clears them.
type imageOverlayRequest struct {
	Name        string   `json:"name"`
	OSSystem    string   `json:"osSystem"`
	Release     string   `json:"release"`
	Tags        []string `json:"tags"`
	DefaultUser string   `json:"defaultUser"`
}

// SetImageOverlay writes the swallow-owned display overlay (name, OS, release) for one OS Image,
// the effective values a user sees over the provider's. The image is selected by the same
// identity GET /images returns — integration, image ID, and architecture — passed as query
// parameters because an image ID contains a slash and cannot be a path segment, and one image
// name can back several architectures. The overlay is swallow-local and never changes the
// provider (docs/decisions/025).
func (h *ProvisioningHandler) SetImageOverlay(c *fiber.Ctx) error {
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
	var req imageOverlayRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	if err := h.imageOverlay.Set(c.Context(), integrationID, imageID, architecture, application.OSImageOverlayInput{
		Name: req.Name, OSSystem: req.OSSystem, Release: req.Release, Tags: req.Tags, DefaultUser: req.DefaultUser,
	}); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ClearImageOverlay removes the swallow-owned overlay for one OS Image, reverting it to the
// provider values. It uses the same query-parameter identity as SetImageOverlay and DeleteImage,
// and returns 204 even when no overlay existed, because the requested end state already holds.
func (h *ProvisioningHandler) ClearImageOverlay(c *fiber.Ctx) error {
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

	if err := h.imageOverlay.Clear(c.Context(), integrationID, imageID, architecture); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListServerTags returns the tags known for a Site, each flagged whether swallow may assign it, so
// the tag editor can offer existing names and disable the provider-computed (automatic) ones. It is
// under /provisioning rather than /servers to avoid colliding with GET /servers/:id, and scoped to a
// Site because tags are provisioner-owned per Site (docs/decisions/031).
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

// EditServerTags applies a tag edit to one or more Servers and returns each Server's effective tags.
// It drives the provisioner when it owns tags (MAAS) and writes swallow-owned tags otherwise, all
// behind one endpoint (docs/decisions/031). Editing tags is metadata, so it is not gated on Server
// Lock; a provider refusal (an automatic tag) is surfaced as a 400 by the shared error mapper.
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

// Reconcile runs a projection pass immediately instead of waiting for the interval.
//
// The report is returned rather than just an acknowledgement, because the interesting
// output is what it refused to do: conflicts need an operator, and hiding them behind
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

	case errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrDeploymentTemplateNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Deployment template not found."))

	case errors.Is(err, provisioningdomain.ErrDeploymentTemplateNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A deployment template with this name already exists for the integration."))

	case errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrProvisioningTaskNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Provisioning task not found."))

	case errors.Is(err, provisioningdomain.ErrProvisioningTaskConflict),
		errors.Is(err, provisioningdomain.ErrServerMutationConflict),
		errors.Is(err, serverdomain.ErrServerLocked),
		errors.Is(err, provisioningdomain.ErrNetworkConfigurationConflict),
		errors.Is(err, provisioningdomain.ErrNetworkConfigurationUnsupported):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, provisioningdomain.ErrInvalidDeploymentTemplate),
		errors.Is(err, provisioningdomain.ErrInvalidDeploymentBatch),
		errors.Is(err, provisioningdomain.ErrInvalidReleaseRequest),
		errors.Is(err, provisioningdomain.ErrInvalidNetworkConfiguration),
		errors.Is(err, provisioningdomain.ErrOSImageOverlayInvalid),
		errors.Is(err, provisioningdomain.ErrInvalidTag):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

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
	// locked target is a 409 conflict, and a rejected operation request is a 400.
	// Genuinely internal failures (repository writes) still keep the 500 fallback.
	case errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, operationdomain.ErrPolicyConflict),
		errors.Is(err, operationdomain.ErrWorkflowControlConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationapp.ErrInvalidOperation),
		errors.Is(err, operationdomain.ErrTargetStateInvalid):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
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
