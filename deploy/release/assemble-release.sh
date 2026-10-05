#!/usr/bin/env bash
# Assemble the release artifacts for one SemVer version from images that are already pushed:
# the Compose bundle (the single-VM installation plus the CLI), the CLI binary,
# release-manifest.json, and SHA256SUMS. publish.sh calls it after building and mirroring; it
# builds nothing, and it reads the registry with crane rather than a local container engine.
#
# Usage: assemble-release.sh COMMIT
# Environment: RELEASE_VERSION, OUT_DIR, IMAGE_REPO, API_DIGEST, DASHBOARD_DIGEST, CLI_DIGEST,
# and the mirrored MONGO_IMAGE, TEMPORAL_POSTGRES_IMAGE, TEMPORAL_SERVER_IMAGE (exact @sha256
# references). OUT_DIR is replaced.
set -euo pipefail

commit="${1:?commit SHA required}"
version="${RELEASE_VERSION:?set RELEASE_VERSION (SemVer without v)}"
[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] ||
  { printf 'RELEASE_VERSION must be SemVer: %s\n' "${version}" >&2; exit 1; }
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${OUT_DIR:?set OUT_DIR}"
image_repo="${IMAGE_REPO:?set IMAGE_REPO, e.g. ghcr.io/<owner>/swallow}"
api_image="${image_repo}@${API_DIGEST:?}"
dashboard_image="${image_repo}@${DASHBOARD_DIGEST:?}"
cli_image="${image_repo}@${CLI_DIGEST:?}"
# The runtime images below are the IMAGE_REPO mirrors written by mirror-runtime-images.sh.
mongo_image="${MONGO_IMAGE:?set MONGO_IMAGE to the mirrored MongoDB digest}"
temporal_postgres_image="${TEMPORAL_POSTGRES_IMAGE:?set TEMPORAL_POSTGRES_IMAGE to the mirrored PostgreSQL digest}"
temporal_server_image="${TEMPORAL_SERVER_IMAGE:?set TEMPORAL_SERVER_IMAGE to the mirrored Temporal Server digest}"
compose_bundle="swallow-compose-${version}.tar.zst"

for image in "${api_image}" "${dashboard_image}" "${cli_image}" "${mongo_image}" \
  "${temporal_postgres_image}" "${temporal_server_image}"; do
  [[ "${image}" =~ @sha256:[0-9a-f]{64}$ ]] ||
    { printf 'image is not digest pinned: %s\n' "${image}" >&2; exit 1; }
done

rm -rf -- "${out}"
mkdir -p "${out}/production/bin" "${out}/testing"

# The cli image is FROM scratch with the static binary at /swallow, so it cannot be run or copied
# out of a container; crane flattens the pushed digest and tar keeps only that file.
crane export "${cli_image}" - | tar -x --no-same-owner -C "${out}" swallow
mv -- "${out}/swallow" "${out}/swallow-linux-amd64"
chmod 0755 "${out}/swallow-linux-amd64"
install -m 0755 "${out}/swallow-linux-amd64" "${out}/production/bin/swallow"

for file in compose.yaml .env.example env.sh prepare-secrets.sh swallowctl local-maas.sh bootstrap.sh \
  README.md README.zh-TW.md; do
  cp "${root}/deploy/production/${file}" "${out}/production/"
done
for file in compose.yaml .env.example prepare.sh seed.sh README.md README.zh-TW.md; do
  cp "${root}/deploy/testing/${file}" "${out}/testing/"
done
cp "${root}/deploy/third-party/offline-media-manifest.json" "${out}/"

jq -n --arg version "${version}" --arg commit "${commit}" \
  --arg api "${api_image}" --arg dashboard "${dashboard_image}" --arg cli "${cli_image}" \
  --arg mongo "${mongo_image}" --arg temporalPostgres "${temporal_postgres_image}" \
  --arg temporalServer "${temporal_server_image}" --arg compose "${compose_bundle}" \
  '{schemaVersion:1,version:$version,commit:$commit,
    platforms:["linux-amd64","ubuntu-24.04-amd64"],
    schemaVersions:{mongo:3,playbookManifest:1},
    images:{api:$api,dashboard:$dashboard,cli:$cli,mongo:$mongo,
      temporalPostgres:$temporalPostgres,temporalServer:$temporalServer},
    compatibility:{host:"ubuntu-24.04-amd64",maas:"3.6",mongodb:"8.0",
      prometheus:"release-managed",dockerCE:"release-managed"},
    artifacts:{composeBundle:$compose,cliBinary:"swallow-linux-amd64",
      thirdPartyMediaManifest:"offline-media-manifest.json"},
    upgrade:{from:[2],to:3,downgradeSupported:false}}' >"${out}/release-manifest.json"

tar -C "${out}" -caf "${out}/${compose_bundle}" production testing release-manifest.json
(
  cd "${out}"
  sha256sum release-manifest.json "${compose_bundle}" swallow-linux-amd64 \
    offline-media-manifest.json >SHA256SUMS
)
