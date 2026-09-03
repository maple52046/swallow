# 012. Swallow owns provisioning network intent

- Status: Accepted
- Date: 2026-09-02

## Context

Swallow historically required a MAAS Machine to have a subnet link and otherwise
asked the operator to configure MAAS directly. That made MAAS defaults such as
`AUTO` observable product behaviour, produced inconsistent released Servers, and
would force a future provisioner consumer to understand MAAS vocabulary.

Operators need one Swallow workflow for DHCP and static deployment, pre-deploy
NIC configuration, and optional release cleanup. MAAS still owns its inventory,
subnets, IP allocation, and execution APIs.

## Decision

The OS Provisioning bounded context owns deployment network intent, policy,
defaults, validation, and workflow. Its deployment modes are DHCP and Static,
with DHCP as the default. Provider adapters translate this published language to
their own APIs and expose provider-managed legacy states only as observations.

The MAAS relationship is an anticorruption layer: `AUTO`, `LINK_UP`, link IDs,
and operation names remain in the adapter. The adapter never uses provider force
options and verifies every mutation by reading the Machine again.

Swallow applies network intent only at explicit operator boundaries. It does not
continuously reconcile deployed hosts, because an unsolicited change could break
running workloads.

Release cleanup is represented by a durable Provisioning Task rather than an
Operation. Operations remain reserved for Swallow-owned playbook execution;
Provisioning Tasks coordinate provider state transitions and link mutations.

## Alternatives considered

- Continue conforming to MAAS defaults: rejected because the product would not
  have stable behaviour across providers and AUTO does not mean DHCP.
- Expose all MAAS modes as deployment modes: rejected because provider vocabulary
  would become Swallow's public domain model.
- Continuously reconcile every NIC: rejected because changing a deployed host's
  network can interrupt service without an operator action.
- Model release cleanup as an Operation: rejected because no Ansible playbook is
  executed and MAAS remains the execution owner.

## Consequences

Adapters that cannot honour network intent must reject deployment before writes;
they may not silently keep provider defaults. Historical MAAS AUTO links remain
visible but are changed to DHCP at the next explicit Swallow deployment unless
the operator selects Static.

Swallow persists only the intent and cleanup coordination it owns. Provider NIC,
subnet, and link identifiers remain live references and are revalidated before
each mutation.

## Current status

Implemented by the Server Network Configuration workflow on 2026-09-02.
