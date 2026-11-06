#!/bin/bash
# Signs a deb publication's Release file with the installation's key (pulp_deb
# AptReleaseSigningService): writes InRelease and Release.gpg and prints where they are. The GPG home
# is in pulp-certs, which swallowctl backs up with the Pulp database.
set -euo pipefail

export GNUPGHOME=/etc/pulp/certs/gpg
release="$(readlink -f "$1")"
out="${PULP_TEMP_WORKING_DIR:-$(dirname "$release")}"
opts=(--batch --yes --armor --digest-algo SHA256 --local-user "$PULP_SIGNING_KEY_FINGERPRINT")

gpg "${opts[@]}" --detach-sign --output "$out/Release.gpg" "$release"
gpg "${opts[@]}" --clearsign --output "$out/InRelease" "$release"

printf '{"signatures": {"inline": "%s", "detached": "%s"}}\n' "$out/InRelease" "$out/Release.gpg"
