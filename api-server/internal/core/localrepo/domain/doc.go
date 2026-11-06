// Package domain is the Local Repository context's model (glossary: Local Repository, Repository
// Source): the per-Site source of every artifact a deployment installs, so hosts fetch packages,
// container images, Python packages, and files from the Site rather than from the internet.
//
// swallow owns what a Site's Local Repository must hold: the artifacts the compiled modules declare
// plus the Repository Sources a user adds. A provider (the Site's `repository` Integration) mirrors
// or caches them and reports their sync state, which is observed state. This package holds the
// format vocabulary, the desired resource and its observed status, the store port, the provider
// port (Repository, RepositoryFactory), the merge rules of the effective spec, and the domain
// errors. It must not name a product, a package manager, or a registry: those words belong to the
// provider adapter and to the modules. It must not import MongoDB, Fiber, or any adapter.
package domain
