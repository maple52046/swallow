package application

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const (
	maxDeploymentTargets = 100
	deploymentWorkers    = 4
)

// DeploymentSettingsInput overrides non-secret template settings.
type DeploymentSettingsInput struct {
	ImageID   *string
	Ephemeral *bool
}

// DeploymentUserDataInput controls cloud-init without making inherited content readable.
type DeploymentUserDataInput struct {
	Mode  string
	Value string
}

// DeploymentNetworkAssignmentInput carries target-specific values that templates
// deliberately cannot save. Empty InterfaceID selects the boot NIC.
type DeploymentNetworkAssignmentInput struct {
	ServerID    string
	InterfaceID string
	SubnetID    string
	IPAddress   string
}

// DeploymentNetworkInput is Swallow's common DHCP/static deployment intent.
type DeploymentNetworkInput struct {
	Mode           string
	SubnetID       string
	DefaultGateway bool
	Assignments    []DeploymentNetworkAssignmentInput
}

// DeployServersInput describes one atomic-preflight batch.
type DeployServersInput struct {
	ServerIDs  []string
	TemplateID string
	Settings   DeploymentSettingsInput
	UserData   DeploymentUserDataInput
	Network    *DeploymentNetworkInput
}

// DeploymentFailureItem reports which dispatch stage refused one target after
// the all-target read-only preflight had succeeded.
type DeploymentFailureItem struct {
	ServerID string `json:"serverId"`
	Stage    string `json:"stage"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// DeployServersResult contains every dispatch outcome in target order.
type DeployServersResult struct {
	Requested int                      `json:"requested"`
	Accepted  []*ProvisioningStateItem `json:"accepted"`
	Failed    []DeploymentFailureItem  `json:"failed"`
}

// DeployServersUseCase validates a whole target set, then dispatches with bounded concurrency.
type DeployServersUseCase struct {
	servers   serverdomain.ServerRepository
	templates provisioningdomain.DeploymentTemplateRepository
	providers provisioningdomain.ProviderFactory
	targets   *DeploymentTargetPreflightService
}

// NewDeployServersUseCase creates the batch deployment use case.
func NewDeployServersUseCase(
	servers serverdomain.ServerRepository,
	templates provisioningdomain.DeploymentTemplateRepository,
	providers provisioningdomain.ProviderFactory,
) *DeployServersUseCase {
	return &DeployServersUseCase{
		servers: servers, templates: templates, providers: providers,
		targets: NewDeploymentTargetPreflightService(servers, providers),
	}
}

type resolvedNetworkAssignment struct {
	interfaceID string
	linkID      string
	subnetID    string
	ipAddress   string
}

type resolvedDeployment struct {
	servers         []*serverdomain.Server
	provider        provisioningdomain.OSProvisioningProvider
	networkProvider provisioningdomain.NetworkConfigurationProvider
	image           *provisioningdomain.OSImage
	ephemeral       bool
	userData        string
	networkMode     provisioningdomain.DeploymentNetworkMode
	defaultGateway  bool
	assignments     []resolvedNetworkAssignment
}

type deploymentOutcome struct {
	item  *ProvisioningStateItem
	stage string
	err   error
}

// Execute performs every validation before making the first provider write.
func (uc *DeployServersUseCase) Execute(
	ctx context.Context,
	input DeployServersInput,
) (*DeployServersResult, error) {
	resolved, err := uc.preflight(ctx, input)
	if err != nil {
		return nil, err
	}

	outcomes := make([]deploymentOutcome, len(resolved.servers))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := deploymentWorkers
	if len(resolved.servers) < workerCount {
		workerCount = len(resolved.servers)
	}
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				server := resolved.servers[index]
				assignment := resolved.assignments[index]
				linkMode := provisioningdomain.NetworkLinkDHCP
				if resolved.networkMode == provisioningdomain.DeploymentNetworkStatic {
					linkMode = provisioningdomain.NetworkLinkStatic
				}
				_, configureErr := resolved.networkProvider.ConfigureNetworkLink(
					ctx,
					server.Source.ProviderMachineID,
					provisioningdomain.NetworkLinkRequest{
						InterfaceID:    assignment.interfaceID,
						LinkID:         assignment.linkID,
						Mode:           linkMode,
						SubnetID:       assignment.subnetID,
						IPAddress:      assignment.ipAddress,
						DefaultGateway: resolved.defaultGateway,
					},
				)
				if configureErr != nil {
					outcomes[index] = deploymentOutcome{
						stage: "network_configuration",
						err:   configureErr,
					}
					continue
				}

				if resolved.provider.Capabilities().DeploymentReadiness {
					if validator, ok := resolved.provider.(provisioningdomain.DeploymentTargetValidator); ok {
						if readinessErr := validator.ValidateDeploymentTarget(
							ctx, server.Source.ProviderMachineID,
						); readinessErr != nil {
							outcomes[index] = deploymentOutcome{
								stage: "network_configuration",
								err:   readinessErr,
							}
							continue
						}
					}
				}

				machine, deployErr := resolved.provider.Deploy(ctx, provisioningdomain.DeployRequest{
					MachineID:    server.Source.ProviderMachineID,
					OSSystem:     resolved.image.OSSystem,
					DistroSeries: resolved.image.ID,
					UserData:     resolved.userData,
					Ephemeral:    resolved.ephemeral,
				})
				if deployErr != nil {
					outcomes[index] = deploymentOutcome{stage: "deployment", err: deployErr}
					continue
				}
				outcomes[index].item = applyProvisioningResult(ctx, uc.servers, server, machine)
			}
		}()
	}
	for index := range resolved.servers {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	result := &DeployServersResult{
		Requested: len(resolved.servers),
		Accepted:  make([]*ProvisioningStateItem, 0, len(resolved.servers)),
		Failed:    make([]DeploymentFailureItem, 0),
	}
	for index, outcome := range outcomes {
		if outcome.err == nil {
			result.Accepted = append(result.Accepted, outcome.item)
			continue
		}
		code, message := deploymentFailure(outcome.err)
		result.Failed = append(result.Failed, DeploymentFailureItem{
			ServerID: resolved.servers[index].ID,
			Stage:    outcome.stage,
			Code:     code,
			Message:  message,
		})
	}
	return result, nil
}

func (uc *DeployServersUseCase) preflight(
	ctx context.Context,
	input DeployServersInput,
) (*resolvedDeployment, error) {
	targets, err := uc.targets.validate(ctx, input.ServerIDs)
	if err != nil {
		return nil, err
	}
	if len(targets.issues) > 0 {
		return nil, deploymentTargetConflict(targets.issues[0])
	}
	servers := targets.servers
	integrationID := targets.integrationID

	imageID := ""
	ephemeral := false
	userData := ""
	networkMode := provisioningdomain.DeploymentNetworkDHCP
	subnetID := ""
	defaultGateway := false
	templateMode := input.TemplateID != ""
	if templateMode {
		template, findErr := uc.templates.FindByID(ctx, input.TemplateID)
		if findErr != nil {
			return nil, findErr
		}
		if template.IntegrationID != integrationID {
			return nil, fmt.Errorf(
				"%w: template and targets use different provisioner integrations",
				provisioningdomain.ErrDeploymentBatchConflict,
			)
		}
		imageID = template.ImageID
		ephemeral = template.Ephemeral
		networkMode = template.NetworkMode
		if networkMode == "" {
			networkMode = provisioningdomain.DeploymentNetworkDHCP
		}
		subnetID = template.SubnetID
		defaultGateway = template.DefaultGateway
		if input.Settings.ImageID != nil {
			imageID = strings.TrimSpace(*input.Settings.ImageID)
		}
		if input.Settings.Ephemeral != nil {
			ephemeral = *input.Settings.Ephemeral
		}
	} else {
		if input.Settings.ImageID == nil || strings.TrimSpace(*input.Settings.ImageID) == "" {
			return nil, fmt.Errorf("%w: settings.imageId is required without a template", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		imageID = strings.TrimSpace(*input.Settings.ImageID)
		if input.Settings.Ephemeral != nil {
			ephemeral = *input.Settings.Ephemeral
		}
	}
	if input.Network != nil {
		networkMode = provisioningdomain.DeploymentNetworkMode(
			strings.ToLower(strings.TrimSpace(input.Network.Mode)),
		)
		if networkMode == "" {
			networkMode = provisioningdomain.DeploymentNetworkDHCP
		}
		subnetID = strings.TrimSpace(input.Network.SubnetID)
		defaultGateway = input.Network.DefaultGateway
	}
	if networkMode != provisioningdomain.DeploymentNetworkDHCP &&
		networkMode != provisioningdomain.DeploymentNetworkStatic {
		return nil, fmt.Errorf(
			"%w: network.mode must be dhcp or static",
			provisioningdomain.ErrInvalidDeploymentBatch,
		)
	}
	if networkMode == provisioningdomain.DeploymentNetworkDHCP && defaultGateway {
		return nil, fmt.Errorf(
			"%w: network.defaultGateway is supported only for static mode",
			provisioningdomain.ErrInvalidDeploymentBatch,
		)
	}

	mode := strings.ToLower(strings.TrimSpace(input.UserData.Mode))
	if mode == "" {
		if templateMode {
			mode = "inherit"
		} else {
			mode = "omit"
		}
	}
	switch mode {
	case "inherit":
		if !templateMode {
			return nil, fmt.Errorf("%w: userData inherit requires a template", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		inherited, userDataErr := uc.templates.UserData(ctx, input.TemplateID)
		if userDataErr != nil && !errors.Is(userDataErr, provisioningdomain.ErrDeploymentTemplateUserDataMissing) {
			return nil, userDataErr
		}
		if userDataErr == nil {
			userData = inherited
		}
	case "replace":
		if input.UserData.Value == "" {
			return nil, fmt.Errorf("%w: userData.value is required for replace", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		userData = input.UserData.Value
	case "omit":
		userData = ""
	default:
		return nil, fmt.Errorf(
			"%w: userData.mode must be inherit, replace, or omit",
			provisioningdomain.ErrInvalidDeploymentBatch,
		)
	}

	provider, image, err := validateDeployImage(ctx, uc.providers, integrationID, imageID)
	if err != nil {
		if errors.Is(err, provisioningdomain.ErrInvalidDeploymentTemplate) {
			return nil, fmt.Errorf("%w: imageId %q is not available from the provisioner", provisioningdomain.ErrInvalidDeploymentBatch, imageID)
		}
		return nil, err
	}
	if ephemeral && !provider.Capabilities().EphemeralDeploy {
		return nil, fmt.Errorf(
			"%w: this provisioner cannot deploy from memory",
			provisioningdomain.ErrDeploymentBatchConflict,
		)
	}
	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", provisioningdomain.ErrDeploymentBatchConflict, err)
	}
	assignments, err := resolveNetworkAssignments(
		ctx, networkProvider, servers, input.Network, networkMode, subnetID)
	if err != nil {
		return nil, err
	}

	return &resolvedDeployment{
		servers:         servers,
		provider:        provider,
		networkProvider: networkProvider,
		image:           image,
		ephemeral:       ephemeral,
		userData:        userData,
		networkMode:     networkMode,
		defaultGateway:  defaultGateway,
		assignments:     assignments,
	}, nil
}

func resolveNetworkAssignments(
	ctx context.Context,
	provider provisioningdomain.NetworkConfigurationProvider,
	servers []*serverdomain.Server,
	input *DeploymentNetworkInput,
	mode provisioningdomain.DeploymentNetworkMode,
	commonSubnetID string,
) ([]resolvedNetworkAssignment, error) {
	requested := make(map[string]DeploymentNetworkAssignmentInput, len(servers))
	targets := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		targets[server.ID] = struct{}{}
	}
	if input != nil {
		for _, assignment := range input.Assignments {
			if _, exists := targets[assignment.ServerID]; !exists {
				return nil, fmt.Errorf(
					"%w: network assignment references a Server outside the batch",
					provisioningdomain.ErrInvalidDeploymentBatch,
				)
			}
			if _, duplicate := requested[assignment.ServerID]; duplicate {
				return nil, fmt.Errorf(
					"%w: network assignments must be unique per Server",
					provisioningdomain.ErrInvalidDeploymentBatch,
				)
			}
			requested[assignment.ServerID] = assignment
		}
	}

	networks, err := inspectDeploymentNetworks(ctx, provider, servers)
	if err != nil {
		return nil, err
	}
	resolved := make([]resolvedNetworkAssignment, len(servers))
	staticIPs := make(map[string]struct{})
	for index, server := range servers {
		assignment := requested[server.ID]
		iface, err := selectDeploymentInterface(networks[index], strings.TrimSpace(assignment.InterfaceID))
		if err != nil {
			return nil, deploymentNetworkConflict(server.ID, err)
		}
		targetSubnetID := strings.TrimSpace(assignment.SubnetID)
		if targetSubnetID == "" {
			targetSubnetID = strings.TrimSpace(commonSubnetID)
		}
		if targetSubnetID == "" {
			targetSubnetID = suggestedSubnet(iface)
		}
		subnet, err := findInterfaceSubnet(iface, targetSubnetID)
		if err != nil {
			return nil, deploymentNetworkConflict(server.ID, err)
		}

		ipAddress := strings.TrimSpace(assignment.IPAddress)
		if mode == provisioningdomain.DeploymentNetworkStatic {
			address, parseErr := netip.ParseAddr(ipAddress)
			if parseErr != nil || !address.Is4() {
				return nil, deploymentNetworkConflict(server.ID, errors.New("enter a valid IPv4 address for Static mode"))
			}
			if subnet.CIDR != "" {
				prefix, prefixErr := netip.ParsePrefix(subnet.CIDR)
				if prefixErr == nil && !prefix.Contains(address) {
					return nil, deploymentNetworkConflict(server.ID, errors.New("the static IP is outside the selected subnet"))
				}
			}
			if _, duplicate := staticIPs[address.String()]; duplicate {
				return nil, fmt.Errorf(
					"%w: static IP addresses must be unique within a deployment batch",
					provisioningdomain.ErrInvalidDeploymentBatch,
				)
			}
			staticIPs[address.String()] = struct{}{}
			ipAddress = address.String()
		} else if ipAddress != "" {
			return nil, fmt.Errorf(
				"%w: ipAddress is valid only for static mode",
				provisioningdomain.ErrInvalidDeploymentBatch,
			)
		}

		linkID := ""
		for _, link := range iface.Links {
			if link.SubnetID != targetSubnetID {
				continue
			}
			if linkID != "" {
				return nil, deploymentNetworkConflict(
					server.ID,
					errors.New("multiple links use the selected subnet; choose and unbind one in Server Network"),
				)
			}
			linkID = link.ID
		}
		resolved[index] = resolvedNetworkAssignment{
			interfaceID: iface.ID,
			linkID:      linkID,
			subnetID:    targetSubnetID,
			ipAddress:   ipAddress,
		}
	}
	return resolved, nil
}

func inspectDeploymentNetworks(
	ctx context.Context,
	provider provisioningdomain.NetworkConfigurationProvider,
	servers []*serverdomain.Server,
) ([]*provisioningdomain.MachineNetwork, error) {
	results := make([]*provisioningdomain.MachineNetwork, len(servers))
	errs := make([]error, len(servers))
	jobs := make(chan int)
	workerCount := deploymentWorkers
	if len(servers) < workerCount {
		workerCount = len(servers)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index], errs[index] = provider.InspectNetwork(
					ctx, servers[index].Source.ProviderMachineID)
			}
		}()
	}
	for index := range servers {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func selectDeploymentInterface(
	network *provisioningdomain.MachineNetwork,
	interfaceID string,
) (*provisioningdomain.NetworkInterface, error) {
	if interfaceID != "" {
		for index := range network.Interfaces {
			if network.Interfaces[index].ID == interfaceID {
				return &network.Interfaces[index], nil
			}
		}
		return nil, errors.New("the selected network interface is not available")
	}
	for index := range network.Interfaces {
		if network.Interfaces[index].Boot {
			return &network.Interfaces[index], nil
		}
	}
	if len(network.Interfaces) == 1 {
		return &network.Interfaces[0], nil
	}
	return nil, errors.New("select a network interface")
}

func suggestedSubnet(iface *provisioningdomain.NetworkInterface) string {
	linked := make(map[string]struct{})
	for _, link := range iface.Links {
		if link.SubnetID != "" {
			linked[link.SubnetID] = struct{}{}
		}
	}
	if len(linked) == 1 {
		for subnetID := range linked {
			return subnetID
		}
	}
	managed := ""
	for _, subnet := range iface.AvailableSubnets {
		if !subnet.Managed {
			continue
		}
		if managed != "" {
			return ""
		}
		managed = subnet.ID
	}
	return managed
}

func findInterfaceSubnet(
	iface *provisioningdomain.NetworkInterface,
	subnetID string,
) (*provisioningdomain.NetworkSubnet, error) {
	if subnetID == "" {
		return nil, errors.New("select a compatible subnet")
	}
	for index := range iface.AvailableSubnets {
		if iface.AvailableSubnets[index].ID == subnetID {
			return &iface.AvailableSubnets[index], nil
		}
	}
	return nil, errors.New("the selected subnet is not available on the interface")
}

func deploymentNetworkConflict(serverID string, err error) error {
	return fmt.Errorf(
		"%w: server %s: %s",
		provisioningdomain.ErrDeploymentBatchConflict,
		serverID,
		err.Error(),
	)
}

func deploymentFailure(err error) (string, string) {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.Kind == provisioningdomain.ProviderErrorRejected {
			return "provider_rejected", providerErr.Detail
		}
		return "provider_unavailable", providerErr.Detail
	}
	if errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		return "provider_rejected", "The provisioner no longer has this machine."
	}
	return "provider_error", "The provisioner could not accept this deployment."
}
