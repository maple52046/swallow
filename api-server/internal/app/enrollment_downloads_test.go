package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The enrollment script is public and points its CLI download at the address it was fetched from,
// including the port and the scheme a TLS proxy forwards.
func TestServeEnrollmentScript(t *testing.T) {
	app := fiber.New()
	app.Get("/downloads/swallow-enroll.sh", serveEnrollmentScript())
	req := httptest.NewRequest(http.MethodGet, "http://10.0.0.5:8080/downloads/swallow-enroll.sh", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "swallow_url='https://10.0.0.5:8080'") {
		t.Errorf("GET = %d, body %q; want the script with the request's address", resp.StatusCode, body)
	}
}

// The CLI is served from api.cliBinary; an unset path or a missing file is a 404 with a reason the
// enrollment script shows.
func TestServeCLIBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "swallow")
	if err := os.WriteFile(binary, []byte("\x7fELF-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		status     int
		body       string
	}{
		{"served", binary, http.StatusOK, "\x7fELF-cli"},
		{"not configured", "", http.StatusNotFound, "api.cliBinary is not set"},
		{"missing file", filepath.Join(t.TempDir(), "absent"), http.StatusNotFound, "missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/downloads/swallow", serveCLIBinary(tc.path))
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/downloads/swallow", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.status || !strings.Contains(string(body), tc.body) {
				t.Errorf("GET = %d %q, want %d containing %q", resp.StatusCode, body, tc.status, tc.body)
			}
		})
	}
}
