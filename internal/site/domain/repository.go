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
	// EnabledOnly restricts results to integrations an operator has not paused.
	EnabledOnly bool
}

// IntegrationRepository persists integrations and their credentials.
//
// Credentials are deliberately not part of the Integration struct: they are written as
// a separate argument and read only through Credential. A caller that wants a
// credential has to ask for one, which makes every such read visible in review.
type IntegrationRepository interface {
	// Create stores the integration and seals credential. An empty credential is
	// allowed for adapters that do not need one.
	Create(ctx context.Context, integration *Integration, credential string) error
	FindByID(ctx context.Context, id string) (*Integration, error)
	List(ctx context.Context, filter IntegrationFilter) ([]*Integration, error)
	// Update replaces the mutable fields. It never touches the credential or the
	// sync state, so that an operator editing a name cannot clear either.
	Update(ctx context.Context, integration *Integration) error
	// ReplaceCredential seals and stores a new credential.
	ReplaceCredential(ctx context.Context, id, credential string) error
	// Credential returns the unsealed credential, or ErrCredentialNotSet.
	Credential(ctx context.Context, id string) (string, error)
	// UpdateSyncState records the outcome of a reconcile attempt.
	UpdateSyncState(ctx context.Context, id string, state SyncState) error
	Delete(ctx context.Context, id string) error
}
