#!/usr/bin/env bash
# Converge the swallow-side records of the single-VM production installation through the
# published api-server HTTP contracts (auth-login, sites-integrations, site-automation,
# ssh-keys, provisioning). It never touches swallow internals or MongoDB directly.
#
# Usage: bootstrap.sh apply | verify
#   apply   idempotent: find or create the Site, register (or re-key) the co-located MAAS as
#           its provisioner Integration, write Site automation defaults only when the Site has
#           none, then wait for the Integration to sync and for the official ubuntu/noble
#           amd64 OS Image to appear complete in the catalog
#   verify  read-only checks for `swallowctl doctor`
#
# Access Keys are deliberately not created: the operator uploads their own.
# Environment overrides: SWALLOW_HTTP_PORT, SWALLOW_SITE_NAME (default "default"),
# SWALLOW_MAAS_ENDPOINT (default SWALLOW_MAAS_URL, the address machines use to reach the
# co-located MAAS, which the API containers reach too; the Dashboard pre-fills a Boot ISO's rack
# address from it), SWALLOW_SSH_KNOWN_HOSTS, SWALLOW_BOOTSTRAP_TIMEOUT (seconds).
set -euo pipefail

command_name="${1:-}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "${script_dir}/env.sh"
load_installation_env "${script_dir}/.env"
secrets_dir="${script_dir}/secrets"
base_url="http://127.0.0.1:${SWALLOW_HTTP_PORT:-80}/api/v1"
site_name="${SWALLOW_SITE_NAME:-default}"
maas_endpoint="${SWALLOW_MAAS_ENDPOINT:-${SWALLOW_MAAS_URL:-http://host.docker.internal:5240/MAAS}}"
wait_timeout="${SWALLOW_BOOTSTRAP_TIMEOUT:-900}"
image_id="ubuntu/noble"
image_arch="amd64"
token=""

log() { printf '[bootstrap] %s\n' "$*"; }
die() { printf '[bootstrap] %s\n' "$*" >&2; exit 1; }

secret() {
  [[ -s "${secrets_dir}/$1" ]] || die "missing secret: secrets/$1"
  printf '%s' "$(<"${secrets_dir}/$1")"
}

# api performs one authenticated request and prints the body. It fails on any non-2xx status
# so callers never parse an error envelope as a resource.
api() {
  local method="$1" path="$2"
  shift 2
  curl --fail-with-body --silent --show-error --max-time 60 -X "${method}" \
    -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
    "${base_url}${path}" "$@"
}

# api_status performs one authenticated GET and prints only the HTTP status, writing the
# body to the given file, for reads where 404 is an expected answer.
api_status() {
  local path="$1" body_file="$2"
  curl --silent --show-error --max-time 60 -o "${body_file}" -w '%{http_code}' \
    -H "Authorization: Bearer ${token}" "${base_url}${path}"
}

login() {
  local password
  password="$(secret bootstrap-admin-password)"
  token="$(curl --fail-with-body --silent --show-error --max-time 30 -X POST "${base_url}/auth/login" \
    -H 'Content-Type: application/json' \
    --data-binary @- <<<"$(jq -nc --arg password "${password}" '{username:"admin",password:$password}')" |
    jq -er '.accessToken')" || die "cannot log in as admin at ${base_url}"
}

find_site_id() {
  api GET /sites | jq -r --arg name "${site_name}" 'first((.items? // .)[] | select(.name == $name) | .id) // empty'
}

find_maas_integration_id() {
  local site_id="$1"
  api GET "/integrations?siteId=${site_id}&kind=provisioner" |
    jq -r 'first((.items? // .)[] | select(.providerKind == "maas") | .id) // empty'
}

ensure_site() {
  local site_id
  site_id="$(find_site_id)"
  if [[ -z "${site_id}" ]]; then
    site_id="$(api POST /sites --data-binary "$(jq -nc --arg name "${site_name}" \
      '{name:$name,description:"Created by swallowctl install"}')" | jq -er '.id')"
    log "created Site '${site_name}' (${site_id})" >&2
  fi
  printf '%s' "${site_id}"
}

# ensure_integration registers the co-located MAAS once. On a rerun it replaces only the
# write-only credential, so a re-initialized MAAS (new API key) is picked up without
# recreating the Integration and the Server identities mapped to it.
ensure_integration() {
  local site_id="$1" integration_id credential
  credential="$(secret maas-api-key)"
  integration_id="$(find_maas_integration_id "${site_id}")"
  if [[ -z "${integration_id}" ]]; then
    integration_id="$(api POST /integrations --data-binary "$(jq -nc \
      --arg site "${site_id}" --arg endpoint "${maas_endpoint}" --arg credential "${credential}" \
      '{siteId:$site,kind:"provisioner",providerKind:"maas",name:"maas",endpoint:$endpoint,credential:$credential}')" |
      jq -er '.id')"
    log "registered the MAAS provisioner Integration (${integration_id}) -> ${maas_endpoint}" >&2
  else
    api PUT "/integrations/${integration_id}/credential" \
      --data-binary "$(jq -nc --arg credential "${credential}" '{credential:$credential}')" >/dev/null
    log "MAAS provisioner Integration already present (${integration_id}); credential refreshed" >&2
  fi
  printf '%s' "${integration_id}"
}

