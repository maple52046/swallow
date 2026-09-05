package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

const (
	deployKubernetesKind        = "deploy-kubernetes"
	uninstallKubernetesKind     = "uninstall-kubernetes"
	uninstallKubernetesPlaybook = "uninstall-kubernetes"
	restoreExportersVar         = "swallow_restore_ansible_exporters"
	platformNameVar             = "swallow_platform_name"
)

// platformDeploymentLauncher composes optional MAAS preparation and k0s automation into
// one durable Operation while retaining the legacy launcher for unavailable Temporal.
type platformDeploymentLauncher struct {
	operations     *operationapp.ExecutionService
	orchestrations *operationapp.OrchestrationService
	deployments    *provisioningapp.DeployServersUseCase
	servers        serverdomain.ServerRepository
}

func (l platformDeploymentLauncher) Launch(ctx context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	if l.orchestrations == nil {
		if provisionFirst {
			return "", fmt.Errorf("durable Platform provisioning is unavailable")
		}
		item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
			Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
			TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
			TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
			RequestedBy: launch.RequestedBy,
		})
		if err != nil {
			return "", err
		}
		return item.ID, nil
	}

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return "", err
	}

	steps := make([]operationdomain.OperationStep, 0, len(launch.TargetServerIDs)+3)
	dependencies := make([]string, 0, len(launch.TargetServerIDs))
	secretStepIDs := make([]string, 0, len(launch.TargetServerIDs))
	preparation := launch.MachinePreparation
	preparation.UserData = ""
	var frozenProvisioning *provisioningapp.DeployServersInput
	userData := ""
	if provisionFirst {
		if l.deployments == nil {
			return "", fmt.Errorf("durable Platform provisioning is unavailable")
		}
		batch, resolvedUserData, resolveErr := l.deployments.ResolveOperationInput(
			ctx,
			platformProvisioningInput(launch.MachinePreparation, launch.TargetServerIDs),
		)
		if resolveErr != nil {
			return "", resolveErr
		}
		frozenProvisioning = &batch
		userData = resolvedUserData
		for _, serverID := range launch.TargetServerIDs {
			stepID := "provision-" + serverID
			targetRequest := batch
			targetRequest.ServerIDs = []string{serverID}
			if batch.Network != nil {
				network := *batch.Network
				network.Assignments = nil
				for _, assignment := range batch.Network.Assignments {
					if assignment.ServerID == serverID {
						network.Assignments = []provisioningapp.DeploymentNetworkAssignmentInput{assignment}
					}
				}
				targetRequest.Network = &network
			}
			steps = append(steps, operationdomain.OperationStep{
				ID: stepID, Kind: "provision-os", Name: "Provision and verify operating system on " + serverID,
				Executor:   operationdomain.StepExecutorMAAS,
				Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
				Parameters: map[string]any{"request": structToMap(targetRequest)},
			})
			dependencies = append(dependencies, stepID)
			secretStepIDs = append(secretStepIDs, stepID)
		}
	}
	installDependencies := dependencies
	if !provisionFirst {
		steps = append(steps, operationdomain.OperationStep{
			ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify existing OS SSH readiness",
			Executor: operationdomain.StepExecutorInternal, Targets: prepared.Targets,
		})
		installDependencies = []string{"wait-for-ssh"}
	}
	steps = append(steps, operationdomain.OperationStep{
		ID: "install-platform", Kind: "ansible-playbook", Name: "Install k0s Platform",
		Executor: operationdomain.StepExecutorAnsible, DependsOn: installDependencies,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
	})
	steps = append(steps, operationdomain.OperationStep{
		ID: "validate-platform", Kind: "validate-platform-health", Name: "Validate Platform health",
		Executor: operationdomain.StepExecutorInternal, DependsOn: []string{"install-platform"},
		Targets: prepared.Targets,
	})
	stepSecrets := map[string]map[string]any{}
	if len(launch.SecretVars) > 0 {
		stepSecrets["install-platform"] = launch.SecretVars
	}
	sharedSecrets := map[string]any{}
	if provisionFirst && frozenProvisioning.UserData.Mode == "replace" && userData != "" {
		sharedSecrets["userData"] = userData
	}
	intentSnapshot := map[string]any{
		"machinePreparation": structToMap(preparation), "extraVars": prepared.ExtraVars,
	}
	if frozenProvisioning != nil {
		intentSnapshot["resolvedProvisioning"] = structToMap(*frozenProvisioning)
	}
	created, err := l.orchestrations.Create(ctx, operationapp.CreateOrchestrationInput{
		ID: operationID, Kind: operationdomain.OperationKindDeployKubernetes,
		IntentSummary:  "Deploy k0s Platform " + launch.Platform.Name,
		IntentSnapshot: intentSnapshot,
		Definition:     "platform-deployment", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
		SecretStepIDs: secretStepIDs, SecretValues: sharedSecrets,
		StepSecretValues: stepSecrets,
	})
	if err != nil {
		return "", err
	}
	materializeInitialDeployments(ctx, l.servers, created.ID, steps)
	return created.ID, nil
}

