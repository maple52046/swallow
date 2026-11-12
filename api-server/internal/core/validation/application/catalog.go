// Package application holds the Validation use cases (decision 081, docs/development/validation.md):
// the suite catalog, accepting a Validation Run and its Workflow, folding what the run's Tasks
// return into its results and status, the gate a deployment's composed suite passes through, and
// the Validation Schedules and their controller. It reaches Servers, Platforms, and the Workflow
// engine only through the ports it declares, implemented in the composition root; it must not
// import an adapter, a module, or another context's application layer.
package application

import (
	"context"
	"fmt"
	"sort"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// Catalog is every Validation Suite: the module suites the build declares, fixed for the process,
// and the user suites in the store.
type Catalog struct {
	modules map[string]validationdomain.Suite
	order   []string
	users   validationdomain.SuiteRepository
}

// NewCatalog builds the catalog over the build's module suites; two module suites with one id are
// refused (the module registry refuses them first).
func NewCatalog(modules []validationdomain.Suite, users validationdomain.SuiteRepository) (*Catalog, error) {
	catalog := &Catalog{modules: map[string]validationdomain.Suite{}, users: users}
	for _, suite := range modules {
		if _, exists := catalog.modules[suite.ID]; exists {
			return nil, fmt.Errorf("validation suite %q is declared twice", suite.ID)
		}
		catalog.modules[suite.ID] = suite
		catalog.order = append(catalog.order, suite.ID)
	}
	sort.Strings(catalog.order)
	return catalog, nil
}

// Get returns the suite id, module or user (retired ones included); ErrSuiteNotFound otherwise.
func (c *Catalog) Get(ctx context.Context, id string) (validationdomain.Suite, error) {
	if suite, ok := c.modules[id]; ok {
		return suite, nil
	}
	return c.users.Get(ctx, id)
}

// Module returns the module suite id, without reading the store.
func (c *Catalog) Module(id string) (validationdomain.Suite, bool) {
	suite, ok := c.modules[id]
	return suite, ok
}

// List returns the module suites by id, then the user suites by name; retired user suites only when
// includeRetired.
func (c *Catalog) List(ctx context.Context, includeRetired bool) ([]validationdomain.Suite, error) {
	suites := make([]validationdomain.Suite, 0, len(c.order))
	for _, id := range c.order {
		suites = append(suites, c.modules[id])
	}
	users, err := c.users.List(ctx, includeRetired)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(users, func(i, j int) bool { return users[i].Name < users[j].Name })
	return append(suites, users...), nil
}
