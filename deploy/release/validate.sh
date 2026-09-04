#!/usr/bin/env bash
set -euo pipefail

if rg -n 'jobTemplateName|providerKind: awx|/webhooks/automation' \
  api-server/docs/development/api-contracts/api-server docs/development/glossaries/terms; then
  printf 'active contracts or glossary still expose AWX-specific API fields\n' >&2
  exit 1
fi
jq -e '.schemaVersion == 1 and (.playbooks | length > 0)' api-server/automation/manifest.json >/dev/null
jq -e '.schemaVersions.mongo == 3 and .upgrade.from == [2] and .upgrade.to == 3 and .upgrade.downgradeSupported == false and
  (.images.temporalPostgres | type == "string") and (.images.temporalServer | type == "string") and (.images.temporalUI | type == "string")' \
  deploy/release/release-manifest.example.json >/dev/null
if ! awk 'NF && $0 !~ /^[A-Za-z0-9_.-]+==[A-Za-z0-9_.+-]+$/ { exit 1 }' \
  api-server/automation/requirements.txt; then
  printf 'every Python dependency must be exactly pinned\n' >&2
  exit 1
fi
while IFS= read -r -d '' script; do
  bash -n "${script}"
done < <(find deploy -type f \( -name '*.sh' -o -name 'swallowctl' \) -print0)
