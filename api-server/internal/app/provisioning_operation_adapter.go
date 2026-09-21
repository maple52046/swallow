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
	operations  *operationapp.WorkflowService
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
	steps := make([]operationdomain.Task, len(input.ServerIDs))
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
		steps[index] = operationdomain.Task{
			ID: stepID, Kind: "provision-os", Name: "Provision and verify operating system on " + serverID,
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
			Parameters: map[string]any{"request": structToMap(targetInput)},
		}
		secretStepIDs = append(secretStepIDs, stepID)
	}
	secretValues := map[string]any{}
	if input.UserData.Mode == "replace" && secretValue != "" {
		secretValues["userData"] = secretValue
	}
	created, err := l.operations.Create(ctx, operationapp.CreateWorkflowInput{
		Kind: operationdomain.WorkflowKindDeployOS, IntentSummary: fmt.Sprintf("Deploy operating system to %d Server(s)", len(input.ServerIDs)),
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
func materializeInitialDeployments(ctx context.Context, servers serverdomain.ServerRepository, operationID string, steps []operationdomain.Task) {
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
	steps := make([]operationdomain.Task, len(inputs))
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
		// Release is the primary provider-recovery path, so its allowed source states come
		// from the Swallow-owned recovery policy (deployed, failed, broken, rescue) rather
		// than a hardcoded deployed-only rule. An absent Server has no live state to act on.
		if server.Absent || server.Provisioning == nil {
			return nil, fmt.Errorf("%w: Server %s has no observed provisioning state to release", provisioningdomain.ErrInvalidReleaseRequest, server.DisplayName())
		}
		if decision := provisioningdomain.EvaluateRecovery(provisioningdomain.RecoveryIntentRelease, provisioningdomain.MachineStatus(server.Provisioning.State)); !decision.Allowed {
			return nil, fmt.Errorf("%w: Server %s %s", provisioningdomain.ErrInvalidReleaseRequest, server.DisplayName(), decision.Reason)
		}
		targetIDs[index] = input.ServerID
		steps[index] = operationdomain.Task{
			ID: "release-" + input.ServerID, Kind: "release-os", Name: "Release " + server.DisplayName(),
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: input.ServerID}},
			Parameters: map[string]any{"request": structToMap(input)},
		}
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, targetIDs); err != nil {
			return nil, err
		}
	}
	created, err := l.operations.Create(ctx, operationapp.CreateWorkflowInput{
		Kind: operationdomain.WorkflowKindReleaseOS, IntentSummary: fmt.Sprintf("Release %d Server(s)", len(inputs)),
		IntentSnapshot: map[string]any{"requests": structsToMaps(inputs)}, Definition: "os-release", DefinitionVersion: 1,
		SiteID: siteID, TargetServerIDs: targetIDs, Steps: steps, RequestedBy: requestedBy, RequestCorrelation: requestID,
	})
	if err != nil {
		return nil, err
	}
	return &provisioningapp.OperationReference{OperationID: created.ID}, nil
}

