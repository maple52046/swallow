# Local Repository

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard` (the Infrastructure area's Local repository page)
- swallow installation tooling (writes the first spec — `serveDeployments` and the Site's OS
  archive source — after registering the `repository` Integration)

## Purpose

Read and change a Site's Local Repository: when the Site chooses it (`spec.serveDeployments`), the
one place the Site's Servers fetch every artifact a deployment installs, so deployments do not reach
the Internet ([decision 078](../../../../../docs/decisions/078-local-repository.md) (local
repository)); otherwise deployments fetch each artifact from the upstream its module declares.
swallow owns what the repository must hold — the artifacts the compiled modules declare plus the
Repository Sources a user adds (the spec) — and, while the spec serves deployments, the worker
converges the Site's `repository` Integration to it and records what it observes. These routes read
that record, browse the content live, replace the spec, and ask for an immediate convergence. They
also define when a deployment is refused because the Site's Local Repository is not ready
([Deployments](#deployments)).

A Site has a Local Repository exactly while it has a `repository` Integration
([sites-integrations.md](sites-integrations.md#repository-settings)); every route answers
`404 not_found` for a Site without one.

## Related Glossary Terms

- [Local Repository](../../../../../docs/development/glossaries/terms/local-repository.md)
- [Repository Source](../../../../../docs/development/glossaries/terms/repository-source.md)
- [Integration](../../../../../docs/development/glossaries/terms/integration.md)
- [Desired State](../../../../../docs/development/glossaries/terms/desired-state.md),
  [Observed State](../../../../../docs/development/glossaries/terms/observed-state.md)

## Endpoints

```text
GET  /api/v1/sites/{siteId}/local-repository
GET  /api/v1/sites/{siteId}/local-repository/content?format=&source=&search=&page=&pageSize=
PUT  /api/v1/sites/{siteId}/local-repository/spec
POST /api/v1/sites/{siteId}/local-repository/sync
```

## Wire Shape

The field-level shape is defined in `openapi/areas/local-repository.yaml`.

| Operation | operationId |
| --------- | ----------- |
| `GET /api/v1/sites/{siteId}/local-repository` | `getLocalRepository` |
| `GET /api/v1/sites/{siteId}/local-repository/content` | `browseLocalRepositoryContent` |
| `PUT /api/v1/sites/{siteId}/local-repository/spec` | `putLocalRepositorySpec` |
| `POST /api/v1/sites/{siteId}/local-repository/sync` | `syncLocalRepository` |

## Authentication

Bearer token (a Session access token or an API Key), as in [conventions](conventions.md).

## Authorization

Every route requires the `admin` role.

## Read A Site's Local Repository

`GET /api/v1/sites/{siteId}/local-repository` is read from swallow's store; the provider is not
called. The response holds:

- `siteId`, `integrationId` (the Site's `repository` Integration), and `contentOrigin` (that
  Integration's `contentOrigin` setting, `null` when unset).
- `spec`: the user's part, `{generation, serveDeployments, sources[]}`. `generation` is `0` before
  the first write. `serveDeployments` is whether the Site's deployments fetch their artifacts from
  this Local Repository; it is `false` until a write sets it, and while it is `false` they fetch them
  from the upstreams the modules declare. Each source has `name`, `format`, `url`, `suites`,
  `components`, and `architectures` (empty arrays when not set).
- `sources[]`: the effective Repository Sources, ordered by `name`: every source a module declares,
  every user source, and the pull-through sources swallow derives — one `oci` source per registry
  host a declared image names (`name` is the host, `url` is `https://<host>`) and the `python`
  source `pypi` (`https://pypi.org/`) when a module declares a Python requirement. Sources of one
  `name` are merged: the first declaration fixes `format` and `url`, and later ones with the same
  `format` and `url` add their suites, components, and architectures. `declaredBy` lists who
  declared it, in order: `module:<id>` or `user` (a pull-through source lists the declarers of its
  images or requirements). A declared source of a pull-through source's name and format keeps its
  own `url` (a mirror of that registry).
