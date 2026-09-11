package maas

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

const apiPrefix = "/MAAS/api/2.0"

// readyMachineJSON and deployingMachineJSON are trimmed copies of real MAAS
// machine objects, keeping the fields the adapter reads.
const readyMachineJSON = `{
  "system_id": "abc123",
  "hostname": "gpu-node-01",
  "fqdn": "gpu-node-01.maas",
  "status": 4,
  "status_name": "Ready",
  "architecture": "amd64/generic",
  "cpu_count": 32,
  "memory": 131072,
  "storage": 512000.0,
  "power_state": "off",
  "osystem": "",
  "distro_series": "",
  "ip_addresses": ["10.0.1.10", "10.0.1.11"],
  "tag_names": ["gpu", "a100"],
  "zone": {"name": "dc-east"},
  "pool": {"name": "gpu-pool"}
}`

const deployingMachineJSON = `{
  "system_id": "abc123",
  "hostname": "gpu-node-01",
  "fqdn": "gpu-node-01.maas",
  "status": 9,
  "status_name": "Deploying",
  "power_state": "on",
  "osystem": "ubuntu",
  "distro_series": "jammy"
}`

// fakeMAAS stands in for a MAAS region controller. Each test registers only the
// responses it needs, and the recorded request lets tests assert on what the
// adapter actually sent.
type fakeMAAS struct {
	server *httptest.Server
	mux    *http.ServeMux

	lastAuthorization string
	lastContentType   string
	lastContentLength int64
	lastOperation           string
	lastForm                map[string]string
	lastMethod              string
	lastRawQuery            string
	lastDeletedBootResource string
}

func newFakeMAAS(t *testing.T) *fakeMAAS {
	t.Helper()

	f := &fakeMAAS{mux: http.NewServeMux()}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastMethod = r.Method
		f.lastRawQuery = r.URL.RawQuery
		f.lastAuthorization = r.Header.Get("Authorization")
		f.lastContentType = r.Header.Get("Content-Type")
		f.lastContentLength = r.ContentLength
		f.lastOperation = r.URL.Query().Get("op")

		if r.Method == http.MethodPost {
			f.lastForm = map[string]string{}
			// Only operations with fields use multipart. Parameterless operations
			// intentionally have no body, so a parse failure is expected there.
			if err := r.ParseMultipartForm(1 << 20); err == nil {
				for name, values := range r.MultipartForm.Value {
					if len(values) > 0 {
						f.lastForm[name] = values[0]
					}
				}
			}
		}

		f.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)

	return f
}

func (f *fakeMAAS) respond(pattern string, statusCode int, body string) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	})
}

func (f *fakeMAAS) onListMachines(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/machines/{$}", statusCode, body)
}

func (f *fakeMAAS) onGetMachine(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/machines/{id}/{$}", statusCode, body)
}

func (f *fakeMAAS) onMachineOperation(statusCode int, body string) {
	f.respond("POST "+apiPrefix+"/machines/{id}/{$}", statusCode, body)
}

func (f *fakeMAAS) onDeleteMachine(statusCode int, body string) {
	f.respond("DELETE "+apiPrefix+"/machines/{id}/{$}", statusCode, body)
}

func (f *fakeMAAS) onVersion(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/version/{$}", statusCode, body)
}

func (f *fakeMAAS) onBootResources(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/boot-resources/{$}", statusCode, body)
}

func (f *fakeMAAS) onDeleteBootResource(statusCode int) {
	f.mux.HandleFunc("DELETE "+apiPrefix+"/boot-resources/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		f.lastDeletedBootResource = r.PathValue("id")
		w.WriteHeader(statusCode)
	})
}

func (f *fakeMAAS) onEvents(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/events/{$}", statusCode, body)
}

