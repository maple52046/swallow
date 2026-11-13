package domain

import (
	"context"
	"time"
)

// AuditAction names one kind of access change; the values are wire values of the access contract.
type AuditAction string

const (
	AuditUserCreated       AuditAction = "user.created"
	AuditUserUpdated       AuditAction = "user.updated"
	AuditUserDeleted       AuditAction = "user.deleted"
	AuditMemberAdded       AuditAction = "member.added"
	AuditMemberUpdated     AuditAction = "member.updated"
	AuditMemberRemoved     AuditAction = "member.removed"
	AuditPoolPolicyUpdated AuditAction = "pool_policy.updated"
	AuditSettingsUpdated   AuditAction = "settings.updated"
)

// AuditEntry is one recorded access change: who did what to whom, and a short summary. It is
// written in the same transaction as the change it records, and never carries a password or
// any other secret.
type AuditEntry struct {
	ID      string
	At      time.Time
	ActorID string
	Actor   string
	Action  AuditAction
	Target  string
	PoolID  string
	Detail  string
}

// AuditRepository stores audit entries and lists the most recent ones, newest first. Record joins
// the caller's transaction through ctx.
type AuditRepository interface {
	Record(ctx context.Context, entry AuditEntry) error
	List(ctx context.Context, limit int) ([]AuditEntry, error)
}
