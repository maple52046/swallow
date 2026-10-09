package maas

import (
	"context"
	"net/http"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

func (f *fakeMAAS) onNodeDevices(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/nodes/{id}/devices/{$}", statusCode, body)
}

func (f *fakeMAAS) onMachineDetail(
	machineStatus int,
	machineBody string,
	powerParametersStatus int,
	powerParametersBody string,
) {
	f.mux.HandleFunc("GET "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("op") == "power_parameters" {
			w.WriteHeader(powerParametersStatus)
			_, _ = w.Write([]byte(powerParametersBody))
			return
		}
		w.WriteHeader(machineStatus)
		_, _ = w.Write([]byte(machineBody))
	})
}

const gpuDevicesJSON = `[
  {"vendor_name": "NVIDIA Corporation", "product_name": "A100", "vendor_id": "10de", "product_id": "20b0",
   "hardware_type_name": "GPU", "bus_name": "PCIE", "pci_address": "0000:81:00.0", "numa_node": 1},
  {"vendor_name": "NVIDIA Corporation", "product_name": "A100", "vendor_id": "10de", "product_id": "20b0",
   "hardware_type_name": "GPU", "bus_name": "PCIE", "pci_address": "0000:47:00.0", "numa_node": 0},
  {"vendor_name": "Matrox", "product_name": "G200", "vendor_id": "102b", "product_id": "0522",
   "hardware_type_name": "GPU", "bus_name": "PCIE", "pci_address": "0000:02:00.0", "numa_node": 0}
]`

func TestListGPUs_AggregatesIdenticalModels(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onNodeDevices(http.StatusOK, gpuDevicesJSON)
	provider := newTestProvider(t, fake)

	gpus, err := provider.ListGPUs(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("ListGPUs: %v", err)
	}

	// Two A100s collapse into one entry with a count of two; the Matrox stays separate.
	if len(gpus) != 2 {
		t.Fatalf("expected 2 distinct GPU models, got %d: %+v", len(gpus), gpus)
	}
	a100 := gpus[0]
	if a100.Vendor != "NVIDIA Corporation" || a100.Model != "A100" {
		t.Errorf("first GPU: got %q %q", a100.Vendor, a100.Model)
	}
	if a100.Count != 2 {
		t.Errorf("expected two A100s collapsed into a count, got %d", a100.Count)
	}
	if gpus[1].Model != "G200" || gpus[1].Count != 1 {
		t.Errorf("second GPU: got %+v", gpus[1])
	}
}

func TestListGPUs_EmptyWhenNoneAttached(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onNodeDevices(http.StatusOK, `[]`)
	provider := newTestProvider(t, fake)

	gpus, err := provider.ListGPUs(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("ListGPUs: %v", err)
	}
	if len(gpus) != 0 {
		t.Fatalf("expected no GPUs, got %+v", gpus)
	}
}

const detailMachineJSONBody = `{
  "system_id": "abc123",
  "hostname": "gpu-node-01",
  "fqdn": "gpu-node-01.maas",
  "status_name": "Deployed",
  "power_state": "on",
  "power_type": "ipmi",
  "architecture": "amd64/generic",
  "cpu_count": 64,
  "cpu_speed": 2600,
  "memory": 262144,
  "osystem": "ubuntu",
  "distro_series": "noble",
  "hwe_kernel": "ga-24.04",
  "locked": true,
  "commissioning_status_name": "Passed",
  "testing_status_name": "Passed",
  "ephemeral_deploy": false,
  "zone": {"name": "dc-east"},
  "pool": {"name": "gpu-pool"},
  "pod": null,
  "hardware_info": {
    "system_vendor": "Dell Inc.",
    "system_product": "PowerEdge R760xa",
    "system_serial": "Unknown",
    "cpu_model": "Intel(R) Xeon(R) Platinum 8480+",
    "mainboard_vendor": "Dell Inc.",
    "mainboard_firmware_vendor": "Dell Inc.",
    "mainboard_firmware_version": "2.1.5",
    "chassis_vendor": "Dell Inc.",
    "chassis_type": "Rack Mount Chassis"
  },
  "physicalblockdevice_set": [
    {"name": "nvme0n1", "model": "Dell Ent NVMe", "serial": "S1A2B3", "size": 1920383410176}
  ],
  "interface_set": [
    {"name": "eth0", "mac_address": "aa:bb:cc:dd:ee:ff", "links": [{"ip_address": "10.0.1.10", "mode": "static"}]}
  ],
  "numanode_set": [
    {"index": 0, "cores": [0,1,2,3], "memory": 131072},
    {"index": 1, "cores": [4,5,6,7], "memory": 131072}
  ]
}`

