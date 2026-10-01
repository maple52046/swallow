# SSH keys and image login users

[繁體中文](../../zh-TW/guides/ssh-keys.md) · [Documentation home](../README.md)

swallow logs in to the Servers it deploys to check SSH readiness and run Ansible.
It manages the keys for that itself, and it knows which account to log in as for
each OS image.

## The two kinds of key

Open **SSH keys** from the account menu (top right).

- **Deployment key** — one key pair for the whole installation. Installation creates
  it (ed25519): `swallowctl install` and `swallowctl upgrade` run
  `swallow-api deployment-key ensure` after the database migration. swallow keeps the
  private key encrypted and never shows it. OS and Platform deployments are refused until
  it exists. swallow uses it for SSH readiness and Ansible on every Site
  unless that Site sets its own automation key.
- **Access keys** — your own keys for logging in to Servers. swallow stores only
  the public key. You can import an existing public key, or generate a key pair; a
  generated private key is shown exactly once, so download or copy it before you
  close the dialog.

## How keys reach Servers

swallow registers every key in each provisioner that can hold SSH keys (MAAS: the
account that owns the Integration's API key). MAAS then writes those keys into the
deployed image's default user. The page shows, per provisioner, whether each key is
**Synced**, **Pending**, **Failed** (with the reason), or **Not supported**.

- Sync runs after every key change, every few minutes, and before each OS
  deployment. **Sync to provisioners** requests one immediately.
- If swallow cannot register the deployment key in a MAAS right before a
  deployment, the deployment stops with `ssh_key_registration_failed`; retry it
  once MAAS is reachable.
- Keys you added in MAAS yourself are never removed by swallow.

Keys are injected only when a Server is deployed. Regenerating or replacing the
deployment key, or deleting an access key, does **not** change Servers that were
already deployed: they keep the keys they were deployed with. Redeploy them (or
update their `authorized_keys`) when that matters.

## Image default users

Each OS image has a **default user**: the account its cloud-init creates, and the
one swallow logs in as on Servers deployed with it.

- Synced Ubuntu, CentOS, and RHEL images have a built-in default (`ubuntu`,
  `centos`, `cloud-user`).
- For an uploaded custom image, set it in the upload dialog or later with
  **Edit** on the OS Images page (for example `cloud-user`).
- The **Default user** column shows the value and whether it is custom or
  built-in; a Server's summary shows it as **Login user**.

When an image has no default user, swallow falls back to the Site's SSH user and
then tries `cloud-user` and `ubuntu`.

## Site overrides

A Site's automation credential can still carry its own private key; when it does,
that Site uses it instead of the deployment key. Leave it empty to use the
deployment key. The Site's SSH user is optional and is only the fallback described
above.

## Backups

The deployment key's private key lives in MongoDB, encrypted with the API
credential key. Back up and restore both together, or swallow will not be able to
use the restored key.

## CLI

```bash
swallow ssh-keys list
swallow ssh-keys generate --name laptop --private-key-out ~/.ssh/id_ed25519_laptop
swallow ssh-keys deployment show
swallow provisioning images upload --integration int1 --name rocky-10 \
  --architecture amd64 --content ./rocky.tgz --default-user cloud-user
```

Exact fields are defined by the
[SSH Keys contract](../../../api-server/docs/development/api-contracts/api-server/ssh-keys.md)
and the
[provisioning contract](../../../api-server/docs/development/api-contracts/api-server/provisioning.md).
