package maas

import (
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// machineJSON is the subset of a MAAS machine object gdcm reads. MAAS returns far
// more (interfaces, block devices, power parameters); anything not listed here is
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
	HardwareUUID string            `json:"hardware_uuid"`
	HardwareInfo *hardwareInfoJSON `json:"hardware_info"`
	InterfaceSet []interfaceJSON   `json:"interface_set"`
}

// namedJSON covers the MAAS objects gdcm only needs a name from.
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

type interfaceJSON struct {
	MACAddress string `json:"mac_address"`
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
// so that gdcm collapses lifecycle states the same way the MAAS UI does. The three
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

// toDomainOSImages converts MAAS boot resources into deployable images.
//
// MAAS reports one boot resource per name and architecture, where the architecture
// carries a kernel flavour ("amd64/hwe-22.04"). gdcm only needs the CPU
// architecture, so entries collapse to one image per name and CPU architecture.
// The resource name ("ubuntu/jammy") is kept as the image ID because that is the
// value the deploy operation expects back as distro_series.
func toDomainOSImages(resources []bootResourceJSON) []*provisioningdomain.OSImage {
	images := make([]*provisioningdomain.OSImage, 0, len(resources))
	seen := make(map[string]struct{}, len(resources))

	for _, r := range resources {
		osSystem, release, found := strings.Cut(r.Name, "/")
		if !found || osSystem == "" || release == "" {
			// Not an OS/release pair; nothing deployable can be built from it.
			continue
		}

		arch, _, _ := strings.Cut(r.Architecture, "/")

		key := r.Name + "|" + arch
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		name := r.Title
		if name == "" {
			name = r.Name
		}

		images = append(images, &provisioningdomain.OSImage{
			ID:           r.Name,
			Name:         name,
			OSSystem:     osSystem,
			Release:      release,
			Architecture: arch,
		})
	}

	return images
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

// matchesFilter applies the domain filter in gdcm, because the MAAS machines
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
