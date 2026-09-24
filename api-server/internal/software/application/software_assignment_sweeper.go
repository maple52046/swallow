package application

import (
	"context"
	"errors"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// SoftwareAssignmentSweeper enforces the OS-lifecycle invariant that a Software Assignment must not
// outlive the software on disk: when a Server leaves `deployed` (release, recover to ready, or a
// reinstall), its non-absent assignments are marked `absent`. It runs periodically off the same
// interval as membership sync. It only marks absent; it never installs or removes software.
type SoftwareAssignmentSweeper struct {
	assignments softwaredomain.AssignmentRepository
	servers     serverdomain.ServerRepository
}

// NewSoftwareAssignmentSweeper constructs the OS-lifecycle assignment sweep.
func NewSoftwareAssignmentSweeper(
	assignments softwaredomain.AssignmentRepository,
	servers serverdomain.ServerRepository,
) *SoftwareAssignmentSweeper {
	return &SoftwareAssignmentSweeper{assignments: assignments, servers: servers}
}

// Execute reconciles every non-absent assignment against its Server's current provisioning state.
// A missing or non-deployed Server marks the assignment absent. It returns the first error so a
// caller can log it; a partial sweep still made progress on the assignments it reached.
func (s *SoftwareAssignmentSweeper) Execute(ctx context.Context) error {
	assignments, err := s.assignments.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, assignment := range assignments {
		if assignment.State == softwaredomain.StateAbsent {
			continue
		}
		if !s.stillDeployed(ctx, assignment.ServerID) {
			if setErr := s.assignments.SetState(ctx, assignment.ServerID, assignment.Kind, softwaredomain.StateAbsent, assignment.LastWorkflowID, nil); setErr != nil {
				return setErr
			}
		}
	}
	return nil
}

// stillDeployed reports whether the Server exists and its provisioning axis is `deployed`. A
// not-found Server (deleted from inventory) counts as no longer deployed. Any other repository
// error is treated as "unknown", leaving the assignment untouched rather than wrongly clearing it.
func (s *SoftwareAssignmentSweeper) stillDeployed(ctx context.Context, serverID string) bool {
	server, err := s.servers.FindByID(ctx, serverID)
	if err != nil {
		if errors.Is(err, serverdomain.ErrServerNotFound) {
			return false
		}
		return true
	}
	return !server.Absent && server.Provisioning != nil && server.Provisioning.State == "deployed"
}
