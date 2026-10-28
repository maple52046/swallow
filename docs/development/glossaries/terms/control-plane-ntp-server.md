# Control-Plane NTP Server

- Bounded context: Swallow product delivery and lifecycle management (Installation).
- Definition: The one NTP service an Installation provides on the swallow host, answering on UDP 123
  at the installation address (`SWALLOW_ADDRESS`). Every Server swallow provisions is pointed at it
  right after its OS comes up (`SWALLOW_API_NTP_SERVER`), whatever upstream it follows.
- Allowed meaning: A host service, never a container. Providing it and choosing its upstream are
  separate. When the provisioner is the co-located MAAS, the installation installs nothing: MAAS's
  own chronyd is the server. Any other provisioner gets the distribution chrony with a
  swallow-owned configuration. Either way the upstream is the default `ntp.ubuntu.com` or the
  servers set with `swallowctl ntp upstream` (`SWALLOW_NTP_UPSTREAM`). When its upstream is
  unreachable it keeps serving its local clock, so the Servers using it still agree with each other.
- Disallowed meaning: Not a Managed Software kind, not a per-Site record, and not the NTP client
  configuration swallow writes on Servers (that is the `configure-ntp` Task of an OS Deployment).
  Not a second chronyd beside MAAS's: only one service owns UDP 123. Its upstream must not take its
  time from the swallow host itself, directly or through a gateway that computes time from that
  host's clock.
- Synonyms: control-plane NTP.
- Deprecated terms: None.
- Examples: "The production installation's Control-Plane NTP Server is the co-located MAAS
  chronyd; `swallowctl ntp upstream set 10.170.168.3` set its upstream." / "`swallowctl doctor`
  reports the Control-Plane NTP Server as serving, but still on its local clock."
- Related terms: Installation, OS Deployment, Time Sync Check.
- Change note: Added 2026-10-28 with decision 065. Revised the same day when the per-Site NTP
  Configuration was removed: Servers always point at this server, and installing it is separated
  from setting its upstream.
