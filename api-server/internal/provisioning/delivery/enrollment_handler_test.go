package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// stubInspections answers LaunchInspection with a fixed result or error and records the request.
type stubInspections struct {
	request  application.InspectionRequest
	accepted *application.InspectionAccepted
	err      error
}

func (s *stubInspections) LaunchInspection(_ context.Context, request application.InspectionRequest) (*application.InspectionAccepted, error) {
	s.request = request
	return s.accepted, s.err
}

func (s *stubInspections) HasInspection(context.Context, string) (bool, error) { return false, nil }

// Inspect answers 202 with the snapshot plus the Workflow, as a requested inspection, and maps a
// refused state to 409 (server-detail-actions.md).
func TestInspectStartsHardwareInspectionWorkflow(t *testing.T) {
	inspections := &stubInspections{accepted: &application.InspectionAccepted{
		ProvisioningStateItem: application.ProvisioningStateItem{ServerID: "server-1", State: "new"},
		WorkflowID:            "workflow-1",
	}}
	handler := &ProvisioningHandler{}
	handler.AttachHardwareInspection(inspections)
	app := fiber.New()
	app.Post("/servers/:id/inspect", handler.Inspect)

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/servers/server-1/inspect", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted || body["workflowId"] != "workflow-1" || body["state"] != "new" || body["resumed"] != false {
		t.Errorf("status %d body %v, want 202 with the snapshot and workflowId", resp.StatusCode, body)
	}
	if inspections.request.ServerID != "server-1" || inspections.request.Origin != provisioningdomain.InspectionOriginRequested {
		t.Errorf("request = %+v, want a requested inspection of server-1", inspections.request)
	}

	inspections.err = fmt.Errorf("%w: Server gpu-07 is deployed", provisioningdomain.ErrInspectionNotAllowed)
	resp, err = app.Test(httptest.NewRequest(http.MethodPost, "/servers/server-1/inspect", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var refused apierror.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&refused); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusConflict || refused.Error.Code != apierror.CodeConflict {
		t.Errorf("refused inspect = %d %+v, want 409 conflict", resp.StatusCode, refused.Error)
	}
}

// enrollmentTestProvider has existing-host enrollment.
type enrollmentTestProvider struct {
	provisioningdomain.OSProvisioningProvider
}

func (enrollmentTestProvider) Name() string { return "maas" }

func (enrollmentTestProvider) ExistingHostEnrollment(context.Context) (*provisioningdomain.ExistingHostEnrollment, error) {
	return &provisioningdomain.ExistingHostEnrollment{Endpoint: "http://maas:5240/MAAS", Token: "a:b:c"}, nil
}

type enrollmentTestProviders struct{}

func (enrollmentTestProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return enrollmentTestProvider{}, nil
}

// The bundle carries the provisioner credential, so it must never be cached. Its command fetches
// the script from the swallowUrl the caller sent, or from the address the request came in on, and
// an unusable swallowUrl is a validation error (server-enrollment.md).
func TestEnrollBundle(t *testing.T) {
	handler := &ProvisioningHandler{}
	handler.AttachHostEnrollment(application.NewHostEnrollmentUseCase(enrollmentTestProviders{}))
	app := fiber.New()
	app.Post("/provisioning/integrations/:id/enroll-bundle", handler.EnrollBundle)
	post := func(body string) (*http.Response, error) {
		req := httptest.NewRequest(http.MethodPost, "http://swallow.lab:8080/provisioning/integrations/maas-a/enroll-bundle", strings.NewReader(body))
		if body != "" {
			req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		}
		return app.Test(req)
	}

	for body, wantScript := range map[string]string{
		"":                                  "'http://swallow.lab:8080/downloads/swallow-enroll.sh'",
		`{"swallowUrl":"http://10.0.0.5/"}`: "'http://10.0.0.5/downloads/swallow-enroll.sh'",
	} {
		resp, err := post(body)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		var bundle application.HostEnrollmentBundle
		if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp.Body.Close()
		if resp.Header.Get(fiber.HeaderCacheControl) != "no-store" || bundle.Token != "a:b:c" || bundle.IntegrationID != "maas-a" {
			t.Errorf("bundle %+v with Cache-Control %q, want the credential and no-store", bundle, resp.Header.Get(fiber.HeaderCacheControl))
		}
		want := "curl -fsSL " + wantScript + " | sudo sh -s -- --provisioner=maas --endpoint 'http://maas:5240/MAAS' --token 'a:b:c'"
		if bundle.Command != want {
			t.Errorf("body %q: Command = %s, want %s", body, bundle.Command, want)
		}
	}

	resp, err := post(`{"swallowUrl":"http://10.0.0.5/dashboard"}`)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("swallowUrl with a path = %d, want 400", resp.StatusCode)
	}
}
