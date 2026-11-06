#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
base_url="${SWALLOW_TEST_URL:-http://127.0.0.1:${SWALLOW_HTTP_PORT:-18080}/api/v1}"
compose=(docker compose --project-directory "${script_dir}" -f "${script_dir}/compose.yaml")

# Deployment Key: the installation step swallowctl runs in production. Idempotent. The project
# directory makes compose load ./.env; images and COMPOSE_PROJECT_NAME exported in the
# environment take precedence.
"${compose[@]}" run --rm --no-deps api-server deployment-key ensure
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

# The stack's Pulp as the Site's repository Integration, as bootstrap.sh registers the installation's
# in production: the API calls it through pulp-web on the control network, and its contentOrigin is
# the address Pulp writes its URLs on (SWALLOW_PULP_ORIGIN in the production compose.yaml), read from
# the resolved Compose model so the two never differ. An existing Integration keeps its id; only its
# endpoint and contentOrigin are pointed here, and its write-only credential is replaced. Bodies
# that carry the credential go on stdin, not on the command line.
pulp_endpoint="http://pulp-web:8080"
pulp_origin="$("${compose[@]}" config --format json |
  jq -er '.services["pulp-api"].environment.SWALLOW_PULP_ORIGIN')"
pulp_credential="$(jq -nc --arg credential "$(<"${script_dir}/secrets/pulp-admin-password")" \
  '{credential:$credential}')"
integration="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}&kind=repository" |
  jq -c 'first((.items? // .)[]) // empty')"
if [[ -z "${integration}" ]]; then
  jq -c --arg site "${site_id}" --arg endpoint "${pulp_endpoint}" --arg origin "${pulp_origin}" \
    '{siteId:$site,kind:"repository",providerKind:"pulp",name:"local-repository",endpoint:$endpoint,
      settings:{contentOrigin:$origin}} + .' <<<"${pulp_credential}" |
    curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/integrations" \
      -H 'Content-Type: application/json' --data-binary @- >/dev/null
else
  integration_id="$(jq -r '.id' <<<"${integration}")"
  patch="$(jq -c --arg endpoint "${pulp_endpoint}" --arg origin "${pulp_origin}" '
    (if .endpoint != $endpoint then {endpoint: $endpoint} else {} end) +
    (if (.settings // {}).contentOrigin != $origin
      then {settings: ((.settings // {}) + {contentOrigin: $origin})} else {} end)' <<<"${integration}")"
  if [[ "${patch}" != '{}' ]]; then
    curl "${curl_flags[@]}" "${auth[@]}" -X PATCH "${base_url}/integrations/${integration_id}" \
      -H 'Content-Type: application/json' --data-binary "${patch}" >/dev/null
  fi
  curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/integrations/${integration_id}/credential" \
    -H 'Content-Type: application/json' --data-binary @- <<<"${pulp_credential}" >/dev/null
fi
curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}&kind=repository" |
  jq -e --arg endpoint "${pulp_endpoint}" --arg origin "${pulp_origin}" \
    'first((.items? // .)[]) | .providerKind == "pulp" and .endpoint == $endpoint and .settings.contentOrigin == $origin' >/dev/null

# The Local Repository's default source, the Ubuntu archive, written only while its spec has never
# been written (generation 0), as bootstrap.sh does, so later edits survive a rerun. A concurrent
# first write refuses this one (409), and the next run reads again.
spec="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites/${site_id}/local-repository" |
  jq -c '.spec // {}')"
if jq -e '(.generation // 0) == 0' <<<"${spec}" >/dev/null; then
  curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/local-repository/spec" \
    -H 'Content-Type: application/json' --data-binary "$(jq -nc '{generation: 0, sources: [{
      name: "ubuntu", format: "deb", url: "http://archive.ubuntu.com/ubuntu/",
      suites: ["noble", "noble-updates", "noble-security"], components: ["main", "universe"],
      architectures: ["amd64"]}]}')" >/dev/null
fi

printf 'testing seed, write-only automation credential, Deployment Key, and Local Repository are present\n'
