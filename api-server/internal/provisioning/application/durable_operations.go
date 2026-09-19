package application

import "context"

// OperationReference is returned when durable provisioning intent has been accepted.
type OperationReference struct {
	OperationID string `json:"operationId"`
}

// RecoverServerInput carries a provider-neutral "Return to Ready" intent for one Server.
// It reuses the Release cleanup control because a recovery path that ends in a Release
// still owns the same static-IP follow-up; disk-erase controls are intentionally absent
// because Recover chooses its primitive by state rather than accepting erase options.
type RecoverServerInput struct {
	ServerID        string
	Comment         string
	UnbindStaticIPs bool
	RequestID       string
}

// DurableOperationLauncher adapts provisioning intent into Operation Steps without making
// the provisioning context depend on Temporal or Operation persistence.
type DurableOperationLauncher interface {
	LaunchDeployment(ctx context.Context, input DeployServersInput, requestedBy, requestID string) (*OperationReference, error)
	LaunchRelease(ctx context.Context, inputs []ReleaseServerInput, requestedBy, requestID string) (*OperationReference, error)
	// LaunchRecover accepts a bounded batch of "Return to Ready" intents for Servers whose
	// provisioning axis is not usable, gating each target on the recovery policy before
	// persisting one recover-server Step per Server.
	LaunchRecover(ctx context.Context, inputs []RecoverServerInput, requestedBy, requestID string) (*OperationReference, error)
}