func newTestProvider(t *testing.T, f *fakeMAAS) *Provider {
	t.Helper()
	client, err := NewClient(f.server.URL, "ck:tk:ts", 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return NewProvider(client)
}

func TestListMachines_MapsMAASFields(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onListMachines(http.StatusOK, "["+readyMachineJSON+"]")
	provider := newTestProvider(t, fake)

	machines, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{})
	if err != nil {
		t.Fatalf("ListMachines: %v", err)
	}
	if len(machines) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(machines))
	}

	m := machines[0]
	if m.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", m.ID, "abc123")
	}
	if m.Hostname != "gpu-node-01" {
		t.Errorf("Hostname: got %q", m.Hostname)
	}
	if m.FQDN != "gpu-node-01.maas" {
		t.Errorf("FQDN: got %q", m.FQDN)
	}
	if m.Status != provisioningdomain.MachineStatusReady {
		t.Errorf("Status: got %q, want %q", m.Status, provisioningdomain.MachineStatusReady)
	}
	if m.ProviderStatus != "Ready" {
		t.Errorf("ProviderStatus: got %q, want %q", m.ProviderStatus, "Ready")
	}
	if m.PowerState != provisioningdomain.PowerStateOff {
		t.Errorf("PowerState: got %q", m.PowerState)
	}
	if m.CPUCores != 32 {
		t.Errorf("CPUCores: got %d, want 32", m.CPUCores)
	}
	if m.MemoryMiB != 131072 {
		t.Errorf("MemoryMiB: got %d, want 131072", m.MemoryMiB)
	}
	if m.StorageGB != 512 {
		t.Errorf("StorageGB: got %v, want 512", m.StorageGB)
	}
	if m.PrimaryIP() != "10.0.1.10" {
		t.Errorf("PrimaryIP: got %q", m.PrimaryIP())
	}
	if m.Zone != "dc-east" {
		t.Errorf("Zone: got %q", m.Zone)
	}
	if m.ResourcePool != "gpu-pool" {
		t.Errorf("ResourcePool: got %q", m.ResourcePool)
	}
	if len(m.Tags) != 2 {
		t.Errorf("Tags: got %v", m.Tags)
	}
}

func TestListMachines_NormalizesStatusCodes(t *testing.T) {
	cases := []struct {
		code int
		want provisioningdomain.MachineStatus
	}{
		{0, provisioningdomain.MachineStatusNew},
		{1, provisioningdomain.MachineStatusCommissioning},
		{2, provisioningdomain.MachineStatusFailed},
		{3, provisioningdomain.MachineStatusBroken},
		{4, provisioningdomain.MachineStatusReady},
		{5, provisioningdomain.MachineStatusAllocated},
		{6, provisioningdomain.MachineStatusDeployed},
		{7, provisioningdomain.MachineStatusRetired},
		{8, provisioningdomain.MachineStatusBroken},
		{9, provisioningdomain.MachineStatusDeploying},
		{10, provisioningdomain.MachineStatusAllocated},
		{11, provisioningdomain.MachineStatusFailed},
		{12, provisioningdomain.MachineStatusReleasing},
		{14, provisioningdomain.MachineStatusReleasing},
		{16, provisioningdomain.MachineStatusRescue},
		{21, provisioningdomain.MachineStatusTesting},
		{22, provisioningdomain.MachineStatusFailed},
		// A status this version has never heard of must not be guessed at.
		{999, provisioningdomain.MachineStatusUnknown},
	}

	for _, tc := range cases {
		got := toDomainMachine(&machineJSON{Status: tc.code}).Status
		if got != tc.want {
			t.Errorf("status %d: got %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestListMachines_AppliesFilters(t *testing.T) {
	body := `[` + readyMachineJSON + `,` + deployingMachineJSON + `]`

	t.Run("by status", func(t *testing.T) {
		fake := newFakeMAAS(t)
		fake.onListMachines(http.StatusOK, body)
		provider := newTestProvider(t, fake)

		machines, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{
			Status: provisioningdomain.MachineStatusDeploying,
		})
		if err != nil {
			t.Fatalf("ListMachines: %v", err)
		}
		if len(machines) != 1 || machines[0].Status != provisioningdomain.MachineStatusDeploying {
			t.Fatalf("expected only the deploying machine, got %d results", len(machines))
		}
	})

	t.Run("by keyword is case insensitive", func(t *testing.T) {
		fake := newFakeMAAS(t)
		fake.onListMachines(http.StatusOK, body)
		provider := newTestProvider(t, fake)

		machines, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{
			Keyword: "GPU-NODE",
		})
		if err != nil {
			t.Fatalf("ListMachines: %v", err)
		}
		if len(machines) != 2 {
			t.Fatalf("expected both machines to match, got %d", len(machines))
		}
	})

	t.Run("keyword with no match", func(t *testing.T) {
		fake := newFakeMAAS(t)
		fake.onListMachines(http.StatusOK, body)
		provider := newTestProvider(t, fake)

		machines, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{
			Keyword: "storage-node",
		})
		if err != nil {
			t.Fatalf("ListMachines: %v", err)
		}
		if len(machines) != 0 {
			t.Fatalf("expected no matches, got %d", len(machines))
		}
	})
}

