package maas

import (
	"context"
	"net/http"
	"testing"

	provisioningdomain "github.com/AFDEAPAC/swallow/internal/provisioning/domain"
)

func (f *fakeMAAS) onNodeDevices(statusCode int, body string) {
	f.respond("GET "+apiPrefix+"/nodes/{id}/devices/{$}", statusCode, body)
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
  "pod": {"name": "kvm-host-3"},
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

func TestGetMachineDetail_ShapesSectionsAndTables(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.onGetMachine(http.StatusOK, detailMachineJSONBody)
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
