// Package application holds the Local Repository use cases: the merge of the compiled modules'
// declarations with a Site's spec into the effective spec (Effective), the controller that
// converges a Site's provider to it (Controller, run by the worker), the API's reads and spec
// writes (Service), and the deployment-time resolver of the hosts' trusted vars (Resolver).
//
// It reaches the provider only through localrepodomain.Repository and the store only through
// localrepodomain.Store; it must not import an adapter, Fiber, or a database driver, and must not
// name a product. HTTP shaping belongs to the delivery package.
package application