func TestGetMachine_NotFoundBecomesDomainError(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onGetMachine(http.StatusNotFound, `Not Found`)
	provider := newTestProvider(t, fake)

	_, err := provider.GetMachine(context.Background(), "missing")
	if !errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		t.Fatalf("expected ErrMachineNotFound, got %v", err)
	}
}
func TestValidateDeploymentTarget_RequiresLinkedSubnet(t *testing.T) {
	t.Run("linked subnet", func(t *testing.T) {
		fake := newFakeMAAS(t)
		fake.onGetMachine(http.StatusOK, `{
		  "system_id": "abc123",
		  "interface_set": [{"mac_address": "52:54:00:50:cd:84", "links": [
		    {"mode": "AUTO", "subnet": {"name": "192.168.110.0/24"}}
		  ]}]
		}`)
		provider := newTestProvider(t, fake)

		if err := provider.ValidateDeploymentTarget(context.Background(), "abc123"); err != nil {
			t.Fatalf("ValidateDeploymentTarget: %v", err)
		}
	})

	t.Run("interface without subnet link", func(t *testing.T) {
		fake := newFakeMAAS(t)
		fake.onGetMachine(http.StatusOK, `{
		  "system_id": "abc123",
		  "interface_set": [{"mac_address": "52:54:00:50:cd:84", "links": []}]
		}`)
		provider := newTestProvider(t, fake)

		err := provider.ValidateDeploymentTarget(context.Background(), "abc123")
		var providerErr *provisioningdomain.ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("ValidateDeploymentTarget: got %v, want ProviderError", err)
		}
		if providerErr.Kind != provisioningdomain.ProviderErrorRejected {
			t.Errorf("ValidateDeploymentTarget kind = %q, want %q", providerErr.Kind, provisioningdomain.ProviderErrorRejected)
		}
		if !strings.Contains(providerErr.Detail, "Network in MAAS") {
			t.Errorf("ValidateDeploymentTarget detail = %q, want MAAS Network remediation", providerErr.Detail)
		}
	})
}

