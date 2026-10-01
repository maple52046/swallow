package domain

import (
	"errors"
	"testing"
)

func TestAutomationConfigurationCheckRunnable(t *testing.T) {
	tests := []struct {
		name          string
		configuration AutomationConfiguration
		want          error
	}{
		{
			name:          "enabled with the deployment key runs without a stored credential",
			configuration: AutomationConfiguration{Enabled: true, CredentialSource: CredentialSourceDeploymentKey},
		},
		{
			name:          "disabled automation is refused before the key is considered",
			configuration: AutomationConfiguration{CredentialSource: CredentialSourceDeploymentKey},
			want:          ErrAutomationDisabled,
		},
		{
			name:          "no deployment key is a missing credential even with a become password",
			configuration: AutomationConfiguration{Enabled: true, HasCredential: true, CredentialSource: CredentialSourceNone},
			want:          ErrAutomationCredentialMissing,
		},
		{
			name:          "an underived source is a missing credential",
			configuration: AutomationConfiguration{Enabled: true},
			want:          ErrAutomationCredentialMissing,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.configuration.CheckRunnable(); !errors.Is(got, tc.want) {
				t.Errorf("CheckRunnable() = %v, want %v", got, tc.want)
			}
		})
	}
}
