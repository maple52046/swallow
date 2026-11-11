# 078. Each Site chooses how its deployments use its Local Repository, a provider swallow converges

- Status: Accepted
- Date: 2026-11-06
- 現行規格：[local-repository.md](../development/local-repository.md)；glossary [Local Repository](../development/glossaries/terms/local-repository.md)、[Local Repository Mode](../development/glossaries/terms/local-repository-mode.md)、[Repository Source](../development/glossaries/terms/repository-source.md)

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

The first form of this decision made every deployment fetch from the Local Repository. On
2026-11-06 the user decided that the installation still installs it and registers it on every Site,
but each Site chooses whether its deployments use it, and that the choice starts off.

On 2026-11-11 the development Site, which used its Local Repository, was refused a RAM deployment of
Ubuntu while the Ubuntu archive synchronized for the first time, and every Site that used it was
refused deployments during the daily resync of each deb source, although the provider still served
the previous synchronization. The user wants to use the Local Repository either as the only source
of a Site that must not reach the Internet, or like a proxy cache that never holds deployments
back, and decided that the choice is one of three modes.

## Decision

1. **Each Site chooses, with a mode, where its deployments fetch their artifacts.** Roles contain no
   Internet URL: everything a module's playbooks fetch is declared in its `module.json` — files with
   their sha256, deb sources with the signing key that verifies them, container images, and pinned
   Python requirements. The Site's Local Repository spec has `mode`, `upstream` by default:
   - `upstream`, or a Site without a `repository` Integration: every artifact comes from the
     upstream its module declares (files verified by their sha256, deb sources by their declared
     keys), and each host keeps its own OS package sources.
   - `offline`: every artifact comes from the Local Repository, and a deployment whose playbooks
     fetch artifacts is refused at acceptance while the Local Repository does not serve everything.
     A source counts as served while a resync of it runs or after one failed, because the provider
     keeps serving its last synchronization.
   - `cache`: what the Local Repository already serves comes from it and the rest from the declared
     upstreams, decided per artifact at acceptance (the OS package sources as one: all through the
     Local Repository once it has deb sources and serves every one, otherwise all from the
     upstreams). A
     deployment never waits for content; it is refused only while the Local Repository itself
     cannot be used (its Integration disabled, the controller's last pass stopped by the
     Integration or its provider, or its addresses not read yet).

   No mode is a silent fallback: swallow never sends an `offline` Site to the upstreams because the
   Local Repository is not ready, a `cache` Site reaches the upstreams because the user chose that
   mode and each run records where each of its artifacts comes from, and disabling the Integration
   pauses the Local Repository without changing the Site's mode. `offline` does not yet make a Site
   air-gapped: the provider still fetches a deb or Python package from its upstream on the first
   request until content can be imported from disk.
2. **The Local Repository is a provider category.** The core defines its port (`Repository`, in
   `internal/core/localrepo/domain`) in neutral vocabulary; its formats are `deb`, `rpm`, `oci`,
   `python`, and `file`. Pulp 3 is its adapter (`internal/adapters/localrepo/pulp`); the names
   `pulp`, `apt`, and `dnf` appear only in the adapter and in modules. It is registered as the
   Site's Integration of kind `repository` (provider kind `pulp`), at most one per Site. The
   Integration's endpoint is where swallow calls the provider's API, its `contentOrigin` setting is
   where Servers read content, and its credential is the provider's admin password.
3. **It is co-located with the control plane and installed on every installation.** Pulp runs as
   services of the production and the development Compose installations, and Servers fetch from it
   at `http://<SWALLOW_ADDRESS>:<port>`. In production its database lives in the shared PostgreSQL
   ([decision 040](040-single-vm-production-installation.md) (single VM production installation)).
   The installation registers the Integration and writes the Site's first spec: the Site's Ubuntu
   source, with `mode` `upstream`. A user chooses another mode for the Site; the development
   installation writes `cache`.