func TestDeploy_SendsMultipartFormWithEncodedUserData(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineOperation(http.StatusOK, deployingMachineJSON)
	provider := newTestProvider(t, fake)

	state, err := provider.Deploy(context.Background(), provisioningdomain.DeployRequest{
		MachineID:    "abc123",
		OSSystem:     "ubuntu",
		DistroSeries: "jammy",
		UserData:     "#cloud-config\npackages: [htop]\n",
		Comment:      "provisioned by swallow",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if fake.lastOperation != "deploy" {
		t.Errorf("operation: got %q, want %q", fake.lastOperation, "deploy")
	}
	// MAAS 2.0 rejects JSON bodies, so the encoding itself is part of the contract.
	if !strings.HasPrefix(fake.lastContentType, "multipart/form-data") {
		t.Errorf("content type: got %q, want multipart/form-data", fake.lastContentType)
	}
	if fake.lastForm["osystem"] != "ubuntu" {
		t.Errorf("osystem: got %q", fake.lastForm["osystem"])
	}
	if fake.lastForm["distro_series"] != "jammy" {
		t.Errorf("distro_series: got %q", fake.lastForm["distro_series"])
	}
	if fake.lastForm["comment"] != "provisioned by swallow" {
		t.Errorf("comment: got %q", fake.lastForm["comment"])
	}

	wantUserData := base64.StdEncoding.EncodeToString([]byte("#cloud-config\npackages: [htop]\n"))
	if fake.lastForm["user_data"] != wantUserData {
		t.Errorf("user_data: got %q, want base64 %q", fake.lastForm["user_data"], wantUserData)
	}

	if state.Status != provisioningdomain.MachineStatusDeploying {
		t.Errorf("status: got %q, want deploying", state.Status)
	}
}

func TestDeploy_OmitsUnsetOptionalFields(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineOperation(http.StatusOK, deployingMachineJSON)
	provider := newTestProvider(t, fake)

	if _, err := provider.Deploy(context.Background(), provisioningdomain.DeployRequest{
		MachineID:    "abc123",
		DistroSeries: "jammy",
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	for _, field := range []string{"osystem", "comment", "user_data"} {
		if _, present := fake.lastForm[field]; present {
			t.Errorf("expected %q to be omitted when unset, got %q", field, fake.lastForm[field])
		}
	}
}

func TestDeploy_RejectionSurfacesMAASMessage(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineOperation(http.StatusBadRequest,
		`{"storage": ["Mount the root '/' filesystem to be able to deploy this node."]}`)
	provider := newTestProvider(t, fake)

	_, err := provider.Deploy(context.Background(), provisioningdomain.DeployRequest{
		MachineID:    "abc123",
		DistroSeries: "jammy",
	})

	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a ProviderError, got %v", err)
	}
	if provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("kind: got %q, want %q", provErr.Kind, provisioningdomain.ProviderErrorRejected)
	}
	if !strings.Contains(provErr.Detail, "Mount the root") {
		t.Errorf("expected MAAS's own explanation in the detail, got %q", provErr.Detail)
	}
	if !strings.Contains(provErr.Detail, "storage") {
		t.Errorf("expected the offending field name in the detail, got %q", provErr.Detail)
	}
}

func TestRelease_UsesReleaseOperation(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineOperation(http.StatusOK, readyMachineJSON)
	provider := newTestProvider(t, fake)

	state, err := provider.Release(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if fake.lastOperation != "release" {
		t.Errorf("operation: got %q, want %q", fake.lastOperation, "release")
	}
	if fake.lastContentType != "" {
		t.Errorf("content type: got %q, want no Content-Type", fake.lastContentType)
	}
	if fake.lastContentLength != 0 {
		t.Errorf("content length: got %d, want 0", fake.lastContentLength)
	}
	if state.Status != provisioningdomain.MachineStatusReady {
		t.Errorf("status: got %q, want ready", state.Status)
	}
}

func TestReleaseWithOptions_MapsErasureControlsWithoutForce(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineOperation(http.StatusOK, readyMachineJSON)
	provider := newTestProvider(t, fake)

	_, err := provider.ReleaseWithOptions(context.Background(), provisioningdomain.ReleaseRequest{
		MachineID:   "abc123",
		Erase:       true,
		SecureErase: true,
		QuickErase:  true,
		Comment:     "retire from test pool",
	})
	if err != nil {
		t.Fatalf("ReleaseWithOptions: %v", err)
	}
	if fake.lastOperation != "release" {
		t.Errorf("operation: got %q, want release", fake.lastOperation)
	}
	if !strings.HasPrefix(fake.lastContentType, "multipart/form-data") {
		t.Errorf("content type: got %q, want multipart/form-data", fake.lastContentType)
	}
	want := map[string]string{
		"erase": "true", "secure_erase": "true", "quick_erase": "true",
		"comment": "retire from test pool",
	}
	for field, value := range want {
		if fake.lastForm[field] != value {
			t.Errorf("%s: got %q, want %q", field, fake.lastForm[field], value)
		}
	}
	if _, present := fake.lastForm["force"]; present {
		t.Error("release options must never force past MAAS safeguards")
	}
}

func TestDeleteMachine_UsesDeleteWithoutForce(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onDeleteMachine(http.StatusNoContent, "")
	provider := newTestProvider(t, fake)

	if err := provider.DeleteMachine(context.Background(), "abc123"); err != nil {
		t.Fatalf("DeleteMachine: %v", err)
	}
	if fake.lastMethod != http.MethodDelete {
		t.Errorf("method: got %q, want DELETE", fake.lastMethod)
	}
	if fake.lastRawQuery != "" {
		t.Errorf("query: got %q, want no force override", fake.lastRawQuery)
	}
}

func TestDeleteMachine_RefusalPreservesProviderMessage(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onDeleteMachine(http.StatusBadRequest, "Machine cannot be deleted while hosting virtual machines.")
	provider := newTestProvider(t, fake)

	err := provider.DeleteMachine(context.Background(), "abc123")
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || !strings.Contains(providerErr.Detail, "hosting virtual machines") {
		t.Fatalf("DeleteMachine error = %v, want provider refusal detail", err)
	}
}

func TestDeleteMachine_NotFoundBecomesDomainError(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onDeleteMachine(http.StatusNotFound, "Not Found")
	provider := newTestProvider(t, fake)

	err := provider.DeleteMachine(context.Background(), "abc123")
	if !errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		t.Fatalf("DeleteMachine error = %v, want ErrMachineNotFound", err)
	}
}

func TestRequestsAreSigned(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onListMachines(http.StatusOK, `[]`)
	provider := newTestProvider(t, fake)

	if _, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{}); err != nil {
		t.Fatalf("ListMachines: %v", err)
	}

	auth := fake.lastAuthorization
	for _, want := range []string{
		`OAuth `,
		`oauth_signature_method="PLAINTEXT"`,
		`oauth_consumer_key="ck"`,
		`oauth_token="tk"`,
		`oauth_signature="&ts"`,
	} {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization header missing %s: %s", want, auth)
		}
	}
}

