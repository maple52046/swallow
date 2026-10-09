#!/usr/bin/env bash
# Install and converge the MAAS 3.6 region+rack that the single-VM production installation
# co-locates with swallow (decision 040). MAAS keeps its state in the Compose PostgreSQL
# instance under its own role and database (never maas-test-db); swallow later reaches it as a
# provisioner Integration. Only the official images.maas.io `ubuntu/noble` amd64 image is
# ensured: third-party images are out of scope for the installation.
#
# Usage: local-maas.sh install | ensure-image | check | stop | purge
#   install       create the MAAS role and database, install and initialize the snap, create
#                 the MAAS admin, store its API key in secrets/maas-api-key, and give the rack
#                 the SSH identity its virsh power driver uses (secrets/maas-virsh-ssh.pub)
#   ensure-image  select ubuntu/noble amd64 on the official source and wait until complete
#   ensure-virsh-identity
#                 only the virsh SSH identity step of install (swallowctl upgrade runs it)
#   check         read-only: MAAS API reachable and ubuntu/noble amd64 complete
#   stop | purge  stop the MAAS services, or remove the snap and the local MAAS marker
#
# Every command is idempotent. install and ensure-image need root; the Compose stack's
# PostgreSQL must be running. Environment overrides: SWALLOW_MAAS_URL (the URL machines use
# to reach MAAS; default http://<primary IPv4>:5240/MAAS), SWALLOW_MAAS_CHANNEL,
# SWALLOW_MAAS_IMAGE_TIMEOUT (seconds), SWALLOW_POSTGRES_HOST_PORT.
set -euo pipefail

command_name="${1:-}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "${script_dir}/env.sh"
load_installation_env "${script_dir}/.env"
compose=(docker compose --project-directory "${script_dir}" -f "${script_dir}/compose.yaml")
secrets_dir="${script_dir}/secrets"
marker="${script_dir}/state/local-maas"
maas_channel="${SWALLOW_MAAS_CHANNEL:-3.6/stable}"
maas_profile="swallow-installer"
maas_local_url="http://127.0.0.1:5240/MAAS"
postgres_port="${SWALLOW_POSTGRES_HOST_PORT:-5432}"
image_timeout="${SWALLOW_MAAS_IMAGE_TIMEOUT:-3600}"
image_name="ubuntu/noble"
image_arch="amd64"
# The MAAS snap runs its rack controller as root with HOME under the snap's data, so this is where
# its ssh (and thus the virsh power driver's qemu+ssh) finds its keys and configuration.
snap_ssh_dir="/var/snap/maas/current/root/.ssh"

log() { printf '[local-maas] %s\n' "$*"; }
die() { printf '[local-maas] %s\n' "$*" >&2; exit 1; }

require_root() {
  [[ "${EUID}" -eq 0 ]] || die 'this command must run as root'
}

secret() {
  [[ -s "${secrets_dir}/$1" ]] || die "missing secret: secrets/$1 (run prepare-secrets.sh)"
  printf '%s' "$(<"${secrets_dir}/$1")"
}

# resolve_maas_url keeps the URL chosen at first install so a rerun never re-points MAAS at a
# different address; machines PXE-boot against it.
resolve_maas_url() {
  local address
  if [[ -n "${SWALLOW_MAAS_URL:-}" ]]; then
    printf '%s' "${SWALLOW_MAAS_URL}"
    return
  fi
  if [[ -s "${marker}" ]] && grep -q '^MAAS_URL=' "${marker}"; then
    sed -n 's/^MAAS_URL=//p' "${marker}"
    return
  fi
  address="$(ip -4 route get 1.1.1.1 2>/dev/null |
    awk '{for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit }}')"
  [[ -n "${address}" ]] || die 'cannot detect the primary IPv4 address; set SWALLOW_MAAS_URL'
  printf 'http://%s:5240/MAAS' "${address}"
}

# ensure_database creates the MAAS role and database inside the shared PostgreSQL instance.
# The password is hex (prepare-secrets.sh), so embedding it in the SQL literal is safe; it is
# sent on stdin rather than on a command line.
ensure_database() {
  local attempt version password
  for attempt in $(seq 1 60); do
    "${compose[@]}" exec -T postgres pg_isready -U temporal >/dev/null 2>&1 && break
    [[ "${attempt}" != 60 ]] || die 'the Compose PostgreSQL did not become ready'
    sleep 2
  done
  version="$("${compose[@]}" exec -T postgres psql -U temporal -d postgres -Atc 'SHOW server_version_num')"
  (( version >= 140000 )) ||
    die "MAAS 3.6 needs PostgreSQL 14 or newer; the PostgreSQL image reports ${version}"
  password="$(secret maas-db-password)"
  [[ "${password}" =~ ^[0-9a-f]+$ ]] || die 'secrets/maas-db-password must be hex'
  "${compose[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -q -U temporal -d postgres >/dev/null <<SQL
SELECT 'CREATE ROLE maas LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'maas')\gexec
ALTER ROLE maas WITH LOGIN PASSWORD '${password}';
SELECT 'CREATE DATABASE maasdb OWNER maas' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'maasdb')\gexec
SQL
  log "PostgreSQL role 'maas' and database 'maasdb' are ready"
}

