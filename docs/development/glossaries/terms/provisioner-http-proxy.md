# Provisioner HTTP Proxy

- Bounded context: OS Provisioning. `api-server` and `dashboard` use this term with the same meaning.
- Definition: The HTTP proxy setting of one provisioner Integration's provider — the proxy the provisioner hands to the machines it deploys for package (APT or YUM) and HTTP/HTTPS traffic, and itself uses to download boot images. For MAAS it is the region configuration `enable_http_proxy`, `use_peer_proxy`, and `http_proxy`.
- Allowed meaning: A **mode** from a closed set, plus the proxy URL the mode needs:

  | Mode (domain value) | UI label | Meaning |
  | --- | --- | --- |
  | `none` | Don't use a proxy | Deployed machines reach package repositories directly. |
  | `builtin` | MAAS built-in | Deployed machines use the provisioner's own proxy (MAAS: TCP 8000 on the region or rack), which reaches the Internet directly. |
  | `external` | External | Deployed machines and the provisioner use the given proxy URL directly. |
  | `peer` | Peer | Deployed machines use the provisioner's own proxy, which forwards to the given proxy URL as its upstream peer. |

  The URL is an `http` or `https` address with a host and optional port, and applies only to `external` and `peer`. The setting is provisioner-owned: swallow reads it live and writes it through, never stores it, and a write replaces mode and URL together. It changes only deployments the provisioner starts afterwards.
- Disallowed meaning: Not an Integration setting — it is not stored in the Integration's `settings` and is not how swallow itself reaches the provisioner (that is the Integration endpoint). Not the generated configuration of the provisioner's proxy service, which the provisioner manages and may overwrite. Not a package repository or mirror, and not the per-subnet permission to use the built-in proxy. A proxy URL never carries credentials in swallow.
- Synonyms: Proxy (UI wording inside a provisioner Integration); MAAS "Proxy" settings.
- Deprecated terms: None.
- Examples: "Set the `maas-tainan` Provisioner HTTP Proxy to `external` with `http://10.170.168.20:18080`, then disk-deploy Ubuntu: the deployment succeeds only if the machine reached its packages through that proxy." / "Nodes that cannot reach the corporate proxy use `peer`: they talk to MAAS on port 8000, and MAAS forwards to the corporate proxy."
- Related terms: Integration, OS Provisioning Provider, OS Deployment, Deploy Target.
- Change note: Added 2026-10-13 ([decision 056](../../../decisions/056-provisioner-http-proxy.md)). Operators had to open the MAAS UI to point deployed machines at a site proxy; swallow now writes the provisioner-owned setting through. The four modes follow the MAAS UI choices and MAAS's own rule for which proxy a deployed machine gets.
