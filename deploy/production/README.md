# Production installation

This directory is the supported Docker Compose installation for Ubuntu 24.04 amd64.
Images are never built here: `.env` must contain exact digests from a promoted release.
The dashboard/Nginx container is the only public service; API and MongoDB stay on an
internal network. TLS material must come from the CA supplied for the installation.

```bash
cp .env.example .env
./prepare-secrets.sh
# Install secrets/tls.crt and secrets/tls.key with mode 0444, then replace image references.
set -a; source .env; set +a
./swallowctl preflight
./swallowctl install
./swallowctl doctor
```

`upgrade` backs up first and rejects active operations unless `--force` is explicit.
Backups include MongoDB, the credential encryption key, and job artifacts. Uninstall
retains state unless `--purge-data` is supplied.

The release bundle also carries [native](native/README.md) templates for customers that
do not permit containers.

For air-gap installation, verify `SHA256SUMS` and its Sigstore bundle, run
`docker load --input swallow-oci-<version>.tar`, and use only the references from
`release-manifest.json`. The installer never builds or silently substitutes tags.

Compose secret source files are readable to the service containers, while the enclosing
`secrets/` directory is mode 0700 and excluded from git. Back up that directory through
the lifecycle tool, not through source control.

Schedule `./swallowctl backup` daily from the host's systemd/cron policy. The tool keeps
exactly seven `daily-*` and four Sunday `weekly-*` recovery points. A restore
force-recreates API and Dashboard so the restored credential encryption key is reloaded.