- `files[]`: the files the modules declare, ordered by `path`, with `url`, `sha256`, and
  `declaredBy`.
- `images[]`: the container images the modules declare, ordered by `reference`, with `declaredBy`.
  The worker fetches each in advance through the pull-through source of its registry host once that
  source is `ready` (until then the image is `pending`), and observes it until the provider stores
  every layer of it (of an index, the linux/amd64 image); a fetch that was interrupted or failed is
  started again.
- `python[]` (`requirement`, `declaredBy`): the Python requirements the modules declare. They have
  no state of their own: they are fetched through the `pypi` source when a Server first asks.
- Per source, file, and image, the observed `state`: `pending` (not fetched yet, or not observed
  since its definition last changed; an image the worker has not observed under its reference),
  `syncing`, `ready`, or `failed`; `message` (the provider's reason, `null` when none); a source's
  `lastSyncedAt` (its last successful sync, `null` before one) and `contentCount` (how many items it
  serves, `0` when unknown); a file's `sizeBytes` (`0` until fetched) and an image's `sizeBytes`
  (the total size of its config and layers, `0` until it is stored).
- `ready`: the spec serves deployments, the Integration is enabled, the worker's last pass was not
  stopped by the Integration or its provider (see [Conditions](#conditions)), every source, file,
  and image is `ready`, and the addresses Servers read the repository at are known. While the spec
  serves deployments, the ones that fetch artifacts are accepted exactly while it is `true`. It is
  computed when the response is built, from the current spec and modules and the recorded state.
- `observedAt`: when the worker last completed a pass on the Integration, `null` before the first.
- `observedGeneration`: the spec generation that pass converged; it equals `spec.generation` once
  the latest spec has been observed.
- `conditions`: the worker's judgements ([Conditions](#conditions)), `[]` before it first looked at
  the Integration.

The state comes from the worker, which converges each Site at its resync (every
`controllerResyncInterval`), after a spec or Integration change, after a sync request, and, while
a source, file, or image is `pending` or `syncing`, again with a growing delay of at most five
minutes (an image waiting for a `failed` source waits for the resync). The Integration's credential
and the provider's own identifiers never appear.

### Conditions

Each condition has the shared shape (`type`, `status`, `reason`, `message`, `lastTransitionTime`,
`observedGeneration`; [desired-state-model.md](../../../../../docs/development/desired-state-model.md)
§4.2) and is written only by the worker's `local-repository` controller. Its `observedGeneration` is
the spec generation it was judged for. The types:

| Type | `true` means | Reasons |
| --- | --- | --- |
| `ready` | Deployments that fetch from the Local Repository are accepted. | `ready`; when `false`, the first thing in the way: `not_serving` (the spec does not serve deployments), `integration_disabled`, a `realized` reason that stopped the last pass (below), `not_observed`, `source_failed`, `file_failed`, `image_failed`, `syncing`, or `endpoints_unknown`. The message lists everything in the way; while the spec does not serve deployments only the settings are (`not_serving`, `integration_disabled`, a `realized` reason that stopped a pass), since nothing is converged. |
| `realized` | The provider serves every effective source, file, and image. | `converged`; `syncing` (a source, file, or image is `pending` or `syncing`), `source_failed`, `file_failed`, `image_failed` (the provider's reason is in the message); or, when the pass stopped, `provider_unreachable` (retried with a growing delay), `credential_missing` (the Integration has no credential), `credential_rejected` (the provider refused it), `content_origin_missing` (the Integration has no `contentOrigin`), `integration_invalid` (the provider adapter refused the Integration's configuration; the message has its reason). |
| `requires_attention` | A user must fix the Integration: `realized` is `credential_missing`, `credential_rejected`, `content_origin_missing`, or `integration_invalid` (the same reason and message). | `none` when `false`. |

A pass that stops records only the conditions: the sources, files, and images keep their last
observed state and `observedAt`, which is then older than the conditions' `lastTransitionTime`. A stopped
pass is tried again when the Integration changes (for example a new credential or setting), at the
next resync, or after a sync request. While the spec does not serve deployments the worker does not
converge it (nothing is synced or fetched, and what the provider already holds stays) and records
only `ready` `false` with `not_serving`; while the Integration is disabled it does the same with
`integration_disabled`. In both cases `realized` keeps its last judgement.

## Browse The Content

`GET /api/v1/sites/{siteId}/local-repository/content` reads one page of content live from the
provider. `format` (`deb`, `rpm`, `oci`, `python`, or `file`) is required; `source` keeps only one
Repository Source's content and `search` only items whose name contains it. `page` and `pageSize`
follow [conventions](conventions.md#pagination); a value outside their range is
`400 validation_error` rather than replaced. The response is the shared page envelope; each item
has `name` and, where its format has them, `version`, `architecture`, `source`, `path`, and
`sizeBytes` (`null` otherwise). The response is `Cache-Control: no-store`.

## Replace The Spec

`PUT /api/v1/sites/{siteId}/local-repository/spec` takes `{generation, serveDeployments, sources[]}`
and replaces the whole spec. `generation` is the spec generation the writer read (`0` before the
first write); when it is not the stored one the write is `409 conflict` and nothing changes.
`serveDeployments` is required (see [Deployments](#deployments)); turning it on starts the worker's
convergence, and deployments are accepted once the Local Repository is `ready`. `sources` is the
whole list and replaces the stored one; the generation then increases by one. Each source:

- `name`: 1 to 63 lower-case letters, digits, `.`, `_`, or `-`, starting and ending with a letter or
  digit, unique in the list. It becomes part of the address Servers read the source at.
- `format`: one of the formats above.
- `url`: an `http` or `https` URL with a host and without credentials (the spec is stored and shown
  in plain text).
- `suites` and `components`: at least one each for `deb`, refused for every other format.
  `architectures`: optional for `deb` and `rpm`, refused otherwise. Values are single words, each
  listed once.

A source that contradicts a module's declaration of the same name (another `format` or `url`), or
takes the name and another format of a pull-through source, is `400 validation_error` naming both.
The write is stored and answered at once with the Local Repository (`200`); the worker converges
the provider afterwards, so the new sources read `pending` until it observes them. Sources a user
removes are removed from the provider at the next convergence.

## Request A Convergence

`POST /api/v1/sites/{siteId}/local-repository/sync` has no body. It asks the worker to converge every
Site's Local Repository now rather than at its next resync and answers `202` at once; the result
appears in a later read. The provider decides which sources need a new sync; the request does not
force one.

## Deployments

A playbook the release manifest marks `fetchesArtifacts` installs or downloads the artifacts its
modules declare. When a Workflow that runs one is accepted — an OS deployment's Tasks, a Platform
deploy, a Managed Software install or uninstall, or `POST /api/v1/workflows` — swallow decides where
the Site's artifacts come from, reading its store (the provider is not called):

- **From the upstreams** when the Site has no `repository` Integration or its spec does not serve
  deployments (`serveDeployments` `false`, the default). Each artifact comes from the upstream its
  module declares: a file from its URL (verified against its sha256), an image from its registry, a
  Python package from PyPI, and a declared deb source from its URL, verified with the signing key
  the module declares; the host keeps its own OS package sources.
- **From the Local Repository** when its spec serves deployments. It must be `ready`; otherwise the
  request is refused with `409 conflict` before any Workflow is created. The message says what to
  do: enable the Integration, fix what stopped the worker's last pass (the `realized` message, for
  example a missing `contentOrigin`), or wait for (or fix) the sources, files, and images it names,
  for example
  `the site's local repository is not ready: source ubuntu is syncing; file k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64 failed: checksum mismatch; image registry.k8s.io/nfd/node-feature-discovery:v0.18.3 is syncing`.
  swallow never falls back to the upstreams for such a Site.

Either way the playbook's Task receives the trusted variable `swallow_artifacts`, frozen at
acceptance, with the same keys:

```yaml
swallow_artifacts:
  source: local_repository        # or upstream
  deb:                            # the apt sources the host adds, ordered by name
    - name: ubuntu
      uri: http://10.170.168.20:8080/pulp/content/deb/ubuntu/
      suites: [noble, noble-updates, noble-security]
      components: [main, universe]
      signing_key_url: ""         # upstream: the declared key's URL
      signing_key_fingerprint: "" # upstream: its 40-hex-digit fingerprint
  deb_signing_key: "-----BEGIN PGP PUBLIC KEY BLOCK-----..."   # local_repository: signs every source
  registry: "10.170.168.20:8080"  # local_repository: the registry's host:port
  registry_mirrors:               # local_repository: upstream registry host -> mirror URL
    docker.io: http://10.170.168.20:8080/v2/docker.io
  python_index: http://10.170.168.20:8080/pypi/pypi/simple/   # local_repository: pip's index
  files:                          # declared path -> the URL to fetch it from
    k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64: http://10.170.168.20:8080/pulp/content/files/k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64
  images:                         # declared reference -> the reference to pull
    docker.io/grafana/grafana:12.4.12: 10.170.168.20:8080/docker.io/grafana/grafana:12.4.12
```

From the Local Repository, `deb` lists every effective deb source at the provider's address and
`deb_signing_key` is the Site's key (`""` when unsigned); from the upstreams, `deb` lists only the
deb sources the modules declare, at their URLs with their signing keys, and `deb_signing_key`,
`registry`, and `python_index` are `""` and `registry_mirrors` is `{}`. `files` and `images` always
list every declared file and image. A kind nothing serves is empty. No caller can set or override
the variable.

A Platform's Workflows started by the worker (a spec change the platform controller converges) are
refused the same way, and the Platform's `realized` Condition reports the message.

## Error Codes

| Code | HTTP Status | Description |
| ---- | ----------: | ----------- |
| `validation_error` | 400 | The spec body is not valid JSON, `generation` is missing or negative, `serveDeployments` is missing, or a source is invalid, listed twice, or contradicts a module's declaration; a content query has no or an unknown `format` or an out-of-range page; or the provider refused the content query (the message is its reason). |
| `not_found` | 404 | The Site does not exist, or it has no `repository` Integration. |
| `conflict` | 409 | A spec write named a `generation` other than the stored one. |
| `provider_unavailable` | 503 | Browsing only: the provider could not be reached or refused the Integration's credential, or the Integration has no credential. |

## Compatibility Notes

- Added 2026-11-06. Before, deployments fetched their artifacts from the Internet; since then a
  deployment whose playbooks are marked `usesLocalRepository` is `409 conflict` on a Site without
  a ready Local Repository (breaking for an installation without one). The same day the response
  gained `observedGeneration` and `conditions`, and `ready` became `false` while the worker's last
  pass was stopped by the Integration or its provider.
- 2026-11-06, later: `images[]` items gained `state`, `message`, and `sizeBytes`, the Conditions
  gained the reason `image_failed`, and `ready` became `false` until every declared image is stored,
  so a deployment is refused while an image it pulls is still being fetched rather than reaching the
  Internet at deploy time.
- 2026-11-06, later (breaking): the spec gained `serveDeployments`, required on every spec write and
  `false` for a record written before. Only a Site whose spec serves deployments is refused while its
  Local Repository is not ready; every other Site, and a Site without a `repository` Integration,
  fetches from the upstreams the modules declare. The Conditions gained the reason `not_serving`.
  The playbook flag `usesLocalRepository` became `fetchesArtifacts` and the trusted variable
  `swallow_local_repository` became `swallow_artifacts`, whose `files` is a map of declared paths to
  URLs and which gained `source` and `images`.

## Implementation Notes

- The record is the `local_repositories` collection, one document per Site; the API writes only its
  spec and the worker's `local-repository` controller only its status
  ([desired-state-model.md](../../../../../docs/development/desired-state-model.md) §7.4).
