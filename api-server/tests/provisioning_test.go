package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maple52046/swallow/internal/adapters/osprovisioning/maas"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// deployOS accepts an OS deployment through the current route; the fixture's launcher runs the
// deployment use case the Workflow's provision-os Tasks run, so the provider calls and the Server
// axis are observable when it returns.
func deployOS(t *testing.T, f *platformFixture, body map[string]any) *http.Response {
	t.Helper()
	return doRequest(t, f.app, "POST", "/api/v1/provisioning/deployment-operations", body, f.adminAuth(t))
}

// Deploy is addressed by server ID; which provisioner to call is derived from the
// server's source. That mapping is what swallow exists to hold.
func TestDeployOperation_ResolvesProviderFromServerSource(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")

	resp := deployOS(t, f, map[string]any{
		"serverIds": []string{"srv-1"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
		"userData":  map[string]any{"mode": "replace", "value": "#cloud-config"},
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if body := parseBody(t, resp); body["operationId"] == "" {
		t.Errorf("the response must name the accepted Workflow, got %v", body)
	}
	if len(f.provider.deployRequests) != 1 {
		t.Fatalf("expected 1 deploy call, got %d", len(f.provider.deployRequests))
	}
	req := f.provider.deployRequests[0]
	if req.MachineID != "machine-srv-1" {
		t.Errorf("the provider must be called with its own machine ID, got %q", req.MachineID)
	}
	if req.DistroSeries != "ubuntu/jammy" || req.UserData != "#cloud-config" || req.Ephemeral {
		t.Errorf("unexpected request: %+v", req)
	}

	// The axis is written straight away so a caller re-reading the server sees "deploying"
	// rather than the previous state until the next reconcile pass.
	resp = doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	provisioning := serverStatusOf(parseBody(t, resp))["provisioning"].(map[string]any)
	if provisioning["state"] != "deploying" {
		t.Fatalf("expected the axis to reflect the deployment, got %v", provisioning["state"])
	}
}

// Deploying from memory leaves the disks untouched and loses everything on reboot, so
// the request has to reach the provisioner and the result has to be visible afterwards.
func TestDeployOperation_RAMIsForwardedAndProjected(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")

	resp := deployOS(t, f, map[string]any{
		"serverIds": []string{"srv-1"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy", "deployTarget": "ram"},
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if len(f.provider.deployRequests) != 1 || !f.provider.deployRequests[0].Ephemeral {
		t.Fatalf("the RAM deployment must reach the provisioner, got %+v", f.provider.deployRequests)
	}

	// Readable afterwards: nothing else on the axis distinguishes this from a machine
	// with the same OS installed on disk.
	resp = doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	provisioning := serverStatusOf(parseBody(t, resp))["provisioning"].(map[string]any)
	if provisioning["ephemeral"] != true {
		t.Errorf("the axis must show ephemerality, got %v", provisioning["ephemeral"])
	}
}

// The one deploy option where being ignored produces the opposite of the instruction:
// asking for "nothing is written to disk" and getting a disk installation. So it is
// refused before the provisioner is called at all.
func TestDeployOperation_RAMRefusedWhenProvisionerCannotDoIt(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")
	f.provider.capabilities.EphemeralDeploy = false

	resp := deployOS(t, f, map[string]any{
		"serverIds": []string{"srv-1"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy", "deployTarget": "ram"},
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if len(f.provider.deployRequests) != 0 {
		t.Fatalf("no deployment may be started at all, got %+v", f.provider.deployRequests)
	}
}

func TestDeployOperation_RefusesAnUnknownDeployTarget(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")

	resp := deployOS(t, f, map[string]any{
		"serverIds": []string{"srv-1"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy", "deployTarget": "floppy"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "validation_error" {
		t.Fatalf("expected validation_error, got %s", code)
	}
}

func TestDeployOperation_UnknownServer(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	resp := deployOS(t, f, map[string]any{
		"serverIds": []string{"nope"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
}

// A provider that refuses or cannot be reached fails that target's provision-os Task with the
// provider's own explanation, which is the actionable part.
func TestDeployOperation_ProviderFailuresKeepTheirWording(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		err        *provisioningdomain.ProviderError
	}{
		{name: "rejected", code: "provider_rejected", err: &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected, Detail: "MAAS refused the request: storage: Mount the root '/' filesystem.",
		}},
		{name: "unavailable", code: "provider_unavailable", err: &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "Could not reach MAAS.",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupPlatform(t)
			seedProvisionerIntegration(t, f)
			seedReadyServer(t, f, "srv-1")
			f.provider.deployErr = tc.err

			resp := deployOS(t, f, map[string]any{
				"serverIds": []string{"srv-1"},
				"settings":  map[string]any{"imageId": "ubuntu/jammy"},
			})
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
			}
			result := f.launcher.lastDeployment
			if result == nil || len(result.Failed) != 1 {
				t.Fatalf("deployment result = %+v, want one failed target", result)
			}
			if failure := result.Failed[0]; failure.Code != tc.code || failure.Message != tc.err.Detail {
				t.Fatalf("failure = %+v, want code %s with the provider's wording", failure, tc.code)
			}
		})
	}
}

func releaseOS(t *testing.T, f *platformFixture, body map[string]any) *http.Response {
	t.Helper()
	return doRequest(t, f.app, "POST", "/api/v1/provisioning/release-operations", body, f.adminAuth(t))
}

func TestReleaseOperation_DoesNotRemoveTheServer(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := releaseOS(t, f, map[string]any{"serverIds": []string{"srv-1"}})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if len(f.provider.releaseCalls) != 1 || f.provider.releaseCalls[0] != "machine-srv-1" {
		t.Fatalf("unexpected release calls: %v", f.provider.releaseCalls)
	}

	// The physical machine still exists and swallow still manages it.
	if _, ok := f.servers.servers["srv-1"]; !ok {
		t.Fatal("release must not delete the server")
	}
}

func TestReleaseOperation_PassesDiskErasureOptions(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := releaseOS(t, f, map[string]any{
		"serverIds": []string{"srv-1"},
		"erase":     true, "secureErase": true, "quickErase": true,
		"comment": "retire from test pool",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	if len(f.provider.releaseRequests) != 1 {
		t.Fatalf("release requests: got %d, want 1", len(f.provider.releaseRequests))
	}
	request := f.provider.releaseRequests[0]
	if request.MachineID != "machine-srv-1" || !request.Erase || !request.SecureErase || !request.QuickErase || request.Comment != "retire from test pool" {
		t.Fatalf("unexpected release request: %+v", request)
	}
}

func TestReleaseOperation_RejectsEraseModesWithoutErase(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := releaseOS(t, f, map[string]any{"serverIds": []string{"srv-1"}, "secureErase": true})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "validation_error" {
		t.Fatalf("expected validation_error, got %s", code)
	}
	if len(f.provider.releaseCalls) != 0 {
		t.Fatalf("provider must not be called for invalid release options: %v", f.provider.releaseCalls)
	}
}

// Images differ per site, so a merged list would offer images the target site cannot
// deploy. The integration has to be named.
func TestListImages_RequiresIntegrationID(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/images", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListImages_Success(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	items := parseArrayBody(t, resp)
	if len(items) != 1 || items[0]["id"] != "ubuntu/jammy" {
		t.Fatalf("unexpected images: %v", items)
	}
}

func TestListImages_UnknownIntegration(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId=nope", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// A user who is not an admin may not list Integrations, but reads the images of every provisioner
// of a Site in one reference read (provisioning.md "Image catalogs of a Site"). It learns only each
// Integration's id, name, and Site, and a provisioner that cannot be read is reported beside the
// ones that could instead of failing the request.
func TestListImageCatalogs_UserReadsEveryProvisionerOfASite(t *testing.T) {
	f := setupPlatform(t)
	ctx := context.Background()
	if err := f.sites.Create(ctx, &sitedomain.Site{ID: testSiteID, Name: "dc-east"}); err != nil {
		t.Fatalf("seed site: %v", err)
	}
	seedProvisionerIntegration(t, f)
	// A second provisioner the fake factory cannot open, so its catalog cannot be read.
	if err := f.integrations.Create(ctx, &sitedomain.Integration{
		ID: "integration-2", SiteID: testSiteID, Kind: sitedomain.IntegrationKindProvisioner,
		ProviderKind: maas.ProviderKind, Name: "maas-west", Endpoint: "http://maas-west:5240/MAAS", Enabled: true,
	}, "ck:tk:ts"); err != nil {
		t.Fatalf("seed second provisioner: %v", err)
	}
	user := map[string]string{"Authorization": "Bearer " + userToken(t, f.jwtSvc)}

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/image-catalogs?siteId="+testSiteID, nil, user)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	items, _ := parseBody(t, resp)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v, want one catalog per provisioner", items)
	}
	east, west := items[0].(map[string]any), items[1].(map[string]any)
	if east["integrationId"] != testIntegrationID || east["integrationName"] != "maas-east" || east["siteId"] != testSiteID || east["error"] != nil {
		t.Fatalf("first catalog = %v, want maas-east read without error", east)
	}
	if images, _ := east["images"].([]any); len(images) != 1 || images[0].(map[string]any)["id"] != "ubuntu/jammy" {
		t.Fatalf("maas-east images = %v, want ubuntu/jammy", east["images"])
	}
	if _, leaked := east["endpoint"]; leaked {
		t.Fatalf("catalog carries the Integration's endpoint: %v", east)
	}
	if west["integrationName"] != "maas-west" || west["error"] == nil {
		t.Fatalf("second catalog = %v, want maas-west with an error", west)
	}
	if images, _ := west["images"].([]any); len(images) != 0 {
		t.Fatalf("maas-west images = %v, want none for an unreadable catalog", west["images"])
	}
}

func TestListImageCatalogs_UnknownSite(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/image-catalogs?siteId=nope", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "not_found" {
		t.Fatalf("error code = %q, want not_found", code)
	}
}

// imageUploadContent is the content the upload tests send; testImageUploadSHA256 is its digest
// in the lowercase hex form the contract requires.
var (
	imageUploadContent    = []byte("fake-image-bytes")
	testImageUploadSHA256 = func() string {
		sum := sha256.Sum256(imageUploadContent)
		return hex.EncodeToString(sum[:])
	}()
)

// createImageUpload records an upload intent through POST /image-uploads and returns the
// response; fields override the defaults of a valid intent for the test content.
func createImageUpload(t *testing.T, f *platformFixture, fields map[string]any) *http.Response {
	t.Helper()
	body := map[string]any{
		"integrationId": testIntegrationID,
		"name":          "ubuntu-24.04-rocm",
		"architecture":  "amd64",
		"sizeBytes":     len(imageUploadContent),
		"sha256":        testImageUploadSHA256,
	}
	for name, value := range fields {
		body[name] = value
	}
	return doRequest(t, f.app, "POST", "/api/v1/provisioning/image-uploads", body, f.adminAuth(t))
}

// createdImageUploadID records a valid intent and returns the upload's id.
func createdImageUploadID(t *testing.T, f *platformFixture, fields map[string]any) string {
	t.Helper()
	resp := createImageUpload(t, f, fields)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /image-uploads status = %d, want 201", resp.StatusCode)
	}
	id, _ := parseBody(t, resp)["id"].(string)
	if id == "" {
		t.Fatal("POST /image-uploads returned no id")
	}
	return id
}

// sendImageUploadContent PUTs raw content to an upload. doRequest forces JSON, so the content
// request is built here with the media type and length the contract requires.
func sendImageUploadContent(t *testing.T, f *platformFixture, id, contentType string, content []byte) *http.Response {
	t.Helper()
	req := httptest.NewRequest("PUT", "/api/v1/provisioning/image-uploads/"+id+"/content", bytes.NewReader(content))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken(t, f.jwtSvc))
	resp, err := f.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

// A successful upload records the intent, then streams the content to the provider and returns
// the created catalog row. The caller never sends a "custom" flag: the provider classifies the
// uploaded image (here as the custom osystem), which is what the response reflects. The provider
// receives the size and digest the caller declared, and the upload is gone afterwards.
func TestImageUpload_Success(t *testing.T) {
	f := setupPlatform(t)

	resp := createImageUpload(t, f, map[string]any{"filetype": "tgz", "title": "Ubuntu 24.04 ROCm", "defaultUser": "cloud-user"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /image-uploads status = %d, want 201", resp.StatusCode)
	}
	created := parseBody(t, resp)
	id, _ := created["id"].(string)
	if id == "" || created["expiresAt"] == "" {
		t.Fatalf("POST /image-uploads body = %v, want an id and expiresAt", created)
	}
	if len(f.provider.uploadedImages) != 0 {
		t.Fatal("creating an upload must not reach the provider")
	}

	resp = sendImageUploadContent(t, f, id, "application/octet-stream", imageUploadContent)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT content status = %d, want 201", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["id"] != "ubuntu-24.04-rocm" || body["architecture"] != "amd64" {
		t.Errorf("image identity = %v/%v, want ubuntu-24.04-rocm/amd64", body["id"], body["architecture"])
	}
	if status := serverStatusOf(body); status["providerOsSystem"] != "custom" || status["defaultUser"] != "cloud-user" {
		t.Errorf("status = %v, want providerOsSystem custom (provider-classified) and defaultUser cloud-user", status)
	}

	if len(f.provider.uploadedImages) != 1 {
		t.Fatalf("recorded uploads = %d, want 1", len(f.provider.uploadedImages))
	}
	uploaded := f.provider.uploadedImages[0]
	if uploaded.Name != "ubuntu-24.04-rocm" || uploaded.FileType != "tgz" ||
		uploaded.Size != int64(len(imageUploadContent)) || uploaded.SHA256 != testImageUploadSHA256 {
		t.Errorf("provider request = %+v, want the declared name, filetype, size, and sha256", uploaded)
	}
	if n := f.imageUploads.count(); n != 0 {
		t.Errorf("remaining uploads = %d, want 0 after the content request", n)
	}
	if resp := sendImageUploadContent(t, f, id, "application/octet-stream", imageUploadContent); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second PUT content status = %d, want 404 (an upload is single-use)", resp.StatusCode)
	}
}

// An intent that breaks a swallow rule, or that the adapter refuses, is refused before anything
// is recorded; a declared size over the installation's limit is 413.
func TestImageUpload_CreateRefusesBadIntent(t *testing.T) {
	tests := []struct {
		name       string
		fields     map[string]any
		adapterErr error
		wantStatus int
	}{
		{name: "missing sha256", fields: map[string]any{"sha256": ""}, wantStatus: http.StatusBadRequest},
		{name: "uppercase sha256", fields: map[string]any{"sha256": strings.ToUpper(testImageUploadSHA256)}, wantStatus: http.StatusBadRequest},
		{name: "zero size", fields: map[string]any{"sizeBytes": 0}, wantStatus: http.StatusBadRequest},
		{name: "invalid default user", fields: map[string]any{"defaultUser": "Cloud User"}, wantStatus: http.StatusBadRequest},
		{
			name:       "adapter refuses the file type",
			fields:     map[string]any{"filetype": "iso"},
			adapterErr: &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "unsupported file type"},
			wantStatus: http.StatusBadRequest,
		},
		{name: "over the upload limit", fields: map[string]any{"sizeBytes": testImageUploadMaxBytes + 1}, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "unknown integration", fields: map[string]any{"integrationId": "no-such-integration"}, wantStatus: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupPlatform(t)
			f.provider.validateUploadErr = tc.adapterErr

			resp := createImageUpload(t, f, tc.fields)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("POST /image-uploads status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if n := f.imageUploads.count(); n != 0 {
				t.Errorf("recorded uploads = %d, want 0 for a refused intent", n)
			}
		})
	}
}

// The content request is checked against its upload. A wrong media type is refused before the
// upload is claimed, so the upload stays usable; a length other than the declared size claims and
// removes it; an unknown upload is 404.
func TestImageUpload_ContentChecks(t *testing.T) {
	f := setupPlatform(t)

	id := createdImageUploadID(t, f, nil)
	if resp := sendImageUploadContent(t, f, id, "text/plain", imageUploadContent); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong Content-Type status = %d, want 400", resp.StatusCode)
	}
	if n := f.imageUploads.count(); n != 1 {
		t.Fatalf("remaining uploads = %d, want 1: a wrong media type must leave the upload usable", n)
	}

	if resp := sendImageUploadContent(t, f, id, "application/octet-stream", []byte("short")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong length status = %d, want 400", resp.StatusCode)
	}
	if len(f.provider.uploadedImages) != 0 {
		t.Error("content of the wrong length must not reach the provider")
	}
	if resp := sendImageUploadContent(t, f, id, "application/octet-stream", imageUploadContent); resp.StatusCode != http.StatusNotFound {
		t.Errorf("PUT after a failed content request status = %d, want 404 (the failure removed the upload)", resp.StatusCode)
	}

	if resp := sendImageUploadContent(t, f, "no-such-upload", "application/octet-stream", imageUploadContent); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown upload status = %d, want 404", resp.StatusCode)
	}
}

// A provider refusal while the content streams (for example content that does not match the
// declared sha256) is a 400 with the provider's wording, and the upload is removed.
func TestImageUpload_ProviderRefusalRemovesUpload(t *testing.T) {
	f := setupPlatform(t)
	f.provider.uploadImageErr = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorRejected,
		Detail: "Saved content does not match given SHA256 value.",
	}

	id := createdImageUploadID(t, f, nil)
	resp := sendImageUploadContent(t, f, id, "application/octet-stream", imageUploadContent)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT content status = %d, want 400", resp.StatusCode)
	}
	if msg, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "SHA256") {
		t.Errorf("error message = %q, want the provider's wording", msg)
	}
	if n := f.imageUploads.count(); n != 0 {
		t.Errorf("remaining uploads = %d, want 0 after a failed content request", n)
	}
}

// An image is identified by integration, ID, and architecture together — the same identity
// the catalog returns — so the delete request carries all three as query parameters.
func TestDeleteImage_Success(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "DELETE",
		"/api/v1/provisioning/images?integrationId="+testIntegrationID+"&imageId=ubuntu-24.04-rocm&architecture=amd64",
		nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if got := f.provider.deletedImages; len(got) != 1 || got[0] != [2]string{"ubuntu-24.04-rocm", "amd64"} {
		t.Fatalf("provider deletions: got %+v, want one (ubuntu-24.04-rocm, amd64)", got)
	}
}

// The image ID and architecture are required: without them the request cannot name an image,
// so it is refused before reaching the provider.
func TestDeleteImage_RequiresImageIdentity(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "DELETE",
		"/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(f.provider.deletedImages) != 0 {
		t.Fatalf("provider must not be asked to delete on an invalid request: %+v", f.provider.deletedImages)
	}
}

