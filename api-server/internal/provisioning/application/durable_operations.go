package application

import "context"

// OperationReference is returned when durable provisioning intent has been accepted.
type OperationReference struct {
	OperationID string `json:"operationId"`
}

// DurableOperationLauncher adapts provisioning intent into Operation Steps without making
// the provisioning context depend on Temporal or Operation persistence.
type DurableOperationLauncher interface {
	LaunchDeployment(ctx context.Context, input DeployServersInput, requestedBy, requestID string) (*OperationReference, error)
	LaunchRelease(ctx context.Context, inputs []ReleaseServerInput, requestedBy, requestID string) (*OperationReference, error)
}
