package domain

import (
	"context"
	"sort"
	"time"
)

// OSImageVerification is swallow-owned attestation that a provider-owned OS Image was proven to
// deploy successfully for a given deploy target.
//
// Verification is not a provider capability: the provider does not certify that its image works,
// swallow does, by running a real deploy on an operator-chosen Server and observing success. It is
// therefore swallow-owned data (like a Provider Data Overlay, docs/decisions/025) keyed by the same
// provider image identity — (IntegrationID, ImageID, Architecture) — because one image name can back
// several architectures. An image with no verification row is simply unverified; absence is normal,
// not an error.
//
// The evidence is per deploy target (disk / ram): an image can be verified for RAM but not disk, or
// both. Each target carries the evidence of the run that proved it.
type OSImageVerification struct {
	IntegrationID string
	ImageID       string
	Architecture  string
	// Targets holds one evidence entry per deploy target that has been proven. A target absent from
	// the map is unverified.
	Targets map[DeployTarget]OSImageVerificationEvidence
	// FailedTargets holds one entry per deploy target whose most recent verification run failed. It
	// exists so a failed verification is visibly distinct from a never-attempted one — the operator
	// can tell "we tried and it does not deploy this way" apart from "we have not tried". A target is
	// in Targets or FailedTargets but never both: recording one outcome clears the other, so the map
	// always reflects the latest run.
	FailedTargets map[DeployTarget]OSImageVerificationFailure
	// UpdatedAt is swallow's own last-write time for this record; not a provider observation.
	UpdatedAt time.Time
}

// OSImageVerificationEvidence records the run that proved one deploy target works, so the
// attestation is auditable rather than a bare boolean.
type OSImageVerificationEvidence struct {
	// VerifiedAt is when the proving deploy succeeded.
	VerifiedAt time.Time
	// OperationID is the verification Workflow that proved it.
	OperationID string
	// ServerID is the Server the verification deploy ran on.
	ServerID string
}

// OSImageVerificationFailure records the most recent verification run that failed for one deploy
// target, so the failure is auditable and the operator can open the run for the provider's reason.
type OSImageVerificationFailure struct {
	// FailedAt is when the proving deploy failed.
	FailedAt time.Time
	// OperationID is the verification Workflow whose proving deploy failed; the provider's reason
	// lives on that Workflow's failed provision Task.
	OperationID string
	// ServerID is the Server the failed verification deploy ran on.
	ServerID string
	// Reason is a short, bounded summary of why it failed, when the recorder has one; the full
	// provider detail stays on the Operation rather than being duplicated here.
	Reason string
}

// VerifiedTargets returns the deploy targets this image has been verified for, sorted for a stable
// projection. Nil-safe: a nil verification yields no targets.
func (v *OSImageVerification) VerifiedTargets() []DeployTarget {
	if v == nil || len(v.Targets) == 0 {
		return nil
	}
	targets := make([]DeployTarget, 0, len(v.Targets))
	for target := range v.Targets {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	return targets
}

// IsVerified reports whether this image has been verified for one deploy target. Nil-safe.
func (v *OSImageVerification) IsVerified(target DeployTarget) bool {
	if v == nil {
		return false
	}
	_, ok := v.Targets[target]
	return ok
}

// FailedTargets returns the deploy targets whose most recent verification run failed, sorted for a
// stable projection. Nil-safe: a nil verification yields no targets.
func (v *OSImageVerification) FailedTargetsList() []DeployTarget {
	if v == nil || len(v.FailedTargets) == 0 {
		return nil
	}
	targets := make([]DeployTarget, 0, len(v.FailedTargets))
	for target := range v.FailedTargets {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	return targets
}

// OSImageVerificationRepository persists swallow-owned OS Image verification attestations.
//
// Implementations must be safe for concurrent use and enforce the (IntegrationID, ImageID,
// Architecture) key as a uniqueness constraint. Kept separate from the display overlay so the
// overlay's "delete when it has no overrides" rule can never erase a verification. A missing record
// is a normal absence (the image is unverified), not an error.
type OSImageVerificationRepository interface {
	// ListByIntegration returns every verification stored for one Integration, or an empty slice.
	ListByIntegration(ctx context.Context, integrationID string) ([]*OSImageVerification, error)
	// Find returns the verification for one image identity, or nil when the image is unverified.
	Find(ctx context.Context, integrationID, imageID, architecture string) (*OSImageVerification, error)
	// RecordTarget idempotently marks one deploy target verified with its evidence, creating the
	// record on first use and adding or replacing only that target's evidence otherwise. Recording a
	// success clears any prior failure for the same target, so the record reflects the latest run.
	RecordTarget(ctx context.Context, integrationID, imageID, architecture string, target DeployTarget, evidence OSImageVerificationEvidence) error
	// RecordFailedTarget idempotently marks one deploy target's most recent verification as failed,
	// creating the record on first use. Recording a failure clears any prior success for the same
	// target, so a target that stops deploying is no longer shown as verified.
	RecordFailedTarget(ctx context.Context, integrationID, imageID, architecture string, target DeployTarget, failure OSImageVerificationFailure) error
	// Delete removes the verification for one image identity. An absent record is success, so the
	// caller can prune verification when the image itself is deleted without a prior existence check.
	Delete(ctx context.Context, integrationID, imageID, architecture string) error
	// DeleteByIntegration removes every verification for one Integration, so deleting an Integration
	// does not leave orphan attestations for images that can no longer be read.
	DeleteByIntegration(ctx context.Context, integrationID string) error
}
