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

	Deployment   *DeploymentAxisItem   `json:"deployment"`
	Provisioning *ProvisioningAxisItem `json:"provisioning"`
	Membership   *MembershipAxisItem   `json:"membership"`
	Health       *HealthAxisItem       `json:"health"`

	// DefaultUser is the effective Server Default User (decision 045), omitted when unknown. It is
	// what automation logs in as, so a client showing an SSH login or a docker-group account must
	// read this rather than provisioning.deployedImageDefaultUser.
	DefaultUser *DefaultUserItem `json:"defaultUser,omitempty"`

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

type DeploymentAxisItem struct {
	State       string `json:"state"`
	OperationID string `json:"operationId"`
	StepID      string `json:"stepId"`
	Attempt     int    `json:"attempt"`
	// Code is the failed Step's stable error code, so a client can render a concise root
	// cause without parsing StatusReason. Empty for a non-failed deployment.
	Code         string  `json:"code"`
	Stage        string  `json:"stage"`
	StatusReason string  `json:"statusReason"`
	StartedAt    string  `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

type ProvisioningAxisItem struct {
	// State is the swallow-defined OS Provisioning State (servers-list.md, decision 048) that
	// clients branch on; ProviderState is the provider's own label and is display only.
	State         string `json:"state"`
	ProviderState string `json:"providerState"`
	// ErrorDescription is the provisioner's own machine-level failure reason, present only for
	// failure states and omitted otherwise. Display and diagnostics only; it lets a client show
	// why a lifecycle action failed (e.g. "Failed to erase disks.") without a separate event read.
	ErrorDescription string `json:"errorDescription,omitempty"`
	PowerState       string `json:"powerState"`
	OSSystem         string `json:"osSystem"`
	DistroSeries     string `json:"distroSeries"`
	// DeployedImageName is the effective display name of the deployed OS image (provider
	// catalog name overlaid with any swallow custom name), mirrored during reconcile. Empty
	// when nothing is deployed or the image could not be resolved from the catalog.
	DeployedImageName string `json:"deployedImageName"`
	// DeployedImageDefaultUser is the deployed image's effective default login user, the account
	// swallow automation logs in as (decision 039). Omitted when unknown.
	DeployedImageDefaultUser string `json:"deployedImageDefaultUser,omitempty"`
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

// DefaultUserItem is the published shape of the effective Server Default User: the account and
// whether it was set on the Server (`server`) or comes from the deployed OS Image (`os_image`).
type DefaultUserItem struct {
	User   string `json:"user"`
	Source string `json:"source"`
}

// ToDefaultUserItem maps the Server's effective default user, or nil when none is known.
func ToDefaultUserItem(s *serverdomain.Server) *DefaultUserItem {
	user, source := s.EffectiveDefaultUser()
	if user == "" {
		return nil
	}
	return &DefaultUserItem{User: user, Source: string(source)}
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
		DefaultUser: ToDefaultUserItem(s),
		Absent:      s.Absent,
		LastSeenAt:  wire.TimePtr(s.LastSeenAt),
		CreatedAt:   wire.Time(s.CreatedAt),
		UpdatedAt:   wire.Time(s.UpdatedAt),
	}

	for _, gpu := range s.Observed.GPUs {
		item.GPUs = append(item.GPUs, ServerGPUItem{
			Vendor: gpu.Vendor,
			Model:  gpu.Model,
			Count:  gpu.Count,
		})
	}

	if d := s.Deployment; d != nil {
		var finishedAt *string
		if d.FinishedAt != nil {
			finishedAt = wire.TimePtr(*d.FinishedAt)
		}
		item.Deployment = &DeploymentAxisItem{
			State:        string(d.State),
			OperationID:  d.OperationID,
			StepID:       d.StepID,
			Attempt:      d.Attempt,
			Code:         d.Code,
			Stage:        d.Stage,
			StatusReason: d.StatusReason,
			StartedAt:    wire.Time(d.StartedAt),
			FinishedAt:   finishedAt,
			UpdatedAt:    wire.Time(d.UpdatedAt),
		}
	}

	if p := s.Provisioning; p != nil {
		item.Provisioning = &ProvisioningAxisItem{
			State:                    p.State,
			ProviderState:            p.ProviderState,
			ErrorDescription:         p.ErrorDescription,
			PowerState:               p.PowerState,
			OSSystem:                 p.OSSystem,
			DistroSeries:             p.DistroSeries,
			DeployedImageName:        p.DeployedImageName,
			DeployedImageDefaultUser: p.DeployedImageDefaultUser,
			Ephemeral:                p.Ephemeral,
			HWEKernel:                p.HWEKernel,
			Locked:                   p.Locked,
			CommissioningStatus:      p.CommissioningStatus,
			TestingStatus:            p.TestingStatus,
			IntegrationID:            p.IntegrationID,
			ObservedAt:               wire.Time(p.ObservedAt),
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
