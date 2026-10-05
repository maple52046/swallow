package application

import (
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// BootMediaItem is the published shape of a Server's Boot Media (contract
// server-detail-actions.md, decisions 047 and 049). Image, Setting, Redfish, Live, and Apply are
// null when no Boot ISO is named, never set, never probed, not read, or no preflight runs; that
// distinction is part of the contract.
type BootMediaItem struct {
	ServerID  string                  `json:"serverId"`
	Image     *BootMediaImageItem     `json:"image"`
	Setting   *BootMediaSettingItem   `json:"setting"`
	Redfish   *RedfishCapabilityItem  `json:"redfish"`
	Live      *BootMediaLiveStateItem `json:"live"`
	LiveError string                  `json:"liveError,omitempty"`
	Apply     *BootMediaApplyItem     `json:"apply"`
}

// BootMediaApplyItem is the running enable preflight: which Boot ISO, which phase, and since when.
// PhaseEndsAt is null unless the phase has a known end.
type BootMediaApplyItem struct {
	ISOID          string  `json:"isoId"`
	Phase          string  `json:"phase"`
	StartedAt      string  `json:"startedAt"`
	PhaseStartedAt string  `json:"phaseStartedAt"`
	PhaseEndsAt    *string `json:"phaseEndsAt"`
}

// BootMediaImageItem is the Boot ISO the setting names, its URL, and whether it is served.
type BootMediaImageItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// BootMediaSettingItem is the swallow-owned setting with the last apply's outcome.
type BootMediaSettingItem struct {
	Enabled bool `json:"enabled"`
	// ISOID is the chosen Boot ISO, null when none is (a setting enabled before Boot ISOs).
	ISOID         *string `json:"isoId"`
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
		Redfish:   ToRedfishCapabilityItem(view.Server.Redfish),
		LiveError: view.LiveError,
	}
	if image := view.Image; image != nil {
		item.Image = &BootMediaImageItem{ID: image.ID, Name: image.Name, URL: image.URL, Available: image.Available, Reason: image.Reason}
	}
	if s := view.Server.BootMedia; s != nil {
		item.Setting = &BootMediaSettingItem{
			Enabled: s.Enabled, ISOID: wire.String(s.ISOID), UpdatedAt: wire.Time(s.UpdatedAt),
			LastAppliedBy: string(s.LastAppliedBy), BootOverride: s.BootOverride, LastError: s.LastError,
		}
		if s.LastAppliedAt != nil {
			item.Setting.LastAppliedAt = wire.TimePtr(*s.LastAppliedAt)
		}
		if s.LastErrorAt != nil {
			item.Setting.LastErrorAt = wire.TimePtr(*s.LastErrorAt)
		}
	}
	if apply := view.Apply; apply != nil {
		item.Apply = &BootMediaApplyItem{
			ISOID: apply.ISOID, Phase: string(apply.Phase),
			StartedAt: wire.Time(apply.StartedAt), PhaseStartedAt: wire.Time(apply.PhaseStartedAt),
		}
		if apply.PhaseEndsAt != nil {
			item.Apply.PhaseEndsAt = wire.TimePtr(*apply.PhaseEndsAt)
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
