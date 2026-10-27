package delivery

import (
	"encoding/json"
	"testing"
)

// The uninstall body carries cancelActiveWorkflows next to the release choice (decision 061), so a
// client can uninstall a Platform whose deployment still holds its Servers in one request.
func TestUninstallRequestDecodesCancelActiveWorkflows(t *testing.T) {
	var req uninstallPlatformRequest
	body := `{"releaseServers":true,"releaseOptions":{"erase":true},"cancelActiveWorkflows":true}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if !req.ReleaseServers || !req.ReleaseOptions.Erase || !req.CancelActiveWorkflows {
		t.Errorf("request = %+v, want releaseServers, erase, and cancelActiveWorkflows set", req)
	}

	var empty uninstallPlatformRequest
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil || empty.CancelActiveWorkflows {
		t.Errorf("empty body: request = %+v, err = %v, want cancelActiveWorkflows false", empty, err)
	}
}
