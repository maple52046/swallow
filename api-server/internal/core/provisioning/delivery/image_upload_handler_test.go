package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// uploadHandlerProvider is an uploader that hashes what it reads, so a test can prove the handler
// delivered exactly the body the client sent through the request stream.
type uploadHandlerProvider struct {
	provisioningdomain.OSProvisioningProvider
	mu       sync.Mutex
	received int64
	digest   string
}

func (p *uploadHandlerProvider) ValidateOSImageUpload(provisioningdomain.UploadOSImageRequest) error {
	return nil
}

func (p *uploadHandlerProvider) UploadOSImage(_ context.Context, req provisioningdomain.UploadOSImageRequest) (*provisioningdomain.OSImage, error) {
	sum := sha256.New()
	n, err := io.Copy(sum, req.Content)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.received, p.digest = n, hex.EncodeToString(sum.Sum(nil))
	p.mu.Unlock()
	return &provisioningdomain.OSImage{ID: req.Name, Name: req.Name, OSSystem: "custom", Release: req.Name,
		Architecture: req.Architecture, SizeBytes: req.Size, Complete: true}, nil
}

type uploadHandlerProviders struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f uploadHandlerProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

// uploadHandlerRecords is the minimal in-memory upload store the handler test needs.
type uploadHandlerRecords struct {
	mu      sync.Mutex
	uploads map[string]*provisioningdomain.OSImageUpload
}

func (r *uploadHandlerRecords) Create(_ context.Context, upload *provisioningdomain.OSImageUpload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *upload
	r.uploads[upload.ID] = &stored
	return nil
}

func (r *uploadHandlerRecords) Claim(_ context.Context, id string, _ time.Time) (*provisioningdomain.OSImageUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[id]
	if !ok {
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	}
	if upload.State == provisioningdomain.OSImageUploadSending {
		return nil, provisioningdomain.ErrOSImageUploadInProgress
	}
	upload.State = provisioningdomain.OSImageUploadSending
	claimed := *upload
	return &claimed, nil
}

func (r *uploadHandlerRecords) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.uploads, id)
	return nil
}

// uploadHandlerBodyLimit is deliberately far below the test content, so the content can only
// arrive through the request stream, as it does for a multi-gigabyte image in production.
const uploadHandlerBodyLimit = 4 << 10

// newUploadHandlerApp mounts the upload handlers at their production paths on an app configured
// like the production one: streamed request bodies and no multipart pre-parse.
func newUploadHandlerApp(t *testing.T, provider *uploadHandlerProvider) *fiber.App {
	t.Helper()
	handler := &ProvisioningHandler{imageUploads: application.NewOSImageUploadService(
		uploadHandlerProviders{provider: provider},
		&uploadHandlerRecords{uploads: map[string]*provisioningdomain.OSImageUpload{}},
		nil, // no test sends a default user, so the metadata store is never written
		1<<30,
	)}
	app := fiber.New(fiber.Config{
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		BodyLimit:                    uploadHandlerBodyLimit,
	})
	app.Use(apicontract.ResponseValidator(t))
	app.Post("/api/v1/provisioning/image-uploads", handler.CreateImageUpload)
	app.Put("/api/v1/provisioning/image-uploads/:id/content", handler.SendImageUploadContent)
	return app
}

// createHandlerUpload records an intent for content and returns the upload id.
func createHandlerUpload(t *testing.T, app *fiber.App, content []byte) string {
	t.Helper()
	sum := sha256.Sum256(content)
	body, _ := json.Marshal(map[string]any{
		"integrationId": "maas-a", "name": "rocky-10", "architecture": "amd64",
		"sizeBytes": len(content), "sha256": hex.EncodeToString(sum[:]),
	})
	req := httptest.NewRequest("POST", "/api/v1/provisioning/image-uploads", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST /image-uploads: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /image-uploads status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("decode created upload: %v (id %q)", err, created.ID)
	}
	return created.ID
}

// Content far larger than the body limit streams through the handler to the provider unchanged,
// and the response is the created image as the contract describes it.
func TestSendImageUploadContentStreamsBody(t *testing.T) {
	provider := &uploadHandlerProvider{}
	app := newUploadHandlerApp(t, provider)
	content := bytes.Repeat([]byte("0123456789abcdef"), 64<<10) // 1 MiB
	id := createHandlerUpload(t, app, content)

	req := httptest.NewRequest("PUT", "/api/v1/provisioning/image-uploads/"+id+"/content", bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("PUT content: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT content status = %d (%s), want 201", resp.StatusCode, raw)
	}
	sum := sha256.Sum256(content)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.received != int64(len(content)) || provider.digest != hex.EncodeToString(sum[:]) {
		t.Errorf("provider read %d bytes with digest %s, want %d bytes with the content's digest", provider.received, provider.digest, len(content))
	}
}

// A chunked body has no declared length to check against the upload, so it is refused before the
// upload is claimed; the same upload then accepts a request with a Content-Length.
func TestSendImageUploadContentRefusesChunkedBody(t *testing.T) {
	provider := &uploadHandlerProvider{}
	app := newUploadHandlerApp(t, provider)
	content := []byte("data")
	id := createHandlerUpload(t, app, content)

	req := httptest.NewRequest("PUT", "/api/v1/provisioning/image-uploads/"+id+"/content", io.NopCloser(strings.NewReader("data")))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("PUT chunked content: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("chunked PUT status = %d, want 400", resp.StatusCode)
	}

	req = httptest.NewRequest("PUT", "/api/v1/provisioning/image-uploads/"+id+"/content", bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err = app.Test(req, -1)
	if err != nil {
		t.Fatalf("PUT content: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("PUT after a refused chunked request status = %d, want 201 (the upload stays usable)", resp.StatusCode)
	}
}