func TestAuthFailureIsClassifiedAsAuth(t *testing.T) {
	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		fake := newFakeMAAS(t)
		fake.onListMachines(statusCode, `Invalid API key`)
		provider := newTestProvider(t, fake)

		_, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{})

		var provErr *provisioningdomain.ProviderError
		if !errors.As(err, &provErr) {
			t.Fatalf("HTTP %d: expected a ProviderError, got %v", statusCode, err)
		}
		if provErr.Kind != provisioningdomain.ProviderErrorAuth {
			t.Errorf("HTTP %d: kind got %q, want %q", statusCode, provErr.Kind, provisioningdomain.ProviderErrorAuth)
		}
	}
}

func TestServerErrorIsClassifiedAsUnavailable(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onListMachines(http.StatusInternalServerError, `boom`)
	provider := newTestProvider(t, fake)

	_, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{})

	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a ProviderError, got %v", err)
	}
	if provErr.Kind != provisioningdomain.ProviderErrorUnavailable {
		t.Errorf("kind: got %q, want %q", provErr.Kind, provisioningdomain.ProviderErrorUnavailable)
	}
}

func TestUnreachableProviderIsClassifiedAsUnavailable(t *testing.T) {
	fake := newFakeMAAS(t)
	provider := newTestProvider(t, fake)
	// Closing the listener makes every call a transport failure.
	fake.server.Close()

	_, err := provider.ListMachines(context.Background(), provisioningdomain.MachineFilter{})

	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a ProviderError, got %v", err)
	}
	if provErr.Kind != provisioningdomain.ProviderErrorUnavailable {
		t.Errorf("kind: got %q, want %q", provErr.Kind, provisioningdomain.ProviderErrorUnavailable)
	}
	if provErr.Err == nil {
		t.Error("expected the underlying transport error to be retained for logs")
	}
}

func TestProbe_ReportsVersion(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onVersion(http.StatusOK, `{"version": "3.6.1", "subversion": "beta"}`)
	provider := newTestProvider(t, fake)

	info, err := provider.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.Name != "maas" {
		t.Errorf("Name: got %q, want maas", info.Name)
	}
	if info.DisplayName != "Ubuntu MAAS" {
		t.Errorf("DisplayName: got %q", info.DisplayName)
	}
	if info.Version != "3.6.1 beta" {
		t.Errorf("Version: got %q, want %q", info.Version, "3.6.1 beta")
	}
}

func TestListOSImages_SplitsNameAndDedupesByArchitecture(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onBootResources(http.StatusOK, `[
	  {"id": 1, "type": "Synced", "name": "ubuntu/jammy", "title": "Ubuntu 22.04 LTS", "architecture": "amd64/hwe-22.04"},
	  {"id": 2, "type": "Synced", "name": "ubuntu/jammy", "title": "Ubuntu 22.04 LTS", "architecture": "amd64/ga-22.04"},
	  {"id": 3, "type": "Synced", "name": "ubuntu/noble", "title": "", "architecture": "arm64/generic"},
	  {"id": 4, "type": "Synced", "name": "grub-efi-signed/uefi", "architecture": "amd64/generic"},
	  {"id": 5, "type": "Uploaded", "name": "ubuntu-24.04-rocm", "title": "Ubuntu 24.04 ROCm", "architecture": "amd64/generic"}
	]`)
	provider := newTestProvider(t, fake)

	images, err := provider.ListOSImages(context.Background())
	if err != nil {
		t.Fatalf("ListOSImages: %v", err)
	}

	// The two jammy kernels collapse into one image, and the bootloader resource
	// is dropped while the Uploaded resource remains available as a custom image.
	if len(images) != 3 {
		t.Fatalf("expected 3 images, got %d: %+v", len(images), images)
	}

	jammy := images[0]
	if jammy.ID != "ubuntu/jammy" {
		t.Errorf("ID: got %q, want the distro_series value ubuntu/jammy", jammy.ID)
	}
	if jammy.Name != "Ubuntu 22.04 LTS" {
		t.Errorf("Name: got %q", jammy.Name)
	}
	if jammy.OSSystem != "ubuntu" || jammy.Release != "jammy" {
		t.Errorf("OSSystem/Release: got %q/%q", jammy.OSSystem, jammy.Release)
	}
	if jammy.Architecture != "amd64" {
		t.Errorf("Architecture: got %q, want amd64 without the kernel flavour", jammy.Architecture)
	}

	noble := images[1]
	if noble.Name != "ubuntu/noble" {
		t.Errorf("expected the resource name as a fallback display name, got %q", noble.Name)
	}

	custom := images[2]
	if custom.ID != "ubuntu-24.04-rocm" {
		t.Errorf("custom ID: got %q, want the provider resource name", custom.ID)
	}
	if custom.Name != "Ubuntu 24.04 ROCm" {
		t.Errorf("custom Name: got %q", custom.Name)
	}
	if custom.OSSystem != "custom" || custom.Release != "ubuntu-24.04-rocm" {
		t.Errorf(
			"custom OSSystem/Release: got %q/%q, want custom/ubuntu-24.04-rocm",
			custom.OSSystem,
			custom.Release,
		)
	}
}

