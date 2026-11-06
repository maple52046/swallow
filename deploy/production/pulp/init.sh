#!/bin/bash
# One-shot initialization of the installation's Pulp: the keys it needs, its schema, the admin
# password, and the signing service its deb publications are signed with. Every step is idempotent,
# so the service runs on every start. It runs as root because fresh volumes are owned by root, and
# hands them to the pulp user the other services run as.
#
# Everything that must survive a restore together with the Pulp database lives in pulp-certs, which
# swallowctl backs up: the key that encrypts database fields, the registry token keys, and the GPG
# home of the signing key. pulp-data holds only artifacts, which are fetched again from upstream.
set -euo pipefail
trap 'chown -R pulp:pulp /etc/pulp/certs /var/lib/pulp' EXIT

# Pulp reads its settings, which name these key files, before it can even wait for the database.
certs=/etc/pulp/certs
if [ ! -s "$certs/database_fields.symmetric.key" ]; then
  openssl rand -base64 32 > "$certs/database_fields.symmetric.key"
fi
if [ ! -s "$certs/container_auth_private_key.pem" ]; then
  openssl ecparam -genkey -name prime256v1 -noout -out "$certs/container_auth_private_key.pem"
  openssl ec -in "$certs/container_auth_private_key.pem" -pubout -out "$certs/container_auth_public_key.pem"
fi

/usr/bin/wait_on_postgres.py
pulpcore-manager migrate --noinput
pulpcore-manager reset-admin-password --password "$(< /run/secrets/pulp_admin_password)"

# The Release files of every deb publication are signed with this installation's own key, so hosts
# verify them with Signed-By instead of trusting the repository unverified.
export GNUPGHOME="$certs/gpg"
uid="swallow local repository"
mkdir -p "$GNUPGHOME"
chmod 0700 "$GNUPGHOME"
if ! gpg --batch --list-secret-keys "$uid" >/dev/null 2>&1; then
  gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key "$uid" rsa4096 sign never
fi
fingerprint=$(gpg --batch --with-colons --list-secret-keys "$uid" | awk -F: '/^fpr:/ { print $10; exit }')
if ! pulpcore-manager shell -c "import sys; from pulpcore.app.models import SigningService; sys.exit(0 if SigningService.objects.filter(name='swallow-deb').exists() else 1)"; then
  pulpcore-manager add-signing-service --class deb:AptReleaseSigningService \
    swallow-deb /usr/local/share/swallow/sign-deb-release.sh "$fingerprint"
fi