const powerParametersJSONBody = `{
  "power_address": "192.0.2.20",
  "power_user": "bmc-admin",
  "power_driver": "LAN_2_0",
  "power_boot_type": "efi",
  "privilege_level": "OPERATOR",
  "cipher_suite_id": "17",
  "mac_address": "aa:bb:cc:dd:ee:ff",
  "power_pass": "bmc-secret",
  "k_g": "also-must-not-leak",
  "unrecognised_secret": "never-decode-this"
}`

func TestGetMachineDetail_ShapesSectionsAndTables(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineDetail(http.StatusOK, detailMachineJSONBody, http.StatusOK, powerParametersJSONBody)
	fake.onNodeDevices(http.StatusOK, gpuDevicesJSON)
	provider := newTestProvider(t, fake)

	detail, err := provider.GetMachineDetail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetMachineDetail: %v", err)
	}

	sections := sectionsByTitle(detail.Sections)
	system, ok := sections["System"]
	if !ok {
		t.Fatal("expected a System section")
	}
	if fieldValue(system, "Vendor") != "Dell Inc." {
		t.Errorf("System vendor: got %q", fieldValue(system, "Vendor"))
	}
	if fieldValue(system, "Product") != "PowerEdge R760xa" {
		t.Errorf("System product: got %q", fieldValue(system, "Product"))
	}
	// "Unknown" is a MAAS placeholder and must be dropped, not shown as the serial.
	for _, f := range system.Fields {
		if f.Label == "Serial" {
			t.Errorf("a placeholder serial must be dropped, got %q", f.Value)
		}
	}

	compute := sections["Compute"]
	if fieldValue(compute, "CPU model") != "Intel(R) Xeon(R) Platinum 8480+" {
		t.Errorf("CPU model: got %q", fieldValue(compute, "CPU model"))
	}

	bmc, ok := sections["BMC"]
	if !ok {
		t.Fatal("expected a BMC section for a physical machine")
	}
	if fieldValue(bmc, "Protocol") != "IPMI" {
		t.Errorf("BMC protocol: got %q", fieldValue(bmc, "Protocol"))
	}
	if fieldValue(bmc, "Address") != "192.0.2.20" {
		t.Errorf("BMC address: got %q", fieldValue(bmc, "Address"))
	}
	if fieldValue(bmc, "Username") != "bmc-admin" {
		t.Errorf("BMC username: got %q", fieldValue(bmc, "Username"))
	}
	if fieldValue(bmc, "Password") != "bmc-secret" {
		t.Errorf("BMC password was not exposed through the explicit allowlist")
	}
	if fieldValue(bmc, "Driver") != "LAN_2_0" || fieldValue(bmc, "Boot type") != "efi" {
		t.Errorf("BMC driver fields: got %+v", bmc.Fields)
	}
	if fieldValue(bmc, "Privilege level") != "OPERATOR" || fieldValue(bmc, "Cipher suite") != "17" {
		t.Errorf("BMC access fields: got %+v", bmc.Fields)
	}
	if fieldValue(bmc, "Power MAC") != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("BMC power MAC: got %q", fieldValue(bmc, "Power MAC"))
	}
	for _, field := range bmc.Fields {
		for _, forbidden := range []string{"also-must-not-leak", "never-decode-this"} {
			if strings.Contains(field.Value, forbidden) {
				t.Errorf("non-allowlisted BMC secret crossed the detail boundary: %+v", bmc.Fields)
			}
		}
	}

	tables := tablesByTitle(detail.Tables)
	if _, ok := tables["Storage"]; !ok {
		t.Error("expected a Storage table")
	}
	if _, ok := tables["Network"]; !ok {
		t.Error("expected a Network table")
	}
	numa, ok := tables["NUMA"]
	if !ok || len(numa.Rows) != 2 {
		t.Errorf("expected a NUMA table with 2 rows, got %+v", numa)
	}
	pci, ok := tables["PCI devices"]
	if !ok || len(pci.Rows) != 3 {
		t.Errorf("expected a PCI table with 3 devices, got %+v", pci)
	}
}

func TestGetMachineDetail_PowerParametersPermissionFailureIsExplicit(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onMachineDetail(
		http.StatusOK,
		`{"system_id":"abc123","power_type":"redfish","pod":null}`,
		http.StatusForbidden,
		`{"detail":"Forbidden"}`,
	)
	fake.onNodeDevices(http.StatusOK, `[]`)
	provider := newTestProvider(t, fake)

	detail, err := provider.GetMachineDetail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetMachineDetail: %v", err)
	}

	bmc := sectionsByTitle(detail.Sections)["BMC"]
	if fieldValue(bmc, "Protocol") != "Redfish" {
		t.Errorf("BMC protocol: got %q", fieldValue(bmc, "Protocol"))
	}
	if want := "Unavailable to the configured integration credential"; fieldValue(bmc, "Connection details") != want {
		t.Errorf("connection detail status: got %q, want %q", fieldValue(bmc, "Connection details"), want)
	}
}

