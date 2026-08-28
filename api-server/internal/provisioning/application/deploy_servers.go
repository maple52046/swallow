package application

import (
	"context"
	"errors"
	"fmt"
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

// DeployServersInput describes one atomic-preflight batch.
type DeployServersInput struct {
	ServerIDs  []string
	TemplateID string
	Settings   DeploymentSettingsInput
	UserData   DeploymentUserDataInput
}

// DeploymentFailureItem reports a provider refusal after successful batch preflight.
type DeploymentFailureItem struct {
	ServerID string `json:"serverId"`
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

type resolvedDeployment struct {
	servers   []*serverdomain.Server
	provider  provisioningdomain.OSProvisioningProvider
	image     *provisioningdomain.OSImage
	ephemeral bool
	userData  string
}

type deploymentOutcome struct {
	item *ProvisioningStateItem
	err  error
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
				machine, deployErr := resolved.provider.Deploy(ctx, provisioningdomain.DeployRequest{
					MachineID:    server.Source.ProviderMachineID,
					OSSystem:     resolved.image.OSSystem,
					DistroSeries: resolved.image.ID,
					UserData:     resolved.userData,
					Ephemeral:    resolved.ephemeral,
				})
				if deployErr != nil {
					outcomes[index].err = deployErr
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
	templateMode := input.TemplateID != ""
	if templateMode {
		template, err := uc.templates.FindByID(ctx, input.TemplateID)
		if err != nil {
			return nil, err
		}
		if template.IntegrationID != integrationID {
			return nil, fmt.Errorf(
				"%w: template and targets use different provisioner integrations",
				provisioningdomain.ErrDeploymentBatchConflict,
			)
		}
		imageID = template.ImageID
		ephemeral = template.Ephemeral
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
		inherited, err := uc.templates.UserData(ctx, input.TemplateID)
		if err != nil && !errors.Is(err, provisioningdomain.ErrDeploymentTemplateUserDataMissing) {
			return nil, err
		}
		if err == nil {
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
	return &resolvedDeployment{
		servers: servers, provider: provider, image: image, ephemeral: ephemeral, userData: userData,
	}, nil
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
