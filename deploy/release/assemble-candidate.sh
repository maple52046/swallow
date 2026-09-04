#!/usr/bin/env bash
set -euo pipefail

commit="${1:?commit SHA required}"
version="0.0.0-candidate"
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${root}/out/candidate"
api_image="ghcr.io/${GITHUB_REPOSITORY_OWNER:?}/swallow-api@${API_DIGEST:?}"
dashboard_image="ghcr.io/${GITHUB_REPOSITORY_OWNER}/swallow-dashboard@${DASHBOARD_DIGEST:?}"
mongo_image="${MONGO_IMAGE:?set MONGO_IMAGE to the release-approved MongoDB digest}"
temporal_postgres_image="${TEMPORAL_POSTGRES_IMAGE:?set TEMPORAL_POSTGRES_IMAGE to the release-approved PostgreSQL digest}"
temporal_server_image="${TEMPORAL_SERVER_IMAGE:?set TEMPORAL_SERVER_IMAGE to the release-approved Temporal Server digest}"
temporal_ui_image="${TEMPORAL_UI_IMAGE:?set TEMPORAL_UI_IMAGE to the release-approved Temporal UI digest}"

for image in "${api_image}" "${dashboard_image}" "${mongo_image}" "${temporal_postgres_image}" "${temporal_server_image}" "${temporal_ui_image}"; do
  [[ "${image}" =~ @sha256:[0-9a-f]{64}$ ]] ||
    { printf 'image is not digest pinned: %s\n' "${image}" >&2; exit 1; }
done

rm -rf -- "${out}"
mkdir -p "${out}/native/bin" "${out}/native/dashboard" "${out}/native/automation" \
  "${out}/native/wheelhouse" "${out}/native/systemd" "${out}/native/nginx" \
  "${out}/native/logrotate" "${out}/production" "${out}/testing"

docker pull "${api_image}"
docker pull "${dashboard_image}"
docker pull "${mongo_image}"
docker pull "${temporal_postgres_image}"
docker pull "${temporal_server_image}"
docker pull "${temporal_ui_image}"

api_container=""
dashboard_container=""
cleanup_containers() {
  [[ -z "${api_container}" ]] || docker rm -f "${api_container}" >/dev/null
  [[ -z "${dashboard_container}" ]] || docker rm -f "${dashboard_container}" >/dev/null
}
trap cleanup_containers EXIT
api_container="$(docker create "${api_image}")"
dashboard_container="$(docker create "${dashboard_image}")"
docker cp "${api_container}:/usr/local/bin/swallow" "${out}/native/bin/swallow"
docker cp "${api_container}:/opt/swallow/automation/." "${out}/native/automation/"
docker cp "${dashboard_container}:/usr/share/nginx/html/." "${out}/native/dashboard/"
docker rm "${api_container}" "${dashboard_container}" >/dev/null
api_container=""
dashboard_container=""
trap - EXIT

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

cp "${root}/deploy/production/compose.yaml" "${out}/production/"
cp "${root}/deploy/production/.env.example" "${out}/production/"
cp "${root}/deploy/production/prepare-secrets.sh" "${out}/production/"
cp "${root}/deploy/production/swallowctl" "${out}/production/"
cp "${root}/deploy/production/README.md" "${out}/production/"
cp "${root}/deploy/testing/compose.yaml" "${out}/testing/"
cp "${root}/deploy/testing/.env.example" "${out}/testing/"
cp "${root}/deploy/testing/prepare.sh" "${out}/testing/"
cp "${root}/deploy/testing/seed.sh" "${out}/testing/"
cp "${root}/deploy/testing/README.md" "${out}/testing/"
cp "${root}/deploy/third-party/offline-media-manifest.json" "${out}/"
cp "${root}/api.spdx.json" "${root}/dashboard.spdx.json" "${out}/"

docker save --output "${out}/swallow-oci-candidate.tar" \
  "${api_image}" "${dashboard_image}" "${mongo_image}" "${temporal_postgres_image}" "${temporal_server_image}" "${temporal_ui_image}"

jq -n --arg version "${version}" --arg commit "${commit}" \
  --arg api "${api_image}" --arg dashboard "${dashboard_image}" --arg mongo "${mongo_image}" \
  --arg temporalPostgres "${temporal_postgres_image}" --arg temporalServer "${temporal_server_image}" --arg temporalUI "${temporal_ui_image}"  \
  '{schemaVersion:1,version:$version,commit:$commit,
    platforms:["linux-amd64","ubuntu-24.04-amd64"],
    schemaVersions:{mongo:3,playbookManifest:1},
    images:{api:$api,dashboard:$dashboard,mongo:$mongo,temporalPostgres:$temporalPostgres,temporalServer:$temporalServer,temporalUI:$temporalUI},
    compatibility:{host:"ubuntu-24.04-amd64",maas:"3.6",mongodb:"8.0",
      prometheus:"release-managed",dockerCE:"release-managed"},
    artifacts:{ociArchive:"swallow-oci-candidate.tar",
      nativeBundle:"swallow-native-candidate.tar.zst",
      thirdPartyMediaManifest:"offline-media-manifest.json"},
    upgrade:{from:[2],to:3,downgradeSupported:false}}' >"${out}/release-manifest.json"

(
  cd "${out}"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum >SHA256SUMS
)
