package maas

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

const (
	// providerName is persisted in server.ProvisioningSource, so it must stay stable.
	providerName        = "maas"
	providerDisplayName = "Ubuntu MAAS"
	// MAAS omits resource sets from its list response, so image sizes require detail
	// reads. Bound those reads to avoid turning one catalog request into an unbounded
	// burst against the region controller.
	bootResourceDetailParallelism = 8
)

// Provider implements provisioningdomain.OSProvisioningProvider against MAAS.
type Provider struct {
	client *Client
	// uploadStageWait bounds how long UploadOSImage waits, after the last byte, for MAAS to report
	// the uploaded image complete; uploadStagePoll is the first interval between those reads.
	// Fields rather than constants only so adapter tests can shorten them.
	uploadStageWait time.Duration
	uploadStagePoll time.Duration
}

// The adapter is an OSImageUploader; the use case discovers the capability by type assertion, so
// a missing method would otherwise surface only as "unsupported" at run time.
var _ provisioningdomain.OSImageUploader = (*Provider)(nil)

func NewProvider(client *Client) *Provider {
	return &Provider{
		client:          client,
		uploadStageWait: uploadStageWaitDefault,
		uploadStagePoll: uploadStagePollInitial,
	}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) Probe(ctx context.Context) (provisioningdomain.ProviderInfo, error) {
	var out versionJSON
	if err := p.client.get(ctx, "/version/", nil, &out); err != nil {
		return provisioningdomain.ProviderInfo{}, translateError(err, "")
	}

	version := out.Version
	if out.Subversion != "" {
		version += " " + out.Subversion
	}

	return provisioningdomain.ProviderInfo{
		Name:        providerName,
		DisplayName: providerDisplayName,
		Version:     version,
	}, nil
}