// platformOperationCanceler implements platformdomain.PlatformOperationCanceler by finding
// the platform's active durable Operations and canceling each one, which releases their
// resource leases so deleting the platform frees its member servers.
type platformOperationCanceler struct {
	orchestrations *operationapp.OrchestrationService
}

// activeOperationsPageSize bounds one platform's active Operation listing. A platform never
// runs anywhere near this many concurrent durable Operations, so a single page is complete.
const activeOperationsPageSize = 100

func (c platformOperationCanceler) CancelActiveForPlatform(ctx context.Context, platformID string) error {
	if c.orchestrations == nil {
		return nil
	}
	items, _, err := c.orchestrations.List(ctx, operationapp.ListOperationsInput{
		PlatformID: platformID,
		Active:     true,
		Page:       pagination.Page{Page: 1, PageSize: activeOperationsPageSize},
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := c.orchestrations.Cancel(ctx, item.ID); err != nil {
			// A run that reached a terminal state between listing and canceling is already
			// in the desired end state; tolerate that control conflict and continue so one
			// finished Operation cannot block deleting the platform.
			if errors.Is(err, operationdomain.ErrOperationControlConflict) {
				continue
			}
			return fmt.Errorf("cancel platform operation %s: %w", item.ID, err)
		}
	}
	return nil
}

// platformMachinePreparationValidator translates the Platform-owned intent to the active
// provisioning contract and performs its full side-effect-free preflight.
type platformMachinePreparationValidator struct {
	deployments *provisioningapp.DeployServersUseCase
}

func (v platformMachinePreparationValidator) Validate(ctx context.Context, _ string, serverIDs []string, preparation platformdomain.MachinePreparation) error {
	return v.deployments.Validate(ctx, platformProvisioningInput(preparation, serverIDs))
}

func platformProvisioningInput(preparation platformdomain.MachinePreparation, serverIDs []string) provisioningapp.DeployServersInput {
	input := provisioningapp.DeployServersInput{
		ServerIDs: append([]string(nil), serverIDs...), TemplateID: preparation.TemplateID,
		Settings: provisioningapp.DeploymentSettingsInput{ImageID: optionalStringPointer(preparation.ImageID), Ephemeral: preparation.Ephemeral},
		UserData: provisioningapp.DeploymentUserDataInput{Mode: preparation.UserDataMode, Value: preparation.UserData},
	}
	if preparation.NetworkMode != "" {
		input.Network = &provisioningapp.DeploymentNetworkInput{
			Mode: preparation.NetworkMode, SubnetID: preparation.SubnetID,
			DefaultGateway: preparation.DefaultGateway,
			Assignments:    make([]provisioningapp.DeploymentNetworkAssignmentInput, len(preparation.Assignments)),
		}
		for index, assignment := range preparation.Assignments {
			input.Network.Assignments[index] = provisioningapp.DeploymentNetworkAssignmentInput{
				ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID,
				SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
			}
		}
	}
	return input
}

func optionalStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// uninstallPlatformStepID is the durable uninstall step that every optional release step
// depends on, so k0s is always removed before any host is released.
const uninstallPlatformStepID = "uninstall-platform"

func (l platformDeploymentLauncher) LaunchUninstall(
	ctx context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	trustedVars := map[string]any{
		restoreExportersVar: launch.RestoreExporters,
		platformNameVar:     launch.Platform.Name,
	}
	if l.orchestrations == nil {
		// Server release is a multi-step durable operation; without Temporal we can only
		// run the legacy single-step uninstall, so reject a release request rather than
		// silently dropping it.
		if launch.ReleaseServers {
			return "", fmt.Errorf("durable Platform uninstall is unavailable; releasing servers requires it")
		}
		item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
			Kind: uninstallKubernetesKind, Intent: "Uninstall k0s platform " + launch.Platform.Name,
			TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
			PlaybookName:       uninstallKubernetesPlaybook,
			TrustedVars:        trustedVars,
			RetryOfOperationID: launch.RetryOfOperationID,
			RequestedBy:        launch.RequestedBy,
		})
		if err != nil {
			return "", err
		}
		return item.ID, nil
	}

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: uninstallKubernetesKind, Intent: "Uninstall k0s platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		PlaybookName: uninstallKubernetesPlaybook, TrustedVars: trustedVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return "", err
	}

	steps := make([]operationdomain.OperationStep, 0, len(launch.TargetServerIDs)+1)
	steps = append(steps, operationdomain.OperationStep{
		ID: uninstallPlatformStepID, Kind: "ansible-playbook", Name: "Uninstall k0s Platform",
		Executor: operationdomain.StepExecutorAnsible, Targets: prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
	})
	if launch.ReleaseServers {
		for _, serverID := range launch.TargetServerIDs {
			releaseInput := provisioningapp.ReleaseServerInput{
				ServerID:        serverID,
				Erase:           launch.ReleaseOptions.Erase,
				SecureErase:     launch.ReleaseOptions.SecureErase,
				QuickErase:      launch.ReleaseOptions.QuickErase,
				UnbindStaticIPs: launch.ReleaseOptions.UnbindStaticIPs,
				Comment:         "Release after uninstalling platform " + launch.Platform.Name,
				RequestID:       operationID,
			}
			steps = append(steps, operationdomain.OperationStep{
				ID: "release-" + serverID, Kind: "release-os", Name: "Release " + serverID,
				Executor: operationdomain.StepExecutorMAAS, DependsOn: []string{uninstallPlatformStepID},
				Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
				Parameters: map[string]any{"request": structToMap(releaseInput)},
			})
		}
	}

	created, err := l.orchestrations.Create(ctx, operationapp.CreateOrchestrationInput{
		ID: operationID, Kind: operationdomain.OperationKindUninstallKubernetes,
		IntentSummary:  "Uninstall k0s Platform " + launch.Platform.Name,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "releaseServers": launch.ReleaseServers},
		Definition:     "platform-uninstall", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RetryOfOperationID: launch.RetryOfOperationID, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// platformDeploymentObserver translates successful platform operations into projections.
type platformDeploymentObserver struct {
	credentials *platformapp.DeploymentCredentialService
	platforms   *platformapp.PlatformService
	operations  *operationapp.ExecutionService
	servers     serverdomain.ServerRepository
}

func (o platformDeploymentObserver) OperationSucceeded(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	if operation.PlatformID == "" {
		return
	}
	switch operation.Kind {
	case operationdomain.OperationKindDeployKubernetes:
		o.completeDeployment(ctx, operation, result)
	case operationdomain.OperationKindUninstallKubernetes:
		o.completeUninstall(ctx, operation)
	}
}

func (o platformDeploymentObserver) completeDeployment(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	credential := platformCredentialFromResult(result)
	if credential == nil {
		slog.Error("platform deployment succeeded but returned no credential",
			"operationId", operation.ID, "platformId", operation.PlatformID)
		return
	}
	if err := o.credentials.Record(ctx, operation.PlatformID, *credential); err != nil {
		slog.Error("record platform deployment credential",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
	}
}

func (o platformDeploymentObserver) completeUninstall(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
) {
	if err := o.platforms.CompleteUninstall(ctx, operation.PlatformID); err != nil {
		slog.Error("complete platform uninstall projections",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
		return
	}
	o.restoreExporters(ctx, operation)
}

func (o platformDeploymentObserver) restoreExporters(ctx context.Context, operation *operationdomain.ExecutionOperation) {
	restore, _ := operation.ExtraVars[restoreExportersVar].(bool)
	if !restore {
		return
	}

	targetIDs := make([]string, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		server, err := o.servers.FindByID(ctx, serverID)
		if err != nil {
			slog.Warn("skip exporter restoration target",
				"operationId", operation.ID, "serverId", serverID, "error", err)
			continue
		}
		if server.Absent || server.Provisioning == nil ||
			server.Provisioning.State != "deployed" || server.Provisioning.Locked {
			continue
		}
		targetIDs = append(targetIDs, serverID)
	}
	if len(targetIDs) == 0 {
		return
	}

	if _, err := o.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: string(operationdomain.OperationKindInstallExporters),
		Intent: "Restore host exporters after uninstalling platform " +
			stringField(operation.ExtraVars, platformNameVar),
		TargetServerIDs: targetIDs,
		PlatformID:      operation.PlatformID,
		RequestedBy:     "system",
	}); err != nil {
		slog.Error("queue exporter restoration after platform uninstall",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
	}
}

// platformCredentialFromResult reads the credential written by a deployment playbook.
func platformCredentialFromResult(result operationdomain.RunnerResult) *platformapp.DeploymentCredential {
	if result.Data == nil {
		return nil
	}
	endpoint := stringField(result.Data, "apiEndpoint")
	token := stringField(result.Data, "token")
	if endpoint == "" || token == "" {
		return nil
	}
	return &platformapp.DeploymentCredential{
		APIEndpoint: endpoint, Token: token,
		CACertificate: stringField(result.Data, "caCertificate"),
	}
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

// AnsibleStepSucceeded adapts a v3 queue result to the same Platform completion policy
// used by historical v2 executions.
func (o platformDeploymentObserver) AnsibleStepSucceeded(
	ctx context.Context,
	execution *operationdomain.AnsibleExecution,
	result operationdomain.RunnerResult,
) error {
	if execution.PlatformID == "" {
		return nil
	}
	operation := &operationdomain.ExecutionOperation{
		ID: execution.OperationID, Kind: execution.Kind, SiteID: execution.SiteID,
		PlatformID: execution.PlatformID, TargetServerIDs: execution.TargetServerIDs,
		ExtraVars: execution.ExtraVars,
	}
	switch execution.Kind {
	case operationdomain.OperationKindDeployKubernetes:
		credential := platformCredentialFromResult(result)
		if credential == nil {
			return fmt.Errorf("deployment returned no Kubernetes credential")
		}
		return o.credentials.Record(ctx, execution.PlatformID, *credential)
	case operationdomain.OperationKindUninstallKubernetes:
		if err := o.platforms.CompleteUninstall(ctx, execution.PlatformID); err != nil {
			return err
		}
		o.restoreExporters(ctx, operation)
	}
	return nil
}
