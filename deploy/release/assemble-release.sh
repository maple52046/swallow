#!/usr/bin/env bash
# Assemble the release artifacts for one SemVer version from images that are already pushed:
# the Compose bundle (the single-VM installation plus the CLI), install.sh with this version
# filled in, the CLI binary, release-manifest.json, and SHA256SUMS. publish.sh calls it after building and mirroring; it
# builds nothing, and it reads the registry with crane rather than a local container engine.
# The manifest's schema versions and upgrade sources come from `swallow-api version --json` of the
# binary in the pushed API image, which must also report this version and commit.
#
# Usage: assemble-release.sh COMMIT
# Environment: RELEASE_VERSION, OUT_DIR, IMAGE_REPO, API_DIGEST, DASHBOARD_DIGEST, CLI_DIGEST,
# and the official MONGO_IMAGE, POSTGRES_IMAGE, TEMPORAL_SERVER_IMAGE, PROMETHEUS_IMAGE,
# PROM_LABEL_PROXY_IMAGE, PULP_IMAGE, PULP_WEB_IMAGE from runtime-images.env (exact @sha256
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
mongo_image="${MONGO_IMAGE:?set MONGO_IMAGE from runtime-images.env}"
postgres_image="${POSTGRES_IMAGE:?set POSTGRES_IMAGE from runtime-images.env}"
temporal_server_image="${TEMPORAL_SERVER_IMAGE:?set TEMPORAL_SERVER_IMAGE from runtime-images.env}"
prometheus_image="${PROMETHEUS_IMAGE:?set PROMETHEUS_IMAGE from runtime-images.env}"
prom_label_proxy_image="${PROM_LABEL_PROXY_IMAGE:?set PROM_LABEL_PROXY_IMAGE from runtime-images.env}"
pulp_image="${PULP_IMAGE:?set PULP_IMAGE from runtime-images.env}"
pulp_web_image="${PULP_WEB_IMAGE:?set PULP_WEB_IMAGE from runtime-images.env}"
compose_bundle="swallow-compose-${version}.tar.zst"

for image in "${api_image}" "${dashboard_image}" "${cli_image}" "${mongo_image}" \
  "${postgres_image}" "${temporal_server_image}" "${prometheus_image}" "${prom_label_proxy_image}" \
  "${pulp_image}" "${pulp_web_image}"; do
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

# swallow-api is a static linux/amd64 binary, so the x86_64 publishing host runs it directly.
api_dir="$(mktemp -d)"
trap 'rm -rf -- "${api_dir}"' EXIT
crane export "${api_image}" - | tar -x --no-same-owner -C "${api_dir}" usr/local/bin/swallow-api
api_info="$("${api_dir}/usr/local/bin/swallow-api" version --json)"
jq -e --arg version "${version}" --arg commit "${commit}" \
  '.version == $version and .commit == $commit and (.schemaVersion | type == "number") and
    (.oldestUpgradableSchemaVersion | type == "number") and .oldestUpgradableSchemaVersion <= .schemaVersion' \
  <<<"${api_info}" >/dev/null ||
  { printf 'the API image does not report release %s at commit %s: %s\n' "${version}" "${commit}" "${api_info}" >&2; exit 1; }
playbook_manifest_schema="$(jq -e '.schemaVersion' "${root}/api-server/modules/manifest.json")"

for file in compose.yaml .env.example env.sh prepare-secrets.sh mongo-entrypoint.sh swallowctl \
  local-maas.sh local-ntp.sh bootstrap.sh prometheus.yml prometheus-exporters.yml.template \
  README.md README.zh-TW.md; do
  cp "${root}/deploy/production/${file}" "${out}/production/"
done
cp -R "${root}/deploy/production/pulp" "${out}/production/"
for file in compose.yaml .env.example prepare.sh seed.sh README.md README.zh-TW.md; do
  cp "${root}/deploy/testing/${file}" "${out}/testing/"
done
cp "${root}/deploy/third-party/offline-media-manifest.json" "${out}/"
sed "s/@SWALLOW_VERSION@/${version}/" "${root}/deploy/release/install.sh" >"${out}/install.sh"
chmod 0755 "${out}/install.sh"

jq -n --arg version "${version}" --arg commit "${commit}" \
  --arg api "${api_image}" --arg dashboard "${dashboard_image}" --arg cli "${cli_image}" \
  --arg mongo "${mongo_image}" --arg postgres "${postgres_image}" \
  --arg temporalServer "${temporal_server_image}" --arg prometheus "${prometheus_image}" \
  --arg promLabelProxy "${prom_label_proxy_image}" --arg pulp "${pulp_image}" \
  --arg pulpWeb "${pulp_web_image}" --arg compose "${compose_bundle}" \
  --argjson apiInfo "${api_info}" --argjson playbookManifest "${playbook_manifest_schema}" \
  '{schemaVersion:1,version:$version,commit:$commit,
    platforms:["linux-amd64","ubuntu-24.04-amd64"],
    schemaVersions:{mongo:$apiInfo.schemaVersion,playbookManifest:$playbookManifest},
    images:{api:$api,dashboard:$dashboard,cli:$cli,mongo:$mongo,
      postgres:$postgres,temporalServer:$temporalServer,prometheus:$prometheus,
      promLabelProxy:$promLabelProxy,pulp:$pulp,pulpWeb:$pulpWeb},
    compatibility:{host:"ubuntu-24.04-amd64",maas:"3.6",mongodb:"8.0",
      prometheus:"release-managed",dockerCE:"release-managed"},
    artifacts:{composeBundle:$compose,cliBinary:"swallow-linux-amd64",
      thirdPartyMediaManifest:"offline-media-manifest.json"},
    upgrade:{from:[range($apiInfo.oldestUpgradableSchemaVersion; $apiInfo.schemaVersion)],
      to:$apiInfo.schemaVersion,downgradeSupported:false}}' >"${out}/release-manifest.json"

tar -C "${out}" -caf "${out}/${compose_bundle}" production testing release-manifest.json
(
  cd "${out}"
  sha256sum release-manifest.json "${compose_bundle}" install.sh swallow-linux-amd64 \
    offline-media-manifest.json >SHA256SUMS
)