# ensure_snap refuses a MAAS this installation did not create: swallowctl manages only the
# co-located MAAS it installs, and must never re-initialize someone else's region.
ensure_snap() {
  local url="$1"
  if snap list maas >/dev/null 2>&1; then
    [[ -f "${marker}" ]] ||
      die 'a MAAS snap is already installed but was not installed by swallowctl; use a clean host'
  else
    log "installing the MAAS snap (${maas_channel})"
    snap install maas --channel="${maas_channel}"
  fi
  install -d -m 0700 "$(dirname -- "${marker}")"
  printf 'MAAS_URL=%s\nMAAS_CHANNEL=%s\n' "${url}" "${maas_channel}" >"${marker}"
}

current_mode() {
  local mode
  mode="$(cat /var/snap/maas/common/snap_mode 2>/dev/null || true)"
  printf '%s' "${mode:-none}"
}

ensure_init() {
  local url="$1" mode password
  mode="$(current_mode)"
  [[ "${mode}" != "region+rack" ]] || return 0
  [[ "${mode}" == "none" ]] || die "MAAS is initialized as '${mode}'; swallow expects region+rack"
  password="$(secret maas-db-password)"
  log "initializing MAAS region+rack, reachable by machines at ${url}"
  maas init region+rack \
    --database-uri "postgres://maas:${password}@127.0.0.1:${postgres_port}/maasdb" \
    --maas-url "${url}" </dev/null
}

wait_for_api() {
  for _ in $(seq 1 90); do
    curl -fsS --max-time 5 "${maas_local_url}/api/2.0/version/" >/dev/null 2>&1 && return 0
    sleep 5
  done
  die 'the MAAS API did not answer within 7.5 minutes'
}

# ensure_admin creates the MAAS admin non-interactively (username, password, and email all
# given, so MAAS does not prompt for an SSH key import) and stores its API key for swallow.
ensure_admin() {
  local key
  if ! maas apikey --username admin >/dev/null 2>&1; then
    log 'creating the MAAS admin account'
    maas createadmin --username admin --password "$(secret maas-admin-password)" \
      --email admin@swallow.invalid </dev/null
  fi
  key="$(maas apikey --username admin | sed -n '1p')"
  [[ -n "${key}" ]] || key="$(maas apikey --username admin --generate | sed -n '1p')"
  [[ "${key}" =~ ^[A-Za-z0-9]+:[A-Za-z0-9]+:[A-Za-z0-9]+$ ]] || die 'unexpected MAAS API key format'
  (umask 077 && printf '%s\n' "${key}" >"${secrets_dir}/maas-api-key")
  chmod 0444 "${secrets_dir}/maas-api-key"
  log 'MAAS admin API key stored in secrets/maas-api-key'
}

# ensure_virsh_identity gives the rack the SSH identity its virsh power driver connects to
# hypervisors with (decision 055): a passphrase-less key in the snap's root home (an existing
# ed25519 or RSA key is kept), and accept-new host key checking as the default for every host, so
# the first connection to a hypervisor records its key and a changed key is refused. More specific
# Host blocks an operator adds earlier in the file still win. The public key is copied to
# secrets/maas-virsh-ssh.pub, which bootstrap.sh records as the Integration's virshSshPublicKey;
# swallow authorizes it on a hypervisor when it enrolls that hypervisor's virtual machines.
ensure_virsh_identity() {
  local key public config="${snap_ssh_dir}/config"
  install -d -m 0700 "${snap_ssh_dir}"
  for key in id_ed25519 id_rsa; do
    [[ -s "${snap_ssh_dir}/${key}" && -s "${snap_ssh_dir}/${key}.pub" ]] && break
    key=""
  done
  if [[ -z "${key}" ]]; then
    key=id_ed25519
    ssh-keygen -q -t ed25519 -N '' -C 'maas-virsh@swallow' -f "${snap_ssh_dir}/${key}"
    log 'generated the MAAS rack SSH key for virsh power'
  fi
  touch "${config}"
  chmod 0600 "${config}"
  if ! grep -q '^# BEGIN swallow virsh$' "${config}"; then
    printf '\n# BEGIN swallow virsh\nHost *\n  StrictHostKeyChecking accept-new\n# END swallow virsh\n' >>"${config}"
    log 'MAAS rack SSH accepts a new hypervisor host key on first connection'
  fi
  public="$(sed -n '1p' "${snap_ssh_dir}/${key}.pub")"
  [[ "${public}" =~ ^ssh-[a-z0-9-]+\ [A-Za-z0-9+/=]+ ]] || die "unexpected public key in ${snap_ssh_dir}/${key}.pub"
  (umask 022 && printf '%s\n' "${public}" >"${secrets_dir}/maas-virsh-ssh.pub")
  chmod 0444 "${secrets_dir}/maas-virsh-ssh.pub"
  log 'MAAS rack virsh public key stored in secrets/maas-virsh-ssh.pub'
}

