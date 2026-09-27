# Introduction

[繁體中文](../zh-TW/introduction.md) · [Documentation home](README.md)

swallow gives datacenter operators one identity and automation layer across
provisioning, runtime Platforms, host software, and observability. It is useful
when the same physical Server appears under different names and identifiers in
MAAS, Kubernetes or Slurm, Prometheus, and automation logs.

![Deploying a Platform](../assets/platform-deployment-wizard.png)

## The problem

A GPU datacenter usually already has specialized systems:

- MAAS discovers machines and controls power and operating-system provisioning.
- Kubernetes or Slurm owns live runtime membership and workload state.
- Prometheus and Alertmanager own metrics and alerts.
- Ansible performs repeatable changes on hosts.

Replacing those systems would lose mature capabilities. Leaving them unrelated
makes operators manually answer questions such as: which MAAS machine is this
Kubernetes node, which alerts belong to it, and which automation last changed
it? swallow solves the correlation and intent problem.

## Ownership model

swallow owns:

- Sites and the Integrations registered for each Site.
- Stable Server identities and the mapping to provider machines.
- Desired policy and durable automation intent.
- Self-deployed Platform records, credentials, and lifecycle intent.
- Software Assignments and the relationship between resources and Workflows.

swallow deliberately does not own:

- Hardware, power, or provisioning facts already owned by MAAS.
- Kubernetes or Slurm live state.
- Time-series metrics or Alertmanager alert state.
- A second copy of credentials that an API is allowed to return; stored
  integration and automation credentials are write-only.

See the binding
[system ownership decision](../decisions/001-system-ownership-boundaries.md)
for the detailed boundary.

## Interfaces

- **Dashboard:** guided operator workflows, fleet status, diagnostics, and
  resource management.
- **CLI:** broad operator coverage with table, JSON, YAML, and streaming
  output. Managed Software currently uses the Dashboard or HTTP API.
- **HTTP API:** provider-owned contracts for external integrations and custom
  automation.

All three use opaque IDs as resource identity. Hostnames and IP addresses are
observed, mutable attributes and must not be used as keys.

## Main operator journeys

1. Create a Site and register its MAAS and monitoring Integrations.
2. Configure Site automation and reconcile MAAS machines into Servers.
3. Inspect inventory, organize Servers with Zones, Pools, and tags, and deploy
   an operating system.
4. Deploy a Kubernetes or Slurm Platform, or install host-level Managed
   Software.
5. Follow the durable Workflow through Jobs, Tasks, events, and logs.
6. Correlate live metrics and alerts back to stable Server and Platform IDs.

## Non-goals and current status

swallow manages only Platforms it deploys itself. It does not register arbitrary
existing Kubernetes or Slurm environments. Teams, user administration, physical
rack topology, and GPU-specific observability are planned design areas and are
not active features.

The project is in active development and has no stable SemVer release. The
Compose topology is the complete runnable topology. Native packaging is a
preview until Temporal and PostgreSQL native service packaging is complete.