// LaunchRecover validates a bounded batch of "Return to Ready" intents and persists one
// recover-server Step per Server. Gating mirrors LaunchRelease (presence, single Site,
// duplicate target, recovery-state, and live lock) so Recover and Release cannot diverge
// on which targets they accept. A Server already ready is accepted and recorded as an
// immediate success by its Step rather than rejected, so a mixed batch converges.
func (l durableProvisioningLauncher) LaunchRecover(ctx context.Context, inputs []provisioningapp.RecoverServerInput, requestedBy, requestID string) (*provisioningapp.OperationReference, error) {
	if len(inputs) == 0 || len(inputs) > 100 {
		return nil, fmt.Errorf("%w: recover requires between 1 and 100 Servers", provisioningdomain.ErrInvalidReleaseRequest)
	}
	seen := map[string]bool{}
	siteID := ""
	steps := make([]operationdomain.Task, len(inputs))
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
		if server.Absent || server.Provisioning == nil {
			return nil, fmt.Errorf("%w: Server %s has no observed provisioning state to recover", provisioningdomain.ErrInvalidReleaseRequest, server.DisplayName())
		}
		if decision := provisioningdomain.EvaluateRecovery(provisioningdomain.RecoveryIntentRecover, provisioningdomain.MachineStatus(server.Provisioning.State)); !decision.Allowed {
			return nil, fmt.Errorf("%w: Server %s %s", provisioningdomain.ErrInvalidReleaseRequest, server.DisplayName(), decision.Reason)
		}
		targetIDs[index] = input.ServerID
		steps[index] = operationdomain.Task{
			ID: "recover-" + input.ServerID, Kind: "recover-server", Name: "Recover " + server.DisplayName(),
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: input.ServerID}},
			Parameters: map[string]any{"request": structToMap(input)},
		}
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, targetIDs); err != nil {
			return nil, err
		}
	}
	created, err := l.operations.Create(ctx, operationapp.CreateWorkflowInput{
		Kind: operationdomain.WorkflowKindRecoverServer, IntentSummary: fmt.Sprintf("Recover %d Server(s) to Ready", len(inputs)),
		IntentSnapshot: map[string]any{"requests": recoverInputsToMaps(inputs)}, Definition: "server-recovery", DefinitionVersion: 1,
		SiteID: siteID, TargetServerIDs: targetIDs, Steps: steps, RequestedBy: requestedBy, RequestCorrelation: requestID,
	})
	if err != nil {
		return nil, err
	}
	return &provisioningapp.OperationReference{OperationID: created.ID}, nil
}

