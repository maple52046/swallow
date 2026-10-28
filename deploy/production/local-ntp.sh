#!/usr/bin/env bash
# Manage the installation's Control-Plane NTP Server (decision 065): the one NTP service on this
# host, answering on UDP 123 at SWALLOW_ADDRESS, that every Server swallow provisions is pointed at.
# It is a host service, never a container. Installing it and choosing its upstream are separate:
#
#   install   depends on the provisioner. With the co-located MAAS (the production installation)
#             there is nothing to install: MAAS's own chronyd already owns UDP 123 and allows every
#             client, and a second chronyd could not bind the port and would fight it over the
#             clock. With any other provisioner it installs the distribution chrony and owns
#             /etc/chrony/chrony.conf: the upstream, the clients it allows, and `local stratum 10`,
#             so Servers keep agreeing with each other while every upstream is unreachable.
#   upstream  points the server at SWALLOW_NTP_UPSTREAM, or at the default (ntp.ubuntu.com, MAAS's
#             own default) when it is unset. With MAAS it writes MAAS's ntp_servers and keeps
#             ntp_external_only=false; with host chrony it rewrites the configuration. swallowctl
#             owns the value (`swallowctl ntp upstream`), so an upstream set in the MAAS UI is
#             replaced on the next run.
#   check     read-only: UDP 123 is served, the upstream is the configured one, and (information
#             only) whether the server is synchronized to it
#
# install and upstream are idempotent and need root. Environment: SWALLOW_NTP_UPSTREAM (IP
# addresses or host names separated by commas or spaces; an address becomes `server <ip> iburst`
# and a name `pool <name> iburst`, as MAAS renders them), SWALLOW_NTP_ALLOW (host chrony only:
# client subnets separated by commas or spaces; unset allows every client), SWALLOW_MAAS_API_KEY
# and SWALLOW_MAAS_ADMIN_USER (MAAS outside an installation: the API key to use, else
# secrets/maas-api-key, else the key of that MAAS user, default admin).
#
# The upstream must not take its time from this host, directly or through a gateway whose served
# time is computed from this host's clock: this host follows it, so the two would chase each other.
set -euo pipefail

command_name="${1:-}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "${script_dir}/env.sh"
load_installation_env "${script_dir}/.env"
secrets_dir="${script_dir}/secrets"
maas_profile="swallow-ntp"
maas_local_url="http://127.0.0.1:5240/MAAS"
# MAAS's chronyd answers chronyc on this socket only (bindcmdaddress in the snap's maas.conf).
maas_chronyc=/snap/maas/current/usr/bin/chronyc
maas_chrony_socket=/run/snap.maas/chrony/chronyd.sock
# MAAS renders ntp_servers into this file and restarts its chronyd when the file changes.
maas_chrony_conf=/var/snap/maas/current/etc/chrony/maas.conf
host_conf=/etc/chrony/chrony.conf
# The default upstream when SWALLOW_NTP_UPSTREAM is unset: MAAS's own default, so both kinds of
# server behave the same.
default_upstream="ntp.ubuntu.com"

log() { printf '[local-ntp] %s\n' "$*"; }
die() { printf '[local-ntp] %s\n' "$*" >&2; exit 1; }

require_root() {
  [[ "${EUID}" -eq 0 ]] || die 'this command must run as root'
}

# split_list prints a comma- or space-separated value one entry per line. read -a never applies
# pathname expansion, so a stray `*` stays a literal and is refused by the caller's pattern.
split_list() {
  local -a entries=()
  local entry
  IFS=$', \t\n' read -r -a entries <<<"${1:-}" || true
  for entry in "${entries[@]}"; do
    [[ -z "${entry}" ]] || printf '%s\n' "${entry}"
  done
}

