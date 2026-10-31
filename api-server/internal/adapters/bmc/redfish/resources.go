package redfish

import (
	"encoding/json"
	"strings"
)

// odataLink is a Redfish reference.
type odataLink struct {
	ID string `json:"@odata.id"`
}

// collection is any Redfish resource collection.
type collection struct {
	Members []odataLink `json:"Members"`
}

// serviceRoot is the subset of /redfish/v1 swallow reads.
type serviceRoot struct {
	RedfishVersion string          `json:"RedfishVersion"`
	Vendor         string          `json:"Vendor"`
	Product        string          `json:"Product"`
	Systems        odataLink       `json:"Systems"`
	Managers       odataLink       `json:"Managers"`
	Oem            json.RawMessage `json:"Oem"`
}

// vendor returns the service's vendor: the Vendor property (Redfish 1.5+), else the first Oem key
// (older AMI and Dell services name themselves only there).
func (r serviceRoot) vendor() string {
	if v := strings.TrimSpace(r.Vendor); v != "" {
		return v
	}
	var oem map[string]json.RawMessage
	if json.Unmarshal(r.Oem, &oem) == nil {
		for key := range oem {
			return key
		}
	}
	return ""
}

// action is one Redfish action entry.
type action struct {
	Target string `json:"target"`
}

// boot is ComputerSystem.Boot.
type boot struct {
	BootSourceOverrideEnabled        string    `json:"BootSourceOverrideEnabled"`
	BootSourceOverrideEnabledAllowed []string  `json:"BootSourceOverrideEnabled@Redfish.AllowableValues"`
	BootSourceOverrideTarget         string    `json:"BootSourceOverrideTarget"`
	BootSourceOverrideTargetAllowed  []string  `json:"BootSourceOverrideTarget@Redfish.AllowableValues"`
	BootSourceOverrideMode           string    `json:"BootSourceOverrideMode"`
	UefiTargetBootSourceOverride     string    `json:"UefiTargetBootSourceOverride"`
	BootNext                         string    `json:"BootNext"`
	BootOrder                        []string  `json:"BootOrder"`
	BootOptions                      odataLink `json:"BootOptions"`
}

// computerSystem is the subset of a ComputerSystem swallow reads.
type computerSystem struct {
	ODataID      string                     `json:"@odata.id"`
	ETag         string                     `json:"@odata.etag"`
	ID           string                     `json:"Id"`
	UUID         string                     `json:"UUID"`
	Manufacturer string                     `json:"Manufacturer"`
	Model        string                     `json:"Model"`
	PowerState   string                     `json:"PowerState"`
	Boot         *boot                      `json:"Boot"`
	Bios         *odataLink                 `json:"Bios"`
	VirtualMedia *odataLink                 `json:"VirtualMedia"`
	Actions      map[string]json.RawMessage `json:"Actions"`
	Links        struct {
		ManagedBy []odataLink `json:"ManagedBy"`
	} `json:"Links"`
}

// manager is the subset of a Manager swallow reads, including AMI's remote-media switch.
type manager struct {
	ODataID         string                     `json:"@odata.id"`
	FirmwareVersion string                     `json:"FirmwareVersion"`
	VirtualMedia    *odataLink                 `json:"VirtualMedia"`
	Actions         map[string]json.RawMessage `json:"Actions"`
	Oem             struct {
		Ami struct {
			VirtualMedia *struct {
				RMediaStatus string `json:"RMediaStatus"`
			} `json:"VirtualMedia"`
		} `json:"Ami"`
	} `json:"Oem"`
}

// virtualMedia is one VirtualMedia device.
type virtualMedia struct {
	ODataID    string   `json:"@odata.id"`
	ID         string   `json:"Id"`
	MediaTypes []string `json:"MediaTypes"`
	Image      string   `json:"Image"`
	ImageName  string   `json:"ImageName"`
	Inserted   *bool    `json:"Inserted"`
	Actions    struct {
		Insert *action `json:"#VirtualMedia.InsertMedia"`
		Eject  *action `json:"#VirtualMedia.EjectMedia"`
	} `json:"Actions"`
}

// isCD reports whether the device presents a CD or DVD to the host.
func (m virtualMedia) isCD() bool {
	for _, mediaType := range m.MediaTypes {
		switch strings.ToUpper(mediaType) {
		case "CD", "DVD":
			return true
		}
	}
	return false
}

// inserted reports the device's Inserted flag; a BMC that omits it is judged by its Image.
func (m virtualMedia) inserted() bool {
	if m.Inserted != nil {
		return *m.Inserted
	}
	return strings.TrimSpace(m.Image) != ""
}

// bootOption is one UEFI boot option.
type bootOption struct {
	ID                string `json:"Id"`
	BootOptionRef     string `json:"BootOptionReference"`
	DisplayName       string `json:"DisplayName"`
	UefiDevicePath    string `json:"UefiDevicePath"`
	BootOptionEnabled *bool  `json:"BootOptionEnabled"`
}

// reference is the BootOrder/BootNext name of the option ("Boot0009").
func (o bootOption) reference() string {
	if o.BootOptionRef != "" {
		return o.BootOptionRef
	}
	if strings.HasPrefix(strings.ToLower(o.ID), "boot") {
		return o.ID
	}
	return "Boot" + o.ID
}

// isVirtualCD reports whether the option boots a BMC virtual CD: its device path is a CD-ROM
// behind USB (how BMCs present virtual media) and its name says it is virtual.
func (o bootOption) isVirtualCD() bool {
	path := strings.ToUpper(o.UefiDevicePath)
	name := strings.ToUpper(o.DisplayName)
	return strings.Contains(path, "CDROM(") && strings.Contains(path, "USB(") && strings.Contains(name, "VIRTUAL")
}

// biosSettings is a Bios resource: the current attributes, or (the settings object) the
// attributes pending for the next POST.
type biosSettings struct {
	ODataID    string         `json:"@odata.id"`
	Attributes map[string]any `json:"Attributes"`
	Settings   *struct {
		SettingsObject odataLink `json:"SettingsObject"`
	} `json:"@Redfish.Settings"`
}

// task is a Redfish Task.
type task struct {
	TaskState  string           `json:"TaskState"`
	TaskStatus string           `json:"TaskStatus"`
	Messages   []redfishMessage `json:"Messages"`
}

// terminal reports whether the task will not change again.
func (t task) terminal() bool {
	switch t.TaskState {
	case "Completed", "Exception", "Killed", "Cancelled", "Interrupted":
		return true
	}
	return false
}

// lastMessage returns the task's final message text.
func (t task) lastMessage() string {
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if message := strings.TrimSpace(t.Messages[i].Message); message != "" {
			return message
		}
	}
	return ""
}

// oemActionTarget returns the target of a vendor action (for example
// "#AMIVirtualMedia.EnableRMedia") listed under Actions.Oem, or "".
func oemActionTarget(actions map[string]json.RawMessage, name string) string {
	raw, ok := actions["Oem"]
	if !ok {
		return ""
	}
	var oem map[string]action
	if json.Unmarshal(raw, &oem) != nil {
		return ""
	}
	return oem[name].Target
}

// actionTarget returns the target of a standard action listed directly under Actions.
func actionTarget(actions map[string]json.RawMessage, name string) string {
	raw, ok := actions[name]
	if !ok {
		return ""
	}
	var a action
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.Target
}