func TestBuildMachineDetail_ShapesRedfishNodeIdentity(t *testing.T) {
	detail := buildMachineDetail(
		&detailMachineJSON{PowerType: "redfish"},
		powerParametersJSON{
			PowerAddress: "https://192.0.2.21/redfish/v1",
			PowerUser:    "redfish-operator",
			PowerPass:    "redfish-secret",
			NodeID:       "System.Embedded.1",
		},
		"",
		nil,
	)

	bmc := sectionsByTitle(detail.Sections)["BMC"]
	if fieldValue(bmc, "Protocol") != "Redfish" || fieldValue(bmc, "Node ID") != "System.Embedded.1" {
		t.Errorf("Redfish identity: got %+v", bmc.Fields)
	}
	if fieldValue(bmc, "Password") != "redfish-secret" {
		t.Error("Redfish password was not exposed through the explicit allowlist")
	}
}

func TestSafePowerAddress_RemovesEmbeddedCredentials(t *testing.T) {
	got := safePowerAddress("https://bmc-admin:must-not-leak@192.0.2.20/redfish/v1?token=also-secret#session")
	if want := "https://192.0.2.20/redfish/v1"; got != want {
		t.Fatalf("safePowerAddress: got %q, want %q", got, want)
	}
}

func TestBuildMachineDetail_OmitsBMCForVirtualMachine(t *testing.T) {
	detail := buildMachineDetail(
		&detailMachineJSON{
			Pod:       &namedJSON{Name: "kvm-host-3"},
			PowerType: "redfish",
		},
		powerParametersJSON{PowerAddress: "https://192.0.2.21/redfish/v1"},
		"",
		nil,
	)

	if _, ok := sectionsByTitle(detail.Sections)["BMC"]; ok {
		t.Fatal("virtual-machine detail must not expose a BMC section")
	}
	if _, ok := sectionsByTitle(detail.Sections)["Power"]; !ok {
		t.Error("virtual-machine detail must describe its power driver in a Power section")
	}
}

// A virsh driver is not a BMC even without a VM host: its libvirt URI is shown as the power address,
// with the SSH user kept and the password never shown.
func TestBuildMachineDetail_VirshDriverGetsPowerSection(t *testing.T) {
	detail := buildMachineDetail(
		&detailMachineJSON{PowerType: "virsh"},
		powerParametersJSON{PowerAddress: "qemu+ssh://maas@tainan-ci/system", PowerID: "simple-pig", PowerPass: "must-not-show"},
		"",
		nil,
	)

	sections := sectionsByTitle(detail.Sections)
	if _, ok := sections["BMC"]; ok {
		t.Fatal("a virsh driver must not be presented as a BMC")
	}
	power := sections["Power"]
	if fieldValue(power, "Driver") != "virsh" || fieldValue(power, "Address") != "qemu+ssh://maas@tainan-ci/system" ||
		fieldValue(power, "Power ID") != "simple-pig" {
		t.Errorf("Power section = %+v", power.Fields)
	}
	for _, field := range power.Fields {
		if field.Value == "must-not-show" {
			t.Errorf("Power section exposes the password as %q", field.Label)
		}
	}
}

// A machine without a power driver gets neither a BMC nor a Power section.
func TestBuildMachineDetail_NoDriverNoPowerSection(t *testing.T) {
	detail := buildMachineDetail(&detailMachineJSON{}, powerParametersJSON{}, "", nil)
	sections := sectionsByTitle(detail.Sections)
	if _, ok := sections["BMC"]; ok {
		t.Error("BMC section without a driver")
	}
	if _, ok := sections["Power"]; ok {
		t.Error("Power section without a driver")
	}
}

func sectionsByTitle(sections []provisioningdomain.DetailSection) map[string]provisioningdomain.DetailSection {
	m := map[string]provisioningdomain.DetailSection{}
	for _, s := range sections {
		m[s.Title] = s
	}
	return m
}

func tablesByTitle(tables []provisioningdomain.DetailTable) map[string]provisioningdomain.DetailTable {
	m := map[string]provisioningdomain.DetailTable{}
	for _, t := range tables {
		m[t.Title] = t
	}
	return m
}

func fieldValue(section provisioningdomain.DetailSection, label string) string {
	for _, f := range section.Fields {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}