# load_settings validates SWALLOW_NTP_UPSTREAM into `upstream` (the default when unset; then
# `upstream_source` is "default") and SWALLOW_NTP_ALLOW into `allowed` before anything acts on
# them. Only IP addresses, host names, and subnets pass, so neither value can inject a chrony
# directive or a MAAS CLI argument.
upstream=()
allowed=()
upstream_source="SWALLOW_NTP_UPSTREAM"
load_settings() {
  local entry
  while IFS= read -r entry; do
    [[ "${entry}" =~ ^[A-Za-z0-9]([A-Za-z0-9.:-]*[A-Za-z0-9])?$ ]] ||
      die "SWALLOW_NTP_UPSTREAM entry is not an IP address or host name: ${entry}"
    upstream+=("${entry}")
  done < <(split_list "${SWALLOW_NTP_UPSTREAM:-}")
  if ((${#upstream[@]} == 0)); then
    upstream=("${default_upstream}")
    upstream_source="default"
  fi
  while IFS= read -r entry; do
    [[ "${entry}" =~ ^[0-9A-Fa-f.:]+(/[0-9]{1,3})?$ ]] ||
      die "SWALLOW_NTP_ALLOW entry is not an address or subnet: ${entry}"
    allowed+=("${entry}")
  done < <(split_list "${SWALLOW_NTP_ALLOW:-}")
}

# desired_upstream prints the validated upstream as single-space-separated entries, the form
# written into MAAS and compared against it.
desired_upstream() {
  local IFS=' '
  printf '%s' "${upstream[*]}"
}

# normalized prints a server list as single-space-separated entries, the form compared and written.
normalized() {
  split_list "$1" | paste -sd ' ' -
}

is_ip_address() {
  [[ "$1" =~ ^[0-9.]+$ || "$1" == *:* ]]
}

# source_line renders one upstream the way MAAS does: an address is one server, a name a pool.
source_line() {
  if is_ip_address "$1"; then
    printf 'server %s iburst\n' "$1"
  else
    printf 'pool %s iburst\n' "$1"
  fi
}

# provisioner_is_maas reports whether the provisioner is a MAAS running on this host, whose chronyd
# is then the NTP server. The production installation always co-locates it.
provisioner_is_maas() {
  command -v snap >/dev/null 2>&1 && snap list maas >/dev/null 2>&1
}

udp_123_served() {
  [[ -n "$(ss -Hlun 'sport = :123' 2>/dev/null)" ]]
}

# --- MAAS as the provisioner -------------------------------------------------------------------

maas_api_key() {
  if [[ -n "${SWALLOW_MAAS_API_KEY:-}" ]]; then
    printf '%s' "${SWALLOW_MAAS_API_KEY}"
  elif [[ -s "${secrets_dir}/maas-api-key" ]]; then
    sed -n '1p' "${secrets_dir}/maas-api-key"
  else
    maas apikey --username "${SWALLOW_MAAS_ADMIN_USER:-admin}" 2>/dev/null | sed -n '1p'
  fi
}

maas_login() {
  local key
  key="$(maas_api_key)"
  [[ -n "${key}" ]] || die 'no MAAS API key: set SWALLOW_MAAS_API_KEY or run as root on the MAAS region host'
  maas login "${maas_profile}" "${maas_local_url}/" "${key}" >/dev/null
}

# maas_config prints one MAAS setting as plain text (a JSON string unquoted, a boolean as true/false).
maas_config() {
  maas "${maas_profile}" maas get-config name="$1" | jq -r 'if . == null then "" else tostring end'
}

apply_maas_upstream() {
  local desired current
  maas_login
  desired="$(desired_upstream)"
  current="$(maas_config ntp_servers)"
  if [[ "$(normalized "${current}")" != "${desired}" ]]; then
    maas "${maas_profile}" maas set-config name=ntp_servers value="${desired}" >/dev/null
    log "MAAS NTP upstream set to ${desired} (${upstream_source}; was ${current:-none})"
  else
    log "MAAS NTP upstream is already ${desired} (${upstream_source})"
  fi
  # ntp_external_only=true would send machines MAAS deploys straight to the upstream and leave this
  # host out of their time path; the design needs this host to serve them.
  if [[ "$(maas_config ntp_external_only)" != "false" ]]; then
    maas "${maas_profile}" maas set-config name=ntp_external_only value=false >/dev/null
    log 'MAAS serves NTP from this host again (ntp_external_only=false)'
  fi
  step_maas_clock_if_far
}

# step_maas_clock_if_far waits for MAAS's chronyd to select an upstream and, when the host clock is
# more than a second off it, has chronyd step the clock once. MAAS's chrony configuration has no
# makestep, so chronyd would otherwise slew at its maximum rate (+8.3%) for minutes. An upstream
# whose served time is derived from this host's own clock (a `chronyd -x` gateway running on this
# host, as root123-intel's first chronos gateway did) cannot follow a slew that fast, and the two
# then run away from real time together; one step keeps every later correction at the ppm level.
#
# MAAS applies a changed ntp_servers asynchronously (it rewrites maas.conf and restarts chronyd
# within about a minute), and until then the old chronyd may still report its previous upstream.
# So the wait is for MAAS's configuration to list every requested source and for chronyd to
# select one of them: the reference must be one of the requested addresses, or, when a host name
# was requested (whose address chronyd resolves), any non-local reference.
step_maas_clock_if_far() {
  local deadline=$((SECONDS + 180)) tracking reference address correction host names=false
  for host in "${upstream[@]}"; do
    is_ip_address "${host}" || names=true
  done
  while ((SECONDS < deadline)); do
    if maas_conf_lists_upstream && tracking="$("${maas_chronyc}" -h "${maas_chrony_socket}" -n tracking 2>/dev/null)"; then
      reference="$(sed -n 's/^Reference ID *: *\([0-9A-Fa-f]*\).*/\1/p' <<<"${tracking}")"
      address="$(sed -n 's/^Reference ID *: *[0-9A-Fa-f]* *(\(.*\))$/\1/p' <<<"${tracking}")"
      if [[ -n "${reference}" && "${reference}" != 00000000 && "${reference}" != 7F7F0101 ]] &&
        { [[ "${names}" == true ]] || printf '%s\n' "${upstream[@]}" | grep -qxF -- "${address}"; }; then
        correction="$(sed -n 's/^System time *: *\([0-9.]*\) seconds.*/\1/p' <<<"${tracking}")"
        if awk -v c="${correction:-0}" 'BEGIN { exit !(c > 1) }'; then
          "${maas_chronyc}" -h "${maas_chrony_socket}" makestep >/dev/null
          log "stepped the host clock by ${correction}s to its NTP upstream instead of slewing"
        fi
        return 0
      fi
    fi
    sleep 5
  done
  log 'MAAS chronyd has not selected its NTP upstream yet; rerun local-ntp.sh check later'
}

# maas_conf_lists_upstream reports whether MAAS has rewritten its chrony configuration with every
# requested source, rendered as MAAS renders them (see source_line).
maas_conf_lists_upstream() {
  local host
  [[ -r "${maas_chrony_conf}" ]] || return 1
  for host in "${upstream[@]}"; do
    grep -qxF -- "$(source_line "${host}")" "${maas_chrony_conf}" || return 1
  done
}

check_maas_ntp() {
  local failed=0 desired current
  if ! maas_login 2>/dev/null; then
    printf 'FAIL  MAAS NTP settings are readable (no usable MAAS API key)\n'
    return 1
  fi
  desired="$(desired_upstream)"
  current="$(maas_config ntp_servers)"
  if [[ "$(normalized "${current}")" == "${desired}" ]]; then
    printf 'ok    MAAS NTP upstream is %s (%s)\n' "${desired}" "${upstream_source}"
  else
    printf 'FAIL  MAAS NTP upstream is %s, not %s (%s; run local-ntp.sh upstream)\n' \
      "${current:-not set}" "${desired}" "${upstream_source}"
    failed=1
  fi
  if [[ "$(maas_config ntp_external_only)" == "false" ]]; then
    printf 'ok    MAAS hands this host to deployed machines as their NTP server\n'
  else
    printf 'FAIL  MAAS ntp_external_only is set, so deployed machines bypass this host\n'
    failed=1
  fi
  report_sync "${maas_chronyc}" -h "${maas_chrony_socket}"
  return "${failed}"
}

# --- host chrony (any provisioner other than MAAS) ---------------------------------------------

# render_host_config prints the swallow-owned chrony configuration from the validated settings
# (load_settings), so the file holds no operator-supplied directive text.
render_host_config() {
  local host subnet
  printf '# Managed by swallow local-ntp.sh (decision 065): the Control-Plane NTP Server.\n'
  printf '# local-ntp.sh upstream rewrites this file; use swallowctl ntp upstream and SWALLOW_NTP_ALLOW instead.\n'
  for host in "${upstream[@]}"; do
    source_line "${host}"
  done
  if ((${#allowed[@]} == 0)); then
    printf 'allow\n'
  else
    for subnet in "${allowed[@]}"; do
      printf 'allow %s\n' "${subnet}"
    done
  fi
  printf '# Keep serving the local clock while every upstream is unreachable, so Servers still agree.\n'
  printf 'local stratum 10\n'
  printf 'driftfile /var/lib/chrony/chrony.drift\n'
  printf 'makestep 1.0 3\n'
  printf 'rtcsync\n'
  printf 'logdir /var/log/chrony\n'
}

# write_host_config rewrites the chrony configuration and restarts chrony when it changed; the
# restart re-arms `makestep`, so a large first correction is a step rather than a long slew.
write_host_config() {
  local rendered
  rendered="$(mktemp)"
  render_host_config >"${rendered}"
  if cmp -s "${rendered}" "${host_conf}"; then
    rm -f -- "${rendered}"
    log "host chrony already follows $(desired_upstream) (${upstream_source})"
    return 0
  fi
  install -D -m 0644 "${rendered}" "${host_conf}"
  rm -f -- "${rendered}"
  systemctl restart chrony.service
  log "host chrony follows $(desired_upstream) (${upstream_source})"
}

install_host_chrony() {
  if ! dpkg-query -W -f='${Status}' chrony 2>/dev/null | grep -q 'install ok installed'; then
    log 'installing chrony'
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y chrony
  fi
  # systemd-timesyncd would discipline the same clock; chrony owns it on this host.
  if systemctl list-unit-files systemd-timesyncd.service >/dev/null 2>&1; then
    systemctl disable --now systemd-timesyncd.service >/dev/null 2>&1 || true
  fi
  systemctl enable chrony.service >/dev/null
  write_host_config
  systemctl start chrony.service
  log 'host chrony serves UDP 123'
}

check_host_chrony() {
  local failed=0 rendered
  if systemctl is-active --quiet chrony.service; then
    printf 'ok    host chrony is running\n'
  else
    printf 'FAIL  host chrony is not running (run local-ntp.sh install)\n'
    failed=1
  fi
  rendered="$(mktemp)"
  render_host_config >"${rendered}"
  if cmp -s "${rendered}" "${host_conf}"; then
    printf 'ok    host chrony follows %s (%s)\n' "$(desired_upstream)" "${upstream_source}"
  else
    printf 'FAIL  %s differs from the configured upstream and SWALLOW_NTP_ALLOW (run local-ntp.sh upstream)\n' "${host_conf}"
    failed=1
  fi
  rm -f -- "${rendered}"
  report_sync chronyc
  return "${failed}"
}

# report_sync prints whether the server follows an upstream. It is information, not a failure: a
# server on its local clock still gives every Server the same time.
report_sync() {
  local tracking reference
  if ! tracking="$("$@" -n tracking 2>/dev/null)"; then
    printf 'info  control-plane NTP synchronization is unknown (chronyc did not answer)\n'
    return 0
  fi
  reference="$(sed -n 's/^Reference ID *: *\([0-9A-Fa-f]*\).*/\1/p' <<<"${tracking}")"
  case "${reference}" in
    "" | 00000000) printf 'info  control-plane NTP server is not synchronized yet\n' ;;
    7F7F0101) printf 'info  control-plane NTP server serves its local clock: no upstream is reachable (check the upstream)\n' ;;
    *) printf 'ok    control-plane NTP server is synchronized to an upstream (%s)\n' \
      "$(sed -n 's/^Reference ID *: *[0-9A-Fa-f]* *(\(.*\))$/\1/p' <<<"${tracking}")" ;;
  esac
}

install_server() {
  require_root
  load_settings
  if provisioner_is_maas; then
    log 'the provisioner is the co-located MAAS: its chronyd is the NTP server, nothing to install'
  else
    install_host_chrony
  fi
}

apply_upstream() {
  require_root
  load_settings
  if provisioner_is_maas; then
    apply_maas_upstream
  else
    if [[ ! -f "${host_conf}" ]] || ! command -v chronyc >/dev/null 2>&1; then
      die 'host chrony is not installed; run local-ntp.sh install first'
    fi
    write_host_config
  fi
}

check() {
  local failed=0
  load_settings
  if udp_123_served; then
    printf 'ok    control-plane NTP server listens on UDP 123 (%s)\n' "$(provisioner_is_maas && printf 'MAAS chronyd' || printf 'host chrony')"
  else
    printf 'FAIL  nothing serves NTP on UDP 123 (run local-ntp.sh install)\n'
    failed=1
  fi
  if provisioner_is_maas; then
    check_maas_ntp || failed=1
  else
    check_host_chrony || failed=1
  fi
  return "${failed}"
}

case "${command_name}" in
  install) install_server ;;
  upstream) apply_upstream ;;
  check) check ;;
  *) printf 'usage: local-ntp.sh {install|upstream|check}\n' >&2; exit 2 ;;
esac
