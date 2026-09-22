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
	Comment    string
	ServerIDs  []string
	TemplateID string
	Settings   DeploymentSettingsInput
	UserData   DeploymentUserDataInput
	Network    *DeploymentNetworkInput
	// VerificationRun marks this deploy as the proving deploy of an image verification. It is not
	// set from the HTTP API; the verify-os-image launcher sets it so the unverified-custom-image
	// deploy gate is bypassed for the very run that establishes the verification. It is frozen into
	// the durable snapshot so the executor's re-resolve also bypasses the gate.
	VerificationRun bool
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
	servers       serverdomain.ServerRepository
	templates     provisioningdomain.DeploymentTemplateRepository
	providers     provisioningdomain.ProviderFactory
	verifications provisioningdomain.OSImageVerificationRepository
	targets       *DeploymentTargetPreflightService
}

// NewDeployServersUseCase creates the batch deployment use case. The verification repository gates
// unverified custom images per deploy target; a nil repository disables the gate (used by tests
// that do not exercise verification).
func NewDeployServersUseCase(
	servers serverdomain.ServerRepository,
	templates provisioningdomain.DeploymentTemplateRepository,
	providers provisioningdomain.ProviderFactory,
	verifications provisioningdomain.OSImageVerificationRepository,
) *DeployServersUseCase {
	return &DeployServersUseCase{
		servers: servers, templates: templates, providers: providers, verifications: verifications,
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
				// Automatic addressing is realized by provider auto-assign (a recorded,
				// stable IP), not raw DHCP; static keeps the caller-chosen address.
				linkMode := provisioningdomain.NetworkLinkAuto
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
					Comment:      input.Comment,
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
	networkMode := provisioningdomain.DeploymentNetworkAutomatic
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
		subnetID = strings.TrimSpace(input.Network.SubnetID)
		defaultGateway = input.Network.DefaultGateway
	}
	// Normalize folds an empty value and the deprecated "dhcp" alias into Automatic, so
	// downstream code and the provider adapter only ever see automatic or static.
	normalizedMode, ok := provisioningdomain.NormalizeDeploymentNetworkMode(string(networkMode))
	if !ok {
		return nil, fmt.Errorf(
			"%w: network.mode must be automatic or static",
			provisioningdomain.ErrInvalidDeploymentBatch,
		)
	}
	networkMode = normalizedMode
	if networkMode != provisioningdomain.DeploymentNetworkStatic && defaultGateway {
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
	// An image the provisioner has not fully staged cannot be deployed: handing it to the
	// provider only produces an opaque mid-install failure. Refuse the batch up front with a
	// clear reason so operators fix the image (finish the upload or re-sync) before retrying.
	if !image.Complete {
		return nil, fmt.Errorf(
			"%w: the %q image is not fully staged by the provisioner and cannot be deployed",
			provisioningdomain.ErrDeploymentBatchConflict,
			image.Name,
		)
	}
	// An image built for one architecture cannot boot on a Server of another; reject the batch
	// rather than letting the provider fail the install opaquely later. Only a known-mismatch is
	// blocked: a Server whose provider has not reported an architecture yet is left to the provider
	// to validate at install, so an un-commissioned Server is not refused on missing data.
	for _, server := range servers {
		serverArch := server.Observed.Architecture
		if serverArch != "" && !provisioningdomain.ArchitecturesCompatible(serverArch, image.Architecture) {
			return nil, fmt.Errorf(
				"%w: Server %s architecture %q cannot run the %q image",
				provisioningdomain.ErrDeploymentBatchConflict,
				server.DisplayName(), serverArch, image.Architecture,
			)
		}
	}
	// Unverified custom images are blocked for the requested deploy target. VerificationRun is the
	// one exception: the verify-os-image Workflow's own proving deploy must be allowed to run the
	// image that is not yet verified. Synced provider images are provider-trusted and bypass.
	if !input.VerificationRun {
		if err := uc.requireCustomImageVerified(ctx, integrationID, image, provisioningdomain.DeployTargetForEphemeral(ephemeral)); err != nil {
			return nil, err
		}
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

// requireCustomImageVerified blocks deploying an unverified custom image for the requested deploy
// target. Verification is a Swallow-owned attestation that a real deploy in that target succeeded;
// a custom (uploaded) image that has not been proven is refused with a Swallow reason that names
// the target and points the operator at verification. Synced provider images are trusted and are
// never gated. A nil repository disables the gate for tests that do not exercise verification.
func (uc *DeployServersUseCase) requireCustomImageVerified(
	ctx context.Context,
	integrationID string,
	image *provisioningdomain.OSImage,
	target provisioningdomain.DeployTarget,
) error {
	if uc.verifications == nil || image == nil || !isCustomImage(image) {
		return nil
	}
	verification, err := uc.verifications.Find(ctx, integrationID, image.ID, image.Architecture)
	if err != nil {
		return err
	}
	if verification.IsVerified(target) {
		return nil
	}
	return fmt.Errorf(
		"%w: this custom image is not verified for %s deployment; verify it on a ready Server first",
		provisioningdomain.ErrDeploymentBatchConflict, target,
	)
}

// isCustomImage reports whether an image is a user-uploaded custom image (provider osSystem
// "custom"), which is the only kind gated by verification; synced provider images are trusted.
func isCustomImage(image *provisioningdomain.OSImage) bool {
	return strings.EqualFold(strings.TrimSpace(image.OSSystem), "custom")
}

// Validate performs the complete deployment preflight without changing provider or
// repository state. Durable workflows call it before persisting an Operation.
func (uc *DeployServersUseCase) Validate(ctx context.Context, input DeployServersInput) error {
	_, err := uc.preflight(ctx, input)
	return err
}

// ResolveOperationInput performs full preflight and freezes provider-neutral deployment
// intent for a durable Operation. Template user data is returned separately so callers can
// seal it before persistence; the returned request never depends on a mutable Template.
func (uc *DeployServersUseCase) ResolveOperationInput(ctx context.Context, input DeployServersInput) (DeployServersInput, string, error) {
	resolved, err := uc.preflight(ctx, input)
	if err != nil {
		return DeployServersInput{}, "", err
	}
	imageID := resolved.image.ID
	ephemeral := resolved.ephemeral
	frozen := DeployServersInput{
		Comment:   input.Comment,
		ServerIDs: append([]string(nil), input.ServerIDs...),
		Settings:  DeploymentSettingsInput{ImageID: &imageID, Ephemeral: &ephemeral},
		UserData:  DeploymentUserDataInput{Mode: "omit"},
		Network: &DeploymentNetworkInput{
			Mode: string(resolved.networkMode), DefaultGateway: resolved.defaultGateway,
			Assignments: make([]DeploymentNetworkAssignmentInput, len(resolved.assignments)),
		},
		// Preserve the verification-run bypass into the frozen snapshot so the durable executor's
		// re-resolve of this deploy does not re-apply the unverified-custom gate to the very run
		// that establishes the verification.
		VerificationRun: input.VerificationRun,
	}
	for index, assignment := range resolved.assignments {
		frozen.Network.Assignments[index] = DeploymentNetworkAssignmentInput{
			ServerID: resolved.servers[index].ID, InterfaceID: assignment.interfaceID,
			SubnetID: assignment.subnetID, IPAddress: assignment.ipAddress,
		}
	}
	if resolved.userData != "" {
		frozen.UserData.Mode = "replace"
	}
	return frozen, resolved.userData, nil
}
