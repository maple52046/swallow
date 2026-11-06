# 075. Module contract v1: one manifest and one Go package per module, assembled by the composition root

- Status: Accepted
- Date: 2026-11-01
- 現行規格：[core-and-extensions.md](../development/core-and-extensions.md) §5、§9；
  [module-authoring.md](../development/module-authoring.md)

## Context

[ADR 070](070-core-providers-and-extensions.md) made what swallow manages an extension module (a plugin)
loaded from a manifest that carries a contract version, and left the manifest format and the Go hook
interface to the first stage. Since the 2026-10-31 layout move every module has a directory under
`api-server/modules/<kind>/<id>/` with its playbooks and roles, but nothing declares a module: the
core recognizes Kubernetes, Slurm, AMD, and each Managed Software kind by name, the launchers in
`internal/app` compose their Workflows, and the only shared declaration is the release's playbook
allowlist (`modules/manifest.json`). Three processes (api, worker, ansible-executor) each assemble
their own view, so a Workflow kind or completion type one process knows can be missing in another.

## Decision

1. **A module declares itself in `modules/<kind>/<id>/module.json`.** The manifest is data:
   `contractVersion`, `kind` (`platform`, `accelerator`, or `software`), `id` (the directory name),
   `version` (SemVer of the module), the playbook names it runs, the Workflow kinds it plans, the Task
   kinds and completion types it declares, the JSON Schema of its spec (when the module defines a
   resource spec), its Go hooks each with the reason data cannot express it, the provider
   capabilities it needs, the HTTP routes it serves, and the `artifacts` its host automation fetches
   from the Site's Local Repository (package sources, checksummed files, container images, pinned
   Python requirements). Artifacts are not owned: two modules may declare the same source or file
   when the declarations agree, and the registry refuses them when they do not. Adding an optional
   field like this one keeps the contract at v1.
2. **A module with Go code has one root Go package in its directory** that embeds its `module.json`
   and returns the module: the manifest plus its hook implementations. Hook interfaces are ports in
   the core context that calls them (the software context for Software modules, the platform context
   for Platform and Accelerator modules). Modules still import only the core, `internal/shared`, and
   their own packages.
3. **The composition root assembles one registry for every process.** `internal/app` lists the modules
   compiled into the build and validates them once: the contract version is supported, each module's
   id and kind match its directory, and a Workflow kind, completion type, or route belongs to exactly
   one module. api, worker, and ansible-executor build the registry with the same function, and a
   failed validation stops the process. The core refuses a contract version it does not support; v1 is
   the only version.
4. **The playbook allowlist stays the runtime source of playbook paths.** `modules/manifest.json` is
   checked against the manifests: every playbook a module names is registered with a path inside that
   module or `shared/`, and every registered playbook belongs to a module or to `shared/`. Generating it
   from the selected modules is part of release assembly, later.
5. **A Workflow a module plans records `<kind>/<id>@<version>` as its definition.** Persisted
   definitions of earlier Workflows keep their values.
6. **Modules depend on each other only through core ports.** A Platform module obtains GPU behavior
   from Accelerator modules through an accelerator port, and composes Managed Software by kind through
   the software catalog the registry provides; it never imports another module.
7. **Conformance tests guard the contract** (core-and-extensions §9): every module directory has a
   valid manifest, the manifest agrees with the allowlist and with the hooks the Go package registers,
   and the import check keeps modules apart.

## Alternatives considered

- **Declare modules only in Go.** Rejected: ADR 070 requires the manifest to be data, and the second
  stage (module packages, ADR 070 §5) needs a manifest that can be read without compiling.
- **Read `module.json` from the content root at runtime.** Rejected: the binary's hooks and the
  manifest would come from different builds; embedding keeps them from the same build. The content
  root still serves the playbooks.
- **A `modules` Go package that imports every module.** Rejected: it would be one package depending on
  every module, which is what the composition root already is.
- **Generate `modules/manifest.json` from the manifests now.** Deferred: a conformance test gives the
  same guarantee without changing the image build; generation belongs to release assembly.
- **YAML manifests.** Rejected: the allowlist and the spec schema are JSON, and one format keeps the
  manifest and its schema in the same language.

## Consequences

- Adding a plugin is a directory with `module.json`, its content, an optional Go package, one line in
  the composition root's module list, and its allowlist entries; the core changes only when a new
  kind of hook is needed.
- Every existing module gains a manifest and a root Go package; hooks move out of the core and
  `internal/app` as each module is extracted, and the vendor-term ratchet records the reduction.
- The three processes can no longer disagree on which Workflow kinds and completion types exist.
- The image build already strips Go source from `modules/`, so the shipped content root gains only
  the manifests.

## Revision history

- 2026-11-06: added the optional `artifacts` field, so a module declares what deployment fetches and the Site's Local Repository can serve it without the internet; the contract version stays 1.
