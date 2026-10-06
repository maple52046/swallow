#!/usr/bin/env bash
set -euo pipefail

if rg -n 'jobTemplateName|providerKind: awx|/webhooks/automation' \
  api-server/docs/development/api-contracts/api-server docs/development/glossaries/terms; then
  printf 'active contracts or glossary still expose AWX-specific API fields\n' >&2
  exit 1
fi
jq -e '.schemaVersion == 1 and (.playbooks | length > 0)' api-server/automation/manifest.json >/dev/null
jq -e '.schemaVersions.mongo == 3 and .upgrade.from == [2] and .upgrade.to == 3 and .upgrade.downgradeSupported == false and
  (.images | keys) == ["api", "cli", "dashboard", "mongo", "postgres", "temporalServer"] and
  (.artifacts.cliBinary | type == "string") and
  ([.images.api, .images.dashboard, .images.cli] | map(startswith("ghcr.io/maple52046/swallow@sha256:")) | all) and
  ([.images.mongo, .images.postgres, .images.temporalServer] | map(test("^docker\\.io/[^@]+@sha256:")) | all)' \
  deploy/release/release-manifest.example.json >/dev/null
while IFS='=' read -r key value; do
  [[ -z "${key}" || "${key}" == \#* ]] && continue
  [[ "${value}" =~ ^[a-z0-9.-]+\.[a-z]+/[^@]+@sha256:[0-9a-f]{64}$ ]] ||
    { printf 'runtime-images.env: %s must be pinned by registry, name, and digest\n' "${key}" >&2; exit 1; }
done < deploy/release/runtime-images.env
# shellcheck disable=SC2016 # the literal text of install.sh's default, not an expansion
grep -qF '"${SWALLOW_VERSION:-@SWALLOW_VERSION@}"' deploy/release/install.sh ||
  { printf 'install.sh must default its version to @SWALLOW_VERSION@, which assemble-release.sh fills in\n' >&2; exit 1; }
if ! awk 'NF && $0 !~ /^[A-Za-z0-9_.-]+==[A-Za-z0-9_.+-]+$/ { exit 1 }' \
  api-server/automation/requirements.txt; then
  printf 'every Python dependency must be exactly pinned\n' >&2
  exit 1
fi
while IFS= read -r -d '' script; do
  bash -n "${script}"
done < <(find deploy -type f \( -name '*.sh' -o -name 'swallowctl' \) -print0)