func (p *Provider) ListMachines(
	ctx context.Context,
	filter provisioningdomain.MachineFilter,
) ([]*provisioningdomain.Machine, error) {
	var out []machineJSON
	if err := p.client.get(ctx, "/machines/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}

	machines := make([]*provisioningdomain.Machine, 0, len(out))
	for i := range out {
		machine := toDomainMachine(&out[i])
		if matchesFilter(machine, filter) {
			machines = append(machines, machine)
		}
	}
	return machines, nil
}

func (p *Provider) GetMachine(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	var out machineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

func (p *Provider) ListOSImages(ctx context.Context) ([]*provisioningdomain.OSImage, error) {
	var out []bootResourceJSON
	if err := p.client.get(ctx, "/boot-resources/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}
	p.populateBootResourceSets(ctx, out)
	return toDomainOSImages(out), nil
}

// populateBootResourceSets enriches deployable list entries with their detailed MAAS
// resource sets. Size is optional metadata: a detail that disappears during the list/detail
// race, or a temporary detail failure, leaves only that image without a size instead of
// making the otherwise usable deployment catalog unavailable.
func (p *Provider) populateBootResourceSets(ctx context.Context, resources []bootResourceJSON) {
	indices := make([]int, 0, len(resources))
	for index, resource := range resources {
		if resource.ID > 0 {
			if _, _, _, ok := bootResourceImageFields(resource); ok {
				indices = append(indices, index)
			}
		}
	}
	if len(indices) == 0 {
		return
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(bootResourceDetailParallelism, len(indices))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				var detail bootResourceJSON
				if err := p.client.get(ctx, bootResourcePath(resources[index].ID), nil, &detail); err == nil {
					resources[index].Sets = detail.Sets
				}
			}
		}()
	}

	for _, index := range indices {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

// DeleteOSImage permanently removes an uploaded MAAS custom image.
//
// MAAS identifies a boot resource by a numeric id the image catalog does not carry, so
// the resource is resolved from its name and CPU architecture first. Only uploaded custom
// images are removable: a synced image is a provider-owned mirror MAAS would re-sync, and
// a name that matches nothing deletable is a rejection, not a silent success. Every
// matching uploaded resource is deleted so the catalog row disappears.
func (p *Provider) DeleteOSImage(ctx context.Context, imageID, architecture string) error {
	var resources []bootResourceJSON
	if err := p.client.get(ctx, "/boot-resources/", nil, &resources); err != nil {
		return translateError(err, "")
	}

	ids := deletableBootResourceIDs(resources, imageID, architecture)
	if len(ids) == 0 {
		return &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected,
			Detail: fmt.Sprintf(
				"No deletable custom image %q (%s) exists. Only uploaded custom images can be deleted.",
				imageID, architecture),
		}
	}

	for _, id := range ids {
		if err := p.client.delete(ctx, bootResourcePath(id)); err != nil {
			return translateError(err, "")
		}
	}
	return nil
}

func bootResourcePath(id int) string {
	return "/boot-resources/" + strconv.Itoa(id) + "/"
}

// uploadChunkSize is the byte length of each PUT during a chunked boot-resource upload. It
// matches the MAAS CLI's own chunk size so each request stays small enough to complete within
// the per-request client timeout while a multi-gigabyte artifact is streamed sequentially.
const uploadChunkSize = 1 << 22 // 4 MiB

// uploadStageWaitDefault bounds the wait for MAAS to mark an uploaded resource set complete after
// the last chunk. MAAS verifies the sha256 and finalizes the file when the declared size has
// arrived, which normally takes seconds; the bound keeps a provider that never finishes from
// holding the content request open indefinitely.
const uploadStageWaitDefault = 5 * time.Minute

// uploadStagePollInitial and uploadStagePollMax are the first and the largest interval between
// reads of an uploaded resource while waiting for it to be staged; the interval doubles.
const (
	uploadStagePollInitial = 2 * time.Second
	uploadStagePollMax     = 15 * time.Second
)

// uploadCleanupTimeout bounds the best-effort delete of an incomplete uploaded resource. It runs
// detached from the request's cancellation, so it needs a bound of its own.
const uploadCleanupTimeout = 30 * time.Second

// maasUploadFileTypes are the file types MAAS accepts for an uploaded boot resource (the choices
// of its boot-resource upload form): root tarballs and the dd disk-image formats. Empty means
// MAAS's default, a root tarball.
var maasUploadFileTypes = []string{
	"tgz", "tbz", "txz", "ddtgz", "ddtbz", "ddtxz", "ddtar", "ddbz2", "ddgz", "ddxz", "ddraw",
}

// maasUploadArchitecturePattern is the "<arch>/<subarch>" form MAAS stores boot resources under,
// for example "amd64/generic" or "arm64/hwe-24.04".
var maasUploadArchitecturePattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9._-]+$`)

// ValidateOSImageUpload refuses, before any content is sent, an upload MAAS would refuse for its
// file type or architecture. It contacts nothing; the name and everything else are still MAAS's
// to accept when the resource is created.
func (p *Provider) ValidateOSImageUpload(req provisioningdomain.UploadOSImageRequest) error {
	if req.FileType != "" && !slices.Contains(maasUploadFileTypes, req.FileType) {
		return &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected,
			Detail: fmt.Sprintf("MAAS does not accept the file type %q; use one of %s.",
				req.FileType, strings.Join(maasUploadFileTypes, ", ")),
		}
	}
	if !maasUploadArchitecturePattern.MatchString(maasUploadArchitecture(req.Architecture)) {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: fmt.Sprintf("MAAS does not accept the architecture %q; use a form such as amd64 or amd64/generic.", req.Architecture),
		}
	}
	return nil
}

// UploadOSImage creates a new MAAS custom (Uploaded) boot resource from user-supplied
// content, streams the artifact to it, and returns once MAAS reports it complete.
//
// MAAS reserves the resource from metadata first — it needs the exact size and sha256 up front —
// and then accepts the bytes in chunks against the upload URI it returns for the incomplete file.
// The artifact is therefore streamed sequentially as it arrives and never held whole in memory.
// The CPU architecture is expanded to MAAS's "<arch>/generic" form and MAAS classifies the result
// as an Uploaded/custom resource on its own; swallow does not label it. When the transfer fails
// after the resource was created, the incomplete resource is deleted so it does not linger as a
// half-uploaded image. After the last chunk the resource is read back until a set is complete
// (bounded by uploadStageWait), so the returned image carries the final size and classification
// and can be deployed; a resource still incomplete then stays in MAAS and is reported as
// unavailable.
func (p *Provider) UploadOSImage(
	ctx context.Context,
	req provisioningdomain.UploadOSImageRequest,
) (*provisioningdomain.OSImage, error) {
	fields := map[string]string{
		"name":         req.Name,
		"architecture": maasUploadArchitecture(req.Architecture),
		"sha256":       req.SHA256,
		"size":         strconv.FormatInt(req.Size, 10),
		"title":        req.Title,
		"filetype":     req.FileType,
	}
	var created bootResourceJSON
	if err := p.client.postMultipart(ctx, "/boot-resources/", fields, &created); err != nil {
		return nil, translateError(err, "")
	}

	uploadURI, ok := incompleteUploadURI(created)
	if !ok {
		// MAAS accepted the metadata but exposed no file to upload to. Nothing was streamed,
		// so this is a provider-side refusal rather than a transport failure.
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS accepted the image metadata but returned no upload target.",
		}
	}

	if err := p.streamUpload(ctx, uploadURI, req.Content); err != nil {
		p.removeIncompleteUpload(ctx, created.ID)
		return nil, err
	}
	return p.awaitStagedUpload(ctx, created.ID)
}

// awaitStagedUpload reads an uploaded resource back until MAAS reports a complete set, so the
// returned image reflects the final size and classification instead of the pre-upload
// placeholder, and is deployable. Reads back off from uploadStagePoll to uploadStagePollMax and
// stop at uploadStageWait or when ctx ends; either way the resource stays in MAAS, so the error
// says it exists but is not ready. A read failure is translated as usual.
func (p *Provider) awaitStagedUpload(ctx context.Context, resourceID int) (*provisioningdomain.OSImage, error) {
	deadline := time.Now().Add(p.uploadStageWait)
	delay := p.uploadStagePoll
	for {
		var detail bootResourceJSON
		if err := p.client.get(ctx, bootResourcePath(resourceID), nil, &detail); err != nil {
			return nil, translateError(err, "")
		}
		images := toDomainOSImages([]bootResourceJSON{detail})
		if len(images) == 0 {
			return nil, &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorRejected,
				Detail: "MAAS stored the image but does not report it as deployable.",
			}
		}
		if images[0].Complete {
			return images[0], nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, notStagedError(nil)
		}
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, notStagedError(ctx.Err())
		case <-timer.C:
		}
		delay = min(delay*2, uploadStagePollMax)
	}
}

// notStagedError reports an uploaded image MAAS received but has not marked complete. The image
// exists in MAAS; swallow's deploy gate refuses it until MAAS finishes staging it.
func notStagedError(cause error) error {
	return &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorUnavailable,
		Detail: "MAAS received the image but has not finished staging it. The image exists in MAAS; " +
			"deployments of it are refused until MAAS reports it complete.",
		Err: cause,
	}
}

// removeIncompleteUpload deletes the uploaded resource a failed transfer left behind, so a broken
// upload does not linger in MAAS as an incomplete image. It is best effort: the transfer's own
// error is what the caller reports, and a resource that cannot be deleted now is an incomplete
// custom image the user can still delete. It runs detached from ctx's cancellation, because the
// transfer may have failed precisely because the request went away.
func (p *Provider) removeIncompleteUpload(ctx context.Context, resourceID int) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uploadCleanupTimeout)
	defer cancel()
	if err := p.client.delete(cleanupCtx, bootResourcePath(resourceID)); err != nil {
		slog.Warn("incomplete uploaded boot resource was not deleted", "bootResourceId", resourceID, "error", err)
	}
}

// streamUpload reads content in fixed-size chunks and PUTs each to the MAAS upload URI until the
// stream is exhausted. A read failure on the source stream is reported as unavailable rather than
// a provider rejection, because it is swallow's own transfer that broke, not MAAS refusing.
func (p *Provider) streamUpload(ctx context.Context, uploadURI string, content io.Reader) error {
	buf := make([]byte, uploadChunkSize)
	for {
		n, readErr := io.ReadFull(content, buf)
		if n > 0 {
			if err := p.client.putUpload(ctx, uploadURI, bytes.NewReader(buf[:n]), int64(n)); err != nil {
				return translateError(err, "")
			}
		}
		switch {
		case readErr == nil:
			continue
		case errors.Is(readErr, io.EOF), errors.Is(readErr, io.ErrUnexpectedEOF):
			return nil
		default:
			return &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorUnavailable,
				Detail: "Reading the upload stream failed before the image was fully sent.",
				Err:    readErr,
			}
		}
	}
}

// incompleteUploadURI returns the chunked-upload target MAAS assigns to the boot-resource file
// that still needs bytes. MAAS nests it under sets -> files, so the create response is walked for
// the first file that is not yet complete and carries an upload URI.
func incompleteUploadURI(r bootResourceJSON) (string, bool) {
	for _, set := range r.Sets {
		for _, file := range set.Files {
			if file.UploadURI != "" && !file.Complete {
				return file.UploadURI, true
			}
		}
	}
	return "", false
}

// maasUploadArchitecture expands a CPU architecture like "amd64" into the "<arch>/subarch" form
// MAAS stores boot resources under, defaulting the subarchitecture to "generic" when the caller
// gives only the CPU architecture. An architecture that already carries a subarchitecture is
// passed through unchanged.
func maasUploadArchitecture(architecture string) string {
	arch := strings.TrimSpace(architecture)
	if arch == "" {
		return ""
	}
	if strings.Contains(arch, "/") {
		return arch
	}
	return arch + "/generic"
}

func (p *Provider) Deploy(
	ctx context.Context,
	req provisioningdomain.DeployRequest,
) (*provisioningdomain.Machine, error) {
	fields := map[string]string{
		"osystem":       req.OSSystem,
		"distro_series": req.DistroSeries,
		"comment":       req.Comment,
	}
	if req.UserData != "" {
		// MAAS serves user data to the machine through its metadata service and
		// expects it base64-encoded on the way in.
		fields["user_data"] = base64.StdEncoding.EncodeToString([]byte(req.UserData))
	}
	if req.Ephemeral {
		// Sent only when asked, so that MAAS keeps deciding what a plain deployment
		// means rather than swallow asserting a default it does not own.
		fields["ephemeral_deploy"] = "true"
	}

	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(req.MachineID), "deploy", fields, &out); err != nil {
		return nil, translateError(err, req.MachineID)
	}

	// MAAS ignores form fields it does not recognise, so an older version would accept
	// this request and quietly install to disk. Only an explicit contradiction is
	// treated as one: silence means the version predates the field and cannot be read
	// either way.
	if req.Ephemeral && out.EphemeralDeploy != nil && !*out.EphemeralDeploy {
		return nil, &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS accepted the deployment but reports it as non-ephemeral, " +
				"so it is installing to disk. The deployment is already running: " +
				"release the machine if that is not wanted.",
		}
	}
	return toDomainMachine(&out), nil
}

// ValidateDeploymentTarget checks the provider-owned network prerequisite without
// mutating the machine. MAAS refuses deployment when no interface is linked to a
// subnet; swallow surfaces that before dispatch but never guesses which subnet to use.
func (p *Provider) ValidateDeploymentTarget(ctx context.Context, machineID string) error {
	var machine machineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &machine); err != nil {
		return translateError(err, machineID)
	}
	for _, iface := range machine.InterfaceSet {
		for _, link := range iface.Links {
			if link.Subnet != nil {
				return nil
			}
		}
	}
	return &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorRejected,
		Detail: "No MAAS interface is linked to a subnet. Configure the machine's " +
			"Network in MAAS, then check deployment readiness again.",
	}
}

// Capabilities reports what this adapter honours. All of these are present on the MAAS
// versions this adapter targets, so every flag is set; the flags exist so a client can
// hide an action a future provider lacks, and so an unsupported request is refused
// rather than silently dropped.
func (p *Provider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{
		EphemeralDeploy:      true,
		DeploymentReadiness:  true,
		NetworkConfiguration: true,
		Power:                true,
		HardwareValidation:   true,
		StateActions:         true,
		MachineDetail:        true,
		HardwareInventory:    true,
		MachineRemoval:       true,
		ReleaseOptions:       true,
		ImageRemoval:         true,
		ImageUpload:          true,
		Grouping:             true,
		Tagging:              true,
		SSHKeyRegistration:   true,
		PowerConfiguration:   true,
		MachineRegistration:  true,
		HTTPProxy:            true,
		SubnetDNS:            true,
	}
}

func (p *Provider) Release(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(machineID), "release", nil, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

// ReleaseWithOptions maps provider-neutral erasure controls onto the MAAS release
// operation. False options are omitted rather than sent as strings, and force is never
// supplied, so MAAS safeguards remain authoritative.
func (p *Provider) ReleaseWithOptions(ctx context.Context, req provisioningdomain.ReleaseRequest) (*provisioningdomain.Machine, error) {
	fields := make(map[string]string, 4)
	if req.Erase {
		fields["erase"] = "true"
	}
	if req.SecureErase {
		fields["secure_erase"] = "true"
	}
	if req.QuickErase {
		fields["quick_erase"] = "true"
	}
	if req.Comment != "" {
		fields["comment"] = req.Comment
	}

	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(req.MachineID), "release", fields, &out); err != nil {
		return nil, translateError(err, req.MachineID)
	}
	return toDomainMachine(&out), nil
}

func machinePath(machineID string) string {
	return "/machines/" + url.PathEscape(machineID) + "/"
}

// translateError converts transport and MAAS HTTP failures into the errors the
// provisioning port defines.
//
// machineID is non-empty for machine-scoped calls, where a 404 means that machine
// is gone rather than that the API endpoint is wrong.
//
// A ProviderError keeps err as its Err and names the failing call in Endpoint (the API root,
// without userinfo) and Operation (method, path, and named operation), which the client recorded
// beside the failure. They are the administrator's evidence; Detail, which the API returns
// verbatim, stays free of addresses.
func translateError(err error, machineID string) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &provisioningdomain.ProviderError{
			Kind:      provisioningdomain.ProviderErrorUnavailable,
			Detail:    "Could not reach MAAS.",
			Endpoint:  transportErr.call.endpoint,
			Operation: transportErr.call.operation,
			Err:       err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		// A decoding or programming failure; not something to blame MAAS for.
		return err
	}

	providerErr := &provisioningdomain.ProviderError{
		Endpoint: apiErr.call.endpoint, Operation: apiErr.call.operation, Err: err,
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound && machineID != "":
		return provisioningdomain.ErrMachineNotFound

	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		// The caller's own credentials were already accepted by swallow, so this can
		// only be the API key swallow is configured with.
		providerErr.Kind, providerErr.Detail = provisioningdomain.ProviderErrorAuth, "MAAS rejected the API key swallow is configured with."

	case apiErr.StatusCode >= http.StatusInternalServerError:
		providerErr.Kind = provisioningdomain.ProviderErrorUnavailable
		providerErr.Detail = fmt.Sprintf("MAAS reported an internal error (HTTP %d).", apiErr.StatusCode)

	default:
		providerErr.Kind, providerErr.Detail = provisioningdomain.ProviderErrorRejected, rejectionDetail(apiErr)
	}
	return providerErr
}

// rejectionDetail renders MAAS's own explanation for a refused request.
//
// MAAS answers either with a plain sentence or with a JSON object mapping a field
// name to its complaints; both are flattened to one line so that whatever MAAS
// objected to reaches the user intact.
func rejectionDetail(e *apiError) string {
	if e.Body == "" {
		return fmt.Sprintf("MAAS refused the request (HTTP %d).", e.StatusCode)
	}

	var byField map[string][]string
	if err := json.Unmarshal([]byte(e.Body), &byField); err == nil && len(byField) > 0 {
		fields := make([]string, 0, len(byField))
		for field := range byField {
			fields = append(fields, field)
		}
		sort.Strings(fields)

		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			parts = append(parts, field+": "+strings.Join(byField[field], " "))
		}
		return "MAAS refused the request: " + strings.Join(parts, "; ")
	}

	return "MAAS refused the request: " + e.Body
}
