#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
base_url="${SWALLOW_TEST_URL:-https://127.0.0.1:18443/api/v1}"

# Deployment Key: the installation step swallowctl runs in production. Idempotent. The project
# directory makes compose load ./.env locally; CI supplies the images and COMPOSE_PROJECT_NAME
# through the environment instead.
docker compose --project-directory "${script_dir}" -f "${script_dir}/compose.yaml" \
  run --rm --no-deps api-server deployment-key ensure
password="$(<"${script_dir}/secrets/bootstrap-admin-password")"
curl_flags=(--fail --silent --show-error --insecure)

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
# Only the become password is stored: automation uses the API-generated Deployment Key, which a
# site private key would override. The check proves the credential stays write-only and that the
# Deployment Key is the effective key.
curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation/credential" \
  -H 'Content-Type: application/json' \
  --data-binary '{"becomePassword":"testing-only-password"}' >/dev/null
curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites/${site_id}/automation" |
  jq -e '.hasCredential == true and .credentialSource == "deploymentKey" and (has("sshPrivateKey") | not) and (has("becomePassword") | not)' >/dev/null
curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/ssh-keys" |
  jq -e 'any(.[]; .purpose == "deployment" and (has("privateKey") | not))' >/dev/null

printf 'testing seed, write-only automation credential, and Deployment Key are present\n'
