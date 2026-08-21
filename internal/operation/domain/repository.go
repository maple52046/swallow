package domain

import "context"

// ListFilter narrows an operation listing. Zero values mean no constraint.
type ListFilter struct {
	SiteID    string
	ClusterID string
	ServerID  string
	Kind      OperationKind
	Status    Status
	// ActiveOnly restricts results to operations that have not reached a terminal
	// status, which is what the poller needs.
	ActiveOnly bool

	Offset int
	Limit  int
}

type ListResult struct {
	Operations []*Operation
	Total      int
}

type OperationRepository interface {
	Create(ctx context.Context, operation *Operation) error
	FindByID(ctx context.Context, id string) (*Operation, error)
	// FindByJobID finds the operation mirroring a controller job, which is how a
	// webhook naming a job is turned into something gdcm can refresh.
	FindByJobID(ctx context.Context, integrationID, jobID string) (*Operation, error)
	List(ctx context.Context, filter ListFilter) (ListResult, error)
	// UpdateAutomation replaces the automation reference, which is the only part of
	// an operation that changes after creation.
	UpdateAutomation(ctx context.Context, id string, ref AutomationRef) error
	// FindActiveByServerIDs returns unfinished operations targeting any of these
	// servers, so that overlapping work can be refused.
	FindActiveByServerIDs(ctx context.Context, serverIDs []string) ([]*Operation, error)
}
