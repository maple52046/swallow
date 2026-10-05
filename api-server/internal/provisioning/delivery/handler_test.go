package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

func TestProvisionerDetailDisablesCaching(t *testing.T) {
	app := fiber.New()
	handler := &ProvisioningHandler{}
	app.Get("/provisioner-detail", handler.ProvisionerDetail)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/provisioner-detail", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Fatalf("Cache-Control: got %q, want %q", got, "no-store")
	}
}

func TestRespondErrorLogsCorrelatedClientSafeProviderDetail(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	app := fiber.New()
	app.Use(requestid.New())
	app.Post("/servers/:id/release", func(c *fiber.Ctx) error {
		return RespondError(c, &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS refused the release.",
			Err:    errors.New("sensitive upstream diagnostic"),
		})
	})

	resp, err := app.Test(httptest.NewRequest(
		http.MethodPost,
		"/servers/server-1/release",
		nil,
	))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var response apierror.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var logEntry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &logEntry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if logEntry["requestId"] != response.Error.RequestID {
		t.Errorf("log requestId: got %v, want %q", logEntry["requestId"], response.Error.RequestID)
	}
	if logEntry["resourceId"] != "server-1" {
		t.Errorf("resourceId: got %v", logEntry["resourceId"])
	}
	if logEntry["providerErrorKind"] != string(provisioningdomain.ProviderErrorRejected) {
		t.Errorf("providerErrorKind: got %v", logEntry["providerErrorKind"])
	}
	if logEntry["detail"] != "MAAS refused the release." {
		t.Errorf("detail: got %v", logEntry["detail"])
	}
	if bytes.Contains(output.Bytes(), []byte("sensitive upstream diagnostic")) {
		t.Fatal("structured log exposed the wrapped upstream error")
	}
}

// TestRespondErrorTranslatesDelegatedOperationErrors pins the durable deploy/release
// contract: failures surfaced by the delegated operation WorkflowService must reach
// the client with their own actionable classification and message, never the opaque
// Internal error. fallback that hid "the Server is busy" from operators.
func TestRespondErrorTranslatesDelegatedOperationErrors(t *testing.T) {
	cases := []struct {
		name                string
		err                 error
		wantStatus          int
		wantCode            apierror.Code
		wantMessageContains string
	}{
		{
			name:                "active durable work is a busy conflict",
			err:                 fmt.Errorf("%w: Server server-1 already has active durable work", operationdomain.ErrTargetsBusy),
			wantStatus:          http.StatusConflict,
			wantCode:            apierror.CodeConflict,
			wantMessageContains: "already has active durable work",
		},
		{
			name:                "invalid operation intent is a validation error",
			err:                 fmt.Errorf("%w: at least one Step is required", operationapp.ErrInvalidOperation),
			wantStatus:          http.StatusBadRequest,
			wantCode:            apierror.CodeValidation,
			wantMessageContains: "at least one Step is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(requestid.New())
			app.Post("/durable", func(c *fiber.Ctx) error { return RespondError(c, tc.err) })

			resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/durable", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			var response apierror.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Error.Code != tc.wantCode {
				t.Fatalf("code: got %q, want %q", response.Error.Code, tc.wantCode)
			}
			if response.Error.Message == "Internal error." {
				t.Fatal("delegated operation error leaked as the opaque Internal error. fallback")
			}
			if !strings.Contains(response.Error.Message, tc.wantMessageContains) {
				t.Fatalf("message %q does not contain %q", response.Error.Message, tc.wantMessageContains)
			}
		})
	}
}

// Boot ISO failures reach the operator with the contract's status and, where it helps fix the
// cause (builder unavailable, in use, build output), the service's own message.
func TestRespondErrorMapsBootISOErrors(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   apierror.Code
		wantText   string
	}{
		{provisioningdomain.ErrBootISONotFound, http.StatusNotFound, apierror.CodeNotFound, "Boot ISO not found"},
		{provisioningdomain.ErrBootISONameTaken, http.StatusConflict, apierror.CodeConflict, "already exists"},
		{fmt.Errorf("%w: 2 Server(s) use it", provisioningdomain.ErrBootISOInUse), http.StatusConflict, apierror.CodeConflict, "2 Server(s)"},
		{&provisioningdomain.BootISOBuilderUnavailableError{Reason: "xorriso is not installed"}, http.StatusConflict, apierror.CodeConflict, "xorriso is not installed"},
		{fmt.Errorf("%w: name is required", provisioningdomain.ErrInvalidBootISO), http.StatusBadRequest, apierror.CodeValidation, "name is required"},
		{fmt.Errorf("%w: genfsimg: no space", provisioningdomain.ErrBootISOBuildFailed), http.StatusInternalServerError, apierror.CodeInternal, "no space"},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			app := fiber.New()
			app.Get("/boot-isos", func(c *fiber.Ctx) error { return RespondError(c, tc.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/boot-isos", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			var response apierror.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.StatusCode != tc.wantStatus || response.Error.Code != tc.wantCode || !strings.Contains(response.Error.Message, tc.wantText) {
				t.Errorf("response = %d %s %q, want %d %s containing %q", resp.StatusCode, response.Error.Code, response.Error.Message, tc.wantStatus, tc.wantCode, tc.wantText)
			}
		})
	}
}

func TestNetworkTargetResponseIncludesDeploymentSuggestion(t *testing.T) {
	response := toNetworkTargetResponse(application.NetworkTarget{
		ServerID: "server-1",
		Suggestion: application.NetworkSuggestion{
			Mode:           provisioningdomain.DeploymentNetworkStatic,
			InterfaceID:    "interface-1",
			SubnetID:       "subnet-1",
			IPAddress:      "192.0.2.42",
			DefaultGateway: true,
		},
	})

	if response.Suggestion.Mode != "static" ||
		response.Suggestion.InterfaceID != "interface-1" ||
		response.Suggestion.SubnetID != "subnet-1" ||
		response.Suggestion.IPAddress != "192.0.2.42" ||
		!response.Suggestion.DefaultGateway {
		t.Fatalf("unexpected network suggestion response: %#v", response.Suggestion)
	}
}

func TestDeploymentNetworkInputPreservesStaticIntent(t *testing.T) {
	input, ok := deployServersInput(deployServersRequest{
		Network: &deploymentNetworkRequest{
			Mode: "static", SubnetID: "subnet-1", DefaultGateway: true,
			Assignments: []deploymentNetworkAssignmentRequest{{
				ServerID: "server-1", InterfaceID: "interface-1",
				SubnetID: "subnet-1", IPAddress: "192.0.2.42",
			}},
		},
	})
	if !ok {
		t.Fatal("deployServersInput rejected a request with no deploy target")
	}

	network := input.Network
	if network == nil {
		t.Fatal("network input was dropped at the HTTP boundary")
	}
	if network.Mode != "static" || network.SubnetID != "subnet-1" || !network.DefaultGateway {
		t.Fatalf("network input = %#v", network)
	}
	if len(network.Assignments) != 1 {
		t.Fatalf("assignments = %d, want 1", len(network.Assignments))
	}
	assignment := network.Assignments[0]
	if assignment.ServerID != "server-1" ||
		assignment.InterfaceID != "interface-1" ||
		assignment.SubnetID != "subnet-1" ||
		assignment.IPAddress != "192.0.2.42" {
		t.Fatalf("assignment = %#v", assignment)
	}
}
