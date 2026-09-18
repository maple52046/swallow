package maas

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// nodeDeviceJSON is one entry from /nodes/{system_id}/devices/, MAAS's PCI and USB
// device inventory. It is the only place a GPU is visible, because the machine object
// itself carries CPU, memory, and storage but not attached accelerators.
type nodeDeviceJSON struct {
	VendorID         string `json:"vendor_id"`
	ProductID        string `json:"product_id"`
	VendorName       string `json:"vendor_name"`
	ProductName      string `json:"product_name"`
	HardwareTypeName string `json:"hardware_type_name"`
	BusName          string `json:"bus_name"`
	PCIAddress       string `json:"pci_address"`
	NUMANode         int    `json:"numa_node"`
}

// devicesPath is the node-scoped device inventory. It lives under /nodes/, not
// /machines/, because in MAAS a device belongs to any node kind, machine or not.
func devicesPath(machineID string) string {
	return "/nodes/" + url.PathEscape(machineID) + "/devices/"
}

// ListGPUs returns the machine's GPUs, collapsing identical models into one entry with a
// count. MAAS answers the hardware_type filter with the string "gpu"; the numeric code
// is rejected.
func (p *Provider) ListGPUs(ctx context.Context, machineID string) ([]provisioningdomain.GPU, error) {
	query := url.Values{}
	query.Set("hardware_type", "gpu")

	var devices []nodeDeviceJSON
	if err := p.client.get(ctx, devicesPath(machineID), query, &devices); err != nil {
		return nil, translateError(err, machineID)
	}
	return aggregateGPUs(devices), nil
}

// aggregateGPUs groups devices by vendor and model, preserving first-seen order so the
// result is stable for tests and for display.
func aggregateGPUs(devices []nodeDeviceJSON) []provisioningdomain.GPU {
	type key struct{ vendor, model string }
	index := map[key]int{}
	var gpus []provisioningdomain.GPU

	for _, d := range devices {
		vendor := cleanField(d.VendorName)
		model := cleanField(d.ProductName)
		if vendor == "" && model == "" {
			// A GPU MAAS could not name at all is still a GPU; fall back to the PCI id
			// so the count stays honest rather than dropping the device.
			model = strings.TrimSpace(d.VendorID + ":" + d.ProductID)
		}
		k := key{vendor, model}
		if i, ok := index[k]; ok {
			gpus[i].Count++
			continue
		}
		index[k] = len(gpus)
		gpus = append(gpus, provisioningdomain.GPU{Vendor: vendor, Model: model, Count: 1})
	}
	return gpus
}

// detailMachineJSON is the machine object as read for a single-machine view. It is
// separate from machineJSON on purpose: the detail view needs nested structures the
// fleet listing deliberately ignores, and keeping them out of machineJSON keeps every
// reconcile pass from decoding disk and NUMA layouts it never uses.
type detailMachineJSON struct {
	Hostname     string `json:"hostname"`
	FQDN         string `json:"fqdn"`
	StatusName   string `json:"status_name"`
	PowerState   string `json:"power_state"`
	PowerType    string `json:"power_type"`
	Architecture string `json:"architecture"`
	CPUCount     int    `json:"cpu_count"`
	CPUSpeed     int    `json:"cpu_speed"`
	Memory       int64  `json:"memory"`
	OSystem      string `json:"osystem"`
	DistroSeries string `json:"distro_series"`
	HWEKernel    string `json:"hwe_kernel"`
	Locked       bool   `json:"locked"`

	CommissioningStatusName string `json:"commissioning_status_name"`
	TestingStatusName       string `json:"testing_status_name"`
	EphemeralDeploy         *bool  `json:"ephemeral_deploy"`

	Zone *namedJSON `json:"zone"`
	Pool *namedJSON `json:"pool"`
	Pod  *namedJSON `json:"pod"`

	HardwareInfo         *hardwareInfoJSON     `json:"hardware_info"`
	PhysicalBlockDevices []blockDeviceJSON     `json:"physicalblockdevice_set"`
	Interfaces           []detailInterfaceJSON `json:"interface_set"`
	NUMANodes            []numaNodeJSON        `json:"numanode_set"`
}

