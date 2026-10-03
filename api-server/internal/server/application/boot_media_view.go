package application

import (
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// BootMediaItem is the published shape of a Server's Boot Media (contract
// server-detail-actions.md, decision 047). Setting, Redfish, and Live are null when never set,
// never probed, or not read; that distinction is part of the contract.
type BootMediaItem struct {
	ServerID  string                  `json:"serverId"`
	Image     BootMediaImageItem      `json:"image"`
	Setting   *BootMediaSettingItem   `json:"setting"`
	Redfish   *RedfishCapabilityItem  `json:"redfish"`
	Live      *BootMediaLiveStateItem `json:"live"`
	LiveError string                  `json:"liveError,omitempty"`
}

// BootMediaImageItem is the installation's ISO: its fixed URL and whether it is served.
type BootMediaImageItem struct {
	URL       string `json:"url"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// BootMediaSettingItem is the swallow-owned setting with the last apply's outcome.
type BootMediaSettingItem struct {
	Enabled       bool    `json:"enabled"`
	UpdatedAt     string  `json:"updatedAt"`
	LastAppliedAt *string `json:"lastAppliedAt"`
	LastAppliedBy string  `json:"lastAppliedBy,omitempty"`
	BootOverride  string  `json:"bootOverride,omitempty"`
	LastError     string  `json:"lastError,omitempty"`
	LastErrorAt   *string `json:"lastErrorAt"`
}

// RedfishCapabilityItem is the latest Redfish capability probe. It never carries a credential.
type RedfishCapabilityItem struct {
	Support           string   `json:"support"`
	Reason            string   `json:"reason,omitempty"`
	ServiceRoot       string   `json:"serviceRoot,omitempty"`
	Vendor            string   `json:"vendor,omitempty"`
	Product           string   `json:"product,omitempty"`
	RedfishVersion    string   `json:"redfishVersion,omitempty"`
	FirmwareVersion   string   `json:"firmwareVersion,omitempty"`
	SystemID          string   `json:"systemId,omitempty"`
	VirtualMedia      bool     `json:"virtualMedia"`
	BootOverrideModes []string `json:"bootOverrideModes"`
	ProbedAt          string   `json:"probedAt"`
}

// BootMediaLiveStateItem is the BMC's live state, read for this response only.
type BootMediaLiveStateItem struct {
	MediaInserted   bool   `json:"mediaInserted"`
	MediaImage      string `json:"mediaImage,omitempty"`
	OverrideEnabled string `json:"overrideEnabled,omitempty"`
	OverrideTarget  string `json:"overrideTarget,omitempty"`
	Ready           bool   `json:"ready"`
}

// ToBootMediaItem maps a BootMediaView onto its published shape.
func ToBootMediaItem(view *BootMediaView) BootMediaItem {
	item := BootMediaItem{
		ServerID:  view.Server.ID,
		Image:     BootMediaImageItem{URL: view.ImageURL, Available: view.ImageAvailable, Reason: view.ImageReason},
		Redfish:   ToRedfishCapabilityItem(view.Server.Redfish),
		LiveError: view.LiveError,
	}
	if s := view.Server.BootMedia; s != nil {
		item.Setting = &BootMediaSettingItem{
			Enabled: s.Enabled, UpdatedAt: wire.Time(s.UpdatedAt),
			LastAppliedBy: string(s.LastAppliedBy), BootOverride: s.BootOverride, LastError: s.LastError,
		}
		if s.LastAppliedAt != nil {
			item.Setting.LastAppliedAt = wire.TimePtr(*s.LastAppliedAt)
		}
		if s.LastErrorAt != nil {
			item.Setting.LastErrorAt = wire.TimePtr(*s.LastErrorAt)
		}
	}
	if live := view.Live; live != nil {
		item.Live = &BootMediaLiveStateItem{
			MediaInserted: live.MediaInserted, MediaImage: live.MediaImage,
			OverrideEnabled: live.OverrideEnabled, OverrideTarget: live.OverrideTarget, Ready: live.Ready(),
		}
	}
	return item
}

// ToRedfishCapabilityItem maps a capability onto its published shape, or nil when never probed.
func ToRedfishCapabilityItem(c *serverdomain.RedfishCapability) *RedfishCapabilityItem {
	if c == nil {
		return nil
	}
	return &RedfishCapabilityItem{
		Support: string(c.Support), Reason: c.Reason, ServiceRoot: c.ServiceRoot,
		Vendor: c.Vendor, Product: c.Product, RedfishVersion: c.RedfishVersion, FirmwareVersion: c.FirmwareVersion,
		SystemID: c.SystemID, VirtualMedia: c.VirtualMedia, BootOverrideModes: wire.Strings(c.BootOverrideModes),
		ProbedAt: wire.Time(c.ProbedAt),
	}
}
