package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidDefaultUser(t *testing.T) {
	for _, user := range []string{"ubuntu", "cloud-user", "amd", "_svc", "root", "u1", strings.Repeat("a", 32)} {
		if !ValidDefaultUser(user) {
			t.Errorf("ValidDefaultUser(%q) = false, want true", user)
		}
	}
	for _, user := range []string{"", "Ubuntu", "1user", "-user", "a b", "user;id", "user$", strings.Repeat("a", 33)} {
		if ValidDefaultUser(user) {
			t.Errorf("ValidDefaultUser(%q) = true, want false", user)
		}
	}
}

// The value set on the Server wins over the image's, and an unknown user is reported as such so
// automation keeps probing instead of guessing.
func TestEffectiveDefaultUser(t *testing.T) {
	tests := []struct {
		name       string
		server     Server
		wantUser   string
		wantSource DefaultUserSource
	}{
		{
			name:       "set on the server",
			server:     Server{DefaultUser: "amd", Provisioning: &ProvisioningStatus{DeployedImageDefaultUser: "ubuntu"}},
			wantUser:   "amd",
			wantSource: DefaultUserSourceServer,
		},
		{
			name:       "from the os image",
			server:     Server{Provisioning: &ProvisioningStatus{DeployedImageDefaultUser: "ubuntu"}},
			wantUser:   "ubuntu",
			wantSource: DefaultUserSourceOSImage,
		},
		{name: "unknown without an image user", server: Server{Provisioning: &ProvisioningStatus{}}},
		{name: "unknown without a provisioning axis", server: Server{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, source := tc.server.EffectiveDefaultUser()
			if user != tc.wantUser || source != tc.wantSource {
				t.Errorf("EffectiveDefaultUser() = %q, %q; want %q, %q", user, source, tc.wantUser, tc.wantSource)
			}
		})
	}
}

// Operators read these messages; each must name the Server and keep the sentinel for mapping.
func TestDefaultUserErrorKeepsSentinelAndContext(t *testing.T) {
	for _, sentinel := range []error{ErrDefaultUserNotDeployed, ErrHostUnreachable, ErrHostPasswordRejected, ErrDeploymentKeyRejected} {
		err := &DefaultUserError{Err: sentinel, Server: "tainan-ci", User: "amd"}
		if !errors.Is(err, sentinel) {
			t.Errorf("errors.Is(%v, %v) = false, want true", err, sentinel)
		}
		if !strings.Contains(err.Error(), "tainan-ci") {
			t.Errorf("DefaultUserError(%v).Error() = %q, want it to name the Server", sentinel, err.Error())
		}
	}
}
