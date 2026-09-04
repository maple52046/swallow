package app

import (
	"context"
	"log/slog"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const (
	deployKubernetesKind        = "deploy-kubernetes"
	uninstallKubernetesKind     = "uninstall-kubernetes"
	uninstallKubernetesPlaybook = "uninstall-kubernetes"
	restoreExportersVar         = "swallow_restore_ansible_exporters"
	platformNameVar             = "swallow_platform_name"
)

// platformDeploymentLauncher adapts platform lifecycle intent onto operation execution.
type platformDeploymentLauncher struct {
	operations *operationapp.ExecutionService
}

func (l platformDeploymentLauncher) Launch(ctx context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: deployKubernetesKind, Intent: "Deploy k0s cluster " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

func (l platformDeploymentLauncher) LaunchUninstall(
	ctx context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: uninstallKubernetesKind, Intent: "Uninstall k0s cluster " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		PlaybookName: uninstallKubernetesPlaybook,
		TrustedVars: map[string]any{
			restoreExportersVar: launch.RestoreExporters,
			platformNameVar:     launch.Platform.Name,
		},
		RetryOfOperationID: launch.RetryOfOperationID,
		RequestedBy:        launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
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
