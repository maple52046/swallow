package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// `provisioning images upload` declares the file's size and SHA-256 when it creates the upload,
// then streams exactly those bytes to the upload's content route with a Content-Length and the
// octet-stream media type, which is what the contract requires.
func TestImageUploadDeclaresAndStreamsContent(t *testing.T) {
	content := []byte("rootfs-tarball-bytes")
	sum := sha256.Sum256(content)
	path := filepath.Join(t.TempDir(), "rocky-10.tgz")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write image file: %v", err)
	}

	var intent map[string]any
	var gotContent []byte
	var gotLength int64
	var gotType string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/provisioning/image-uploads":
			_ = json.NewDecoder(r.Body).Decode(&intent)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"upload-1","expiresAt":"2026-11-05T08:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/provisioning/image-uploads/upload-1/content":
			gotContent, _ = io.ReadAll(r.Body)
			gotLength, gotType = r.ContentLength, r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"rocky-10","architecture":"amd64"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	err := runCommand(t, provisioningImageUploadCmd(),
		"--integration", "int1", "--name", "rocky-10", "--architecture", "amd64",
		"--filetype", "tgz", "--default-user", "cloud-user", "--content", path)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	want := map[string]any{
		"integrationId": "int1", "name": "rocky-10", "architecture": "amd64", "filetype": "tgz",
		"defaultUser": "cloud-user", "sizeBytes": float64(len(content)), "sha256": hex.EncodeToString(sum[:]),
	}
	for key, value := range want {
		if intent[key] != value {
			t.Errorf("intent[%q] = %v, want %v", key, intent[key], value)
		}
	}
	if _, ok := intent["title"]; ok {
		t.Errorf("intent carries title %v, want it omitted when the flag is not set", intent["title"])
	}
	if string(gotContent) != string(content) || gotLength != int64(len(content)) || gotType != "application/octet-stream" {
		t.Errorf("content = %q (length %d, type %q), want the file with its length as application/octet-stream",
			gotContent, gotLength, gotType)
	}
}

// An empty file is refused before any request, since the contract requires a positive size.
func TestImageUploadRefusesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.tgz")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write image file: %v", err)
	}
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s for an empty file", r.Method, r.URL.Path)
	})

	err := runCommand(t, provisioningImageUploadCmd(),
		"--integration", "int1", "--name", "empty", "--architecture", "amd64", "--content", path)
	if err == nil {
		t.Fatal("upload of an empty file succeeded, want an error")
	}
}
