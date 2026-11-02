package kubeapi

import (
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// A result with the cluster CA is recorded with that CA and without insecureSkipVerify, so the API
// certificate is verified; one without (an earlier role) keeps the unverified Integration;
// one without a token is no credential.
func TestCredentialRecordsTheClusterCA(t *testing.T) {
	const ca = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	reader := CredentialReader{}
	withCA, ok := reader.Credential(map[string]any{"apiEndpoint": "https://10.0.0.1:6443", "token": "t", "caCertificate": ca})
	if !ok || withCA.Settings[platformdomain.SettingCACertificate] != ca+"\n" || withCA.Settings[platformdomain.SettingInsecureSkipVerify] != "" ||
		withCA.Settings[SettingControllerLeaseDiscovery] != "true" {
		t.Errorf("with a CA: %+v, %v", withCA, ok)
	}
	withoutCA, ok := reader.Credential(map[string]any{"apiEndpoint": "https://10.0.0.1:6443", "token": "t"})
	if !ok || withoutCA.Settings[platformdomain.SettingInsecureSkipVerify] != "true" || withoutCA.Settings[platformdomain.SettingCACertificate] != "" {
		t.Errorf("without a CA: %+v, %v", withoutCA, ok)
	}
	if _, ok := reader.Credential(map[string]any{"apiEndpoint": "https://10.0.0.1:6443"}); ok || !reader.Required() {
		t.Error("a result without a token is no credential, and the cluster needs one")
	}
}
