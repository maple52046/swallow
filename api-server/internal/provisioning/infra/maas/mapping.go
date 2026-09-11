package maas

import (
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// machineJSON is the subset of a MAAS machine object swallow reads. MAAS returns far
// more (block devices, NUMA topology, power parameters); anything not listed here is
// deliberately ignored.
type machineJSON struct {
	SystemID     string     `json:"system_id"`
	Hostname     string     `json:"hostname"`
	FQDN         string     `json:"fqdn"`
	Status       int        `json:"status"`
	StatusName   string     `json:"status_name"`
	Architecture string     `json:"architecture"`
	CPUCount     int        `json:"cpu_count"`
	Memory       int64      `json:"memory"`
	Storage      float64    `json:"storage"`
	PowerState   string     `json:"power_state"`
	OSystem      string     `json:"osystem"`
	DistroSeries string     `json:"distro_series"`
	IPAddresses  []string   `json:"ip_addresses"`
	TagNames     []string   `json:"tag_names"`
	Zone         *namedJSON `json:"zone"`
	Pool         *namedJSON `json:"pool"`
	Pod          *namedJSON `json:"pod"`
	Locked       bool       `json:"locked"`

	CommissioningStatusName string `json:"commissioning_status_name"`
	TestingStatusName       string `json:"testing_status_name"`

	// EphemeralDeploy is a pointer so that "MAAS did not report this" stays
	// distinguishable from "MAAS reported false". The difference matters when
	// verifying that a requested ephemeral deployment was actually honoured: silence
	// from a version that predates the field is not evidence either way, whereas an
	// explicit false is evidence that the request was ignored.
	EphemeralDeploy *bool  `json:"ephemeral_deploy"`
	HWEKernel       string `json:"hwe_kernel"`

	// Hardware identity. MAAS populates these from DMI during commissioning, so an
	// uncommissioned machine reports none of them and older MAAS versions omit
	// hardware_uuid entirely. All three are therefore optional.
	HardwareUUID    string             `json:"hardware_uuid"`
	HardwareInfo    *hardwareInfoJSON  `json:"hardware_info"`
	InterfaceSet    []interfaceJSON    `json:"interface_set"`
	BootInterface   *interfaceJSON     `json:"boot_interface"`
	GatewayLinkIPv4 *interfaceLinkJSON `json:"gateway_link_ipv4"`
}

// namedJSON covers the MAAS objects swallow only needs a name from.
type namedJSON struct {
	Name string `json:"name"`
}

// hardwareInfoJSON is MAAS's DMI-derived hardware description. Every field is a plain
// string that may be a placeholder like "Unknown"; callers decide what to keep.
type hardwareInfoJSON struct {
	SystemVendor  string `json:"system_vendor"`
	SystemProduct string `json:"system_product"`
	SystemSerial  string `json:"system_serial"`
	CPUModel      string `json:"cpu_model"`

	MainboardVendor          string `json:"mainboard_vendor"`
	MainboardProduct         string `json:"mainboard_product"`
	MainboardFirmwareVendor  string `json:"mainboard_firmware_vendor"`
	MainboardFirmwareVersion string `json:"mainboard_firmware_version"`
	MainboardFirmwareDate    string `json:"mainboard_firmware_date"`
	ChassisVendor            string `json:"chassis_vendor"`
	ChassisType              string `json:"chassis_type"`
}

// interfaceJSON is the MAAS network shape translated by network.go. IDs remain
// opaque outside the adapter even though MAAS serializes them as integers.
type interfaceJSON struct {
	ID            int                 `json:"id"`
	Name          string              `json:"name"`
	Type          string              `json:"type"`
	MACAddress    string              `json:"mac_address"`
	Enabled       bool                `json:"enabled"`
	LinkConnected *bool               `json:"link_connected"`
	VLAN          *vlanJSON           `json:"vlan"`
	Links         []interfaceLinkJSON `json:"links"`
}

// interfaceLinkJSON retains MAAS vocabulary only until the network adapter maps it.
type interfaceLinkJSON struct {
	ID        int         `json:"id"`
	Mode      string      `json:"mode"`
	IPAddress string      `json:"ip_address"`
	Subnet    *subnetJSON `json:"subnet"`
}

type vlanJSON struct {
	ID int `json:"id"`
}

type subnetJSON struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	GatewayIP string    `json:"gateway_ip"`
	Managed   bool      `json:"managed"`
	VLAN      *vlanJSON `json:"vlan"`
}

