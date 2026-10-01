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

## Release and recovery

Release returns a provider machine toward ready state and may include explicit
erase options. Recovery handles failed, broken, or rescue states according to
the configured provider recovery policy. Power-off warnings must be reviewed
when memory-backed state or active work could be lost.

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
```

Use JSON/YAML request files for structured payloads so fields remain aligned
with the provider-owned contract.
