#!/usr/bin/env bash
# Seed the dev stack for the Prometheus monitoring demo.
#
# Idempotently, against the running dev api-server:
#   - creates the Deployment Key (the dev stack has no swallowctl install step, so this
#     post-install seed runs `swallow-api deployment-key ensure` inside the api container),
#   - registers a site,
#   - (optional) a MAAS provisioner integration, so lab machines reconcile into servers, with
#     the rack's virsh SSH public key as its virshSshPublicKey setting when one is given,
#   - site automation settings (automation always logs in with the Deployment Key) and an
#     optional become password, so host automation such as the Node Exporter install that each
#     OS deployment runs can work,
#   - the in-compose Prometheus as the Site's metrics integration, with the in-compose
#     prom-label-proxy as its labelProxyUrl setting, the way bootstrap.sh registers a production
#     installation's Site Prometheus, so the dashboard can read metrics,
#   - (optional) the in-compose Pulp as the Site's repository integration, with PULP_ORIGIN as its
#     contentOrigin setting, and the Local Repository's default `ubuntu` source, the way
#     bootstrap.sh registers a production installation's Local Repository (decision 078), except
#     that the spec is in mode cache: the dev Site fetches what the Local Repository serves from it
#     and the rest from the upstreams, so it deploys before the first synchronization completes,
#     where a production Site starts in mode upstream.
#
# Everything site- or environment-specific (MAAS URL/key, become password, known_hosts) comes from
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

# --- Deployment Key (installation step; the API itself never creates it) ---
# The dev container builds the binary with air at /tmp/air/swallow-api and already carries the
# Mongo URI and credential key in its environment. Idempotent: an existing key is kept.
docker compose --project-directory "${script_dir}" -f "${script_dir}/compose.yaml" \
  exec -T api-server /tmp/air/swallow-api deployment-key ensure
admin_user="${SWALLOW_API_BOOTSTRAP_ADMIN_USERNAME:-admin}"
admin_pass="${SWALLOW_API_BOOTSTRAP_ADMIN_PASSWORD:-admin}"

site_name="${SWALLOW_SEED_SITE_NAME:-dc-east}"
prometheus_url="${SWALLOW_PROMETHEUS_URL:-http://prometheus:9090}"
label_proxy_url="${SWALLOW_LABEL_PROXY_URL:-http://prom-label-proxy:8080}"
grafana_url="${SWALLOW_GRAFANA_URL:-}"

ssh_user="${SWALLOW_SSH_USER:-ubuntu}"
ssh_port="${SWALLOW_SSH_PORT:-22}"
ssh_known_hosts="${SWALLOW_SSH_KNOWN_HOSTS:-}"
become_password="${SWALLOW_BECOME_PASSWORD:-}"

# The in-compose Pulp (compose.yaml): the api-server calls it through pulp-web on the Compose
# network, and deployed Servers read content at PULP_ORIGIN, which is also Pulp's CONTENT_ORIGIN.
pulp_endpoint="http://pulp-web:8080"
pulp_origin="${PULP_ORIGIN:-}"
pulp_admin_password="${PULP_ADMIN_PASSWORD:-admin}"

maas_url="${SWALLOW_MAAS_URL:-}"
maas_credential="${SWALLOW_MAAS_CREDENTIAL:-}"
# The public key the MAAS rack's virsh power driver connects to hypervisors with (decision 055), as
# a key line or a file holding one; for a local MAAS snap the key is under
# /var/snap/maas/current/root/.ssh (readable by root only, so copy the .pub somewhere first).
maas_virsh_key="${SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY:-}"
if [[ -z "${maas_virsh_key}" && -n "${SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY_FILE:-}" ]]; then
  maas_virsh_key="$(sed -n '1p' "${SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY_FILE}")"
fi

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