4. **swallow owns what the Local Repository must contain; the provider owns the artifacts.** The
   content it must hold is the union of the module declarations (`module.json` `artifacts`: package
   sources, files, images, and Python requirements) and the Site's user Repository Sources, a spec
   with a `generation` written through the API. The Ubuntu archive `ubuntu` is a Site source, not a
   module declaration, because it belongs to the OS the Site deploys rather than to any module. While
   the mode is `cache` or `offline`, a controller in the worker converges that content into the
   provider through the port and records each source's, file's, and image's observed state, and
   whether each source is served, as the Provisioner HTTP Proxy controller does
   ([decision 071](071-provisioner-realized-desired-state.md) (provisioner realized desired state)).
   While it is `upstream`, the controller leaves the provider alone (nothing is synchronized or
   fetched, and what it already holds stays) and records the Local Repository as not ready because
   it does not serve deployments. swallow stores no artifact itself.
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
6. **Hosts are pointed at the chosen source by one shared role.** A playbook flagged
   `fetchesArtifacts` in the release manifest receives the trusted variable `swallow_artifacts`,
   frozen at acceptance: the Site's mode, where apt, pip, and the container runtimes fetch from, and,
   for every declared file and image, where to fetch it. The shared role `artifact_sources`
   configures each of them on its own. Toward the Local Repository it points apt (verified by the
   Site's key, with the host's other apt sources disabled), pip, and the container runtimes
   (containerd, Docker, Podman) at it. Toward the upstreams it adds each declared deb source with its
   declared key, verified against the declared fingerprint, keeps the host's OS sources, and removes
   what it wrote for the Local Repository, so a host converges to what its latest run was accepted
   with the next time such a playbook runs on it. Content tests fail a role that contains an
   Internet URL.
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
- **Every deployment fetches from the Local Repository** (the first form of this decision).
  Replaced by the Site's choice, by the user's decision of 2026-11-06.
- **Fetch from the upstreams while a chosen Local Repository is not ready.** Rejected: a Site that
  chose the Local Repository would reach the Internet without saying so, which is a same-shaped
  substitute for a missing capability; an `offline` deployment is refused and names what to wait for
  or fix. `cache` mode is not this alternative: the user chooses it knowing that what the Local
  Repository does not hold yet comes from the upstreams, and each run records it.
- **Keep the boolean and only relax readiness** (accept once every source has synchronized once,
  without waiting for images). Rejected: the first synchronization of the Ubuntu archive would still
  hold every deployment back, which is what the user wanted to avoid.
- **Mix the OS package sources per source in `cache` mode.** Rejected: a user source has no signing
  key to verify from its upstream, and the host's own OS sources can be disabled only once the
  Site's OS archive is served, so apt goes to one side as a whole.

## Consequences

- While a Site uses its Local Repository, each artifact crosses the Internet once per Site, so
  deployment time no longer grows with the number of Servers times the outbound bandwidth, and
  content stays the same between deployments. A Site without Internet access can later import
  content instead of synchronizing it.
- An `offline` Site cannot deploy until its Local Repository is ready, and the first request for an
  artifact waits for the provider to fetch it. A `cache` Site deploys at once, and its Servers reach
  the upstreams for what the Local Repository does not hold yet. An `upstream` Site deploys from the
  upstreams: its Servers need to reach them, and the artifacts are still pinned and verified, files
  by their sha256 and deb sources by their declared keys, whichever mode fetches them.
- Modules must declare every artifact their playbooks fetch, and the signing key of every deb
  source they declare; installing another version is a `module.json` change. The module contract
  ([decision 075](075-module-contract-v1.md) (module contract v1)) gains `artifacts`.
- Every installation gains the Pulp services, a database in the shared PostgreSQL, and a signing key
  per Site, whether or not its Site uses them, and decision 040 is revised accordingly.
- Hosts configured for the Local Repository use only it: the distribution's package sources are
  disabled there, and apt reaches the repository directly rather than through the provisioner's
  proxy. They return to their own sources when a playbook that fetches artifacts runs on them after
  the Site switched to `upstream`, or while a `cache` Site's Local Repository does not serve every
  deb source.
- `oci` and `python` content is served over plain HTTP like the rest of the installation; `deb`
  content is verified by the Site's signature and files by their sha256.

## Revision history

- 2026-11-06: each Site chooses with `serveDeployments` (off by default; the installation registers the Integration with it off) whether its deployments fetch from the Local Repository or from the upstreams their modules declare, with module-declared deb sources verified by their declared signing keys; the manifest flag, trusted variable, and shared role became `fetchesArtifacts`, `swallow_artifacts`, and `artifact_sources`.
- 2026-11-11: the Site's choice became a mode, `upstream`, `cache`, or `offline` (the former `false` and `true`), so a Site can use its Local Repository as a cache that never holds deployments back; a source counts as served while a resync of it runs, and `artifact_sources` configures apt, pip, and the container runtimes each on its own.
