# Hypervisor

- Bounded context: swallow-wide (Server, OS Provisioning). `api-server`, `dashboard`, and `cli` use this term with the same meaning.
- Definition: A `deployed` Server that runs libvirt virtual machines which swallow can enroll and give Boot Media, reached by swallow over SSH with the installation's Deployment Key.
- Allowed meaning: Being a Hypervisor is a role a Server plays, not a separate record: any present, deployed Server with a primary address whose login account (its effective Server Default User, or an account an operator names) may use the system libvirt daemon. swallow reads its libvirt domains and changes only a domain's CD-ROM and boot order (the `libvirt` Boot Media method). A virtual machine's Power Configuration (driver `virsh`, address `qemu+ssh://<account>@<host>/system`) names its Hypervisor through the host. The provisioner, not swallow, switches the virtual machines' power, and needs its own SSH access to the same account.
- Disallowed meaning: Not a provisioner VM host (a MAAS pod, which manages its virtual machines itself and is outside swallow). Not a Platform. Not a host swallow keeps a separate credential for: only the Deployment Key is used. Not a host that is not a swallow Server.
- Synonyms: libvirt host, KVM host (operator wording).
- Deprecated terms: None.
- Examples: "`tainan-ci` is a deployed Server whose `ubuntu` account is in the `libvirt` group, so it is the Hypervisor of `lab-afde-mi308-1..3`; an operator enrolls them by name." / "A VM whose address names `192.168.100.1`, which is not a swallow Server, has no Hypervisor in swallow, so it has no Boot Media method."
- Related terms: Server, Server Enrollment, Power Configuration, Boot Media, Deployment Key, Server Default User, Server Lock.
- Change note: Added 2026-10-09 ([decision 055](../../../decisions/055-libvirt-virtual-machine-enrollment.md)) so libvirt virtual machines can be enrolled by name and booted from a Boot ISO without anyone logging in to the host.
