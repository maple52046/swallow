# Compose installation assets

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

This directory defines the production-style Docker Compose installation
contract for Ubuntu 24.04 amd64. swallow is in active development and currently
publishes no stable SemVer release; evaluate this lifecycle with an exact
candidate or promoted manifest rather than treating it as a general-availability
claim.

Images are never built here. `.env` must contain exact digests from the
release/candidate manifest. Dashboard/Nginx is the only public service; API,
MongoDB, Temporal, worker, and Ansible executor stay on internal networks. TLS
material must come from the CA supplied for the installation.

```bash
cp .env.example .env
./prepare-secrets.sh
# Install secrets/tls.crt and secrets/tls.key, then set exact image references.
set -a; source .env; set +a
./swallowctl preflight
./swallowctl install
./swallowctl doctor
```

`install` runs the database migration and then creates the deployment key
(`swallow-api deployment-key ensure`), the SSH key swallow uses to log in to the
Servers it deploys; `upgrade` repeats both and keeps an existing key. The API
starts without it, but OS and Platform deployments are refused until it exists.

`upgrade` takes a backup first and rejects active Workflows unless
`--force` is explicit. Backups contain MongoDB, the credential-encryption key,
and job artifacts. Uninstall retains state unless `--purge-data` is supplied.

For air-gap installation, verify `SHA256SUMS` and its Sigstore bundle, load
`swallow-oci-<version>.tar`, and use only references from
`release-manifest.json`. The installer never builds or silently substitutes
tags.

The `secrets/` directory is mode `0700` and excluded from Git. Back it up
through the lifecycle tool, not source control. Schedule
`./swallowctl backup` daily; retention is seven daily and four Sunday weekly
recovery points. Restore force-recreates API and Dashboard so the restored
credential key is reloaded.

The [native path](native/README.md) is an incomplete preview. Until native
Temporal/PostgreSQL systemd packaging is complete, use this Compose topology for
functional Workflow execution.
