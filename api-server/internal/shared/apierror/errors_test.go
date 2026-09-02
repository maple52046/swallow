package apierror_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"github.com/maple52046/swallow/internal/shared/apierror"
)

func TestRespondIncludesRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(requestid.New())
	app.Get("/", func(c *fiber.Ctx) error {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request."))
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var payload apierror.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	headerRequestID := resp.Header.Get(fiber.HeaderXRequestID)
	if headerRequestID == "" {
		t.Fatal("X-Request-ID header is empty")
	}
	if payload.Error.RequestID != headerRequestID {
		t.Errorf("requestId: got %q, want header value %q", payload.Error.RequestID, headerRequestID)
	}
	if payload.Error.Code != apierror.CodeValidation {
		t.Errorf("code: got %q, want %q", payload.Error.Code, apierror.CodeValidation)
	}
	if payload.Error.Message != "Invalid request." {
		t.Errorf("message: got %q", payload.Error.Message)
	}
}
