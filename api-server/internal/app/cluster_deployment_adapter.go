package app

import (
	"context"
	"log/slog"

	clusterapp "github.com/maple52046/swallow/internal/cluster/application"
	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

const (
	deployKubernetesKind        = "deploy-kubernetes"
	uninstallKubernetesKind     = "uninstall-kubernetes"
	uninstallKubernetesPlaybook = "uninstall-kubernetes"
	restoreExportersVar         = "swallow_restore_ansible_exporters"
	clusterNameVar              = "swallow_cluster_name"
)

// clusterDeploymentLauncher adapts cluster lifecycle intent onto operation execution.
type clusterDeploymentLauncher struct {
	operations *operationapp.ExecutionService
}

func (l clusterDeploymentLauncher) Launch(ctx context.Context, launch clusterdomain.DeploymentLaunch) (string, error) {
	item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: deployKubernetesKind, Intent: "Deploy k0s cluster " + launch.Cluster.Name,
		TargetServerIDs: launch.TargetServerIDs, ClusterID: launch.Cluster.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

func (l clusterDeploymentLauncher) LaunchUninstall(
	ctx context.Context,
	launch clusterdomain.UninstallLaunch,
) (string, error) {
	item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: uninstallKubernetesKind, Intent: "Uninstall k0s cluster " + launch.Cluster.Name,
		TargetServerIDs: launch.TargetServerIDs, ClusterID: launch.Cluster.ID,
		PlaybookName: uninstallKubernetesPlaybook,
		TrustedVars: map[string]any{
			restoreExportersVar: launch.RestoreExporters,
			clusterNameVar:      launch.Cluster.Name,
		},
		RetryOfOperationID: launch.RetryOfOperationID,
		RequestedBy:        launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// clusterDeploymentObserver translates successful cluster operations into projections.
type clusterDeploymentObserver struct {
	credentials *clusterapp.DeploymentCredentialService
	clusters    *clusterapp.ClusterService
	operations  *operationapp.ExecutionService
	servers     serverdomain.ServerRepository
}

func (o clusterDeploymentObserver) OperationSucceeded(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	if operation.ClusterID == "" {
		return
	}
	switch operation.Kind {
	case operationdomain.OperationKindDeployKubernetes:
		o.completeDeployment(ctx, operation, result)
	case operationdomain.OperationKindUninstallKubernetes:
		o.completeUninstall(ctx, operation)
	}
}

func (o clusterDeploymentObserver) completeDeployment(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	credential := clusterCredentialFromResult(result)
	if credential == nil {
		slog.Error("cluster deployment succeeded but returned no credential",
			"operationId", operation.ID, "clusterId", operation.ClusterID)
		return
	}
	if err := o.credentials.Record(ctx, operation.ClusterID, *credential); err != nil {
		slog.Error("record cluster deployment credential",
			"operationId", operation.ID, "clusterId", operation.ClusterID, "error", err)
	}
}

func (o clusterDeploymentObserver) completeUninstall(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
) {
	if err := o.clusters.CompleteUninstall(ctx, operation.ClusterID); err != nil {
		slog.Error("complete cluster uninstall projections",
			"operationId", operation.ID, "clusterId", operation.ClusterID, "error", err)
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
		Intent: "Restore host exporters after uninstalling cluster " +
			stringField(operation.ExtraVars, clusterNameVar),
		TargetServerIDs: targetIDs,
		ClusterID:       operation.ClusterID,
		RequestedBy:     "system",
	}); err != nil {
		slog.Error("queue exporter restoration after cluster uninstall",
			"operationId", operation.ID, "clusterId", operation.ClusterID, "error", err)
	}
}

// clusterCredentialFromResult reads the credential written by a deployment playbook.
func clusterCredentialFromResult(result operationdomain.RunnerResult) *clusterapp.DeploymentCredential {
	if result.Data == nil {
		return nil
	}
	endpoint := stringField(result.Data, "apiEndpoint")
	token := stringField(result.Data, "token")
	if endpoint == "" || token == "" {
		return nil
	}
	return &clusterapp.DeploymentCredential{
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