# ensure_automation writes defaults only for a Site with no configuration, so an operator's
# later edits survive every rerun. Automation is enabled (Ansible Tasks refuse to run
# otherwise) with no playbook mappings. Without operator-supplied known_hosts the static block
# is a comment: the executor scans each target's host key per run and merges this block.
# Automation always logs in with the Deployment Key (decision 041), so no Site credential is
# written; an operator adds a become password later only if their hosts need one.
ensure_automation() {
  local site_id="$1" status body known_hosts
  local scanned_note="# swallow scans each target's SSH host key at run time; add static entries for hosts it cannot scan"
  body="$(mktemp)"
  status="$(api_status "/sites/${site_id}/automation" "${body}")"
  rm -f -- "${body}"
  case "${status}" in
    200) log 'Site automation already configured; left unchanged'; return 0 ;;
    404) ;;
    *) die "reading Site automation returned HTTP ${status}" ;;
  esac
  known_hosts="${SWALLOW_SSH_KNOWN_HOSTS:-${scanned_note}}"
  api PUT "/sites/${site_id}/automation" --data-binary "$(jq -nc --arg knownHosts "${known_hosts}" \
    '{enabled:true,sshPort:22,knownHosts:$knownHosts,playbookMappings:{}}')" >/dev/null
  log 'Site automation enabled with the Deployment Key and no playbook mappings'
}

# automation_runnable mirrors the site-automation contract's rule for Ansible Tasks: automation
# enabled and the Deployment Key present (credentialSource=deploymentKey).
automation_runnable() {
  api GET "/sites/$1/automation" | jq -e '.enabled and .credentialSource == "deploymentKey"' >/dev/null
}

integration_synced() {
  api GET "/integrations/$1" | jq -e '.sync.lastSucceededAt != null and .sync.lastError == null' >/dev/null
}

# integration_last_error prints the provider error the Integration last reported, so a failed
# wait or check says why (for example an unreachable endpoint or a rejected API key).
integration_last_error() {
  api GET "/integrations/$1" 2>/dev/null | jq -r '.sync.lastError // "none reported yet"' 2>/dev/null ||
    printf 'unknown'
}

catalog_has_image() {
  api GET "/provisioning/images?integrationId=$1" | jq -e --arg id "${image_id}" --arg arch "${image_arch}" \
    'any(.[]; .id == $id and .architecture == $arch and ((.sizeBytes // 0) > 0))' >/dev/null
}

deployment_key_present() {
  api GET /ssh-keys | jq -e 'any((.items? // .)[]; .purpose == "deployment")' >/dev/null
}

wait_until() {
  local description="$1"
  shift
  local deadline=$((SECONDS + wait_timeout))
  until "$@" 2>/dev/null; do
    (( SECONDS < deadline )) || return 1
    sleep 10
  done
  log "${description}: ok"
}

apply() {
  local site_id integration_id
  login
  site_id="$(ensure_site)"
  integration_id="$(ensure_integration "${site_id}")"
  ensure_automation "${site_id}"
  deployment_key_present || die 'the Deployment Key is missing; run swallowctl install'
  wait_until 'MAAS Integration sync' integration_synced "${integration_id}" ||
    die "timed out after ${wait_timeout}s waiting for the MAAS Integration to sync (last error: $(integration_last_error "${integration_id}"))"
  wait_until "${image_id} ${image_arch} in the OS Image catalog" catalog_has_image "${integration_id}" ||
    die "timed out after ${wait_timeout}s waiting for a complete ${image_id} ${image_arch} in the OS Image catalog"
}

report() {
  local description="$1"
  shift
  if "$@" 2>/dev/null; then
    printf 'ok    %s\n' "${description}"
  else
    printf 'FAIL  %s\n' "${description}"
    return 1
  fi
}

verify() {
  local site_id integration_id failed=0
  login
  printf 'ok    admin login through the Dashboard proxy\n'
  report 'Deployment Key exists' deployment_key_present || failed=1
  site_id="$(find_site_id)"
  if [[ -z "${site_id}" ]]; then
    printf "FAIL  Site '%s' exists\n" "${site_name}"
    return 1
  fi
  printf "ok    Site '%s' exists\n" "${site_name}"
  report 'Site automation is enabled with an effective SSH key' automation_runnable "${site_id}" || failed=1
  integration_id="$(find_maas_integration_id "${site_id}")"
  if [[ -z "${integration_id}" ]]; then
    printf 'FAIL  MAAS provisioner Integration is registered\n'
    return 1
  fi
  if ! report 'MAAS provisioner Integration synced without error' integration_synced "${integration_id}"; then
    printf '      last error: %s\n' "$(integration_last_error "${integration_id}")"
    failed=1
  fi
  report "OS Image catalog lists a complete ${image_id} ${image_arch}" catalog_has_image "${integration_id}" || failed=1
  return "${failed}"
}

case "${command_name}" in
  apply) apply ;;
  verify) verify ;;
  *) printf 'usage: bootstrap.sh {apply|verify}\n' >&2; exit 2 ;;
esac
