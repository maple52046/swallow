# Provisioner Subnet DNS

## Status

Active

## Owner Component

`api-server`

## Consumer Components

- `dashboard`
- `cli` (the `swallow` operator command-line client; no command yet)

## Purpose

List the subnets of one provisioner Integration and read and write each subnet's Subnet DNS: the
DNS servers the provisioner hands to machines it deploys with an `automatic` or `static` address
on that subnet ([decision 058](../../../../../docs/decisions/058-deployment-dhcp-and-subnet-dns.md)).
The provisioner owns the subnets and the setting (for MAAS, the subnet's `dns_servers`); swallow
reads them live on every request, writes the DNS servers through, and stores nothing. It is not
part of the Integration's `settings` ([sites-integrations.md](sites-integrations.md)).

Base path, bearer authentication, error envelope, and timestamps follow
[conventions.md](conventions.md); only endpoint-specific behavior is described below.

## Related Glossary Terms

- Subnet DNS
- Network Configuration
- OS Deployment
- Integration
- OS Provisioning Provider

## Authentication and Authorization

Bearer token; both endpoints require `admin`.

## Endpoints

```text
GET /api/v1/provisioning/integrations/{id}/subnets
PUT /api/v1/provisioning/integrations/{id}/subnets/{subnetId}/dns-servers
```

| Path parameter | Meaning |
| --- | --- |
| `id` | The `integrationId` of a `provisioner` Integration. |
| `subnetId` | The provisioner's own subnet id, as `GET .../subnets` reports it. |

Both routes need the provisioner's Subnet DNS capability (MAAS has it). Responses carry
`Cache-Control: no-store`.

## Subnet

| Field | Meaning |
| --- | --- |
| `id` | The provisioner's subnet id. |
| `name` | The provisioner's subnet name. |
| `cidr` | The subnet's address range. |
| `gatewayAddress` | The subnet's gateway, `""` when it has none. |
| `managed` | Whether the provisioner allocates addresses on the subnet. |
| `provisionerDhcp` | Whether the provisioner itself serves DHCP on the subnet (MAAS: DHCP on for the subnet's VLAN, or relayed to it); false means any DHCP there is the site's, and network inspection suggests `dhcp` deployments on it ([provisioning.md](provisioning.md)). |
| `attached` | Whether the subnet is a real network, by the provisioner's own markers: a provisioner controller interface is on it; the provisioner serves its DHCP (`provisionerDhcp`); a machine's boot interface is on it; or a machine interface backed by a NIC with hardware identity is on it (the NIC itself, or a bond, VLAN, or bridge with one among its parents). False for a subnet only host-internal interfaces carry — container, Kubernetes, or VM bridges, veth pairs — or none at all; deployments never use such a subnet, and consumers may list it apart. A hypervisor network counts once a managed VM boots from it; a secondary network that only a NIC without hardware identity reaches (some virtual NICs) does not. |
| `dnsServers` | The Subnet DNS, in the provisioner's order; `[]` when the subnet has none and deployments on it fall back to the provisioner's own DNS. |

## List

`GET /api/v1/provisioning/integrations/{id}/subnets` returns `200 OK`:

```json
{
  "integrationId": "c3d4…",
  "subnets": [
    {
      "id": "1",
      "name": "10.170.168.0/23",
      "cidr": "10.170.168.0/23",
      "gatewayAddress": "10.170.169.254",
      "managed": true,
      "provisionerDhcp": false,
      "attached": true,
      "dnsServers": ["165.204.124.158", "165.204.219.249"]
    }
  ]
}
```

`subnets` is in the order the provisioner lists them, and `[]` when it has none.

## Write

`PUT /api/v1/provisioning/integrations/{id}/subnets/{subnetId}/dns-servers` replaces the subnet's
Subnet DNS and returns `200 OK` with the Subnet, read back from the provisioner after the write.

```json
{
  "dnsServers": ["165.204.124.158", "165.204.219.249"]
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `dnsServers` | Yes | An array of up to 8 IPv4 or IPv6 addresses, in the order deployed machines should use them. Surrounding whitespace is removed and each address is reported in its canonical form. Refused when an entry is empty, not an IP address, or carries a zone; when two entries are the same address; or when there are more than 8. `[]` clears the setting. |

swallow validates the body before calling the provisioner; the provisioner validates the value
again and may refuse it. A write does not test that the servers answer, and it affects only
deployments the provisioner starts afterwards; it never changes a deployed machine. `dhcp`
deployments take DNS from the DHCP server and are not affected.

## Error Codes

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_error` | The body is not a JSON object; `dnsServers` is missing, not an array, or breaks a rule above; the Integration is not a `provisioner` or has no adapter; the provisioner lacks the capability; or the provisioner refused the value (the message carries its explanation). |
| 404 | `not_found` | The Integration does not exist, or the provisioner has no subnet `subnetId`. |
| 503 | `provider_unavailable` | The provisioner could not be reached, the Integration has no credential, or its credential may not change subnets (a MAAS account that is not an administrator). |

The List answers `404` and `503` the same way, and `400` only for an Integration that is not a
capable provisioner.

## Compatibility Notes

New in 2026-10-13; additive.

## Implementation Notes

The MAAS adapter lists with `GET /subnets/` (`dns_servers`, and `vlan.dhcp_on` or
`vlan.relay_vlan` for `provisionerDhcp`) and writes with `PUT /subnets/{subnetId}/` (multipart
`dns_servers`, the addresses joined by spaces; an explicit empty value clears it). `attached`
comes from `vlan.dhcp_on`/`vlan.relay_vlan`, the interface links of `GET /rackcontrollers/` and
`GET /regioncontrollers/` (any link counts), and those of `GET /machines/`: a machine interface
counts when it is the machine's `boot_interface`, or when it or one of its `parents` (followed
recursively) has a non-empty `vendor`. MAAS records veth pairs with `type` `physical`, so the type
alone cannot tell a NIC from a container's end of a veth; the vendor can. MAAS creates a subnet
for every new CIDR an enrolled host reports, so hosts running containers or Kubernetes can add one
per host; `attached` lets a consumer keep those out of the way without changing the provisioner's
data.
