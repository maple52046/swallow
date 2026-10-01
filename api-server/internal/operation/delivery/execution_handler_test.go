package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// recordingAutomationRepo records the credential the handler asks the service to store.
type recordingAutomationRepo struct {
	operationdomain.AutomationConfigurationRepository
	stored *operationdomain.AutomationCredential
}

func (r *recordingAutomationRepo) ReplaceCredential(_ context.Context, _ string, credential operationdomain.AutomationCredential) error {
	r.stored = &credential
	return nil
}

// TestPutAutomationCredential covers the site-automation contract after decision 041: the body
// carries only becomePassword, and any sshPrivateKey value is refused so no caller believes a
// Site key override took effect.
func TestPutAutomationCredential(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantCode     string
		wantStored   bool
		wantPassword string
	}{
		{name: "become password is stored", body: `{"becomePassword":"pw"}`,
			wantStatus: http.StatusNoContent, wantStored: true, wantPassword: "pw"},
		{name: "empty body clears the credential", body: `{}`,
			wantStatus: http.StatusNoContent, wantStored: true},
		{name: "null sshPrivateKey is treated as absent", body: `{"sshPrivateKey":null,"becomePassword":"pw"}`,
			wantStatus: http.StatusNoContent, wantStored: true, wantPassword: "pw"},
		{name: "a private key is refused", body: `{"sshPrivateKey":"-----BEGIN OPENSSH PRIVATE KEY-----","becomePassword":"pw"}`,
			wantStatus: http.StatusBadRequest, wantCode: "validation_error"},
		{name: "an empty private key is refused too", body: `{"sshPrivateKey":""}`,
			wantStatus: http.StatusBadRequest, wantCode: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingAutomationRepo{}
			handler := NewExecutionHandler(nil, application.NewAutomationConfigurationService(repo, nil, nil))
			app := fiber.New()
			app.Put("/sites/:id/automation/credential", handler.PutAutomationCredential)

			req := httptest.NewRequest(http.MethodPut, "/sites/site-1/automation/credential", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("PUT credential %s: %v", tc.body, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("PUT credential %s status = %d, want %d", tc.body, resp.StatusCode, tc.wantStatus)
			}
			if tc.wantCode != "" {
				raw, _ := io.ReadAll(resp.Body)
				var envelope struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(raw, &envelope); err != nil {
					t.Fatalf("decode error envelope %q: %v", raw, err)
				}
				if envelope.Error.Code != tc.wantCode || !strings.Contains(envelope.Error.Message, "Deployment Key") {
					t.Errorf("error = %+v, want code %q naming the Deployment Key", envelope.Error, tc.wantCode)
				}
			}
			if (repo.stored != nil) != tc.wantStored {
				t.Fatalf("stored = %+v, want stored %v", repo.stored, tc.wantStored)
			}
			if repo.stored != nil && (repo.stored.SSHPrivateKey != "" || repo.stored.BecomePassword != tc.wantPassword) {
				t.Errorf("stored credential = %+v, want only become password %q", repo.stored, tc.wantPassword)
			}
		})
	}
}
