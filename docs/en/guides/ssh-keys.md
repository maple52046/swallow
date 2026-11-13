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
  it exists. swallow uses it for SSH readiness and Ansible on every Site; a Site
  cannot override it with another private key.
- **Access keys** — your own keys for logging in to Servers. swallow stores only
  the public key. You can import an existing public key, or generate a key pair; a
  generated private key is shown exactly once, so download or copy it before you
  close the dialog.

## How keys reach Servers

On an installation with a Directory (every Compose installation), your access keys
reach the Servers you may log in to through it: a Server with the directory client
reads them when you connect, so adding or deleting a key, or joining or leaving a
Pool, takes effect within about a minute without redeploying. You log in as your
swallow username; your home directory is created at your first login.

swallow also registers the deployment key and the **admins'** access keys in each
provisioner that can hold SSH keys (MAAS: the account that owns the Integration's API
key), so admins can still get in if the Directory is down. Other users' keys are not
registered there, so their page shows the deployment key's public key and their own
access keys without provisioner status. MAAS then writes the registered keys into the
deployed image's default user. For an admin, the page shows, per provisioner, whether
each key is **Synced**, **Pending**, **Failed** (with the reason), or **Not supported**.

- Sync runs after every key change, every few minutes, and before each OS
  deployment. An admin's **Sync to provisioners** requests one immediately; replacing
  or regenerating the deployment key is also an admin's.
- If swallow cannot register the deployment key in a MAAS right before a
  deployment, the deployment stops with `ssh_key_registration_failed`; retry it
  once MAAS is reachable.
- Keys you added in MAAS yourself are never removed by swallow.

Keys the provisioner writes are injected only when a Server is deployed.
Regenerating or replacing the deployment key, or deleting an admin's access key,
does **not** change that copy on Servers that were already deployed: they keep the
keys they were deployed with. Redeploy them (or
update their `authorized_keys`) when that matters.

## Image default users

Each OS image has a **default user**: the account its cloud-init creates, and the
one swallow logs in as on Servers deployed with it.

- Synced Ubuntu, CentOS, and RHEL images have a built-in default (`ubuntu`,
  `centos`, `cloud-user`).
- For an uploaded custom image, set it in the upload dialog or later with
  **Edit** on the OS Images page (for example `cloud-user`).
- The **Default user** column shows the effective user name. A deployed
  Server's summary has a **Connection** card with that login user, its address,
  and a ready-to-copy `ssh <user>@<address>` command.

When an image has no default user, swallow falls back to the Site's SSH user and
then tries `cloud-user` and `ubuntu`.

## A Server's own default user

A Server whose OS was installed outside swallow (an existing Server) does not
have the deployment key, and its image-derived user may be wrong. Set its default
user on the Server's **Summary** → **Connection** card with **Set default user**
or **Change**:

- Enter the account (for example `amd`). Optionally give the account's
  **password once**: swallow logs in with it and adds the deployment key to the
  account's `~/.ssh/authorized_keys`. The password is not stored. The host must
  allow password logins over SSH for this.
- Without a password, add the key yourself with the command the dialog shows,
  run as that account on the host.
- swallow then logs in with the deployment key to prove it works, and only then
  saves the account. It also reports whether the account can use sudo without a
  password; if sudo asks for one, automation uses the Site's become password, and
  an account that cannot use sudo cannot run installs or Platform deploys.

Any deployed Server can override its image default this way. **Use the OS image
default** removes the override. A value set on the Server belongs to its current
OS: swallow clears it when it deploys a new OS to the Server or when the Server
is released. Docker CE adds the Server's default user to the `docker` group (see
Managed Software).

## One key for every Site

Every Site's automation logs in with the deployment key; a Site cannot carry its own
private key. To use another key everywhere, replace the deployment key (**Replace**
on this page, or `swallow ssh-keys deployment replace`). A Site's automation
credential holds only the optional become password, and its SSH user is optional and
only the fallback described above.

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