// powerParametersJSON deliberately decodes only the connection facts approved for the
// admin-only live-detail response. PowerPass is the sole secret required for manual BMC
// administration; K_g values, tokens, private keys, and unknown JSON fields are discarded
// before they can cross the API boundary.
type powerParametersJSON struct {
	PowerAddress   string `json:"power_address"`
	PowerUser      string `json:"power_user"`
	PowerPass      string `json:"power_pass"`
	NodeID         string `json:"node_id"`
	PowerDriver    string `json:"power_driver"`
	PowerBootType  string `json:"power_boot_type"`
	PrivilegeLevel string `json:"privilege_level"`
	CipherSuiteID  string `json:"cipher_suite_id"`
	MACAddress     string `json:"mac_address"`
}

func (p powerParametersJSON) hasDisplayValues() bool {
	return cleanField(p.PowerAddress) != "" ||
		cleanField(p.PowerUser) != "" ||
		cleanField(p.PowerPass) != "" ||
		cleanField(p.NodeID) != "" ||
		cleanField(p.PowerDriver) != "" ||
		cleanField(p.PowerBootType) != "" ||
		cleanField(p.PrivilegeLevel) != "" ||
		cleanField(p.CipherSuiteID) != "" ||
		cleanField(p.MACAddress) != ""
}

type blockDeviceJSON struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
	Size   int64  `json:"size"`
}

type detailInterfaceJSON struct {
	Name       string `json:"name"`
	MACAddress string `json:"mac_address"`
	Links      []struct {
		IPAddress string `json:"ip_address"`
		Mode      string `json:"mode"`
	} `json:"links"`
}

type numaNodeJSON struct {
	Index  int   `json:"index"`
	Cores  []int `json:"cores"`
	Memory int64 `json:"memory"`
}

// GetMachineDetail proxies MAAS live for one machine and shapes the answer into the
// provider-neutral MachineDetail. The machine object and device inventory are mandatory;
// a physical machine with a power type also gets an admin-only power_parameters read. That
// optional read degrades to an explicit section status so BMC permission does not hide the
// rest of the hardware detail.
//
// It is not cached: this is read one machine at a time, so a live read is always fresh
// and swallow needs no schema for MAAS's disk and NUMA shapes.
func (p *Provider) GetMachineDetail(ctx context.Context, machineID string) (*provisioningdomain.MachineDetail, error) {
	var m detailMachineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &m); err != nil {
		return nil, translateError(err, machineID)
	}

	powerParameters := powerParametersJSON{}
	powerParametersStatus := ""
	if nameOf(m.Pod) == "" && cleanField(m.PowerType) != "" {
		if err := p.client.getOperation(ctx, machinePath(machineID), "power_parameters", &powerParameters); err != nil {
			powerParametersStatus = powerParametersUnavailableStatus(err)
		} else if !powerParameters.hasDisplayValues() {
			powerParametersStatus = "No connection parameters reported by MAAS"
		}
	}

	var devices []nodeDeviceJSON
	if err := p.client.get(ctx, devicesPath(machineID), nil, &devices); err != nil {
		return nil, translateError(err, machineID)
	}

	return buildMachineDetail(&m, powerParameters, powerParametersStatus, devices), nil
}

