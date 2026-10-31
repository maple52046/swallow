package application

import (
	"context"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// ProvisioningStateItem reports the provisioning axis right after a lifecycle action.
//
// It is a snapshot of what the provisioner returned on accepting the request, not a
// completion report: deployment continues asynchronously and the reconciler is what
// tracks it to completion.
type ProvisioningStateItem struct {
	ServerID            string `json:"serverId"`
	State               string `json:"state"`
	ProviderState       string `json:"providerState"`
	PowerState          string `json:"powerState"`
	OSSystem            string `json:"osSystem"`
	DistroSeries        string `json:"distroSeries"`
	Ephemeral           bool   `json:"ephemeral"`
	HWEKernel           string `json:"hweKernel"`
	Locked              bool   `json:"locked"`
	CommissioningStatus string `json:"commissioningStatus"`
	TaskID              string `json:"taskId,omitempty"`
	TestingStatus       string `json:"testingStatus"`
	ObservedAt          string `json:"observedAt"`
	// InspectionRequired reports that the provisioner's storage layout cannot take a disk
	// deployment until the Server is inspected again; the reason says why. Omitted when false.
	InspectionRequired       bool   `json:"inspectionRequired,omitempty"`
	InspectionRequiredReason string `json:"inspectionRequiredReason,omitempty"`
}

// applyProvisioningResult writes the post-action state onto the server so that a
// caller reading the server back sees "deploying" immediately rather than the previous
// state until the next reconcile pass.
//
// A failure to persist is swallowed: the action already succeeded at the provisioner,
// and the reconciler will converge. Reporting a failure here would suggest the
// deployment did not start, which would be worse than a briefly stale axis.
func applyProvisioningResult(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) *ProvisioningStateItem {
	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	item := updateProvisioningProjection(server, machine)
	_ = servers.Upsert(ctx, server)
	return item
}

func projectedEphemeral(machine *provisioningdomain.Machine) bool {
	if !machine.Ephemeral {
		return false
	}
	switch machine.Status {
	case provisioningdomain.MachineStatusDeploying,
		provisioningdomain.MachineStatusDeployed,
		provisioningdomain.MachineStatusReleasing:
		return true
	default:
		return false
	}
}

// updateProvisioningProjection applies a provider observation in memory. Callers choose
// whether persistence is best effort after an accepted action or required for a live
// refresh whose sole purpose is to advance the stored projection.
func updateProvisioningProjection(
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) *ProvisioningStateItem {
	now := time.Now().UTC()

	// This projection carries the previously mirrored deployed-image name and default user
	// forward rather than blanking them, because resolving them needs the image catalog.
	// RefreshServer fills them when a deploy completes (an empty name on a deployed machine), and
	// reconcile refreshes them thereafter. They are carried forward only while the machine still
	// reports the same OS and release: a redeploy to another image changes those at deploy start,
	// and a stale default user would make automation log in to the new image as the old image's
	// account.
	deployedImageName, deployedImageDefaultUser := "", ""
	if previous := server.Provisioning; previous != nil &&
		previous.OSSystem == machine.OSSystem && previous.DistroSeries == machine.DistroSeries {
		deployedImageName = previous.DeployedImageName
		deployedImageDefaultUser = previous.DeployedImageDefaultUser
	}

	server.Provisioning = &serverdomain.ProvisioningStatus{
		State:                    string(machine.Status),
		StateSince:               serverdomain.NextStateSince(server.Provisioning, string(machine.Status), now),
		ProviderState:            machine.ProviderStatus,
		ErrorDescription:         machine.ErrorDescription,
		PowerState:               string(machine.PowerState),
		OSSystem:                 machine.OSSystem,
		DistroSeries:             machine.DistroSeries,
		DeployedImageName:        deployedImageName,
		DeployedImageDefaultUser: deployedImageDefaultUser,
		Ephemeral:                projectedEphemeral(machine),
		HWEKernel:                machine.HWEKernel,
		Locked:                   machine.Locked,
		InspectionRequired:       machine.InspectionRequired,
		InspectionRequiredReason: machine.InspectionRequiredReason,
		CommissioningStatus:      machine.CommissioningStatus,
		TestingStatus:            machine.TestingStatus,
		IntegrationID:            server.Source.IntegrationID,
		ObservedAt:               now,
	}
	server.UpdatedAt = now

	return &ProvisioningStateItem{
		ServerID:                 server.ID,
		State:                    server.Provisioning.State,
		ProviderState:            server.Provisioning.ProviderState,
		PowerState:               server.Provisioning.PowerState,
		OSSystem:                 server.Provisioning.OSSystem,
		DistroSeries:             server.Provisioning.DistroSeries,
		Ephemeral:                server.Provisioning.Ephemeral,
		HWEKernel:                server.Provisioning.HWEKernel,
		Locked:                   server.Provisioning.Locked,
		CommissioningStatus:      server.Provisioning.CommissioningStatus,
		TestingStatus:            server.Provisioning.TestingStatus,
		ObservedAt:               now.Format(time.RFC3339),
		InspectionRequired:       server.Provisioning.InspectionRequired,
		InspectionRequiredReason: server.Provisioning.InspectionRequiredReason,
	}
}
