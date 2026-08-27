#!/usr/bin/env bash
# Seed the dev stack for the Prometheus monitoring demo.
#
# Idempotently registers, against the running dev api-server:
#   - a site,
#   - (optional) a MAAS provisioner integration, so lab machines reconcile into servers,
#   - site automation settings + SSH credential, so exporter playbooks can run,
#   - the install/uninstall-exporters playbook mappings,
#   - a metrics integration pointing at the in-compose Prometheus, so the dashboard can
#     read metrics.
#
# Everything site- or environment-specific (MAAS URL/key, SSH key, known_hosts) comes from
# the environment or a .env file, so nothing secret is committed. Values left unset skip
# the step that needs them, with a printed note.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Load .env if present so a developer can keep MAAS/SSH values out of their shell history.
if [[ -f "${script_dir}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${script_dir}/.env"
  set +a
fi

base_url="${SWALLOW_DEV_URL:-http://127.0.0.1:30051/api/v1}"
admin_user="${SWALLOW_API_BOOTSTRAP_ADMIN_USERNAME:-admin}"
admin_pass="${SWALLOW_API_BOOTSTRAP_ADMIN_PASSWORD:-admin}"

site_name="${SWALLOW_SEED_SITE_NAME:-dc-east}"
prometheus_url="${SWALLOW_PROMETHEUS_URL:-http://prometheus:9090}"
grafana_url="${SWALLOW_GRAFANA_URL:-}"

ssh_user="${SWALLOW_SSH_USER:-ubuntu}"
ssh_port="${SWALLOW_SSH_PORT:-22}"
ssh_known_hosts="${SWALLOW_SSH_KNOWN_HOSTS:-}"
ssh_private_key="${SWALLOW_SSH_PRIVATE_KEY:-}"
become_password="${SWALLOW_BECOME_PASSWORD:-}"

maas_url="${SWALLOW_MAAS_URL:-}"
maas_credential="${SWALLOW_MAAS_CREDENTIAL:-}"

curl_flags=(--fail --silent --show-error)

token="$(curl "${curl_flags[@]}" -X POST "${base_url}/auth/login" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -nc --arg u "${admin_user}" --arg p "${admin_pass}" '{username:$u,password:$p}')" |
  jq -er '.accessToken')"
auth=(-H "Authorization: Bearer ${token}")

# --- site (find or create) ---
site_id="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites" |
  jq -r --arg n "${site_name}" 'first((.items? // .)[] | select(.name == $n) | .id)? // ""')"
if [[ -z "${site_id}" ]]; then
  site_id="$(curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/sites" \
    -H 'Content-Type: application/json' \
    --data-binary "$(jq -nc --arg n "${site_name}" '{name:$n}')" | jq -er '.id')"
fi
printf 'site %s (%s)\n' "${site_name}" "${site_id}"

# --- metrics integration -> in-compose Prometheus (find or create) ---
metrics_id="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}" |
  jq -r 'first((.items? // .)[] | select(.kind == "metrics") | .id)? // ""')"
if [[ -z "${metrics_id}" ]]; then
  settings='{}'
  if [[ -n "${grafana_url}" ]]; then
    settings="$(jq -nc --arg g "${grafana_url}" '{grafanaUrl:$g}')"
  fi
  curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/integrations" \
    -H 'Content-Type: application/json' \
    --data-binary "$(jq -nc \
      --arg site "${site_id}" --arg endpoint "${prometheus_url}" --argjson settings "${settings}" \
      '{siteId:$site,kind:"metrics",providerKind:"prometheus",name:"prometheus",endpoint:$endpoint,settings:$settings}')" \
    >/dev/null
  printf 'registered metrics integration -> %s\n' "${prometheus_url}"
else
  printf 'metrics integration already present (%s)\n' "${metrics_id}"
fi

# --- MAAS provisioner integration (optional) ---
if [[ -n "${maas_url}" && -n "${maas_credential}" ]]; then
  maas_id="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}" |
    jq -r 'first((.items? // .)[] | select(.kind == "provisioner") | .id)? // ""')"
  if [[ -z "${maas_id}" ]]; then
    curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/integrations" \
      -H 'Content-Type: application/json' \
      --data-binary "$(jq -nc \
        --arg site "${site_id}" --arg endpoint "${maas_url}" --arg cred "${maas_credential}" \
        '{siteId:$site,kind:"provisioner",providerKind:"maas",name:"maas",endpoint:$endpoint,credential:$cred}')" \
      >/dev/null
    printf 'registered MAAS provisioner -> %s\n' "${maas_url}"
  else
    printf 'MAAS provisioner already present (%s)\n' "${maas_id}"
  fi
else
  printf 'skipping MAAS: set SWALLOW_MAAS_URL and SWALLOW_MAAS_CREDENTIAL to register it\n'
fi

# --- site automation: settings + exporter playbook mappings ---
automation_enabled=false
if [[ -n "${ssh_known_hosts}" ]]; then
  automation_enabled=true
fi
curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -nc \
    --argjson enabled "${automation_enabled}" \
    --arg user "${ssh_user}" --argjson port "${ssh_port}" --arg kh "${ssh_known_hosts}" \
    '{enabled:$enabled,sshUser:$user,sshPort:$port,knownHosts:$kh,
      playbookMappings:{"install-exporters":"install-exporters","uninstall-exporters":"uninstall-exporters"}}')" \
  >/dev/null
printf 'automation configured (enabled=%s), exporter playbooks mapped\n' "${automation_enabled}"

# --- site automation credential (optional but required for exporter installs to run) ---
if [[ -n "${ssh_private_key}" ]]; then
  curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation/credential" \
    -H 'Content-Type: application/json' \
    --data-binary "$(jq -nc --arg key "${ssh_private_key}" --arg become "${become_password}" \
      '{sshPrivateKey:$key} + (if $become == "" then {} else {becomePassword:$become} end)')" \
    >/dev/null
  printf 'automation SSH credential stored\n'
else
  printf 'skipping SSH credential: set SWALLOW_SSH_PRIVATE_KEY to enable exporter installs\n'
fi

cat <<EOF

Done. Next:
  - Tag the AMD GPU server in MAAS with 'amd-gpu' (currently: tainan-ci.maas).
  - Deploy an OS to a lab machine; swallow auto-creates an install-exporters operation.
  - Prometheus UI: http://127.0.0.1:${PROMETHEUS_PORT:-9090}
EOF