func buildMachineDetail(
	m *detailMachineJSON,
	powerParameters powerParametersJSON,
	powerParametersStatus string,
	devices []nodeDeviceJSON,
) *provisioningdomain.MachineDetail {
	detail := &provisioningdomain.MachineDetail{}

	provisioning := provisioningdomain.DetailSection{Title: "Provisioning", Fields: []provisioningdomain.DetailField{
		{Label: "Status", Value: m.StatusName},
		{Label: "Power", Value: cleanField(m.PowerState)},
		{Label: "OS", Value: strings.TrimSpace(m.OSystem + " " + m.DistroSeries)},
		{Label: "Kernel", Value: m.HWEKernel},
		{Label: "Ephemeral", Value: yesNo(m.EphemeralDeploy)},
		{Label: "Locked", Value: boolLabel(m.Locked)},
		{Label: "Commissioning", Value: m.CommissioningStatusName},
		{Label: "Testing", Value: m.TestingStatusName},
		{Label: "Zone", Value: nameOf(m.Zone)},
		{Label: "Resource pool", Value: nameOf(m.Pool)},
		{Label: "VM host", Value: nameOf(m.Pod)},
	}}
	detail.Sections = append(detail.Sections, dropEmptyFields(provisioning))

	compute := provisioningdomain.DetailSection{Title: "Compute", Fields: []provisioningdomain.DetailField{
		{Label: "Architecture", Value: m.Architecture},
		{Label: "CPU cores", Value: intLabel(m.CPUCount)},
		{Label: "CPU speed", Value: mhz(m.CPUSpeed)},
		{Label: "Memory", Value: mib(m.Memory)},
	}}
	if hw := m.HardwareInfo; hw != nil {
		compute.Fields = append(compute.Fields,
			provisioningdomain.DetailField{Label: "CPU model", Value: cleanField(hw.CPUModel)},
		)
	}
	detail.Sections = append(detail.Sections, dropEmptyFields(compute))

	if nameOf(m.Pod) == "" {
		bmc := dropEmptyFields(provisioningdomain.DetailSection{Title: "BMC", Fields: []provisioningdomain.DetailField{
			{Label: "Protocol", Value: managementProtocol(m.PowerType)},
			{Label: "Address", Value: safePowerAddress(powerParameters.PowerAddress)},
			{Label: "Username", Value: cleanField(powerParameters.PowerUser)},
			{Label: "Password", Value: cleanField(powerParameters.PowerPass)},
			{Label: "Node ID", Value: cleanField(powerParameters.NodeID)},
			{Label: "Driver", Value: cleanField(powerParameters.PowerDriver)},
			{Label: "Boot type", Value: cleanField(powerParameters.PowerBootType)},
			{Label: "Privilege level", Value: cleanField(powerParameters.PrivilegeLevel)},
			{Label: "Cipher suite", Value: cleanField(powerParameters.CipherSuiteID)},
			{Label: "Power MAC", Value: cleanField(powerParameters.MACAddress)},
			{Label: "Connection details", Value: powerParametersStatus},
		}})
		if len(bmc.Fields) > 0 {
			detail.Sections = append(detail.Sections, bmc)
		}
	}

	if hw := m.HardwareInfo; hw != nil {
		system := provisioningdomain.DetailSection{Title: "System", Fields: []provisioningdomain.DetailField{
			{Label: "Vendor", Value: cleanField(hw.SystemVendor)},
			{Label: "Product", Value: cleanField(hw.SystemProduct)},
			{Label: "Serial", Value: cleanField(hw.SystemSerial)},
			{Label: "Chassis", Value: cleanField(hw.ChassisVendor)},
			{Label: "Chassis type", Value: cleanField(hw.ChassisType)},
			{Label: "Mainboard", Value: cleanField(hw.MainboardVendor)},
			{Label: "Firmware", Value: strings.TrimSpace(cleanField(hw.MainboardFirmwareVendor) + " " + cleanField(hw.MainboardFirmwareVersion))},
			{Label: "Firmware date", Value: cleanField(hw.MainboardFirmwareDate)},
		}}
		detail.Sections = append(detail.Sections, dropEmptyFields(system))
	}

	detail.Tables = appendNonEmptyTable(detail.Tables, storageTable(m.PhysicalBlockDevices))
	detail.Tables = appendNonEmptyTable(detail.Tables, networkTable(m.Interfaces))
	detail.Tables = appendNonEmptyTable(detail.Tables, numaTable(m.NUMANodes))
	detail.Tables = appendNonEmptyTable(detail.Tables, pciTable(devices))

	return detail
}

func storageTable(disks []blockDeviceJSON) provisioningdomain.DetailTable {
	table := provisioningdomain.DetailTable{Title: "Storage", Columns: []string{"Name", "Model", "Serial", "Size"}}
	for _, d := range disks {
		table.Rows = append(table.Rows, []string{d.Name, cleanField(d.Model), cleanField(d.Serial), gb(d.Size)})
	}
	return table
}

