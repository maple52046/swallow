package application

import (
	"context"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/store"
)

// Controller is the SSH Key controller (glossary: Controller; decision 071). Its key is a
// provisioner Integration id, and one Reconcile is one realization pass over every SSH Key on that
// provisioner (Service.ReconcileProvisioner): add what is missing, adopt what already matches,
// remove only what swallow registered and no longer wants. A key removed in the provisioner is
// therefore added back at the next pass, which the periodic resync guarantees.
//
// A key change converges every provisioner, an Integration change converges that Integration, and
// a convergence request (POST /ssh-keys/sync) converges everything.
type Controller struct {
	service *Service
}

var _ convergence.Controller = (*Controller)(nil)

// NewController wires the controller to the service whose realization pass it runs. The worker's
// pre-deployment ensure must use the same service, so the two never interleave a pass.
func NewController(service *Service) *Controller {
	return &Controller{service: service}
}

// Name implements convergence.Controller.
func (c *Controller) Name() string { return ControllerName }

// Collections implements convergence.Controller.
func (c *Controller) Collections() []string {
	return []string{"ssh_keys", "integrations", convergence.RequestCollection}
}

// KeysForChange implements convergence.Controller.
func (c *Controller) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	switch change.Collection {
	case "integrations":
		return []string{change.Key}, nil
	case convergence.RequestCollection:
		if change.Key != ControllerName {
			return nil, nil
		}
		return c.Keys(ctx)
	default:
		return c.Keys(ctx)
	}
}

// Keys implements convergence.Controller with every enabled provisioner and every Integration that
// still has realization records.
func (c *Controller) Keys(ctx context.Context) ([]string, error) {
	return c.service.Targets(ctx)
}

// Reconcile implements convergence.Controller.
func (c *Controller) Reconcile(ctx context.Context, request convergence.Request) error {
	return c.service.ReconcileProvisioner(ctx, request.Key)
}
