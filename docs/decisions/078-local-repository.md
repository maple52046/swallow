# 078. Deployments fetch every artifact from the Site's Local Repository, a provider swallow converges

- Status: Accepted
- Date: 2026-11-06
- 現行規格：[local-repository.md](../development/local-repository.md)；glossary [Local Repository](../development/glossaries/terms/local-repository.md)、[Repository Source](../development/glossaries/terms/repository-source.md)

## Context

Every role swallow ran fetched its artifacts from the Internet while it deployed: apt packages from
the Ubuntu archive and vendor archives, binaries from GitHub releases (the k0s binary among them),
container images from Docker Hub, Quay, and `registry.k8s.io`, and Python packages from PyPI. That
had four consequences:

- **Deployment time grew with the number of Servers and with the Site's outbound bandwidth**,
  because every Server downloaded the same artifacts again. Outbound bandwidth also changes widely
  over time. On the evening of 2026-11-05 three lab virtual machines that downloaded k0s at the same
  time got about 60 KB/s each (27 to 97 KB/s on that path); the download of the 262 MB k0s binary was
  cut off at about 230 MB and the deployment failed. The next morning the same path gave 7 MB/s.
- **A Site without Internet access could not deploy at all.**
- **Nothing was pinned or verified at the Site.** What a deployment installed depended on what each
  upstream served that day, and an upstream that removed a release broke deployments that named it.
- **The only help at the Site was the provisioner's HTTP proxy**
  ([Provisioner HTTP Proxy](../development/glossaries/terms/provisioner-http-proxy.md)), which only
  forwards package traffic and still reaches the upstream on every miss.

The user also asked that what the Site holds is not hard-coded: modules declare what they need,
users can add more, and container images and Python packages come through pull-through caches
rather than from a set of images someone maintains.

## Decision

1. **Every artifact a deployment installs comes from the Site's Local Repository.** Roles contain no
   Internet URL: what a module's playbooks fetch is declared in its `module.json` and read from the
   Local Repository. A deployment whose playbooks use it is refused at acceptance while the Site's
   Local Repository is not ready; swallow never falls back to the Internet.
2. **The Local Repository is a provider category.** The core defines its port (`Repository`, in
   `internal/core/localrepo/domain`) in neutral vocabulary; its formats are `deb`, `rpm`, `oci`,
   `python`, and `file`. Pulp 3 is its adapter (`internal/adapters/localrepo/pulp`); the names
   `pulp`, `apt`, and `dnf` appear only in the adapter and in modules. It is registered as the
   Site's Integration of kind `repository` (provider kind `pulp`), at most one per Site. The
   Integration's endpoint is where swallow calls the provider's API, its `contentOrigin` setting is
   where Servers read content, and its credential is the provider's admin password.
3. **It is co-located with the control plane.** Pulp runs as services of the production and the
   development Compose installations, and Servers fetch from it at
   `http://<SWALLOW_ADDRESS>:<port>`. In production its database lives in the shared PostgreSQL
   ([decision 040](040-single-vm-production-installation.md) (single VM production installation)),
   and the installation registers the Integration and the Site's Ubuntu source.
4. **swallow owns what the Local Repository must contain; the provider owns the artifacts.** The
   content it must hold is the union of the module declarations (`module.json` `artifacts`: package
   sources, files, images, and Python requirements) and the Site's user Repository Sources, a spec
   with a `generation` written through the API. The Ubuntu archive `ubuntu` is a Site source, not a
   module declaration, because it belongs to the OS the Site deploys rather than to any module. A
   controller in the worker converges that content into the provider through the port and records
   each source's and file's observed state, as the Provisioner HTTP Proxy controller does
   ([decision 071](071-provisioner-realized-desired-state.md) (provisioner realized desired state)).
   swallow stores no artifact itself.
5. **Each format uses the provider mechanism that fetches it once and keeps it at the Site.**
   - `oci`: a pull-through cache per registry host that a declared image names (`docker.io`,
     `quay.io`, and so on); the declared images are fetched in advance.
   - `python`: a pull-through cache of PyPI, created when a module declares a Python requirement.
   - `deb`: Pulp has no pull-through for it, so each declared source is synchronized `on_demand`
     (its metadata is mirrored, a package is fetched on its first request and kept) and published
     with Release files signed with the Site's own key, so hosts verify them instead of trusting the
     repository unverified.
   - `file`: each declared file is downloaded once by the provider and compared with its declared
     sha256; roles verify it again.
   - `rpm` has the same source shape and is designed in; its adapter branch and the dnf client are
     added when swallow deploys an RPM-based OS. arm64 content and offline import bundles (Pulp's
     import and export) come later.
6. **Hosts are pointed at it by one shared role.** A playbook flagged `usesLocalRepository` in the
   release manifest receives the Local Repository's addresses as the trusted variable
   `swallow_local_repository`, and the shared role `local_repo_client` points apt, pip, and the
   container runtimes (containerd, Docker, Podman) at it. Content tests fail a role that contains
   an Internet URL.
7. **Pulp is used unmodified, as a separate service, over HTTP.** pulpcore and its deb, container,
   python, file, and rpm plugins are licensed GPLv2-or-later. swallow neither modifies nor links
   them and talks to them only through their HTTP API, so commercial use is allowed. A release that
   ships Pulp's image itself, instead of pinning the upstream image, takes on the GPL's
   redistribution obligations (making the corresponding source available). This is not legal advice;
   a legal review is recommended before commercial distribution.

## Alternatives considered

- **Bake the artifacts into OS images.** Rejected: every combination of Platform, version, and
  option would need its own image, and the user does not want to maintain many images. A Server
  that keeps its OS would still fetch from the Internet.
- **Nexus Repository Community Edition.** Rejected: its usage caps limit how much a Site can hold
  and serve.
- **One tool per format** (an apt mirror, a registry mirror, a Python index, and a file server).
  Rejected: more services to install, upgrade, and back up, and one adapter per tool.
- **swallow's own repository service.** Rejected: it would reimplement mirroring, package metadata,
  signing, and the registry protocol, which mature tools already do; swallow owns what the Site must
  hold, not the artifacts.
- **Keep the provisioner's apt proxy (MAAS's built-in proxy).** Rejected: it covers apt only, pins
  nothing, and still fetches from the upstream on every miss.

## Consequences

- Each artifact crosses the Internet once per Site, so deployment time no longer grows with the
  number of Servers times the outbound bandwidth, and content stays the same between deployments.
  A Site without Internet access can later import content instead of synchronizing it.
- Deployments depend on the Local Repository: a Site without a ready one cannot run them, and the
  first request for an artifact waits for the provider to fetch it.
- Modules must declare every artifact their playbooks fetch; installing another version is a
  `module.json` change. The module contract
  ([decision 075](075-module-contract-v1.md) (module contract v1)) gains `artifacts`.
- The installation gains the Pulp services, a database in the shared PostgreSQL, and a signing key
  per Site, and decision 040 is revised accordingly.
- Hosts configured by `local_repo_client` use only the Local Repository: the distribution's package
  sources are disabled there, and apt reaches the repository directly rather than through the
  provisioner's proxy.
- `oci` and `python` content is served over plain HTTP like the rest of the installation; `deb`
  content is verified by the Site's signature and files by their sha256.
