package kubeapi

import (
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// SettingControllerLeaseDiscovery makes the reader also report dedicated k0s controllers from
// their kube-node-lease leases. Off by default because the lease naming is a k0s implementation
// detail; a Platform swallow itself deploys turns it on. It is stored on Integrations, so renaming
// it silently drops the setting from every existing one.
const SettingControllerLeaseDiscovery = "controllerLeaseDiscovery"

// CredentialCompletion is the completion type of the deploy Task whose result is the cluster's read
// credential. It is persisted on Tasks, so it stays stable.
const CredentialCompletion = "kubernetes-credential"

// CredentialReader is the Kubernetes module's platform-credential hook: the k0s credential role's
// result is the API endpoint, the reader token, and the cluster CA.
type CredentialReader struct{}

// PlatformType is kubernetes.
func (CredentialReader) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeKubernetes
}

// Completion is CredentialCompletion.
func (CredentialReader) Completion() string { return CredentialCompletion }

// Required is true: without a credential swallow cannot read the cluster it deployed.
func (CredentialReader) Required() bool { return true }

// Credential reads apiEndpoint, token, and caCertificate. Controller lease discovery is on because
// any supported topology may use dedicated control-plane Servers that do not register as nodes. The
// CA is recorded so the API certificate is verified against it (DF-01); only a result without one
// (an earlier role) is recorded with insecureSkipVerify.
func (CredentialReader) Credential(data map[string]any) (platformdomain.Credential, bool) {
	endpoint, token := stringField(data, "apiEndpoint"), stringField(data, "token")
	if endpoint == "" || token == "" {
		return platformdomain.Credential{}, false
	}
	settings := map[string]string{SettingControllerLeaseDiscovery: "true"}
	if ca := strings.TrimSpace(stringField(data, "caCertificate")); ca != "" {
		settings[platformdomain.SettingCACertificate] = ca + "\n"
	} else {
		settings[platformdomain.SettingInsecureSkipVerify] = "true"
	}
	return platformdomain.Credential{Endpoint: endpoint, Token: token, Settings: settings}, true
}

func stringField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}
