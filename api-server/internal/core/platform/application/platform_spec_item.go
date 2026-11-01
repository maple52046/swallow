package application

import (
	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// PlatformSpecItem is the wire shape of a Platform spec (platforms.md, "Spec and status"). The
// settings are the type's own fields; the machine preparation has the shape a spec is written in,
// except that inline user data is never returned, only its mode.
type PlatformSpecItem struct {
	Desired            string                  `json:"desired"`
	Members            []PlatformMemberItem    `json:"members"`
	Settings           map[string]any          `json:"settings"`
	MachinePreparation *MachinePreparationItem `json:"machinePreparation"`
	Removal            *PlatformRemovalItem    `json:"removal"`
}

// PlatformMemberItem is one member of a spec: a Server and its roles in the type's vocabulary.
type PlatformMemberItem struct {
	ServerID string   `json:"serverId"`
	Roles    []string `json:"roles"`
}

// MachinePreparationItem is how members that are not deployed get their operating system.
type MachinePreparationItem struct {
	Mode       string                         `json:"mode"`
	TemplateID string                         `json:"templateId,omitempty"`
	Settings   MachinePreparationSettingsItem `json:"settings"`
	UserData   MachinePreparationUserDataItem `json:"userData"`
	Network    *MachinePreparationNetworkItem `json:"network,omitempty"`
}

// MachinePreparationSettingsItem names the OS image and the deploy target ("disk" or "ram").
type MachinePreparationSettingsItem struct {
	ImageID      string  `json:"imageId,omitempty"`
	DeployTarget *string `json:"deployTarget,omitempty"`
}

// MachinePreparationUserDataItem is the user data mode; the value is never returned.
type MachinePreparationUserDataItem struct {
	Mode string `json:"mode,omitempty"`
}

// MachinePreparationNetworkItem is the network intent for members that get an OS.
type MachinePreparationNetworkItem struct {
	Mode           string                             `json:"mode"`
	SubnetID       string                             `json:"subnetId,omitempty"`
	DefaultGateway bool                               `json:"defaultGateway"`
	Assignments    []MachinePreparationAssignmentItem `json:"assignments"`
}

// MachinePreparationAssignmentItem is one member's interface, subnet, and address.
type MachinePreparationAssignmentItem struct {
	ServerID    string `json:"serverId"`
	InterfaceID string `json:"interfaceId,omitempty"`
	SubnetID    string `json:"subnetId,omitempty"`
	IPAddress   string `json:"ipAddress,omitempty"`
}

// PlatformRemovalItem is how an absent Platform's uninstall treats its members.
type PlatformRemovalItem struct {
	ReleaseServers        bool `json:"releaseServers"`
	Erase                 bool `json:"erase"`
	SecureErase           bool `json:"secureErase"`
	QuickErase            bool `json:"quickErase"`
	UnbindStaticIPs       bool `json:"unbindStaticIps"`
	CancelActiveWorkflows bool `json:"cancelActiveWorkflows"`
}

// PlatformStatusItem is what swallow realized and judged for the spec: the generation the applied
// spec realized, the shared conditions, the applied spec, and the Workflow realizing a newer one.
type PlatformStatusItem struct {
	ObservedGeneration  int64                       `json:"observedGeneration"`
	Conditions          []convergence.ConditionItem `json:"conditions"`
	Applied             *PlatformSpecItem           `json:"applied"`
	RealizingWorkflowID *string                     `json:"realizingWorkflowId"`
}

// ToPlatformSpecItem maps a spec onto its wire shape; nil stays nil.
func ToPlatformSpecItem(spec *platformdomain.PlatformSpec) *PlatformSpecItem {
	if spec == nil {
		return nil
	}
	item := &PlatformSpecItem{Desired: string(spec.Desired), Members: make([]PlatformMemberItem, 0, len(spec.Members)), Settings: spec.Settings}
	if item.Settings == nil {
		item.Settings = map[string]any{}
	}
	for _, member := range spec.Members {
		roles := member.Roles
		if roles == nil {
			roles = []string{}
		}
		item.Members = append(item.Members, PlatformMemberItem{ServerID: member.ServerID, Roles: roles})
	}
	if preparation := spec.MachinePreparation; preparation.Mode != "" {
		item.MachinePreparation = toMachinePreparationItem(preparation)
	}
	if removal := spec.Removal; removal != nil {
		item.Removal = &PlatformRemovalItem{
			ReleaseServers: removal.ReleaseServers, Erase: removal.ReleaseOptions.Erase, SecureErase: removal.ReleaseOptions.SecureErase,
			QuickErase: removal.ReleaseOptions.QuickErase, UnbindStaticIPs: removal.ReleaseOptions.UnbindStaticIPs,
			CancelActiveWorkflows: removal.CancelActiveWorkflows,
		}
	}
	return item
}

func toMachinePreparationItem(preparation platformdomain.MachinePreparation) *MachinePreparationItem {
	item := &MachinePreparationItem{
		Mode: string(preparation.Mode), TemplateID: preparation.TemplateID,
		Settings: MachinePreparationSettingsItem{ImageID: preparation.ImageID},
		UserData: MachinePreparationUserDataItem{Mode: preparation.UserDataMode},
	}
	if preparation.Ephemeral != nil {
		target := "disk"
		if *preparation.Ephemeral {
			target = "ram"
		}
		item.Settings.DeployTarget = &target
	}
	if preparation.NetworkMode != "" || len(preparation.Assignments) > 0 {
		network := &MachinePreparationNetworkItem{
			Mode: preparation.NetworkMode, SubnetID: preparation.SubnetID, DefaultGateway: preparation.DefaultGateway,
			Assignments: make([]MachinePreparationAssignmentItem, 0, len(preparation.Assignments)),
		}
		for _, assignment := range preparation.Assignments {
			network.Assignments = append(network.Assignments, MachinePreparationAssignmentItem{
				ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID, SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
			})
		}
		item.Network = network
	}
	return item
}

func toPlatformStatusItem(status platformdomain.PlatformStatus) PlatformStatusItem {
	item := PlatformStatusItem{
		ObservedGeneration: status.ObservedGeneration, Conditions: convergence.ConditionItems(status.Conditions),
		Applied: ToPlatformSpecItem(status.Applied),
	}
	if status.Realizing != nil {
		item.RealizingWorkflowID = &status.Realizing.WorkflowID
	}
	return item
}
