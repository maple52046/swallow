package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

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
