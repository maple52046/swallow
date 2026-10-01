#!/usr/bin/env bash
# Assemble the release artifacts for one SemVer version from images that are already pushed:
# the Compose bundle (the single-VM installation plus the CLI), the native preview bundle, the
# air-gap OCI archive, the CLI binary, release-manifest.json, and SHA256SUMS. publish.sh calls
# it after building and mirroring; it builds nothing itself.
#
# Usage: assemble-release.sh COMMIT
# Environment: RELEASE_VERSION, OUT_DIR, IMAGE_REPO, API_DIGEST, DASHBOARD_DIGEST, CLI_DIGEST,
# and the mirrored MONGO_IMAGE, TEMPORAL_POSTGRES_IMAGE, TEMPORAL_SERVER_IMAGE,
# TEMPORAL_UI_IMAGE (exact @sha256 references). OUT_DIR is replaced.
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
temporal_ui_image="${TEMPORAL_UI_IMAGE:?set TEMPORAL_UI_IMAGE to the mirrored Temporal UI digest}"
runtime_images=("${api_image}" "${dashboard_image}" "${mongo_image}" "${temporal_postgres_image}" "${temporal_server_image}" "${temporal_ui_image}")
oci_archive="swallow-oci-${version}.tar"
native_bundle="swallow-native-${version}.tar.zst"
compose_bundle="swallow-compose-${version}.tar.zst"

for image in "${runtime_images[@]}" "${cli_image}"; do
  [[ "${image}" =~ @sha256:[0-9a-f]{64}$ ]] ||
    { printf 'image is not digest pinned: %s\n' "${image}" >&2; exit 1; }
done

rm -rf -- "${out}"
mkdir -p "${out}/native/bin" "${out}/native/dashboard" "${out}/native/automation" \
  "${out}/native/wheelhouse" "${out}/native/systemd" "${out}/native/nginx" \
  "${out}/native/logrotate" "${out}/production/bin" "${out}/testing"

for image in "${runtime_images[@]}" "${cli_image}"; do
  docker pull "${image}"
done

api_container=""
dashboard_container=""
cli_container=""
cleanup_containers() {
  [[ -z "${api_container}" ]] || docker rm -f "${api_container}" >/dev/null
  [[ -z "${dashboard_container}" ]] || docker rm -f "${dashboard_container}" >/dev/null
  [[ -z "${cli_container}" ]] || docker rm -f "${cli_container}" >/dev/null
}
trap cleanup_containers EXIT
api_container="$(docker create "${api_image}")"
dashboard_container="$(docker create "${dashboard_image}")"
cli_container="$(docker create "${cli_image}")"
docker cp "${api_container}:/usr/local/bin/swallow-api" "${out}/native/bin/swallow-api"
docker cp "${api_container}:/opt/swallow/automation/." "${out}/native/automation/"
docker cp "${dashboard_container}:/usr/share/nginx/html/." "${out}/native/dashboard/"
docker cp "${cli_container}:/swallow" "${out}/swallow-linux-amd64"
docker rm "${api_container}" "${dashboard_container}" "${cli_container}" >/dev/null
api_container=""
dashboard_container=""
cli_container=""
trap - EXIT
chmod 0755 "${out}/swallow-linux-amd64"
install -m 0755 "${out}/swallow-linux-amd64" "${out}/production/bin/swallow"

# The host Python resolves the wheelhouse; publish from Ubuntu 24.04 so binary wheels match the
# native target's Python.
python3 -m pip download --dest "${out}/native/wheelhouse" \
  --requirement "${out}/native/automation/requirements.txt"

cp "${root}/deploy/production/native/swallow-api.service" "${out}/native/systemd/"
cp "${root}/deploy/production/native/swallow-backup.service" "${out}/native/systemd/"
cp "${root}/deploy/production/native/swallow-backup.timer" "${out}/native/systemd/"
cp "${root}/deploy/production/native/swallow.tmpfiles" "${out}/native/systemd/"
cp "${root}/deploy/production/native/swallow-nginx.conf" "${out}/native/nginx/swallow.conf"
cp "${root}/deploy/production/native/swallow.logrotate" "${out}/native/logrotate/swallow"
cp "${root}/deploy/production/native/swallowctl" "${out}/native/swallowctl"
cp "${root}/deploy/production/native/prepare-secrets.sh" "${out}/native/prepare-secrets.sh"

for file in compose.yaml .env.example env.sh prepare-secrets.sh swallowctl local-maas.sh bootstrap.sh \
  README.md README.zh-TW.md; do
  cp "${root}/deploy/production/${file}" "${out}/production/"
done
for file in compose.yaml .env.example prepare.sh seed.sh README.md README.zh-TW.md; do
  cp "${root}/deploy/testing/${file}" "${out}/testing/"
done
cp "${root}/deploy/third-party/offline-media-manifest.json" "${out}/"

# The CLI is a host binary, not a runtime service, so the air-gap archive carries only the
# images Compose starts; the binary ships beside it and inside the Compose bundle.
docker save --output "${out}/${oci_archive}" "${runtime_images[@]}"

jq -n --arg version "${version}" --arg commit "${commit}" \
  --arg api "${api_image}" --arg dashboard "${dashboard_image}" --arg cli "${cli_image}" \
  --arg mongo "${mongo_image}" --arg temporalPostgres "${temporal_postgres_image}" \
  --arg temporalServer "${temporal_server_image}" --arg temporalUI "${temporal_ui_image}" \
  --arg oci "${oci_archive}" --arg native "${native_bundle}" --arg compose "${compose_bundle}" \
  '{schemaVersion:1,version:$version,commit:$commit,
    platforms:["linux-amd64","ubuntu-24.04-amd64"],
    schemaVersions:{mongo:3,playbookManifest:1},
    images:{api:$api,dashboard:$dashboard,cli:$cli,mongo:$mongo,
      temporalPostgres:$temporalPostgres,temporalServer:$temporalServer,temporalUI:$temporalUI},
    compatibility:{host:"ubuntu-24.04-amd64",maas:"3.6",mongodb:"8.0",
      prometheus:"release-managed",dockerCE:"release-managed"},
    artifacts:{ociArchive:$oci,nativeBundle:$native,composeBundle:$compose,
      cliBinary:"swallow-linux-amd64",thirdPartyMediaManifest:"offline-media-manifest.json"},
    upgrade:{from:[2],to:3,downgradeSupported:false}}' >"${out}/release-manifest.json"

tar -C "${out}" -caf "${out}/${native_bundle}" native release-manifest.json
tar -C "${out}" -caf "${out}/${compose_bundle}" production testing release-manifest.json
(
  cd "${out}"
  sha256sum release-manifest.json "${oci_archive}" "${native_bundle}" "${compose_bundle}" \
    swallow-linux-amd64 offline-media-manifest.json >SHA256SUMS
)
