package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// CapabilitiesItem is what a provisioner offers, so a client can present exactly the
// actions that exist rather than buttons that always fail.
type CapabilitiesItem struct {
	EphemeralDeploy    bool `json:"ephemeralDeploy"`
	Power              bool `json:"power"`
	HardwareValidation bool `json:"hardwareValidation"`
	OperatorState      bool `json:"operatorState"`
	MachineDetail      bool `json:"machineDetail"`
	HardwareInventory  bool `json:"hardwareInventory"`
	MachineRemoval     bool `json:"machineRemoval"`
}

// DetailFieldItem, DetailSectionItem, and DetailTableItem are the display-oriented shape
// the dashboard renders generically. They mirror the provider-neutral domain types so
// that a second provisioner fills the same structure with its own content.
type DetailFieldItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type DetailSectionItem struct {
	Title  string            `json:"title"`
	Fields []DetailFieldItem `json:"fields"`
}

type DetailTableItem struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// ProvisionerDetailItem is the live single-machine view plus the provisioner's
// capabilities. Capabilities are always present because they describe the adapter, not
// the remote system; sections and tables are the live proxy result.
type ProvisionerDetailItem struct {
	Capabilities CapabilitiesItem    `json:"capabilities"`
	Sections     []DetailSectionItem `json:"sections"`
	Tables       []DetailTableItem   `json:"tables"`
}

// GetProvisionerDetailUseCase proxies the provisioner for one machine's full detail.
//
// It reads live rather than from the projection because a machine's firmware, disks, and
// PCI map are only ever looked at one machine at a time: mirroring them would add a
// schema and a staleness story for data that is always fresh when read on demand. See
// docs/decisions/001.
type GetProvisionerDetailUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

func NewGetProvisionerDetailUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *GetProvisionerDetailUseCase {
	return &GetProvisionerDetailUseCase{servers: servers, providers: providers}
}

func (uc *GetProvisionerDetailUseCase) Execute(ctx context.Context, serverID string) (*ProvisionerDetailItem, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}

	caps := provider.Capabilities()
	item := &ProvisionerDetailItem{
		Capabilities: CapabilitiesItem{
			EphemeralDeploy:    caps.EphemeralDeploy,
			Power:              caps.Power,
			HardwareValidation: caps.HardwareValidation,
			OperatorState:      caps.OperatorState,
			MachineDetail:      caps.MachineDetail,
			HardwareInventory:  caps.HardwareInventory,
			MachineRemoval:     caps.MachineRemoval,
		},
		Sections: []DetailSectionItem{},
		Tables:   []DetailTableItem{},
	}

	inspector, ok := provider.(provisioningdomain.MachineDetailInspector)
	if !ok {
		// The provisioner offers no detail. Capabilities still answer, so a client can
		// render the actions it does support without a detail dump.
		return item, nil
	}

	detail, err := inspector.GetMachineDetail(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}

	for _, section := range detail.Sections {
		fields := make([]DetailFieldItem, 0, len(section.Fields))
		for _, f := range section.Fields {
			fields = append(fields, DetailFieldItem{Label: f.Label, Value: f.Value})
		}
		if len(fields) == 0 {
			continue
		}
		item.Sections = append(item.Sections, DetailSectionItem{Title: section.Title, Fields: fields})
	}
	for _, table := range detail.Tables {
		item.Tables = append(item.Tables, DetailTableItem{Title: table.Title, Columns: table.Columns, Rows: table.Rows})
	}

	return item, nil
}
