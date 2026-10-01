# Compatibility and limitations

[繁體中文](../../zh-TW/reference/compatibility.md) · [Documentation home](../README.md)

## Project maturity

swallow is in active development. The repository has no stable SemVer release,
so compatibility guarantees are limited to the documented active contracts and
the explicit one-release migration notes in those contracts.

## Supported development and installation targets

- The production installation targets one Ubuntu 24.04 amd64 VM running swallow on
  Docker Compose plus a co-located MAAS 3.6 region+rack. It consumes immutable image
  digests, generates restricted secrets, and serves plain HTTP on port 80; TLS terminates in
  front of it when required. It needs outbound access to GHCR and `images.maas.io`.
- The installation synchronizes only the official `ubuntu/noble` amd64 OS Image;
  third-party images and offline MAAS image media are not automated.
- Development Compose is the contributor path.
- Native Ubuntu packaging is preview/incomplete because native Temporal and
  PostgreSQL systemd packaging has not landed. Native API startup without the
  full orchestration topology cannot execute Workflows.

## External systems

The current implemented integrations and automation target:

- Ubuntu MAAS 3.6 for provisioning;
- Prometheus-compatible metrics, Alertmanager, and Grafana;
- Kubernetes deployed through the shipped k0s automation;
- Slurm deployed through the shipped automation;
- Temporal for durable orchestration;
- MongoDB 8 for swallow-owned state.

Exact release media versions and digests belong to the candidate/release and
third-party manifests, not to this overview.

## Compatibility aliases

- `/api/v1/operations` and Dashboard `/operations` are deprecated aliases
  for Workflows.
- `/api/v1/clusters` and Dashboard `/clusters` are deprecated aliases for
  Platforms.
- New clients use canonical routes and terminology.

## Not implemented

Planned API design mentions Teams, user administration, physical datacenter/
room/rack topology, access/SSH-key management, workload abstractions, and
GPU-specific observability. They must not be presented as active capability.

## Security and support

This repository does not currently publish a license, formal support policy, or
security-reporting policy. Do not infer those commitments from the availability
of source or installation assets.
