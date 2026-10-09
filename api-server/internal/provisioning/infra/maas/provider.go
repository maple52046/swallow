package maas

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
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
}

func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
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

// UploadOSImage creates a new MAAS custom (Uploaded) boot resource from operator-supplied
// content and streams the artifact to it.
//
// MAAS reserves the resource from metadata first — it needs the exact size and sha256 up front —
// and then accepts the bytes in chunks against the upload URI it returns for the incomplete file.
// The artifact is therefore streamed sequentially and never held whole in memory. The CPU
// architecture is expanded to MAAS's "<arch>/generic" form and MAAS classifies the result as an
// Uploaded/custom resource on its own; swallow does not label it. The completed resource is read
// back so the returned image carries the final size and classification rather than the pre-upload
// placeholder.
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
		return nil, err
	}

	// Re-read the resource so the returned image reflects the completed set size and the
	// provider's final classification instead of the pre-upload placeholder.
	var detail bootResourceJSON
	if err := p.client.get(ctx, bootResourcePath(created.ID), nil, &detail); err != nil {
		return nil, translateError(err, "")
	}
	images := toDomainOSImages([]bootResourceJSON{detail})
	if len(images) == 0 {
		return nil, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS stored the image but does not report it as deployable.",
		}
	}
	return images[0], nil
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
// subnet; Swallow surfaces that before dispatch but never guesses which subnet to use.
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
		OperatorState:        true,
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
func translateError(err error, machineID string) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: "Could not reach MAAS.",
			Err:    err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		// A decoding or programming failure; not something to blame MAAS for.
		return err
	}

	switch {
	case apiErr.StatusCode == http.StatusNotFound && machineID != "":
		return provisioningdomain.ErrMachineNotFound

	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		// The caller's own credentials were already accepted by swallow, so this can
		// only be the API key swallow is configured with.
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorAuth,
			Detail: "MAAS rejected the API key swallow is configured with.",
			Err:    err,
		}

	case apiErr.StatusCode >= http.StatusInternalServerError:
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: fmt.Sprintf("MAAS reported an internal error (HTTP %d).", apiErr.StatusCode),
			Err:    err,
		}

	default:
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: rejectionDetail(apiErr),
			Err:    err,
		}
	}
}

// rejectionDetail renders MAAS's own explanation for a refused request.
//
// MAAS answers either with a plain sentence or with a JSON object mapping a field
// name to its complaints; both are flattened to one line so that whatever MAAS
// objected to reaches the operator intact.
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
