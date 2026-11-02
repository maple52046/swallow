package domain

import (
	"context"
	"errors"
	"time"
)

// ProvisioningTaskKind identifies durable provider-backed coordination owned by
// the OS Provisioning context rather than by the Ansible Operation context.
type ProvisioningTaskKind string

const (
	// ProvisioningTaskReleaseNetworkCleanup waits for Release to reach Ready,
	// then removes unchanged static links captured before dispatch.
	ProvisioningTaskReleaseNetworkCleanup ProvisioningTaskKind = "release_network_cleanup"
)

// ProvisioningTaskStatus is the durable task execution lifecycle.
type ProvisioningTaskStatus string

const (
	ProvisioningTaskPending   ProvisioningTaskStatus = "pending"
	ProvisioningTaskRunning   ProvisioningTaskStatus = "running"
	ProvisioningTaskSucceeded ProvisioningTaskStatus = "succeeded"
	ProvisioningTaskFailed    ProvisioningTaskStatus = "failed"
)

// ProvisioningTaskPhase gives a user useful progress without exposing the
// provider request or persisted cleanup snapshot.
type ProvisioningTaskPhase string

const (
	ProvisioningTaskWaitingForRelease ProvisioningTaskPhase = "waiting_for_release"
	ProvisioningTaskWaitingForReady   ProvisioningTaskPhase = "waiting_for_ready"
	ProvisioningTaskCleaningNetwork   ProvisioningTaskPhase = "cleaning_network"
	ProvisioningTaskComplete          ProvisioningTaskPhase = "complete"
)

// StaticNetworkLinkSnapshot is the exact pre-release binding a cleanup task may
// remove. Any live mismatch means the user changed it and cleanup preserves it.
type StaticNetworkLinkSnapshot struct {
	InterfaceID string
	LinkID      string
	SubnetID    string
	IPAddress   string
}

// ProvisioningTask stores only the coordination data swallow owns. Provider
// credentials and request bodies must never be added to this aggregate.
type ProvisioningTask struct {
	ID                string
	Kind              ProvisioningTaskKind
	ServerID          string
	IntegrationID     string
	ProviderMachineID string
	Status            ProvisioningTaskStatus
	Phase             ProvisioningTaskPhase
	Attempt           int
	Snapshot          []StaticNetworkLinkSnapshot
	Error             string
	RequestID         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ProvisioningTaskRepository stores cleanup tasks and their history order. The release Task that
// owns the Server's resource lease is the only writer of a task while it runs, so the repository
// keeps no lease of its own.
type ProvisioningTaskRepository interface {
	Create(ctx context.Context, task *ProvisioningTask) error
	FindByID(ctx context.Context, id string) (*ProvisioningTask, error)
	ListByServer(ctx context.Context, serverID string) ([]*ProvisioningTask, error)
	Save(ctx context.Context, task *ProvisioningTask) error
	// Retry returns a failed task to pending and counts the attempt.
	Retry(ctx context.Context, id string, now time.Time) error
}

var (
	// ErrProvisioningTaskNotFound means a task ID is unknown.
	ErrProvisioningTaskNotFound = errors.New("provisioning task not found")
	// ErrProvisioningTaskConflict means the task is not in a retryable state.
	ErrProvisioningTaskConflict = errors.New("provisioning task conflict")
)
