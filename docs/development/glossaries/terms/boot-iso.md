# Boot ISO

- Bounded context: OS Provisioning. `api-server`, `dashboard`, and `cli` use this term with the same meaning.
- Definition: A swallow-built iPXE boot ISO for one provisioner Integration: when a machine boots it, it takes an address from the site's own DHCP and then chains to that provisioner's MAAS rack (`http://<rack>:5248/ipxe.cfg`).
- Allowed meaning: Built on demand by an operator, who chooses the provisioner Integration and gives the rack address; swallow renders the iPXE script from its fixed, verified template (no free-form script) and packages it with pinned iPXE binaries into one ISO that boots on both BIOS and UEFI. swallow stores the file and serves it, unauthenticated and with HTTP Range, at a URL derived from the installation's Boot Media base URL and the Boot ISO's id; the BMC streams it on demand at every boot. A Boot ISO is selected by a Server's Boot Media, and only a Server of the same Integration may select it. It cannot be deleted while a Server's enabled Boot Media uses it.
- Disallowed meaning: Not an OS Image and not what is installed on a Server. Not the provisioner's own boot resources. Not signed for Secure Boot: a Server booting it must have Secure Boot off. Not an installation file supplied outside swallow (that model is retired).
- Synonyms: iPXE boot ISO; boot loader ISO (operator wording).
- Deprecated terms: The installation's Boot Media ISO (`SWALLOW_API_BOOT_MEDIA_ISO_PATH`), replaced by Boot ISOs built in swallow.
- Examples: "An operator builds a Boot ISO named `tainan-rack` for the Tainan MAAS integration with rack address `10.170.168.20`; its script chains to `http://10.170.168.20:5248/ipxe.cfg`." / "`tainan-ci` enables Boot Media with the `tainan-rack` Boot ISO; a Boot ISO of another Site's provisioner is not offered."
- Related terms: Boot Media, BMC, OS Provisioning Provider, Integration, Site, Server.
- Change note: Added 2026-10-05 ([decision 049](../../../decisions/049-boot-iso-builder.md)) when swallow began building per-provisioner iPXE ISOs instead of serving one installation-supplied file.
