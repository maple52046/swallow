# Local Repository Mode

- Bounded context: Local Repository. `api-server` and `dashboard` use this term with the same meaning.
- Definition: How a Site's deployments use its Local Repository: not at all, as a cache in front of the upstreams, or as their only source. It is the user's choice in the Local Repository spec (`mode`).
- Allowed meaning: One value per Site, from this closed set:

  | Mode | Dashboard label | Meaning |
  | --- | --- | --- |
  | `upstream` | Upstream sources | Deployments fetch every artifact from the upstream its module declares (files verified by their sha256, a module's deb sources by their declared keys), and each Server keeps its own OS package sources. The controller converges nothing and the provider keeps what it holds. The default, which the installation writes. |
  | `cache` | Cache | Deployments fetch from the Local Repository what it already serves, and the rest from the upstreams their modules declare, decided per artifact when the Workflow is accepted (the OS package sources as one: all through the Local Repository once it has deb sources and serves every one, otherwise all from the upstreams); they never wait for it to synchronize, and the controller keeps filling it. A deployment is refused only while the Local Repository itself cannot be used: its Integration disabled, the controller's last pass stopped by the Integration or its provider, or its addresses not read yet. |
  | `offline` | Offline | Deployments fetch every artifact from the Local Repository only, so Servers never reach the Internet, and are accepted only while it serves everything they may fetch (the Local Repository is `ready`). Until content can be imported from disk, the Local Repository itself still fetches a deb or Python package from its upstream on the first request for it, so the Site's control plane needs Internet access to fill it. |

  The dashboard label is a UI label; the value is the domain term. Workflows already accepted keep the sources they were accepted with; a host follows the Site's current mode the next time a playbook that fetches artifacts runs on it.
- Disallowed meaning: Not how the provider fills the Local Repository (synchronizing, pulling through, or, later, importing): `offline` says where Servers fetch from, not that the provider has no Internet access. Not a fallback: `cache` is the user's explicit choice and every run records where each of its artifacts comes from; an `offline` Site that is not ready is refused, never sent to the upstreams.
- Synonyms: None.
- Deprecated terms: `serveDeployments` (the boolean the mode replaced: `false` is `upstream`, `true` is `offline`).
- Examples: "The development Site uses `cache`, so a RAM deployment of Ubuntu noble is accepted while the `ubuntu` source is still synchronizing: its Servers keep their own Ubuntu sources and fetch the k0s binary from the Local Repository once it holds it." / "A Site in `offline` mode keeps deploying while the daily resync of `ubuntu` runs, because the previous synchronization is still served."
- Related terms: Local Repository, Repository Source, Site, Desired State.
- Change note: Added 2026-11-11 by the user's decision that a Site uses its Local Repository in one of three modes instead of the boolean `serveDeployments`: the development Site was refused deployments while the Ubuntu archive synchronized for the first time, and the user wants it to work like a proxy cache while a Site that must not reach the Internet keeps the strict behavior ([decision 078](../../../decisions/078-local-repository.md) (local repository)).