maas_login() {
  maas login "${maas_profile}" "${maas_local_url}/" "$(secret maas-api-key)" >/dev/null
}

maas_cli() {
  maas "${maas_profile}" "$@"
}

# noble_complete reports whether a synced ubuntu/noble amd64 boot resource has a complete
# resource set: the same signal swallow's catalog uses to report a deployable size.
noble_complete() {
  local id
  for id in $(maas_cli boot-resources read | jq -r --arg name "${image_name}" --arg arch "${image_arch}/" \
    '.[] | select(.name == $name and .type == "Synced" and (.architecture | startswith($arch))) | .id'); do
    maas_cli boot-resource read "${id}" | jq -e '[.sets[]? | .complete] | any' >/dev/null && return 0
  done
  return 1
}

ensure_selection() {
  local source_id selection selection_id arch
  local -a arches=() args=()
  source_id="$(maas_cli boot-sources read |
    jq -r 'first(.[] | select(.url | test("images\\.maas\\.io")) | .id) // empty')"
  [[ -n "${source_id}" ]] ||
    die 'MAAS has no official images.maas.io boot source; restore the default source first'
  selection="$(maas_cli boot-source-selections read "${source_id}" |
    jq -c 'first(.[] | select(.os == "ubuntu" and .release == "noble")) // empty')"
  if [[ -z "${selection}" ]]; then
    log "selecting ${image_name} ${image_arch} from images.maas.io"
    maas_cli boot-source-selections create "${source_id}" os=ubuntu release=noble \
      arches="${image_arch}" subarches='*' labels='*' >/dev/null
  elif ! jq -e --arg arch "${image_arch}" '.arches | (index($arch) or index("*"))' <<<"${selection}" >/dev/null; then
    selection_id="$(jq -r '.id' <<<"${selection}")"
    mapfile -t arches < <(jq -r '.arches[]' <<<"${selection}")
    for arch in "${arches[@]}" "${image_arch}"; do
      args+=("arches=${arch}")
    done
    log "adding ${image_arch} to the existing ${image_name} selection"
    maas_cli boot-source-selection update "${source_id}" "${selection_id}" "${args[@]}" >/dev/null
  fi
}

ensure_image() {
  local deadline importing
  maas_login
  ensure_selection
  if noble_complete; then
    log "${image_name} ${image_arch} is complete"
    return 0
  fi
  log 'importing boot resources from images.maas.io (this downloads several hundred MB)'
  maas_cli boot-resources import >/dev/null
  deadline=$((SECONDS + image_timeout))
  until noble_complete; do
    (( SECONDS < deadline )) ||
      die "${image_name} ${image_arch} was not complete after ${image_timeout}s; check outbound access to images.maas.io"
    sleep 30
    importing="$(maas_cli boot-resources is-importing 2>/dev/null || printf 'unknown')"
    # An import that started at init (before the selection existed) can finish without noble;
    # starting another one is idempotent.
    if [[ "${importing}" == "false" ]]; then
      maas_cli boot-resources import >/dev/null
    fi
    log "waiting for ${image_name} ${image_arch} (importing: ${importing})"
  done
  log "${image_name} ${image_arch} is complete"
}

check() {
  local failed=0
  if curl -fsS --max-time 5 "${maas_local_url}/api/2.0/version/" >/dev/null 2>&1; then
    printf 'ok    MAAS API answers at %s\n' "${maas_local_url}"
  else
    printf 'FAIL  MAAS API does not answer at %s\n' "${maas_local_url}"
    return 1
  fi
  if maas_login && noble_complete; then
    printf 'ok    MAAS has a complete %s %s boot resource\n' "${image_name}" "${image_arch}"
  else
    printf 'FAIL  MAAS has no complete %s %s boot resource\n' "${image_name}" "${image_arch}"
    failed=1
  fi
  if [[ -s "${secrets_dir}/maas-virsh-ssh.pub" ]]; then
    printf 'ok    MAAS rack has an SSH identity for virsh power\n'
  else
    printf 'FAIL  MAAS rack has no SSH identity for virsh power (run swallowctl upgrade)\n'
    failed=1
  fi
  return "${failed}"
}

install_maas() {
  local url
  require_root
  url="$(resolve_maas_url)"
  ensure_database
  ensure_snap "${url}"
  ensure_init "${url}"
  wait_for_api
  ensure_admin
  ensure_virsh_identity
}

case "${command_name}" in
  install) install_maas ;;
  ensure-image) require_root; ensure_image ;;
  ensure-virsh-identity) require_root; ensure_virsh_identity ;;
  check) check ;;
  stop)
    require_root
    if snap list maas >/dev/null 2>&1; then snap stop maas; fi
    ;;
  purge)
    require_root
    if snap list maas >/dev/null 2>&1; then snap remove --purge maas; fi
    rm -f -- "${marker}" "${secrets_dir}/maas-api-key" "${secrets_dir}/maas-virsh-ssh.pub"
    ;;
  *) printf 'usage: local-maas.sh {install|ensure-image|ensure-virsh-identity|check|stop|purge}\n' >&2; exit 2 ;;
esac
