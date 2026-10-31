package domain

import (
	"context"
	"errors"
	"fmt"
)

// MutationGuard verifies provider-owned protection immediately before Swallow accepts
// or starts work that can change one or more Servers.
type MutationGuard interface {
	RequireUnlocked(ctx context.Context, serverIDs []string) error
}

var (
	// ErrServerLocked means the provisioner confirms that a Server is protected.
	ErrServerLocked = errors.New("server is locked")
	// ErrServerLockUnavailable means Swallow cannot safely establish the live lock state.
	ErrServerLockUnavailable = errors.New("server lock state is unavailable")
)

// ServerLockedError identifies the protected Server and gives the operator the only
// remediation that makes mutation valid.
type ServerLockedError struct {
	Name string
}

func (e *ServerLockedError) Error() string {
	return fmt.Sprintf("Server %q is locked. Unlock it before starting this action.", e.Name)
}

func (e *ServerLockedError) Unwrap() error {
	return ErrServerLocked
}

// ServerLockUnavailableError prevents a provider outage from becoming permission to
// mutate a Server whose live protection state is unknown.
type ServerLockUnavailableError struct {
	Name string
}

func (e *ServerLockUnavailableError) Error() string {
	return fmt.Sprintf("Lock state for Server %q could not be confirmed. Try again when the provisioner is available.", e.Name)
}

func (e *ServerLockUnavailableError) Unwrap() error {
	return ErrServerLockUnavailable
}
