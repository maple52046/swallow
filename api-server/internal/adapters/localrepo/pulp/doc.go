// Package pulp is the Pulp 3 adapter of the Local Repository provider port
// (localrepodomain.Repository and RepositoryFactory): it serves a Site's Repository Sources, Files,
// and declared images from the Pulp instance registered as the Site's `repository` Integration,
// through Pulp's REST API (as the admin user) and, to fetch declared images, its container registry
// API.
//
// Every Pulp word stays in this package: remotes, repositories, distributions, publications,
// tasks, signing services, pull-through caches, blobs, content origins, and the alias of an
// upstream registry's host. The core sees only formats, sync states, and host endpoints.
//
// The adapter keeps no sync state in memory. Each call looks Pulp's objects up by name and reads
// Pulp's task records and stored content to report a state, so a restarted or second api-server
// process gives the same answer, and nothing is lost when one is stopped mid-way. The one exception
// is the adapter's own download of a declared image, which Pulp has no task for: the process keeps
// which images it is downloading and which failed recently, only to avoid starting a download twice
// and to pace retries, while whether an image is stored is still read from Pulp. Names and base
// paths are a stored contract with existing Pulp instances: a Source named n is served by Pulp
// objects named n, and the file repository by objects named "files"; changing either orphans what an
// installation already holds.
//
// The package writes only to Pulp, never to swallow's store, and decides nothing about what a Site
// must serve; that is the core's effective spec. It must not import another adapter or a module.
package pulp
