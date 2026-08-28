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

const deploymentReadinessWorkers = 4

// DeploymentTargetIssue describes one Server-specific prerequisite that blocks
// the current batch. Codes and messages are application data; delivery adapters own
// their wire representation.
type DeploymentTargetIssue struct {
	ServerID string
	Code     string
	Message  string
}

// PreflightDeploymentTargetsResult is a completed, side-effect-free inspection.
// Valid false is not an execution error: Issues contains the operator-remediable
// conflicts in that case.
type PreflightDeploymentTargetsResult struct {
	Valid         bool
	IntegrationID string
	Issues        []DeploymentTargetIssue
}

// validatedDeploymentTargets carries canonical Server projections and ordered
// readiness issues between the shared inspection and final deployment use cases.
type validatedDeploymentTargets struct {
	servers       []*serverdomain.Server
	integrationID string
	issues        []DeploymentTargetIssue
}

// targetReadinessOutcome keeps concurrent provider reads deterministic by storing
// each result at its input index until all workers have joined.
type targetReadinessOutcome struct {
	issue *DeploymentTargetIssue
	err   error
}

// DeploymentTargetPreflightService applies the target rules shared by the read-only
// endpoint and final deployment. It performs no provider writes and bounds optional
// provider-owned readiness reads to four concurrent calls.
type DeploymentTargetPreflightService struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

// NewDeploymentTargetPreflightService wires repositories that must be safe for
// concurrent reads; nil dependencies are a composition-root programming error.
func NewDeploymentTargetPreflightService(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *DeploymentTargetPreflightService {
	return &DeploymentTargetPreflightService{servers: servers, providers: providers}
}

// Execute reports target conflicts without dispatching a provider action. Malformed
// input and failed repository/provider reads remain errors; a successfully inspected
// but blocked batch returns a result whose Valid field is false.
func (s *DeploymentTargetPreflightService) Execute(
	ctx context.Context,
	serverIDs []string,
) (*PreflightDeploymentTargetsResult, error) {
	validated, err := s.validate(ctx, serverIDs)
	if err != nil {
		return nil, err
	}
	return &PreflightDeploymentTargetsResult{
		Valid:         len(validated.issues) == 0,
		IntegrationID: validated.integrationID,
		Issues:        validated.issues,
	}, nil
}

// validate resolves canonical Server projections before touching the provisioner.
// Local conflicts short-circuit remote inspection so an outage cannot hide an
// immediately actionable batch problem.
func (s *DeploymentTargetPreflightService) validate(
	ctx context.Context,
	serverIDs []string,
) (*validatedDeploymentTargets, error) {
	if len(serverIDs) == 0 || len(serverIDs) > maxDeploymentTargets {
		return nil, fmt.Errorf(
			"%w: serverIds must contain between 1 and %d targets",
			provisioningdomain.ErrInvalidDeploymentBatch,
			maxDeploymentTargets,
		)
	}

	seen := make(map[string]struct{}, len(serverIDs))
	validated := &validatedDeploymentTargets{
		servers: make([]*serverdomain.Server, 0, len(serverIDs)),
		issues:  make([]DeploymentTargetIssue, 0),
	}
	for _, id := range serverIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("%w: serverIds cannot contain an empty ID", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("%w: serverIds must be unique", provisioningdomain.ErrInvalidDeploymentBatch)
		}
		seen[id] = struct{}{}

		server, err := s.servers.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		validated.servers = append(validated.servers, server)
		if validated.integrationID == "" {
			validated.integrationID = server.Source.IntegrationID
		}
		switch {
		case server.Source.IntegrationID != validated.integrationID:
			validated.issues = append(validated.issues, DeploymentTargetIssue{
				ServerID: id,
				Code:     "integration_mismatch",
				Message:  "All targets must belong to the same provisioner integration.",
			})
		case server.Absent:
			validated.issues = append(validated.issues, DeploymentTargetIssue{
				ServerID: id,
				Code:     "absent",
				Message:  "The Server is absent from its provisioner.",
			})
		case server.Provisioning == nil || server.Provisioning.State != string(provisioningdomain.MachineStatusReady):
			validated.issues = append(validated.issues, DeploymentTargetIssue{
				ServerID: id,
				Code:     "not_ready",
				Message:  "The Server provisioning state is not ready.",
			})
		}
	}
	if len(validated.issues) > 0 {
		return validated, nil
	}

	provider, err := s.providers.For(ctx, validated.integrationID)
	if err != nil {
		return nil, err
	}
	if !provider.Capabilities().DeploymentReadiness {
		return validated, nil
	}
	inspector, ok := provider.(provisioningdomain.DeploymentTargetValidator)
	if !ok {
		return nil, fmt.Errorf("provider %q advertises deployment readiness without implementing it", provider.Name())
	}

	outcomes := make([]targetReadinessOutcome, len(serverIDs))
	jobs := make(chan int)
	workerCount := deploymentReadinessWorkers
	if len(serverIDs) < workerCount {
		workerCount = len(serverIDs)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				err := inspector.ValidateDeploymentTarget(ctx, validated.servers[index].Source.ProviderMachineID)
				outcomes[index].issue, outcomes[index].err = deploymentReadinessIssue(serverIDs[index], err)
			}
		}()
	}
	for index := range serverIDs {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	for _, outcome := range outcomes {
		if outcome.err != nil {
			return nil, outcome.err
		}
		if outcome.issue != nil {
			validated.issues = append(validated.issues, *outcome.issue)
		}
	}
	return validated, nil
}

// deploymentReadinessIssue turns remediable provider state into report data while
// preserving connectivity, authentication, and internal failures as execution errors.
func deploymentReadinessIssue(serverID string, err error) (*DeploymentTargetIssue, error) {
	if err == nil {
		return nil, nil
	}
	if errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		return &DeploymentTargetIssue{
			ServerID: serverID,
			Code:     "provider_machine_missing",
			Message:  "The provisioner no longer has this machine.",
		}, nil
	}
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorRejected {
		return &DeploymentTargetIssue{
			ServerID: serverID,
			Code:     "provider_not_ready",
			Message:  providerErr.Detail,
		}, nil
	}
	return nil, err
}

// deploymentTargetConflict converts a read-only report issue into the sentinel used
// by final deployment to reject the batch before any provider write.
func deploymentTargetConflict(issue DeploymentTargetIssue) error {
	return fmt.Errorf(
		"%w: server %s: %s",
		provisioningdomain.ErrDeploymentBatchConflict,
		issue.ServerID,
		issue.Message,
	)
}