// LaunchImageVerification proves one OS Image works for one deploy target by deploying it on an
// operator-chosen ready Server, recording the swallow-owned attestation on success, then
// auto-releasing the Server. The proving deploy reuses the ordinary provision-os executor, so a
// success already means the provider installed the requested image in the requested target and SSH
// came up; VerificationRun bypasses the unverified-custom-image gate for this one establishing run.
func (l durableProvisioningLauncher) LaunchImageVerification(ctx context.Context, input provisioningapp.ImageVerificationInput, requestedBy, requestID string) (*provisioningapp.OperationReference, error) {
	if strings.TrimSpace(input.ServerID) == "" || strings.TrimSpace(input.ImageID) == "" ||
		strings.TrimSpace(input.IntegrationID) == "" || strings.TrimSpace(input.Architecture) == "" {
		return nil, fmt.Errorf("%w: integrationId, imageId, architecture, and serverId are required", provisioningdomain.ErrInvalidDeploymentBatch)
	}
	if !input.DeployTarget.Valid() {
		return nil, fmt.Errorf("%w: deployTarget must be disk or ram", provisioningdomain.ErrInvalidDeploymentBatch)
	}
	server, err := l.servers.FindByID(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	if server.Absent || server.Provisioning == nil {
		return nil, fmt.Errorf("%w: Server %s has no observed provisioning state to verify on", provisioningdomain.ErrInvalidDeploymentBatch, server.DisplayName())
	}
	// The chosen Server must belong to the image's Integration; otherwise the image is not in that
	// Server's provider catalog and verifying on it would prove nothing about this image.
	if server.Source.IntegrationID != input.IntegrationID {
		return nil, fmt.Errorf("%w: Server %s does not belong to the image's Integration", provisioningdomain.ErrInvalidDeploymentBatch, server.DisplayName())
	}
	// Verify only on a ready Server so verification never disturbs one carrying real work; the
	// Server is auto-released back to ready afterwards.
	if provisioningdomain.MachineStatus(server.Provisioning.State) != provisioningdomain.MachineStatusReady {
		return nil, fmt.Errorf("%w: Server %s must be ready to verify an image (it is %s)", provisioningdomain.ErrInvalidDeploymentBatch, server.DisplayName(), server.Provisioning.State)
	}
	if !architectureMatches(server.Observed.Architecture, input.Architecture) {
		return nil, fmt.Errorf("%w: Server %s architecture %q cannot verify a %q image", provisioningdomain.ErrInvalidDeploymentBatch, server.DisplayName(), server.Observed.Architecture, input.Architecture)
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, []string{input.ServerID}); err != nil {
			return nil, err
		}
	}

	// Resolve the proving deploy through the ordinary path so it inherits the server-ready, image
	// completeness, and ephemeral-capability preflight; VerificationRun keeps the unverified-custom
	// gate from blocking the very run that will establish the verification.
	imageID := input.ImageID
	ephemeral := input.DeployTarget.Ephemeral()
	resolvedInput, _, err := l.deployments.ResolveOperationInput(ctx, provisioningapp.DeployServersInput{
		ServerIDs:       []string{input.ServerID},
		Settings:        provisioningapp.DeploymentSettingsInput{ImageID: &imageID, Ephemeral: &ephemeral},
		UserData:        provisioningapp.DeploymentUserDataInput{Mode: "omit"},
		VerificationRun: true,
	})
	if err != nil {
		return nil, err
	}

	provisionStepID := "provision-" + input.ServerID
	recordStepID := "record-verification"
	releaseStepID := "release-" + input.ServerID
	target := []operationdomain.ResourceReference{{Kind: "server", ID: input.ServerID}}
	steps := []operationdomain.Task{
		{
			ID: provisionStepID, Kind: "provision-os",
			Name:       fmt.Sprintf("Verify %s (%s deploy) on %s", imageID, input.DeployTarget, server.DisplayName()),
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    target,
			Parameters: map[string]any{"request": structToMap(resolvedInput)},
		},
		{
			ID: recordStepID, Kind: "record-image-verification", Name: "Record image verification",
			Executor:  operationdomain.RunnerKindInternal,
			Targets:   target,
			DependsOn: []string{provisionStepID},
			Parameters: map[string]any{
				"integrationId": input.IntegrationID,
				"imageId":       input.ImageID,
				"architecture":  input.Architecture,
				"deployTarget":  string(input.DeployTarget),
			},
		},
		{
			ID: releaseStepID, Kind: "release-os", Name: "Release " + server.DisplayName(),
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    target,
			DependsOn:  []string{recordStepID},
			Parameters: map[string]any{"request": structToMap(provisioningapp.ReleaseServerInput{ServerID: input.ServerID})},
		},
	}
	created, err := l.operations.Create(ctx, operationapp.CreateWorkflowInput{
		Kind:           operationdomain.WorkflowKindVerifyOSImage,
		IntentSummary:  fmt.Sprintf("Verify OS Image %s for %s deployment", imageID, input.DeployTarget),
		IntentSnapshot: map[string]any{"request": structToMap(input)},
		Definition:     "os-image-verification", DefinitionVersion: 1,
		SiteID: server.Source.SiteID, TargetServerIDs: []string{input.ServerID}, Steps: steps,
		RequestedBy: requestedBy, RequestCorrelation: requestID,
	})
	if err != nil {
		return nil, err
	}
	materializeInitialDeployments(ctx, l.servers, created.ID, steps)
	return &provisioningapp.OperationReference{OperationID: created.ID}, nil
}

// architectureMatches compares a provider-reported Server architecture against an OS Image
// architecture so the verify preflight neither rejects a valid pairing nor accepts a
// cross-architecture one. It delegates to the domain rule so the launcher and the deploy gate share
// one definition of architecture compatibility.
func architectureMatches(serverArch, imageArch string) bool {
	return provisioningdomain.ArchitecturesCompatible(serverArch, imageArch)
}

func recoverInputsToMaps(values []provisioningapp.RecoverServerInput) []map[string]any {
	result := make([]map[string]any, len(values))
	for index, value := range values {
		value.RequestID = strings.TrimSpace(value.RequestID)
		result[index] = structToMap(value)
	}
	return result
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
