package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// durableProvisioningLauncher performs full preflight before saving a v3 Operation.
type durableProvisioningLauncher struct {
	deployments *provisioningapp.DeployServersUseCase
	operations  *operationapp.OrchestrationService
	servers     serverdomain.ServerRepository
	protection  serverdomain.MutationGuard
}

func (l durableProvisioningLauncher) LaunchDeployment(ctx context.Context, input provisioningapp.DeployServersInput, requestedBy, requestID string) (*provisioningapp.OperationReference, error) {
	resolvedInput, secretValue, err := l.deployments.ResolveOperationInput(ctx, input)
	if err != nil {
		return nil, err
	}
	input = resolvedInput
	if len(input.ServerIDs) == 0 {
		return nil, fmt.Errorf("%w: serverIds is required", provisioningdomain.ErrInvalidDeploymentBatch)
	}
	// Reject the deployment at acceptance if any target is locked. The executor
	// re-checks the lock again before each host mutation; this early check fails fast
	// and keeps a locked Server from ever entering a deploy Operation.
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, input.ServerIDs); err != nil {
			return nil, err
		}
	}
	first, err := l.servers.FindByID(ctx, input.ServerIDs[0])
	if err != nil {
		return nil, err
	}
	steps := make([]operationdomain.OperationStep, len(input.ServerIDs))
	secretStepIDs := make([]string, 0, len(input.ServerIDs))
	for index, serverID := range input.ServerIDs {
		targetInput := input
		targetInput.ServerIDs = []string{serverID}
		if input.Network != nil {
			network := *input.Network
			network.Assignments = nil
			for _, assignment := range input.Network.Assignments {
				if assignment.ServerID == serverID {
					network.Assignments = []provisioningapp.DeploymentNetworkAssignmentInput{assignment}
				}
			}
			targetInput.Network = &network
		}
		stepID := "provision-" + serverID
		steps[index] = operationdomain.OperationStep{
			ID: stepID, Kind: "provision-os", Name: "Provision and verify operating system on " + serverID,
			Executor:   operationdomain.StepExecutorMAAS,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
			Parameters: map[string]any{"request": structToMap(targetInput)},
		}
		secretStepIDs = append(secretStepIDs, stepID)
	}
	secretValues := map[string]any{}
	if input.UserData.Mode == "replace" && secretValue != "" {
		secretValues["userData"] = secretValue
	}
	created, err := l.operations.Create(ctx, operationapp.CreateOrchestrationInput{
		Kind: operationdomain.OperationKindDeployOS, IntentSummary: fmt.Sprintf("Deploy operating system to %d Server(s)", len(input.ServerIDs)),
		IntentSnapshot: map[string]any{"request": structToMap(input)}, Definition: "os-deployment", DefinitionVersion: 1,
		SiteID: first.Source.SiteID, TargetServerIDs: input.ServerIDs, Steps: steps,
		RequestedBy: requestedBy, RequestCorrelation: requestID,
		SecretStepIDs: secretStepIDs, SecretValues: secretValues,
	})
	if err != nil {
		return nil, err
	}
	materializeInitialDeployments(ctx, l.servers, created.ID, steps)
	return &provisioningapp.OperationReference{OperationID: created.ID}, nil
}

// materializeInitialDeployments closes the API-to-worker visibility gap so returning
// to Servers immediately starts polling the accepted durable deployment.
func materializeInitialDeployments(ctx context.Context, servers serverdomain.ServerRepository, operationID string, steps []operationdomain.OperationStep) {
	if servers == nil {
		return
	}
	now := time.Now().UTC()
	for _, step := range steps {
		if step.Kind != "provision-os" {
			continue
		}
		serverID := firstTargetServer(step)
		if serverID == "" {
			continue
		}
		attempt := step.Attempt
		if attempt < 1 {
			attempt = 1
		}
		if err := servers.SetDeployment(ctx, serverID, &serverdomain.DeploymentStatus{
			State: serverdomain.DeploymentDeploying, OperationID: operationID,
			StepID: step.ID, Attempt: attempt, StartedAt: now, UpdatedAt: now,
		}); err != nil {
			slog.Error("materialize initial Server deployment projection", "operationId", operationID, "serverId", serverID, "error", err)
		}
	}
}

func (l durableProvisioningLauncher) LaunchRelease(ctx context.Context, inputs []provisioningapp.ReleaseServerInput, requestedBy, requestID string) (*provisioningapp.OperationReference, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, fmt.Errorf("%w: release requires between 1 and 100 Servers", provisioningdomain.ErrInvalidReleaseRequest)
	}
	seen := map[string]bool{}
	siteID := ""
	steps := make([]operationdomain.OperationStep, len(inputs))
	targetIDs := make([]string, len(inputs))
	for index, input := range inputs {
		if seen[input.ServerID] {
			return nil, fmt.Errorf("%w: duplicate Server %s", provisioningdomain.ErrInvalidReleaseRequest, input.ServerID)
		}
		seen[input.ServerID] = true
		server, err := l.servers.FindByID(ctx, input.ServerID)
		if err != nil {
			return nil, err
		}
		if siteID == "" {
			siteID = server.Source.SiteID
		} else if siteID != server.Source.SiteID {
			return nil, fmt.Errorf("%w: all Servers must belong to one Site", provisioningdomain.ErrInvalidReleaseRequest)
		}
		if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" {
			return nil, fmt.Errorf("%w: Server %s is not deployed", provisioningdomain.ErrInvalidReleaseRequest, server.DisplayName())
		}
		targetIDs[index] = input.ServerID
		steps[index] = operationdomain.OperationStep{
			ID: "release-" + input.ServerID, Kind: "release-os", Name: "Release " + server.DisplayName(),
			Executor:   operationdomain.StepExecutorMAAS,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: input.ServerID}},
			Parameters: map[string]any{"request": structToMap(input)},
		}
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, targetIDs); err != nil {
			return nil, err
		}
	}
	created, err := l.operations.Create(ctx, operationapp.CreateOrchestrationInput{
		Kind: operationdomain.OperationKindReleaseOS, IntentSummary: fmt.Sprintf("Release %d Server(s)", len(inputs)),
		IntentSnapshot: map[string]any{"requests": structsToMaps(inputs)}, Definition: "os-release", DefinitionVersion: 1,
		SiteID: siteID, TargetServerIDs: targetIDs, Steps: steps, RequestedBy: requestedBy, RequestCorrelation: requestID,
	})
	if err != nil {
		return nil, err
	}
	return &provisioningapp.OperationReference{OperationID: created.ID}, nil
}

func structToMap(value any) map[string]any {
	raw, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}

func structsToMaps(values []provisioningapp.ReleaseServerInput) []map[string]any {
	result := make([]map[string]any, len(values))
	for index, value := range values {
		value.RequestID = strings.TrimSpace(value.RequestID)
		result[index] = structToMap(value)
	}
	return result
}
