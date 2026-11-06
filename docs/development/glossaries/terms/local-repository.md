# Local Repository

- Bounded context: Local Repository. `api-server` and `dashboard` use this term with the same meaning.
- Definition: The one place a Site's Servers fetch every artifact a swallow deployment installs — OS packages, container images, Python packages, and files — kept by a Provider Adapter next to the control plane, so deployments do not reach the Internet.
- Allowed meaning: One per Site, registered as the Site's Integration of kind `repository` (provider kind `pulp`, Pulp 3 today): its endpoint is where swallow calls the provider's API, its `contentOrigin` setting is where Servers read content, and its write-only credential is the provider's admin password. swallow owns what it must contain — the `artifacts` the compiled modules declare in their `module.json`, together with the Site's user Repository Sources (a spec with a `generation`; the Ubuntu archive `ubuntu` is one) — and a controller in the worker converges that content into the provider and records the observed state of each source, file, and declared image. The artifacts themselves stay in the provider. The Local Repository is `ready` when the Site's `repository` Integration is enabled, the controller's last pass was not stopped by the Integration or its provider, every Repository Source, declared file, and declared image is `ready` (an image once the provider stores every layer of it, so no Server pull reaches the upstream), and the addresses Servers read it at are known. Its content has one of these formats, the core's only words for them:

  | Format | What it holds |
  | --- | --- |
  | `deb` | Debian and Ubuntu packages from declared sources, synchronized on demand and signed with the Site's own key. |
  | `rpm` | RPM packages from declared sources; designed in, not yet realized. |
  | `oci` | Container images, through one pull-through cache per registry host. |
  | `python` | Python packages, through a pull-through cache of PyPI. |
  | `file` | Single files, each downloaded once and verified against its declared sha256. |

  A playbook flagged `usesLocalRepository` receives the Local Repository's addresses as the trusted variable `swallow_local_repository`, and a deployment that runs one is refused at acceptance while the Site's Local Repository is not ready.
- Disallowed meaning: Not the Provisioner HTTP Proxy, which only forwards a provisioner's package traffic and pins nothing. Not where OS Images live (the provisioner keeps them), not a store of swallow's own data, and not a registry users push images to. Not "Pulp": Pulp is its current Provider Adapter, and `pulp`, `apt`, and `dnf` never appear in the core. Not a fallback: a deployment never fetches from the Internet when the Local Repository is missing.
- Synonyms: Local repo (informal).
- Deprecated terms: Artifact Repository (the planning name of its Provider Interface).
- Examples: "The Kubernetes deployment downloads the k0s binary from the Local Repository's files and verifies its declared sha256, instead of from GitHub." / "A Site without a `repository` Integration cannot deploy a Kubernetes Platform: the request is refused with 409, naming the Integration to register."
- Related terms: Repository Source, Integration, Site, Provider Interface, Provider Adapter, Module Manifest, Controller, Desired State, Observed State, Provisioner HTTP Proxy.
- Change note: Added 2026-11-06 for [decision 078](../../../decisions/078-local-repository.md) (local repository): every role fetched its artifacts from the Internet, so deployment time grew with the number of Servers and the outbound bandwidth, offline Sites could not deploy, and nothing was pinned at the Site. The plan called its Provider Interface "Artifact Repository"; the term is Local Repository, matching the Go context `localrepo` and the dashboard page, and "artifact" stays the word for what it holds. Changed the same day: `ready` also requires every declared image to be stored, after a lab deployment timed out pulling an image whose one-time prefetch had stopped partway while the repository already read `ready`.