type bootResourceJSON struct {
	ID           int    `json:"id"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Architecture string `json:"architecture"`
}

type versionJSON struct {
	Version    string `json:"version"`
	Subversion string `json:"subversion"`
}

// maasStatusToDomain maps MAAS NODE_STATUS codes onto normalized machine statuses.
//
// The grouping follows MAAS's own SIMPLIFIED_NODE_STATUSES_MAP (src/maasserver/enum.py)
// so that swallow collapses lifecycle states the same way the MAAS UI does. The three
// codes MAAS leaves out of that map — MISSING, RESERVED and RETIRED — are mapped
// explicitly rather than to "unknown", because each has a clear equivalent.
//
// Mapping keys on the numeric code because that is MAAS's stable documented value;
// status_name is a display label and is carried separately as ProviderStatus.
var maasStatusToDomain = map[int]provisioningdomain.MachineStatus{
	0:  provisioningdomain.MachineStatusNew,           // NEW
	1:  provisioningdomain.MachineStatusCommissioning, // COMMISSIONING
	2:  provisioningdomain.MachineStatusFailed,        // FAILED_COMMISSIONING
	3:  provisioningdomain.MachineStatusBroken,        // MISSING: MAAS cannot contact it
	4:  provisioningdomain.MachineStatusReady,         // READY
	5:  provisioningdomain.MachineStatusAllocated,     // RESERVED: held for a named deployment
	6:  provisioningdomain.MachineStatusDeployed,      // DEPLOYED
	7:  provisioningdomain.MachineStatusRetired,       // RETIRED
	8:  provisioningdomain.MachineStatusBroken,        // BROKEN
	9:  provisioningdomain.MachineStatusDeploying,     // DEPLOYING
	10: provisioningdomain.MachineStatusAllocated,     // ALLOCATED
	11: provisioningdomain.MachineStatusFailed,        // FAILED_DEPLOYMENT
	12: provisioningdomain.MachineStatusReleasing,     // RELEASING
	13: provisioningdomain.MachineStatusFailed,        // FAILED_RELEASING
	14: provisioningdomain.MachineStatusReleasing,     // DISK_ERASING
	15: provisioningdomain.MachineStatusFailed,        // FAILED_DISK_ERASING
	16: provisioningdomain.MachineStatusRescue,        // RESCUE_MODE
	17: provisioningdomain.MachineStatusRescue,        // ENTERING_RESCUE_MODE
	18: provisioningdomain.MachineStatusFailed,        // FAILED_ENTERING_RESCUE_MODE
	19: provisioningdomain.MachineStatusRescue,        // EXITING_RESCUE_MODE
	20: provisioningdomain.MachineStatusFailed,        // FAILED_EXITING_RESCUE_MODE
	21: provisioningdomain.MachineStatusTesting,       // TESTING
	22: provisioningdomain.MachineStatusFailed,        // FAILED_TESTING
}

var maasPowerStateToDomain = map[string]provisioningdomain.PowerState{
	"on":    provisioningdomain.PowerStateOn,
	"off":   provisioningdomain.PowerStateOff,
	"error": provisioningdomain.PowerStateError,
}

// bytesPerMBToGB converts the decimal megabytes MAAS reports for storage into the
// gigabytes the MAAS UI displays.
const bytesPerMBToGB = 1000

func toDomainMachine(m *machineJSON) *provisioningdomain.Machine {
	status, ok := maasStatusToDomain[m.Status]
	if !ok {
		status = provisioningdomain.MachineStatusUnknown
	}

	powerState, ok := maasPowerStateToDomain[strings.ToLower(m.PowerState)]
	if !ok {
		powerState = provisioningdomain.PowerStateUnknown
	}

	machine := &provisioningdomain.Machine{
		ID:                  m.SystemID,
		Hostname:            m.Hostname,
		FQDN:                m.FQDN,
		Status:              status,
		ProviderStatus:      m.StatusName,
		PowerState:          powerState,
		Architecture:        m.Architecture,
		CPUCores:            m.CPUCount,
		MemoryMiB:           m.Memory,
		StorageGB:           m.Storage / bytesPerMBToGB,
		OSSystem:            m.OSystem,
		DistroSeries:        m.DistroSeries,
		HWEKernel:           m.HWEKernel,
		Locked:              m.Locked,
		CommissioningStatus: m.CommissioningStatusName,
		TestingStatus:       m.TestingStatusName,
		IPAddresses:         m.IPAddresses,
		Tags:                m.TagNames,
	}

	if m.EphemeralDeploy != nil {
		machine.Ephemeral = *m.EphemeralDeploy
	}

	if m.Zone != nil {
		machine.Zone = m.Zone.Name
	}
	if m.Pool != nil {
		machine.ResourcePool = m.Pool.Name
	}
	if m.Pod != nil {
		machine.Pod = m.Pod.Name
	}

	machine.SystemUUID = m.HardwareUUID
	if hw := m.HardwareInfo; hw != nil {
		machine.SerialNumber = hw.SystemSerial
		// Placeholders like "Unknown" are dropped: MAAS reports them for every field a
		// vendor left blank, and mirroring them as if they were data makes a fleet look
		// as though it were all one make. See serverdomain hardware normalization.
		machine.SystemVendor = cleanField(hw.SystemVendor)
		machine.SystemProduct = cleanField(hw.SystemProduct)
		machine.CPUModel = cleanField(hw.CPUModel)
	}
	for _, iface := range m.InterfaceSet {
		// Skip blanks rather than carrying them: an empty identifier would match
		// every machine that also lacks one.
		if iface.MACAddress != "" {
			machine.MACAddresses = append(machine.MACAddresses, iface.MACAddress)
		}
	}

	return machine
}

// isMAASBootloaderOSSystem identifies bootstrap artifacts returned by the MAAS
// boot-resources endpoint alongside deployable operating systems. Passing one
// of these names to machine deploy would offer firmware plumbing as an OS.
func isMAASBootloaderOSSystem(osSystem string) bool {
	switch osSystem {
	case "bootloader", "grub-efi", "grub-efi-signed", "grub-ieee1275", "pxelinux":
		return true
	default:
		return false
	}
}

// toDomainOSImages converts MAAS boot resources into deployable images.
//
// MAAS reports one boot resource per name and architecture, where the architecture
// carries a kernel flavour ("amd64/hwe-22.04"). swallow only needs the CPU
// architecture, so entries collapse to one image per name and CPU architecture.
// Synced images derive their OS and release from names such as "ubuntu/jammy".
// Uploaded images are MAAS custom images: their opaque resource name remains the
// distro_series while the adapter supplies the required "custom" osystem.
func toDomainOSImages(resources []bootResourceJSON) []*provisioningdomain.OSImage {
	images := make([]*provisioningdomain.OSImage, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))

	for _, r := range resources {
		imageID := strings.TrimSpace(r.Name)
		if imageID == "" {
			continue
		}

		var osSystem, release string
		if strings.EqualFold(r.Type, "Uploaded") {
			osSystem = "custom"
			release = strings.TrimPrefix(imageID, "custom/")
		} else {
			var found bool
			osSystem, release, found = strings.Cut(imageID, "/")
			if !found || osSystem == "" || release == "" || isMAASBootloaderOSSystem(osSystem) {
				continue
			}
		}
		if release == "" {
			continue
		}

		arch, _, _ := strings.Cut(r.Architecture, "/")

		key := imageID + "|" + arch
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		name := r.Title
		if name == "" {
			name = imageID
		}

		images = append(images, &provisioningdomain.OSImage{
			ID:           imageID,
			Name:         name,
			OSSystem:     osSystem,
			Release:      release,
			Architecture: arch,
		})
	}

	return images
}

// deletableBootResourceIDs returns the numeric ids of the uploaded (custom) boot resources
// whose name and CPU architecture match the catalog image. It mirrors toDomainOSImages'
// identity — the resource name is the image ID and only its CPU architecture is compared —
// so a catalog row maps back to exactly the resources that back it. Synced resources are
// excluded: they are provider-owned mirrors MAAS would re-sync after deletion.
func deletableBootResourceIDs(resources []bootResourceJSON, imageID, architecture string) []int {
	var ids []int
	for _, r := range resources {
		if !strings.EqualFold(r.Type, "Uploaded") {
			continue
		}
		if strings.TrimSpace(r.Name) != imageID {
			continue
		}
		arch, _, _ := strings.Cut(r.Architecture, "/")
		if arch != architecture {
			continue
		}
		ids = append(ids, r.ID)
	}
	return ids
}

// maasPlaceholders are the strings MAAS fills a DMI field with when the vendor left it
// blank. They are not identity and not data, so they are dropped from display fields.
var maasPlaceholders = map[string]bool{
	"unknown":        true,
	"":               true,
	"not specified":  true,
	"default string": true,
	"none":           true,
	"n/a":            true,
}

// cleanField blanks a MAAS placeholder so a mirrored hardware field is either real or
// empty, never the literal "Unknown".
func cleanField(value string) string {
	trimmed := strings.TrimSpace(value)
	if maasPlaceholders[strings.ToLower(trimmed)] {
		return ""
	}
	return trimmed
}

// matchesFilter applies the domain filter in swallow, because the MAAS machines
// endpoint has no equivalent free-text or normalized-status query.
func matchesFilter(m *provisioningdomain.Machine, filter provisioningdomain.MachineFilter) bool {
	if filter.Status != "" && m.Status != filter.Status {
		return false
	}
	if filter.Keyword != "" {
		keyword := strings.ToLower(filter.Keyword)
		if !strings.Contains(strings.ToLower(m.Hostname), keyword) &&
			!strings.Contains(strings.ToLower(m.FQDN), keyword) {
			return false
		}
	}
	return true
}
