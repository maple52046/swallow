package slurmapi

import (
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The slurmrestd credential pins the reported API version and skips TLS verification; a deployment
// without slurmrestd returns none, which does not fail it.
func TestCredentialPinsTheAPIVersion(t *testing.T) {
	reader := CredentialReader{}
	credential, ok := reader.Credential(map[string]any{"slurmrestdEndpoint": "http://10.0.0.1:6820", "token": "jwt", "apiVersion": "v0.0.42"})
	if !ok || credential.Endpoint != "http://10.0.0.1:6820" || credential.Settings[SettingAPIVersion] != "v0.0.42" ||
		credential.Settings[platformdomain.SettingInsecureSkipVerify] != "true" {
		t.Errorf("credential = %+v, %v", credential, ok)
	}
	if _, ok := reader.Credential(map[string]any{}); ok || reader.Required() {
		t.Error("no slurmrestd is no credential, and the deployment does not need one")
	}
}
