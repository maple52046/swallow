# Repository Source

- Bounded context: Local Repository. `api-server` and `dashboard` use this term with the same meaning.
- Definition: One upstream that a Site's Local Repository mirrors or caches, named within that Local Repository.
- Allowed meaning: Two kinds:
  - A **declared source** has a `name`, a `format`, a `url`, and, for an OS package archive (`deb`; `rpm` is designed in), the `suites`, `components`, and `architectures` taken from it. A module declares it in its `module.json` `artifacts`, or a user adds it to the Site's Local Repository spec; `declaredBy` lists who declared it, `module:<id>` or `user`. A module's OS package archive also declares its `signingKey` (an https URL and the key's fingerprint), with which a host verifies the archive when the Site's deployments fetch from the upstreams; a user source has none and is served only through the Local Repository. Declarations of the same name merge: the first fixes the format and URL, and later ones with the same format and URL add their suites, components, and architectures; one that contradicts them is left out, and a user spec that would contradict a module is refused. The OS archive (`ubuntu` for Ubuntu) is a user source the installation writes, not a module declaration, because it belongs to the OS the Site deploys.
  - A **pull-through source** is derived from the declarations: one `oci` source per container registry host that a declared image names (`docker.io`, `quay.io`, …), and one `python` source `pypi` (`https://pypi.org/`) when a module declares a Python requirement. An artifact is fetched from it on its first request and kept; the declared images are fetched in advance. A declared source of the same name and format replaces the upstream's URL, for example with a mirror of that registry.

  Each source has an observed state, from this closed set (the same values describe each declared file and each declared image):

  | State | Meaning |
  | --- | --- |
  | `pending` | The provider accepted it but has not started fetching it, or it was not observed since its definition changed; a declared image is also `pending` while the pull-through source of its registry is not `ready`. |
  | `syncing` | The provider is fetching it. |
  | `ready` | Its content is served; Servers can fetch from it. |
  | `failed` | The last fetch failed; its message says why, and a later convergence tries again. |
- Disallowed meaning: Not a declared file: a file is fetched once from its own URL and verified by its sha256, with no upstream kept. Not an Integration, and not the package source a Server is configured with — a Server is pointed at the Local Repository's copy of it while the Site uses its Local Repository, and otherwise at a module's declared archive directly, verified by its declared key. Not a Provisioner HTTP Proxy.
- Synonyms: Source (inside the Local Repository context); upstream.
- Deprecated terms: None.
- Examples: "The Docker CE module declares the source `docker-ce`: `https://download.docker.com/linux/ubuntu`, suite `noble`, component `stable`, `amd64`, signed by the key at `https://download.docker.com/linux/ubuntu/gpg` with fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`." / "Declaring the image `quay.io/k0sproject/pause:3.10.2-0` makes the adapter create the pull-through source `quay.io`."
- Related terms: Local Repository, Module Manifest, Site, Desired State, Observed State, Provider Adapter.
- Change note: Added 2026-11-06 with Local Repository ([decision 078](../../../decisions/078-local-repository.md) (local repository)). The same day the state values were extended to declared images, which became observed items of the Local Repository. Also the same day a module's OS package archive gained its `signingKey`, because a Site may choose to fetch from the upstreams, where a host points at the archive itself.
