package domain

import (
	"context"
	"errors"
	"fmt"
)

// ErrServerClaimedByPlatform means an operator asked to release, recover a deployed, or delete a
// Server that a Platform's accepted deployment claims (decision 067). Each would take the node out
// from under the Platform, so the request is refused and the Servers must be released through the
// Platform's uninstall instead. Delivery maps it to 409 conflict.
var ErrServerClaimedByPlatform = errors.New("server belongs to a platform")

// ClaimRefusedAction is the operator action a Platform Claim refused; it chooses the refusal's
// next step.
type ClaimRefusedAction int

const (
	// ClaimRefusedRelease is a Release Operation or the deprecated single-Server release.
	ClaimRefusedRelease ClaimRefusedAction = iota
	// ClaimRefusedRecover is a Recover of a deployed Server, which would release it.
	ClaimRefusedRecover
	// ClaimRefusedDelete is a Server Delete, which removes the provider Machine and the Server.
	ClaimRefusedDelete
)

// ServerClaimedByPlatformError names the Server and the Platform that claims it, so the refusal
// tells the operator where to go. PlatformName and PlatformType are empty when the Platform could
// not be read; the message then names its id. It unwraps to ErrServerClaimedByPlatform.
type ServerClaimedByPlatformError struct {
	Server       string
	PlatformID   string
	PlatformName string
	PlatformType string
	Action       ClaimRefusedAction
}

func (e *ServerClaimedByPlatformError) Error() string {
	platform := "Platform " + e.PlatformID
	if e.PlatformName != "" {
		platform = "Platform " + e.PlatformName
		if e.PlatformType != "" {
			platform = e.PlatformType + " " + platform
		}
	}
	next := "instead of releasing the Server directly"
	switch e.Action {
	case ClaimRefusedRecover:
		next = "instead of recovering it, which would release the deployed Server"
	case ClaimRefusedDelete:
		next = "or delete its record before deleting the Server"
	}
	return fmt.Sprintf("Server %s belongs to %s; uninstall the Platform (with releaseServers to also release its Servers) %s",
		e.Server, platform, next)
}

func (e *ServerClaimedByPlatformError) Unwrap() error { return ErrServerClaimedByPlatform }

// PlatformNames resolves a claiming Platform's display name and type for the refusal message. It
// is a port so provisioning never reads the platform context's model; an error or an unknown id
// leaves the message naming the Platform by id.
type PlatformNames interface {
	PlatformName(ctx context.Context, platformID string) (name, platformType string, err error)
}
