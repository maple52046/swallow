package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// InventorySweepReport summarises one pass over one provisioner's attached-hardware
// inventory.
type InventorySweepReport struct {
	IntegrationID   string  `json:"integrationId"`
	IntegrationName string  `json:"integrationName"`
	Servers         int     `json:"servers"`
	Updated         int     `json:"updated"`
	Skipped         int     `json:"skipped"`
	Error           *string `json:"error"`
}

// InventorySweepUseCase refreshes the GPU inventory of every projected server.
//
// It is deliberately separate from the reconciler. Attached devices cost a call per
// machine and change only at commissioning, so folding them into the 60-second reconcile
// would multiply its request count for data that is nearly static. This runs on its own
// slower cadence and writes only the GPU field, which the reconciler is careful not to
// overwrite.
type InventorySweepUseCase struct {
	integrations sitedomain.IntegrationRepository
	servers      serverdomain.ServerRepository
	providers    provisioningdomain.ProviderFactory
}

func NewInventorySweepUseCase(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *InventorySweepUseCase {
	return &InventorySweepUseCase{integrations: integrations, servers: servers, providers: providers}
}

// ExecuteAll sweeps every enabled provisioner. One failing provisioner does not stop the
// others, the same isolation the reconciler has.
func (uc *InventorySweepUseCase) ExecuteAll(ctx context.Context) ([]InventorySweepReport, error) {
	integrations, err := uc.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}

	reports := make([]InventorySweepReport, 0, len(integrations))
	for _, integration := range integrations {
		reports = append(reports, uc.sweep(ctx, integration))
	}
	return reports, nil
}

func (uc *InventorySweepUseCase) sweep(ctx context.Context, integration *sitedomain.Integration) InventorySweepReport {
	report := InventorySweepReport{
		IntegrationID:   integration.ID,
		IntegrationName: integration.Name,
	}

	provider, err := uc.providers.For(ctx, integration.ID)
	if err != nil {
		return fail(report, err)
	}

	// A provisioner that cannot enumerate attached hardware is not an error: it simply
	// contributes no GPU data, and the fleet's GPU counts stay at whatever another
	// provisioner reports.
	inspector, ok := provider.(provisioningdomain.HardwareInventoryInspector)
	if !ok {
		return report
	}

	// Absent servers are skipped: their machine is not currently reachable through the
	// provisioner, so a device read would only fail.
	result, err := uc.servers.List(ctx, serverdomain.ListFilter{
		IntegrationID: integration.ID,
	})
	if err != nil {
		return fail(report, err)
	}
	report.Servers = len(result.Servers)

	for _, server := range result.Servers {
		gpus, err := inspector.ListGPUs(ctx, server.Source.ProviderMachineID)
		if err != nil {
			// One machine failing must not abandon the rest of the sweep: a single
			// unreachable BMC or a machine mid-reinstall is normal.
			report.Skipped++
			continue
		}
		if err := uc.servers.SetGPUs(ctx, server.ID, toServerGPUs(gpus)); err != nil {
			report.Skipped++
			continue
		}
		report.Updated++
	}

	return report
}

// toServerGPUs is the provider-to-Server classification boundary. Provisioners expose a generic
// GPU class; the Server projection adds the compute/display distinction before persistence.
func toServerGPUs(gpus []provisioningdomain.GPU) []serverdomain.GPU {
	out := make([]serverdomain.GPU, 0, len(gpus))
	for _, g := range gpus {
		out = append(out, serverdomain.GPU{
			Vendor: g.Vendor,
			Model:  g.Model,
			Count:  g.Count,
			Kind:   serverdomain.ClassifyGPUKind(g.Vendor, g.Model),
		})
	}
	return out
}

func fail(report InventorySweepReport, cause error) InventorySweepReport {
	message := cause.Error()
	report.Error = &message
	return report
}
