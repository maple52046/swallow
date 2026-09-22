package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

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

// ImageVerificationInput carries the intent to prove one OS Image works for one deploy target by
// deploying it on an operator-chosen ready Server. By default the Server is returned to the ready
// pool after the verification (success or failure), so a verification consumes the Server only
// briefly; KeepServer leaves the Server deployed instead, for an operator who wants to keep and use
// (or inspect) the verified deployment.
type ImageVerificationInput struct {
	IntegrationID string       `json:"integrationId"`
	ImageID       string       `json:"imageId"`
	Architecture  string       `json:"architecture"`
	DeployTarget  DeployTarget `json:"deployTarget"`
	ServerID      string       `json:"serverId"`
	// KeepServer leaves the chosen Server deployed after the verification instead of returning it
	// to the ready pool. Default false: the verification borrows the Server and gives it back.
	KeepServer bool   `json:"keepServer,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
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
	// LaunchImageVerification proves an image for a deploy target: a provision-os step in the
	// target mode, a record finalize step (success or failure), then — unless the input keeps the
	// Server — a recover-server step that returns the chosen Server to the ready pool.
	LaunchImageVerification(ctx context.Context, input ImageVerificationInput, requestedBy, requestID string) (*OperationReference, error)
}

// DeployTarget is re-exported from the domain vocabulary for the launcher input; disk installs to
// disk, ram runs from memory (ephemeral).
type DeployTarget = provisioningdomain.DeployTarget