# --- metrics integration -> in-compose Prometheus ---
# A Site has at most one metrics integration, so an existing one is never duplicated: it keeps its
# id, name, and other settings, and only its endpoint, labelProxyUrl, and grafanaUrl (when
# SWALLOW_GRAFANA_URL is set) are updated when they differ. An update replaces the whole settings
# map, so those settings are merged into the current ones.
metrics="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}&kind=metrics" |
  jq -c 'first((.items? // .)[]) // empty')"
if [[ -z "${metrics}" ]]; then
  settings="$(jq -nc --arg l "${label_proxy_url}" --arg g "${grafana_url}" \
    '{labelProxyUrl:$l} + (if $g != "" then {grafanaUrl:$g} else {} end)')"
  curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/integrations" \
    -H 'Content-Type: application/json' \
    --data-binary "$(jq -nc \
      --arg site "${site_id}" --arg endpoint "${prometheus_url}" --argjson settings "${settings}" \
      '{siteId:$site,kind:"metrics",providerKind:"prometheus",name:"site-prometheus",endpoint:$endpoint,settings:$settings}')" \
    >/dev/null
  printf 'registered metrics integration -> %s\n' "${prometheus_url}"
else
  metrics_id="$(jq -r '.id' <<<"${metrics}")"
  patch="$(jq -c --arg endpoint "${prometheus_url}" --arg l "${label_proxy_url}" --arg g "${grafana_url}" '
    ((.settings // {}) + {labelProxyUrl: $l} + (if $g != "" then {grafanaUrl: $g} else {} end)) as $settings |
    (if .endpoint != $endpoint then {endpoint: $endpoint} else {} end) +
    (if $settings != (.settings // {}) then {settings: $settings} else {} end)' <<<"${metrics}")"
  if [[ "${patch}" == '{}' ]]; then
    printf 'metrics integration already present (%s)\n' "${metrics_id}"
  else
    curl "${curl_flags[@]}" "${auth[@]}" -X PATCH "${base_url}/integrations/${metrics_id}" \
      -H 'Content-Type: application/json' --data-binary "${patch}" >/dev/null
    printf 'updated metrics integration (%s) -> %s\n' "${metrics_id}" "${prometheus_url}"
  fi
fi

# --- repository integration -> in-compose Pulp (optional) ---
# A Site has at most one repository integration: an existing one keeps its id, name, and other
# settings, and only its endpoint and contentOrigin are repointed when they differ. Its write-only
# credential is replaced on every run. The first spec, the default `ubuntu` source in mode cache,
# is written only while the Local Repository spec has never been written (generation 0), so later
# edits survive a rerun, a switch of the mode included.
if [[ -n "${pulp_origin}" ]]; then
  repository="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}&kind=repository" |
    jq -c 'first((.items? // .)[]) // empty')"
  if [[ -z "${repository}" ]]; then
    repository_id="$(curl "${curl_flags[@]}" "${auth[@]}" -X POST "${base_url}/integrations" \
      -H 'Content-Type: application/json' \
      --data-binary "$(jq -nc \
        --arg site "${site_id}" --arg endpoint "${pulp_endpoint}" --arg origin "${pulp_origin}" \
        --arg cred "${pulp_admin_password}" \
        '{siteId:$site,kind:"repository",providerKind:"pulp",name:"local-repository",endpoint:$endpoint,
          settings:{contentOrigin:$origin},credential:$cred}')" | jq -er '.id')"
    printf 'registered repository integration (%s) -> %s, content at %s\n' "${repository_id}" "${pulp_endpoint}" "${pulp_origin}"
  else
    repository_id="$(jq -r '.id' <<<"${repository}")"
    patch="$(jq -c --arg endpoint "${pulp_endpoint}" --arg origin "${pulp_origin}" '
      (if .endpoint != $endpoint then {endpoint: $endpoint} else {} end) +
      (if (.settings // {}).contentOrigin != $origin
        then {settings: ((.settings // {}) + {contentOrigin: $origin})} else {} end)' <<<"${repository}")"
    if [[ "${patch}" != '{}' ]]; then
      curl "${curl_flags[@]}" "${auth[@]}" -X PATCH "${base_url}/integrations/${repository_id}" \
        -H 'Content-Type: application/json' --data-binary "${patch}" >/dev/null
    fi
    curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/integrations/${repository_id}/credential" \
      -H 'Content-Type: application/json' \
      --data-binary "$(jq -nc --arg cred "${pulp_admin_password}" '{credential:$cred}')" >/dev/null
    printf 'repository integration present (%s) -> %s, content at %s\n' "${repository_id}" "${pulp_endpoint}" "${pulp_origin}"
  fi
  generation="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/sites/${site_id}/local-repository" |
    jq -r '.spec.generation // 0')"
  if [[ "${generation}" == 0 ]]; then
    curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/local-repository/spec" \
      -H 'Content-Type: application/json' --data-binary "$(jq -nc '{generation: 0, mode: "cache", sources: [{
        name: "ubuntu", format: "deb", url: "http://archive.ubuntu.com/ubuntu/",
        suites: ["noble", "noble-updates", "noble-security"], components: ["main", "universe"],
        architectures: ["amd64"]}]}')" >/dev/null
    printf "Local Repository: added the default source 'ubuntu' in cache mode; deployments fetch what it serves from it and the rest from the upstreams\n"
  fi
else
  printf 'skipping the Local Repository: set PULP_ORIGIN to the address Servers reach this host'"'"'s Pulp at\n'
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
  # An update replaces the whole settings map, so the key is merged into the current settings.
  if [[ -n "${maas_virsh_key}" ]]; then
    maas_id="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations?siteId=${site_id}" |
      jq -r 'first((.items? // .)[] | select(.kind == "provisioner") | .id)? // ""')"
    settings="$(curl "${curl_flags[@]}" "${auth[@]}" "${base_url}/integrations/${maas_id}" | jq -c '.settings // {}')"
    curl "${curl_flags[@]}" "${auth[@]}" -X PATCH "${base_url}/integrations/${maas_id}" \
      -H 'Content-Type: application/json' \
      --data-binary "$(jq -nc --argjson settings "${settings}" --arg key "${maas_virsh_key}" \
        '{settings: ($settings + {virshSshPublicKey: $key})}')" >/dev/null
    printf 'recorded the MAAS rack virsh public key on the provisioner\n'
  else
    printf 'no MAAS virsh key: set SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY(_FILE) to enroll libvirt virtual machines\n'
  fi
else
  printf 'skipping MAAS: set SWALLOW_MAAS_URL and SWALLOW_MAAS_CREDENTIAL to register it\n'
fi

# --- site automation settings ---
automation_enabled=false
if [[ -n "${ssh_known_hosts}" ]]; then
  automation_enabled=true
fi
curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation" \
  -H 'Content-Type: application/json' \
  --data-binary "$(jq -nc \
    --argjson enabled "${automation_enabled}" \
    --arg user "${ssh_user}" --argjson port "${ssh_port}" --arg kh "${ssh_known_hosts}" \
    '{enabled:$enabled,sshUser:$user,sshPort:$port,knownHosts:$kh,playbookMappings:{}}')" \
  >/dev/null
printf 'automation configured (enabled=%s)\n' "${automation_enabled}"

# --- site become password (optional) ---
# Automation always logs in with the Deployment Key created above, which the API registers in
# MAAS (decision 039); a Site stores only its become password.
if [[ -n "${become_password}" ]]; then
  curl "${curl_flags[@]}" "${auth[@]}" -X PUT "${base_url}/sites/${site_id}/automation/credential" \
    -H 'Content-Type: application/json' \
    --data-binary "$(jq -nc --arg become "${become_password}" '{becomePassword:$become}')" \
    >/dev/null
  printf 'site become password stored\n'
fi
printf 'automation logs in with the Deployment Key\n'

cat <<EOF

Done. Next:
  - Deploy an OS to a lab machine; the OS deployment installs the Node Exporter, which
    Prometheus then discovers in its swallow-exporters job.
  - Prometheus UI: http://127.0.0.1:${PROMETHEUS_PORT:-9090}
EOF
