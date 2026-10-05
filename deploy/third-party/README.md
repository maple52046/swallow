# Third-party services

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

Third-party media is versioned separately from the Swallow core bundle.

| Component | Supported production topology |
| --- | --- |
| Docker CE | Host prerequisite. `swallowctl install` adds it from Docker's Ubuntu apt repository when missing; never the convenience script. |
| MongoDB 8 | Managed by the Swallow Compose or native bundle. |
| PostgreSQL | One Compose instance: Temporal databases plus the co-located MAAS `maasdb`, each with its own role. |
| Ubuntu MAAS 3.6 | Co-located on the single production VM, installed and owned by `swallowctl` (region+rack snap). Never `maas-test-db`. |
| Ansible | Embedded in the API Execution Environment or native offline venv; not a service. |
| Prometheus | One independent project per site, using persistent storage and Swallow HTTP service discovery. |

The installation pulls swallow's own images from GHCR, the official MongoDB, PostgreSQL, and
Temporal images from Docker Hub, and the official `ubuntu/noble` boot resource from
`images.maas.io`, so it needs outbound access to all three. DNS, DHCP/PXE/BMC networking and
target hardware are site-provided inputs rather than Swallow-managed resources.
`offline-media-manifest.json` describes the media an air-gapped site would mirror; offline
installation of MAAS images is not automated yet.

References:

- Docker Engine for Ubuntu: <https://docs.docker.com/engine/install/ubuntu/>
- MAAS install: <https://maas.io/docs/how-to-install-maas>
- MongoDB 8 on Ubuntu: <https://www.mongodb.com/docs/v8.0/tutorial/install-mongodb-on-ubuntu/>
- Prometheus installation: <https://prometheus.io/docs/prometheus/latest/installation/>
