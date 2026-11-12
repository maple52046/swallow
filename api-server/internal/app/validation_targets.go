package app

import (
	"context"
	"errors"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
)

// validationTargets is the validation context's Targets port over the Server and Platform stores:
// whether a Server is deployed and claimed, and a Platform's type, lifecycle, members, and the access
// Servers its module names for its spec.
type validationTargets struct {
	servers   serverdomain.ServerRepository
	platforms platformdomain.PlatformRepository
	lifecycle platformdomain.LifecycleReader
	modules   map[platformdomain.PlatformType]platformdomain.PlatformModule
}

// Server implements validationapp.Targets.
func (t validationTargets) Server(ctx context.Context, id string) (validationapp.TargetServer, error) {
	server, err := t.servers.FindByID(ctx, id)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return validationapp.TargetServer{}, fmt.Errorf("%w: Server %s", validationdomain.ErrTargetNotFound, id)
	}
	if err != nil {
		return validationapp.TargetServer{}, err
	}
	target := validationapp.TargetServer{
		ID: server.ID, Name: server.DisplayName(), SiteID: server.Source.SiteID,
		Deployed: !server.Absent && server.Provisioning != nil && server.Provisioning.State == "deployed",
	}
	if server.PlatformClaim != nil {
		target.ClaimedBy = server.PlatformClaim.PlatformID
	}
	return target, nil
}

// Platform implements validationapp.Targets. The members and access Servers come from the spec the
// Platform last applied (what is deployed), or its spec before the first one applied.
func (t validationTargets) Platform(ctx context.Context, id string) (validationapp.TargetPlatform, error) {
	platform, err := t.platforms.FindByID(ctx, id)
	if errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return validationapp.TargetPlatform{}, fmt.Errorf("%w: Platform %s", validationdomain.ErrTargetNotFound, id)
	}
	if err != nil {
		return validationapp.TargetPlatform{}, err
	}
	snapshots, err := t.lifecycle.Read(ctx, []string{id})
	if err != nil {
		return validationapp.TargetPlatform{}, err
	}
	target := validationapp.TargetPlatform{
		ID: platform.ID, Name: platform.Name, SiteID: platform.SiteID, Type: string(platform.Type),
		Active: snapshots[id].State == platformdomain.PlatformLifecycleActive,
	}
	spec := platform.Status.Applied
	if spec == nil {
		spec = platform.Spec
	}
	if spec == nil {
		return target, nil
	}
	for _, member := range spec.Members {
		target.Members = append(target.Members, validationapp.PlatformMember{ServerID: member.ServerID, Roles: member.Roles})
	}
	if module, ok := t.modules[platform.Type]; ok {
		target.AccessServerIDs = module.AccessServers(*spec)
	}
	return target, nil
}

// validationWorkflowCanceler is the validation context's WorkflowCanceler over the Workflow service.
type validationWorkflowCanceler struct {
	workflows *workflowapp.WorkflowService
}

// CancelWorkflow implements validationapp.WorkflowCanceler.
func (c validationWorkflowCanceler) CancelWorkflow(ctx context.Context, workflowID, reason string) error {
	return c.workflows.CancelFor(ctx, workflowID, reason)
}