// The catalog carries a resource name, not the numeric boot-resource id MAAS needs to
// delete one, so the adapter resolves the uploaded resource from its name and CPU
// architecture before issuing the delete.
func TestDeleteOSImage_ResolvesUploadedResourceAndDeletesByID(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onBootResources(http.StatusOK, `[
	  {"id": 1, "type": "Synced", "name": "ubuntu/jammy", "architecture": "amd64/hwe-22.04"},
	  {"id": 7, "type": "Uploaded", "name": "ubuntu-24.04-rocm", "title": "Ubuntu 24.04 ROCm", "architecture": "amd64/generic"}
	]`)
	fake.onDeleteBootResource(http.StatusNoContent)
	provider := newTestProvider(t, fake)

	if err := provider.DeleteOSImage(context.Background(), "ubuntu-24.04-rocm", "amd64"); err != nil {
		t.Fatalf("DeleteOSImage: %v", err)
	}
	if fake.lastMethod != http.MethodDelete {
		t.Errorf("method: got %q, want DELETE", fake.lastMethod)
	}
	if fake.lastDeletedBootResource != "7" {
		t.Errorf("deleted boot resource id: got %q, want 7", fake.lastDeletedBootResource)
	}
}

// A synced image is a provider-owned mirror MAAS would re-sync, so deletion is refused
// rather than attempted; the same rejection covers a name that matches no uploaded image.
func TestDeleteOSImage_RefusesSyncedImage(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onBootResources(http.StatusOK, `[
	  {"id": 1, "type": "Synced", "name": "ubuntu/jammy", "architecture": "amd64/hwe-22.04"}
	]`)
	// No DELETE handler is registered: a refusal must never reach the delete path.
	provider := newTestProvider(t, fake)

	err := provider.DeleteOSImage(context.Background(), "ubuntu/jammy", "amd64")
	var providerErr *provisioningdomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("DeleteOSImage error = %v, want a ProviderErrorRejected", err)
	}
	if fake.lastMethod == http.MethodDelete {
		t.Error("a synced image must not be deleted")
	}
}

func TestListMachineEvents_QueriesBySystemIDAndMapsFields(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onEvents(http.StatusOK, `{
		"events": [{
			"id": 4812,
			"username": "admin",
			"level": "AUDIT",
			"created": "Wed, 02 Sep. 2026 01:02:03",
			"type": "Request from user",
			"description": "Started releasing machine."
		}]
	}`)
	provider := newTestProvider(t, fake)

	events, err := provider.ListMachineEvents(context.Background(), "abc123", 17)
	if err != nil {
		t.Fatalf("ListMachineEvents: %v", err)
	}
	if fake.lastMethod != http.MethodGet || fake.lastOperation != "query" {
		t.Fatalf("request: got %s op=%q, want GET op=query", fake.lastMethod, fake.lastOperation)
	}
	if !strings.Contains(fake.lastRawQuery, "id=abc123") || !strings.Contains(fake.lastRawQuery, "limit=17") {
		t.Errorf("query: got %q, want system id and limit", fake.lastRawQuery)
	}
	if len(events) != 1 {
		t.Fatalf("events: got %d, want 1", len(events))
	}
	event := events[0]
	if event.ID != "4812" || event.Level != "audit" || event.Actor != "admin" {
		t.Errorf("mapped event: %+v", event)
	}
	if event.Type != "Request from user" || event.Description != "Started releasing machine." {
		t.Errorf("event content: %+v", event)
	}
	if event.OccurredAt != "2026-09-02T01:02:03Z" {
		t.Errorf("occurredAt: got %q, want normalized RFC3339", event.OccurredAt)
	}
}
