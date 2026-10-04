# OS provisioning

[繁體中文](../../zh-TW/guides/os-provisioning.md) · [Documentation home](../README.md)

OS provisioning changes provider-owned machine state through a durable
Workflow. A deployment request records intent first, then the provisioner and
automation stages converge the Server toward the requested result.

## Prepare an image

Under **Provisioning → Images**:

1. Select the Site/provisioner catalog.
2. Use an existing provider image or upload a supported custom image.
3. Set the image's default user if it is a custom image (the account its
   cloud-init creates; synced Ubuntu, CentOS, and RHEL images have a built-in
   one). swallow logs in to deployed Servers as this user — see
   [SSH keys and image login users](ssh-keys.md).
4. Verify the image before deployment.
5. Review verification failures and target compatibility.

Uploading an image from the Dashboard is in development, and release builds
hide the Upload action; see [Features in development](dashboard.md#features-in-development).

Upload limits, content types, and provider requirements are defined by the
[active provisioning contract](../../../api-server/docs/development/api-contracts/api-server/provisioning.md).
Deleting an image acts on its owning provider or overlay according to that
contract.

## Deployment templates

A Deployment Template records reusable operator choices, not executable
automation. It can preselect an image and settings while the deployment request
still validates each current target. Templates never bypass current eligibility.

Templates in the Dashboard are in development; release builds hide them and
deploy with a custom configuration. The API and CLI are unaffected.

## Deploy an operating system

The Dashboard wizard validates:

- selected Servers belong to the intended Site and are deployable;
- the image is available and verified for the target;
- the requested disk or RAM deployment target is supported;
- network settings and release options are valid;
- no lock or active conflicting Workflow blocks the Server.

Submission creates one durable Workflow for the selected targets. Follow it in
**Workflows**; do not infer completion from the request returning successfully.
While a Server deploys, its detail page shows the provisioner's current
installation stage (for example *Configuring OS*). If that stage stops advancing
for 25 minutes, the Workflow step asks for attention instead of waiting out the
two-hour limit; the provider deployment is left running for you to inspect,
retry, or release.

## Boot Media for networks without provisioner DHCP

Some Servers sit on a network whose DHCP is run by the site, not by the
provisioner, so they cannot PXE-boot into it. For those, swallow serves an iPXE
boot ISO and has the Server's BMC mount it as virtual media and boot it first:
the ISO takes an address from the site's DHCP and chains to the provisioner.

- **Installation:** place the iPXE ISO file and set the base URL BMCs reach
  swallow at (see [Configuration](../reference/configuration.md#boot-media)).
  The ISO URL is fixed by the installation; you never type one per Server.
- **Detection:** swallow probes every newly enrolled Server's BMC for Redfish
  virtual media and boot override, whatever power driver the provisioner uses,
  and shows the result on the Server's **Summary → Management controller** card.
  **Re-detect Redfish** probes again, for example after a BMC firmware update.
- **Enable:** **Enable Boot Media** runs a preflight against the real BMC — it
  mounts the ISO, directs the next boots at it, and saves the setting only when
  both worked. Support differs by hardware and firmware, so a refusal shows the
  BMC's own explanation. The Server is not rebooted.
- **Deploy:** every OS deployment of a Server with Boot Media enabled first runs
  an *Ensure Boot Media* step that re-applies it, because a BMC can lose the
  mount (for example after a BMC restart) or the boot order. If the BMC cannot
  be reached, that step asks for attention and the deployment waits; retry it
  from **Workflows** once the BMC answers. If the Server still misses the ISO —
  the BMC dropped it when the provisioner powered the Server on, or the BIOS
  booted the disk — swallow mounts the ISO again two minutes in, and if the
  Server has not network-booted ten minutes in, re-applies Boot Media and
  restarts it once through Redfish. Such a deployment takes ten to fifteen
  minutes longer.
- **Check BMC** reads what the BMC reports right now. **Disable** stops
  re-applying it and asks the BMC to eject the ISO.

## Hardware inspection and tests

From a Server's **Take action → Hardware checks**, **Inspect hardware** asks the
provisioner to inventory the machine's hardware again. MAAS calls this action
*Commission*. The Server reads **Inspecting** while it runs; **Test** similarly
reads **Testing** while the provider runs hardware tests. The Summary card's
**Inspection** field is the provider's last inspection result, such as *Passed*.

## Release and recovery

Release returns a provider machine to the available pool without deleting its
Server. It is available from deployed, allocated, failed, broken, or rescue
state and may include explicit erase options. After the action is accepted, the
Servers list shows **Releasing** while the provider works and then **Ready**
when the machine is back in the pool; the list follows this transition without
requiring you to leave the page.

Recovery handles allocated, failed, broken, or rescue states according to the
configured provider recovery policy. Power-off warnings must be reviewed when
memory-backed state or active work could be lost.

## Network inspection

Network configuration reads and writes provider-owned interfaces and links.
Automatic addressing requests provider auto-assignment; it does not invent a
separate swallow address allocator.

## CLI

```bash
swallow provisioning images list --integration int1
swallow provisioning templates list --site-id site1
swallow provisioning deploy --file deploy.yaml
swallow provisioning release --server server1 --erase
swallow servers inspect server1
swallow servers redfish-probe server1
swallow servers boot-media enable server1
swallow servers boot-media get server1 --live
```

Use JSON/YAML request files for structured payloads so fields remain aligned
with the provider-owned contract.
