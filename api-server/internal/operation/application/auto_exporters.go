package application

import (
	"context"
	"log/slog"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// autoExporterInstallPlaybook is passed explicitly so auto-install does not depend on the
// site having mapped the install-exporters kind; the playbook is a manifest entry, so the
// catalog still validates it.
const autoExporterInstallPlaybook = "install-exporters"

// ExporterOwnerResolver reports the effective exporter owner of a server, so the
// auto-deploy use case does not need to know how ownership is decided (locked machine,
// platform policy, or the ansible default). It returns one of "ansible", "k8s", or
// "unmanaged". Implemented outside the operation context to keep platform policy there.
type ExporterOwnerResolver interface {
	EffectiveExporterOwner(ctx context.Context, server *serverdomain.Server) (string, error)
}

// AutoExporterDeployUseCase installs the Ansible exporters on a server once it reaches the
// deployed state, so an operator does not have to trigger monitoring by hand.
//
// It is deliberately conservative: it only acts on servers whose effective exporter owner
// is ansible (so a locked or k8s-owned host is left alone), and it creates at most one
// install-exporters operation per server. A site whose automation is disabled or has no
// credential is skipped without error, because "not wired up yet" is a normal early state
// rather than a failure.
type AutoExporterDeployUseCase struct {
	servers        serverdomain.ServerRepository
	operations     operationdomain.ExecutionRepository
	executor       *ExecutionService
	orchestrations operationdomain.WorkflowRepository
	owners         ExporterOwnerResolver
}

// NewAutoExporterDeployUseCase wires the auto-install loop's dependencies.
func NewAutoExporterDeployUseCase(
	servers serverdomain.ServerRepository,
	operations operationdomain.ExecutionRepository,
	executor *ExecutionService,
	owners ExporterOwnerResolver,
	orchestrations ...operationdomain.WorkflowRepository,
) *AutoExporterDeployUseCase {
	useCase := &AutoExporterDeployUseCase{servers: servers, operations: operations, executor: executor, owners: owners}
	if len(orchestrations) > 0 {
		useCase.orchestrations = orchestrations[0]
	}
	return useCase
}

// Run scans deployed servers and installs exporters where they are missing and owned by
// ansible. It is safe to call on an interval: creation is deduplicated per server, and the
// underlying playbook is idempotent, so a repeated pass does no extra work.
func (uc *AutoExporterDeployUseCase) Run(ctx context.Context) error {
	// Unpaginated because this is a complete sweep, matching how discovery reads servers.
	result, err := uc.servers.List(ctx, serverdomain.ListFilter{
		ProvisioningState: "deployed",
		Limit:             0,
	})
	if err != nil {
		return err
	}

	for _, server := range result.Servers {
		owner, err := uc.owners.EffectiveExporterOwner(ctx, server)
		if err != nil {
			slog.Warn("auto-exporters: could not resolve owner", "serverId", server.ID, "error", err)
			continue
		}
		// Only ansible-owned hosts are auto-installed. A locked machine resolves to
		// unmanaged and a k8s-owned host to k8s, and both must be left untouched.
		if owner != string(platformExporterOwnerAnsible) {
			continue
		}

		already, err := uc.hasInstallOperation(ctx, server.ID)
		if err != nil {
			slog.Warn("auto-exporters: dedup check failed", "serverId", server.ID, "error", err)
			continue
		}
		if already {
			continue
		}

		_, err = uc.executor.Create(ctx, CreateExecutionInput{
			Kind:            string(operationdomain.WorkflowKindInstallExporters),
			Intent:          "Automatic exporter install after OS deployment",
			TargetServerIDs: []string{server.ID},
			PlaybookName:    autoExporterInstallPlaybook,
			RequestedBy:     "system",
		})
		if err != nil {
			// Disabled automation, a missing credential, a busy target, or a lock are
			// all expected reasons not to act now; log at debug and move on rather than
			// failing the whole sweep.
			slog.Debug("auto-exporters: skipped server", "serverId", server.ID, "reason", err)
			continue
		}
		slog.Info("auto-exporters: queued install", "serverId", server.ID)
	}
	return nil
}

// hasInstallOperation reports whether an install-exporters operation already exists for
// the server in any state, so a one-shot auto-install is not repeated on every sweep. A
// failed attempt is left for an operator to retry rather than being recreated in a loop.
func (uc *AutoExporterDeployUseCase) hasInstallOperation(ctx context.Context, serverID string) (bool, error) {
	existing, err := uc.operations.List(ctx, operationdomain.ExecutionListFilter{
		ServerID: serverID,
		Kind:     operationdomain.WorkflowKindInstallExporters,
		Limit:    1,
	})
	if err != nil {
		return false, err
	}
	if existing.Total > 0 {
		return true, nil
	}
	if uc.orchestrations != nil {
		v3, total, listErr := uc.orchestrations.List(ctx, operationdomain.WorkflowFilter{ServerID: serverID, Kind: operationdomain.WorkflowKindInstallExporters, Limit: 1})
		_ = v3
		if listErr != nil {
			return false, listErr
		}
		return total > 0, nil
	}
	return false, nil
}

// platformExporterOwnerAnsible mirrors the platform domain's ansible owner value without
// importing the platform package, keeping the operation context free of platform types. The
// resolver returns the same string.
const platformExporterOwnerAnsible = "ansible"