// swallow's metadata is merged into the provider catalog at read: after an edit the catalog shows
// the metadata as the spec and the swallow values (name, OS, release) as effective while keeping
// the provider values, and clearing it reverts to the provider values. The provider is never asked
// to change.
func TestImageMetadata_SetMergeAndClear(t *testing.T) {
	f := setupPlatform(t)
	metadataPath := "/api/v1/provisioning/images/metadata?integrationId=" + testIntegrationID + "&imageId=ubuntu/jammy&architecture=amd64"

	resp := doRequest(t, f.app, "PUT", metadataPath,
		map[string]any{"name": "  Golden Ubuntu  ", "osSystem": "Ubuntu LTS", "release": "22.04", "tags": []string{" gpu ", "gpu", "ml"}}, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set metadata: expected 204, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	items := parseArrayBody(t, resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 image, got %d", len(items))
	}
	image := items[0]
	spec, status := image["spec"].(map[string]any), serverStatusOf(image)
	if status["name"] != "Golden Ubuntu" || spec["name"] != "Golden Ubuntu" || status["providerName"] != "Ubuntu 22.04 LTS" {
		t.Fatalf("name merge = %v, want effective and spec \"Golden Ubuntu\" over provider \"Ubuntu 22.04 LTS\"", image)
	}
	if status["osSystem"] != "Ubuntu LTS" || spec["osSystem"] != "Ubuntu LTS" || status["providerOsSystem"] != "ubuntu" {
		t.Fatalf("os merge = %v, want effective and spec \"Ubuntu LTS\" over provider \"ubuntu\"", image)
	}
	if status["release"] != "22.04" || spec["release"] != "22.04" || status["providerRelease"] != "jammy" {
		t.Fatalf("release merge = %v, want effective and spec \"22.04\" over provider \"jammy\"", image)
	}
	tags, ok := spec["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "gpu" || tags[1] != "ml" {
		t.Fatalf("tags = %v, want normalized [gpu ml]", spec["tags"])
	}

	resp = doRequest(t, f.app, "DELETE", metadataPath, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear metadata: expected 204, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	items = parseArrayBody(t, resp)
	image = items[0]
	spec, status = image["spec"].(map[string]any), serverStatusOf(image)
	if status["name"] != "Ubuntu 22.04 LTS" || spec["name"] != "" {
		t.Fatalf("after clear name = %v, want provider label and no metadata name", image)
	}
	if status["osSystem"] != "ubuntu" || spec["osSystem"] != "" || status["release"] != "jammy" || spec["release"] != "" {
		t.Fatalf("after clear os/release = %v, want provider values and empty metadata", image)
	}
	if tags, ok := spec["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("after clear tags = %v, want empty array", spec["tags"])
	}
}

// An over-long override is a validation error, so a runaway label cannot be stored.
func TestImageMetadata_RejectsOverLongField(t *testing.T) {
	f := setupPlatform(t)
	resp := doRequest(t, f.app, "PUT",
		"/api/v1/provisioning/images/metadata?integrationId="+testIntegrationID+"&imageId=ubuntu/jammy&architecture=amd64",
		map[string]any{"name": strings.Repeat("x", 201)}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("over-long name: expected 400, got %d", resp.StatusCode)
	}
}

// The image identity is required for the metadata just as it is for delete: without image ID and
// architecture the request cannot name an image.
func TestImageMetadata_RequiresImageIdentity(t *testing.T) {
	f := setupPlatform(t)
	resp := doRequest(t, f.app, "PUT",
		"/api/v1/provisioning/images/metadata?integrationId="+testIntegrationID,
		map[string]any{"name": "Golden Ubuntu"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing identity: expected 400, got %d", resp.StatusCode)
	}
}

// The reconcile endpoint returns the report rather than a bare acknowledgement,
// because conflicts need a user and would otherwise go unnoticed.
func TestReconcileEndpoint_ReturnsReportWithConflicts(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	f.servers.servers["srv-a"] = &serverdomain.Server{
		ID:       "srv-a",
		Source:   serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "a"},
		Hardware: serverdomain.Hardware{SystemUUID: "uuid-dup"},
	}
	f.servers.servers["srv-b"] = &serverdomain.Server{
		ID:       "srv-b",
		Source:   serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "b"},
		Hardware: serverdomain.Hardware{SystemUUID: "uuid-dup"},
	}

	machine := testMachine("c", "gpu-node-03")
	machine.SystemUUID = "uuid-dup"
	f.provider.withMachine(machine)

	resp := doRequest(t, f.app,
		"POST", "/api/v1/provisioning/integrations/"+testIntegrationID+"/reconcile", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	conflicts, ok := parseBody(t, resp)["conflicts"].([]any)
	if !ok || len(conflicts) != 1 {
		t.Fatalf("expected the report to surface 1 conflict, got %v", parseBody(t, resp)["conflicts"])
	}
	conflict := conflicts[0].(map[string]any)
	if conflict["reason"] == "" {
		t.Error("a conflict must explain itself: an operator has to resolve it")
	}
}

func TestReconcileEndpoint_RejectsNonProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedNonProvisionerIntegration(t, f)

	resp := doRequest(t, f.app,
		"POST", "/api/v1/provisioning/integrations/"+testNonProvisionerIntegrationID+"/reconcile", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// A targeted refresh reads the provider directly, so an accepted asynchronous action
// can converge without waiting for the fleet-wide inventory interval.
func TestRefreshServer_AdvancesLifecycleAndVolatileObservations(t *testing.T) {
	f := setupPlatform(t)
	server := f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(server *serverdomain.Server) {
		server.Provisioning.State = "releasing"
		server.Provisioning.ProviderState = "Releasing"
		server.Provisioning.Ephemeral = true
	})
	machine := testMachine("machine-srv-1", "provider-renamed-node")
	machine.Status = provisioningdomain.MachineStatusReady
	machine.ProviderStatus = "Ready"
	machine.Ephemeral = true
	machine.IPAddresses = nil
	f.provider.withMachine(machine)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["state"] != "ready" {
		t.Fatalf("refresh state: got %v, want ready", body["state"])
	}
	if server.Provisioning.State != "ready" || server.Provisioning.ProviderState != "Ready" {
		t.Fatalf("projection was not advanced: %+v", server.Provisioning)
	}
	if server.Provisioning.Ephemeral {
		t.Fatal("a Ready Server must not retain a stale ephemeral deployment qualifier")
	}
	if len(server.Observed.Addresses) != 0 {
		t.Fatalf("targeted refresh addresses: got %v, want none", server.Observed.Addresses)
	}
	if server.Observed.Hostname != "gpu-node-01" {
		t.Fatalf("targeted refresh must not rewrite inventory identity, got %q", server.Observed.Hostname)
	}
}

func TestRefreshServer_UnknownServer(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/missing/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
