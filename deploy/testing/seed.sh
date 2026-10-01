#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
base_url="${SWALLOW_TEST_URL:-http://127.0.0.1:${SWALLOW_HTTP_PORT:-18080}/api/v1}"

# Deployment Key: the installation step swallowctl runs in production. Idempotent. The project
# directory makes compose load ./.env; images and COMPOSE_PROJECT_NAME exported in the
# environment take precedence.
docker compose --project-directory "${script_dir}" -f "${script_dir}/compose.yaml" \
  run --rm --no-deps api-server deployment-key ensure
password="$(<"${script_dir}/secrets/bootstrap-admin-password")"
curl_flags=(--fail --silent --show-error)

token="$(curl "${curl_flags[@]}" -X POST "${base_url}/auth/login" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -nc --arg password "${password}" '{username:"admin",password:$password}')" |
  jq -er '.accessToken')"

auth=(-H "Authorization: Bearer ${token}")
site_id="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites" |
  jq -r 'first((.items? // .)[] | select(.name == "testing") | .id)? // ""')"
if [[ -z "${site_id}" ]]; then
  site_id="$(curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/sites" \
    -H 'Content-Type: application/json' --data-binary '{"name":"testing"}' |
    jq -er '.id')"
fi

curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation" \
  -H 'Content-Type: application/json' \
  --data-binary '{"enabled":true,"sshUser":"ubuntu","sshPort":22,"knownHosts":"testing.invalid ssh-ed25519 AAAATESTINGONLY","playbookMappings":{}}' >/dev/null
# A Site stores only its become password; automation always uses the Deployment Key (decision
# 041). The checks prove the credential stays write-only, that a Site private key is refused
# rather than ignored, and that the Deployment Key is the effective key.
curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation/credential" \
  -H 'Content-Type: application/json' \
  --data-binary '{"becomePassword":"testing-only-password"}' >/dev/null
refused="$(curl --silent --show-error -o /dev/null -w '%{http_code}' "${auth[@]}" \
  -X PUT "${base_url}/sites/${site_id}/automation/credential" \
  -H 'Content-Type: application/json' --data-binary '{"sshPrivateKey":"testing-only-key"}')"
[[ "${refused}" == "400" ]] ||
  { printf 'a Site private key must be refused with 400, got HTTP %s\n' "${refused}" >&2; exit 1; }
curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites/${site_id}/automation" |
  jq -e '.hasCredential == true and .credentialSource == "deploymentKey" and (has("sshPrivateKey") | not) and (has("becomePassword") | not)' >/dev/null
curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/ssh-keys" |
  jq -e 'any(.[]; .purpose == "deployment" and (has("privateKey") | not))' >/dev/null

printf 'testing seed, write-only automation credential, and Deployment Key are present\n'
