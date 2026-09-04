package application

import (
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ServerItem is the API representation of a server projection.
//
// Optional fields are pointers so that "not observed" serializes as null. That
// distinction is load-bearing here: a server with no hostname is normal before
// commissioning, and an axis that is null has never been observed rather than being
// in some default state.
type ServerItem struct {
	ID     string           `json:"id"`
	Source ServerSourceItem `json:"source"`

	Hostname             *string         `json:"hostname"`
	FQDN                 *string         `json:"fqdn"`
	Addresses            []string        `json:"addresses"`
	Architecture         string          `json:"architecture"`
	CPUCores             int             `json:"cpuCores"`
	CPUModel             string          `json:"cpuModel"`
	MemoryMiB            int64           `json:"memoryMiB"`
	StorageGB            float64         `json:"storageGB"`
	GPUs                 []ServerGPUItem `json:"gpus"`
	SystemVendor         string          `json:"systemVendor"`
	SystemProduct        string          `json:"systemProduct"`
	ProviderZone         string          `json:"providerZone"`
	ProviderResourcePool string          `json:"providerResourcePool"`
	ProviderPod          string          `json:"providerPod"`
	Tags                 []string        `json:"tags"`

	Hardware ServerHardwareItem `json:"hardware"`

	Provisioning *ProvisioningAxisItem `json:"provisioning"`
	Membership   *MembershipAxisItem   `json:"membership"`
	Health       *HealthAxisItem       `json:"health"`

	Absent     bool    `json:"absent"`
	LastSeenAt *string `json:"lastSeenAt"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ServerSourceItem struct {
	SiteID            string `json:"siteId"`
	IntegrationID     string `json:"integrationId"`
	ProviderMachineID string `json:"providerMachineId"`
}

type ServerHardwareItem struct {
	SystemUUID   *string  `json:"systemUuid"`
	SerialNumber *string  `json:"serialNumber"`
	MACAddresses []string `json:"macAddresses"`
}

type ServerGPUItem struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Count  int    `json:"count"`
}

type ProvisioningAxisItem struct {
	State         string `json:"state"`
	ProviderState string `json:"providerState"`
	PowerState    string `json:"powerState"`
	OSSystem      string `json:"osSystem"`
	DistroSeries  string `json:"distroSeries"`
	// Ephemeral means the deployed OS runs from memory: anything written to it is
	// lost on reboot. Clients must show it, because no other field distinguishes such
	// a machine from one with the same OS installed on disk.
	Ephemeral           bool   `json:"ephemeral"`
	HWEKernel           string `json:"hweKernel"`
	Locked              bool   `json:"locked"`
	CommissioningStatus string `json:"commissioningStatus"`
	TestingStatus       string `json:"testingStatus"`
	IntegrationID       string `json:"integrationId"`
	ObservedAt          string `json:"observedAt"`
}

// MembershipAxisItem is the platform-membership projection of a Server. PlatformID
// is canonical; ClusterID mirrors it as the deprecated one-release alias so existing
// clients keep working during the Cluster -> Platform migration.
type MembershipAxisItem struct {
	PlatformID string `json:"platformId"`
	ClusterID  string `json:"clusterId"`
	NodeName   string `json:"nodeName"`
	Role       string `json:"role"`
	State      string `json:"state"`
	ObservedAt string `json:"observedAt"`
}

type HealthAxisItem struct {
	State      string `json:"state"`
	ObservedAt string `json:"observedAt"`
}

// ToServerItem maps a server projection onto its API shape.
func ToServerItem(s *serverdomain.Server) ServerItem {
	item := ServerItem{
		ID: s.ID,
		Source: ServerSourceItem{
			SiteID:            s.Source.SiteID,
			IntegrationID:     s.Source.IntegrationID,
			ProviderMachineID: s.Source.ProviderMachineID,
		},
		Hostname:             wire.String(s.Observed.Hostname),
		FQDN:                 wire.String(s.Observed.FQDN),
		Addresses:            wire.Strings(s.Observed.Addresses),
		Architecture:         s.Observed.Architecture,
		CPUCores:             s.Observed.CPUCores,
		CPUModel:             s.Observed.CPUModel,
		MemoryMiB:            s.Observed.MemoryMiB,
		StorageGB:            s.Observed.StorageGB,
		GPUs:                 make([]ServerGPUItem, 0, len(s.Observed.GPUs)),
		SystemVendor:         s.Observed.SystemVendor,
		SystemProduct:        s.Observed.SystemProduct,
		ProviderZone:         s.Observed.ProviderZone,
		ProviderResourcePool: s.Observed.ProviderResourcePool,
		ProviderPod:          s.Observed.ProviderPod,
		Tags:                 wire.Strings(s.Observed.Tags),
		Hardware: ServerHardwareItem{
			SystemUUID:   wire.String(s.Hardware.SystemUUID),
			SerialNumber: wire.String(s.Hardware.SerialNumber),
			MACAddresses: wire.Strings(s.Hardware.MACAddresses),
		},
		Absent:     s.Absent,
		LastSeenAt: wire.TimePtr(s.LastSeenAt),
		CreatedAt:  wire.Time(s.CreatedAt),
		UpdatedAt:  wire.Time(s.UpdatedAt),
	}

	for _, gpu := range s.Observed.GPUs {
		item.GPUs = append(item.GPUs, ServerGPUItem{
			Vendor: gpu.Vendor,
			Model:  gpu.Model,
			Count:  gpu.Count,
		})
	}

	if p := s.Provisioning; p != nil {
		item.Provisioning = &ProvisioningAxisItem{
			State:               p.State,
			ProviderState:       p.ProviderState,
			PowerState:          p.PowerState,
			OSSystem:            p.OSSystem,
			DistroSeries:        p.DistroSeries,
			Ephemeral:           p.Ephemeral,
			HWEKernel:           p.HWEKernel,
			Locked:              p.Locked,
			CommissioningStatus: p.CommissioningStatus,
			TestingStatus:       p.TestingStatus,
			IntegrationID:       p.IntegrationID,
			ObservedAt:          wire.Time(p.ObservedAt),
		}
	}

	if m := s.Membership; m != nil {
		item.Membership = &MembershipAxisItem{
			PlatformID: m.PlatformID,
			ClusterID:  m.PlatformID,
			NodeName:   m.NodeName,
			Role:       m.Role,
			State:      m.State,
			ObservedAt: wire.Time(m.ObservedAt),
		}
	}

	if h := s.Health; h != nil {
		item.Health = &HealthAxisItem{
			State:      h.State,
			ObservedAt: wire.Time(h.ObservedAt),
		}
	}

	return item
}
