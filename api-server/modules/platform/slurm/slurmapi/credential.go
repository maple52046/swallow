package slurmapi

import (
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// SettingAPIVersion selects the slurmrestd endpoint version, which tracks the Slurm release. It is
// stored on Integrations, so renaming it silently drops the setting from every existing one.
const SettingAPIVersion = "slurmApiVersion"

// CredentialCompletion is the completion type of the deploy Task whose result is the slurmrestd
// read credential. It is persisted on Tasks, so it stays stable.
const CredentialCompletion = "slurm-credential"

// CredentialReader is the Slurm module's platform-credential hook: the Slurm credential role's
// result is the slurmrestd base URL, a Slurm JWT, and the endpoint version the controller exposes.
type CredentialReader struct{}

// PlatformType is slurm.
func (CredentialReader) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeSlurm
}

// Completion is CredentialCompletion.
func (CredentialReader) Completion() string { return CredentialCompletion }

// Required is false: slurmctld and slurmd work without slurmrestd, which only powers membership
// reads, so its absence is not a deploy failure.
func (CredentialReader) Required() bool { return false }

// Credential reads slurmrestdEndpoint, token, and the optional apiVersion, which pins the endpoint
// version the reader queries. insecureSkipVerify is set because slurmrestd is commonly reached over
// plain HTTP or a self-signed endpoint inside the site, and membership is read-only.
func (CredentialReader) Credential(data map[string]any) (platformdomain.Credential, bool) {
	endpoint, token := stringField(data, "slurmrestdEndpoint"), stringField(data, "token")
	if endpoint == "" || token == "" {
		return platformdomain.Credential{}, false
	}
	settings := map[string]string{platformdomain.SettingInsecureSkipVerify: "true"}
	if version := stringField(data, "apiVersion"); version != "" {
		settings[SettingAPIVersion] = version
	}
	return platformdomain.Credential{Endpoint: endpoint, Token: token, Settings: settings}, true
}

func stringField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return strings.TrimSpace(value)
}
