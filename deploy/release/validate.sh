#!/usr/bin/env bash
set -euo pipefail

if rg -n 'jobTemplateName|providerKind: awx|/webhooks/automation' \
  api-server/docs/development/api-contracts/api-server docs/development/glossaries/terms; then
  printf 'active contracts or glossary still expose AWX-specific API fields\n' >&2
  exit 1
fi
jq -e '.schemaVersion == 1 and (.playbooks | length > 0)' api-server/modules/manifest.json >/dev/null
# assemble-release.sh writes the schema fields from the binary; the example must show the same
# values the current source would produce.
migration_go=api-server/internal/adapters/store/mongodb/migration/migration.go
current_schema="$(sed -n 's/^const CurrentSchemaVersion = \([0-9][0-9]*\)$/\1/p' "${migration_go}")"
oldest_schema="$(sed -n 's/^const OldestUpgradableSchemaVersion = \([0-9][0-9]*\)$/\1/p' "${migration_go}")"
[[ -n "${current_schema}" && -n "${oldest_schema}" ]] ||
  { printf '%s must declare CurrentSchemaVersion and OldestUpgradableSchemaVersion as integer constants\n' "${migration_go}" >&2; exit 1; }
jq -e --argjson current "${current_schema}" --argjson oldest "${oldest_schema}" \
  '.schemaVersions == {mongo: $current, playbookManifest: 1} and
    .upgrade.from == [range($oldest; $current)] and .upgrade.to == $current' \
  deploy/release/release-manifest.example.json >/dev/null ||
  { printf 'release-manifest.example.json: schemaVersions and upgrade must match schema %s upgrading from %s (%s)\n' \
      "${current_schema}" "${oldest_schema}" "${migration_go}" >&2; exit 1; }
jq -e '.upgrade.downgradeSupported == false and
  (.images | keys) == ["api", "cli", "dashboard", "mongo", "postgres", "prometheus", "temporalServer"] and
  (.artifacts.cliBinary | type == "string") and
  ([.images.api, .images.dashboard, .images.cli] | map(startswith("ghcr.io/maple52046/swallow@sha256:")) | all) and
  ([.images.mongo, .images.postgres, .images.temporalServer, .images.prometheus] | map(test("^docker\\.io/[^@]+@sha256:")) | all)' \
  deploy/release/release-manifest.example.json >/dev/null
# Every runtime image the manifest carries is pinned in runtime-images.env, and every image the
# production compose.yaml requires comes from the manifest.
for key in MONGO_IMAGE POSTGRES_IMAGE TEMPORAL_SERVER_IMAGE PROMETHEUS_IMAGE; do
  grep -q "^${key}=" deploy/release/runtime-images.env ||
    { printf 'runtime-images.env must pin %s\n' "${key}" >&2; exit 1; }
done
while IFS= read -r variable; do
  grep -q "\"${variable}=" deploy/production/swallowctl ||
    { printf 'swallowctl does not write %s from the release manifest\n' "${variable}" >&2; exit 1; }
done < <(sed -n 's/.*\${\(SWALLOW_[A-Z_]*_IMAGE\):?.*/\1/p' deploy/production/compose.yaml | sort -u)
while IFS='=' read -r key value; do
  [[ -z "${key}" || "${key}" == \#* ]] && continue
  [[ "${value}" =~ ^[a-z0-9.-]+\.[a-z]+/[^@]+@sha256:[0-9a-f]{64}$ ]] ||
    { printf 'runtime-images.env: %s must be pinned by registry, name, and digest\n' "${key}" >&2; exit 1; }
done < deploy/release/runtime-images.env
# shellcheck disable=SC2016 # the literal text of install.sh's default, not an expansion
grep -qF '"${SWALLOW_VERSION:-@SWALLOW_VERSION@}"' deploy/release/install.sh ||
  { printf 'install.sh must default its version to @SWALLOW_VERSION@, which assemble-release.sh fills in\n' >&2; exit 1; }
if ! awk 'NF && $0 !~ /^[A-Za-z0-9_.-]+==[A-Za-z0-9_.+-]+$/ { exit 1 }' \
  api-server/internal/adapters/hostautomation/ansible/requirements.txt; then
  printf 'every Python dependency must be exactly pinned\n' >&2
  exit 1
fi
while IFS= read -r -d '' script; do
  bash -n "${script}"
done < <(find deploy -type f \( -name '*.sh' -o -name 'swallowctl' \) -print0)
