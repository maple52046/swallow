# Third-party services

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Third-party media is versioned separately from the swallow core bundle.

| Component | Supported production topology |
| --- | --- |
| Docker CE | Host prerequisite. `swallowctl install` adds it from Docker's Ubuntu apt repository when missing; never the convenience script. |
| MongoDB 8 | Managed by the swallow Compose or native bundle. |
| PostgreSQL | One Compose instance: Temporal databases, the Local Repository's `pulp` database, and the co-located MAAS `maasdb`, each with its own role. |
| Ubuntu MAAS 3.6 | Co-located on the single production VM, installed and owned by `swallowctl` (region+rack snap). Never `maas-test-db`. |
| Ansible | Embedded in the API Execution Environment or native offline venv; not a service. |
| Prometheus | One per Site, deployed with the production installation for its Site (Compose service `prometheus`, digest-pinned image) and registered as that Site's metrics Integration; it discovers the Site's exporters from swallow. |
| prom-label-proxy | One in front of each Site Prometheus (Compose service `prom-label-proxy`, digest-pinned image, internal network only), the metrics Integration's `labelProxyUrl`; it confines each query to one Platform's `platform_id`. |
| Pulp 3 | The Site's Local Repository, one per Site, deployed with the production installation (Compose services `pulp-init`, `pulp-api`, `pulp-content`, `pulp-worker`, and `pulp-web`; the digest-pinned `pulp-minimal` and `pulp-web` images) on the shared PostgreSQL, and registered as that Site's `repository` Integration. When the Site chooses its cache or offline mode, deployed Servers fetch packages, files, container images, and Python packages from it, at `pulp-web`'s port (in cache mode what it already serves, in offline mode everything). Used unmodified over its HTTP API; releases reference its GPLv2-or-later images by digest and never ship them. |

The installation pulls swallow's own images from GHCR, the official MongoDB, PostgreSQL,
Temporal, and Prometheus images and the Pulp project's `pulp-minimal` and `pulp-web` images from
Docker Hub, the prom-label-proxy image from Quay, and the official `ubuntu/noble` boot resource
from `images.maas.io`, so it needs outbound access to all four. Deployments need outbound access
to the sources, files, container registries (Docker Hub, Quay, `registry.k8s.io`), and PyPI that
swallow's modules declare. While the Site uses its Local Repository, Pulp reaches them, together
with the Site's sources (by default the Ubuntu archive, `archive.ubuntu.com`), and deployed Servers
reach only the Local Repository for artifacts, not the Internet; otherwise the deployed Servers
reach them, and their own OS package sources, themselves. DNS, DHCP/PXE/BMC networking and
target hardware are site-provided inputs rather than swallow-managed resources.
`offline-media-manifest.json` describes the media an air-gapped site would mirror; offline
installation of MAAS images is not automated yet, and the Local Repository cannot import its
content offline yet either.

References:

- Docker Engine for Ubuntu: <https://docs.docker.com/engine/install/ubuntu/>
- MAAS install: <https://maas.io/docs/how-to-install-maas>
- MongoDB 8 on Ubuntu: <https://www.mongodb.com/docs/v8.0/tutorial/install-mongodb-on-ubuntu/>
- Prometheus installation: <https://prometheus.io/docs/prometheus/latest/installation/>
- prom-label-proxy: <https://github.com/prometheus-community/prom-label-proxy>
- Pulp: <https://pulpproject.org/>
- The Local Repository's design: [local-repository.md](../../docs/development/local-repository.md)
