package domain

import "context"

type SiteRepository interface {
	Create(ctx context.Context, site *Site) error
	FindByID(ctx context.Context, id string) (*Site, error)
	List(ctx context.Context) ([]*Site, error)
	Update(ctx context.Context, site *Site) error
	Delete(ctx context.Context, id string) error
}

// IntegrationFilter narrows an integration listing. Zero values mean no constraint.
type IntegrationFilter struct {
	SiteID string
	Kind   IntegrationKind
	// EnabledOnly restricts results to integrations a user has not paused.
	EnabledOnly bool
}

// IntegrationRepository persists integrations and their credentials.
//
// Credentials are deliberately not part of the Integration struct: they are written as
// a separate argument and read only through Credential. A caller that wants a
// credential has to ask for one, which makes every such read visible in review.
// FindByID and List fill Integration.HasOperatorCredential from the stored record without
// opening the operator credential; Create and Update never write it.
type IntegrationRepository interface {
	// Create stores the integration and seals credential. An empty credential is
	// allowed for adapters that do not need one.
	Create(ctx context.Context, integration *Integration, credential string) error
	FindByID(ctx context.Context, id string) (*Integration, error)
	List(ctx context.Context, filter IntegrationFilter) ([]*Integration, error)
	// Update replaces the mutable fields. It never touches the credential or the
	// sync state, so that a user editing a name cannot clear either.
	Update(ctx context.Context, integration *Integration) error
	// ReplaceCredential seals and stores a new credential.
	ReplaceCredential(ctx context.Context, id, credential string) error
	// Credential returns the unsealed credential, or ErrCredentialNotSet.
	Credential(ctx context.Context, id string) (string, error)
	// UpdateSyncState records the outcome of a reconcile attempt.
	UpdateSyncState(ctx context.Context, id string, state SyncState) error
	Delete(ctx context.Context, id string) error
}

// OperatorCredentialRepository persists an Integration's optional operator credential: a second
// secret, beside the one ReplaceCredential and Credential handle, that authorizes what the
// Integration's credential must not (for a deployed Platform, changing its runtime).
//
// It follows the credential's rules: the value is sealed at rest with the same key, is never part
// of the Integration struct or any API response, and is read only through OperatorCredential, so
// every read is visible in review. It is optional per Integration: one written before it existed
// simply has none, with no migration. Deleting the Integration deletes it, and replacing the
// credential leaves it alone. A caller that writes both credentials of one Integration does so in
// one transaction, so the two always come from the same writer.
type OperatorCredentialRepository interface {
	// ReplaceOperatorCredential seals and stores credential as the Integration's operator
	// credential, or removes the stored one when credential is empty. It returns
	// ErrIntegrationNotFound when no Integration has id.
	ReplaceOperatorCredential(ctx context.Context, id, credential string) error
	// OperatorCredential returns the unsealed operator credential, ErrCredentialNotSet when the
	// Integration holds none, or ErrIntegrationNotFound when no Integration has id.
	OperatorCredential(ctx context.Context, id string) (string, error)
}
