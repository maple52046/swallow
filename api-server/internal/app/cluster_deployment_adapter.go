package app

import (
	"context"
	"log/slog"

	clusterapp "github.com/maple52046/swallow/internal/cluster/application"
	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// deployKubernetesKind is the operation kind that builds a cluster. It is referenced here
// rather than imported as a constant to keep the adapter's coupling to a single string.
const deployKubernetesKind = "deploy-kubernetes"

// clusterDeploymentLauncher adapts the cluster context's launcher port onto the operation
// service, so the cluster context can start a build operation without depending on the
// operation context. The role assignment and k0s settings arrive as trusted vars, and the
// VRRP password as a sealed secret var, both assembled by the cluster deploy use case.
type clusterDeploymentLauncher struct {
	operations *operationapp.ExecutionService
}

func (l clusterDeploymentLauncher) Launch(ctx context.Context, launch clusterdomain.DeploymentLaunch) (string, error) {
	item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind:            deployKubernetesKind,
		Intent:          "Deploy k0s cluster " + launch.Cluster.Name,
		TargetServerIDs: launch.TargetServerIDs,
		ClusterID:       launch.Cluster.ID,
		TrustedVars:     launch.TrustedVars,
		SecretVars:      launch.SecretVars,
		RequestedBy:     launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// clusterDeploymentObserver adapts a successful operation into a cluster credential record.
// It implements the operation context's completion observer, inspects only the operations
// it recognises, and translates the run's captured result into the cluster context's own
// vocabulary, so neither context depends on the other's internals.
type clusterDeploymentObserver struct {
	credentials *clusterapp.DeploymentCredentialService
}

func (o clusterDeploymentObserver) OperationSucceeded(ctx context.Context, operation *operationdomain.ExecutionOperation, result operationdomain.RunnerResult) {
	if operation.Kind != operationdomain.OperationKindDeployKubernetes || operation.ClusterID == "" {
		return
	}
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

// clusterCredentialFromResult reads the credential a k0s deployment playbook writes to its
// result file. Missing endpoint or token yields nil, which the caller logs.
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
		APIEndpoint:   endpoint,
		Token:         token,
		CACertificate: stringField(result.Data, "caCertificate"),
	}
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}
