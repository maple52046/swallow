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

// The reader token and the operator token of the current role become the Credential's two tokens;
// a result of an earlier role, which minted only the one token, is still a credential, with no
// operator token, and an operator token alone is no credential.
func TestCredentialReadsTheOperatorTokenApartFromTheReaderToken(t *testing.T) {
	reader := CredentialReader{}
	both, ok := reader.Credential(map[string]any{
		"apiEndpoint": "https://10.0.0.1:6443", "token": "reader", "operatorToken": " operator\n",
	})
	if !ok || both.Token != "reader" || both.OperatorToken != "operator" {
		t.Errorf("current role: Token %q, OperatorToken %q, ok %v; want reader and operator", both.Token, both.OperatorToken, ok)
	}
	earlier, ok := reader.Credential(map[string]any{"apiEndpoint": "https://10.0.0.1:6443", "token": "only"})
	if !ok || earlier.Token != "only" || earlier.OperatorToken != "" {
		t.Errorf("earlier role: Token %q, OperatorToken %q, ok %v; want only and none", earlier.Token, earlier.OperatorToken, ok)
	}
	if _, ok := reader.Credential(map[string]any{"apiEndpoint": "https://10.0.0.1:6443", "operatorToken": "operator"}); ok {
		t.Error("a result with only an operator token must not be a credential: background reads need the reader token")
	}
}
