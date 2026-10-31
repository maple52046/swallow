package maas

import (
	"context"
	"errors"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// The SSH key registrar wraps MAAS account SSH keys. The tests pin the exact endpoints and form
// field, because a wrong path would register the key nowhere and a wrong field would be refused.

func TestSSHKeyRegistrationCapability_Advertised(t *testing.T) {
	provider := &Provider{}
	if !provider.Capabilities().SSHKeyRegistration {
		t.Errorf("MAAS should advertise the SSHKeyRegistration capability")
	}
	var _ provisioningdomain.SSHKeyRegistrar = provider
}

func TestListSSHKeys_MapsIDAndKey(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/account/prefs/sshkeys/{$}", http.StatusOK,
		`[{"id":7,"key":"ssh-ed25519 AAAA one","keysource":null},{"id":12,"key":"ssh-rsa BBBB two"}]`)
	provider := newTestProvider(t, fake)

	keys, err := provider.ListSSHKeys(context.Background())
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	want := []provisioningdomain.ProviderSSHKey{
		{ID: "7", PublicKey: "ssh-ed25519 AAAA one"},
		{ID: "12", PublicKey: "ssh-rsa BBBB two"},
	}
	if len(keys) != len(want) {
		t.Fatalf("ListSSHKeys() = %+v, want %+v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("ListSSHKeys()[%d] = %+v, want %+v", i, keys[i], want[i])
		}
	}
}

func TestAddSSHKey_PostsKeyField(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("POST "+apiPrefix+"/account/prefs/sshkeys/{$}", http.StatusCreated,
		`{"id":31,"key":"ssh-ed25519 AAAA swallow-deployment"}`)
	provider := newTestProvider(t, fake)

	key, err := provider.AddSSHKey(context.Background(), "ssh-ed25519 AAAA swallow-deployment")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if key.ID != "31" {
		t.Errorf("AddSSHKey() ID = %q, want 31", key.ID)
	}
	if got := fake.lastForm["key"]; got != "ssh-ed25519 AAAA swallow-deployment" {
		t.Errorf("AddSSHKey form key = %q, want the authorized_keys line", got)
	}
	if fake.lastOperation != "" {
		t.Errorf("AddSSHKey must be a collection create, not op=%q", fake.lastOperation)
	}
}

func TestAddSSHKey_RefusalIsRejected(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("POST "+apiPrefix+"/account/prefs/sshkeys/{$}", http.StatusBadRequest,
		`{"key": ["This key has already been added for this user."]}`)
	provider := newTestProvider(t, fake)

	_, err := provider.AddSSHKey(context.Background(), "ssh-ed25519 AAAA")
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("AddSSHKey() error = %v, want ProviderErrorRejected", err)
	}
}

func TestRemoveSSHKey_DeletesAndToleratesMissing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{name: "deleted", status: http.StatusNoContent},
		{name: "already gone", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			var deleted string
			fake.mux.HandleFunc("DELETE "+apiPrefix+"/account/prefs/sshkeys/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
				deleted = r.PathValue("id")
				w.WriteHeader(tc.status)
			})
			provider := newTestProvider(t, fake)

			if err := provider.RemoveSSHKey(context.Background(), "31"); err != nil {
				t.Fatalf("RemoveSSHKey() error = %v, want nil", err)
			}
			if deleted != "31" {
				t.Errorf("RemoveSSHKey deleted id %q, want 31", deleted)
			}
		})
	}
}