func networkTable(interfaces []detailInterfaceJSON) provisioningdomain.DetailTable {
	table := provisioningdomain.DetailTable{Title: "Network", Columns: []string{"Name", "MAC", "Addresses"}}
	for _, iface := range interfaces {
		addrs := make([]string, 0, len(iface.Links))
		for _, link := range iface.Links {
			if link.IPAddress != "" {
				addrs = append(addrs, link.IPAddress)
			}
		}
		table.Rows = append(table.Rows, []string{iface.Name, iface.MACAddress, strings.Join(addrs, ", ")})
	}
	return table
}

func numaTable(nodes []numaNodeJSON) provisioningdomain.DetailTable {
	table := provisioningdomain.DetailTable{Title: "NUMA", Columns: []string{"Node", "Cores", "Memory"}}
	for _, n := range nodes {
		// The node index and core count are printed directly, not via intLabel: 0 is a
		// real NUMA node number, and blanking it would hide the first node entirely.
		table.Rows = append(table.Rows, []string{
			fmt.Sprintf("%d", n.Index),
			fmt.Sprintf("%d", len(n.Cores)),
			mib(n.Memory),
		})
	}
	return table
}

func pciTable(devices []nodeDeviceJSON) provisioningdomain.DetailTable {
	table := provisioningdomain.DetailTable{Title: "PCI devices", Columns: []string{"Address", "Type", "Vendor", "Product"}}
	sorted := append([]nodeDeviceJSON(nil), devices...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PCIAddress < sorted[j].PCIAddress })
	for _, d := range sorted {
		if d.BusName != "" && !strings.EqualFold(d.BusName, "PCIE") && !strings.EqualFold(d.BusName, "PCI") {
			continue
		}
		table.Rows = append(table.Rows, []string{d.PCIAddress, d.HardwareTypeName, cleanField(d.VendorName), cleanField(d.ProductName)})
	}
	return table
}

// dropEmptyFields removes fields whose value cleaned out to nothing, so a section shows
// only what MAAS actually knows rather than a column of blanks.
func dropEmptyFields(section provisioningdomain.DetailSection) provisioningdomain.DetailSection {
	kept := section.Fields[:0]
	for _, f := range section.Fields {
		if strings.TrimSpace(f.Value) != "" {
			kept = append(kept, f)
		}
	}
	section.Fields = kept
	return section
}

func appendNonEmptyTable(tables []provisioningdomain.DetailTable, table provisioningdomain.DetailTable) []provisioningdomain.DetailTable {
	if len(table.Rows) == 0 {
		return tables
	}
	return append(tables, table)
}

func nameOf(n *namedJSON) string {
	if n == nil {
		return ""
	}
	return n.Name
}

// powerParametersUnavailableStatus keeps the optional admin-only read from hiding all
// machine detail while making a permission or provider failure explicit to the operator.
func powerParametersUnavailableStatus(err error) string {
	var apiErr *apiError
	if errors.As(err, &apiErr) &&
		(apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		return "Unavailable to the configured integration credential"
	}
	return "Unavailable from MAAS"
}

// safePowerAddress keeps the useful BMC endpoint while removing URL userinfo, query
// parameters, and fragments that could carry credentials or access tokens.
func safePowerAddress(value string) string {
	address := cleanField(value)
	if address == "" {
		return ""
	}
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	address, _, _ = strings.Cut(address, "?")
	address, _, _ = strings.Cut(address, "#")
	if at := strings.LastIndexByte(address, '@'); at >= 0 {
		address = address[at+1:]
	}
	return address
}

func managementProtocol(powerType string) string {
	switch strings.ToLower(cleanField(powerType)) {
	case "ipmi":
		return "IPMI"
	case "redfish":
		return "Redfish"
	default:
		return cleanField(powerType)
	}
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func yesNo(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "yes"
	}
	return "no"
}

func intLabel(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

func mhz(speed int) string {
	if speed == 0 {
		return ""
	}
	return fmt.Sprintf("%d MHz", speed)
}

func mib(m int64) string {
	if m == 0 {
		return ""
	}
	return fmt.Sprintf("%d MiB", m)
}

func gb(bytes int64) string {
	if bytes == 0 {
		return ""
	}
	return fmt.Sprintf("%.0f GB", float64(bytes)/1e9)
}
