# Third-party services

Third-party media is versioned separately from the Swallow core bundle.

| Component | Supported production topology |
| --- | --- |
| Docker CE | Host prerequisite from Docker's Ubuntu apt repository, or verified offline deb media. Never the convenience script. |
| MongoDB 8 | Managed by the Swallow Compose or native bundle. |
| Ubuntu MAAS 3.6 | Dedicated Ubuntu 24.04 host/VM with production PostgreSQL and region+rack controllers. Never co-located with Swallow and never `maas-test-db`. |
| Ansible | Embedded in the API Execution Environment or native offline venv; not a service. |
| Prometheus | One independent project per site, using persistent storage and Swallow HTTP service discovery. |

Connected and air-gap procedures must consume versions and checksums from
`offline-media-manifest.json`. MAAS boot resources must be synchronized before a site
is disconnected. DNS, DHCP/PXE/BMC networking, CA certificates, and target hardware are
site-provided installation inputs rather than Swallow-managed resources.

References:

- Docker Engine for Ubuntu: <https://docs.docker.com/engine/install/ubuntu/>
- MAAS install: <https://maas.io/docs/how-to-install-maas>
- MongoDB 8 on Ubuntu: <https://www.mongodb.com/docs/v8.0/tutorial/install-mongodb-on-ubuntu/>
- Prometheus installation: <https://prometheus.io/docs/prometheus/latest/installation/>
