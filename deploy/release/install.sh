#!/usr/bin/env bash
# Install or upgrade swallow on one Ubuntu 24.04 amd64 host with one command:
#
#   curl -fsSL https://github.com/maple52046/swallow/releases/download/v<VERSION>/install.sh \
#     | sudo bash -s -- [--address ADDRESS] [--dir DIRECTORY] [--version VERSION]
#
# It downloads the release's Compose bundle, verifies it against the release's SHA256SUMS,
# extracts it into DIRECTORY, and hands over to production/swallowctl: `upgrade` when an
# earlier release finished installing there, `install` otherwise (a first install, a rerun, or
# resuming an install that stopped). Extracting replaces the shipped files and keeps .env,
# secrets/, backups/, and state/, which no bundle contains. Run with --help for the options.
set -euo pipefail

log() { printf '[install] %s\n' "$*"; }
die() { printf '[install] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
usage: install.sh [--address ADDRESS] [--dir DIRECTORY] [--version VERSION]
  --address  the IP address or host name that machines and BMCs use to reach this host
             (default: the address of the interface with the default route)
  --dir      the installation directory (default /opt/swallow)
  --version  the release to install (default: the release this script was published with)
Each option may also be set in the environment: SWALLOW_ADDRESS, SWALLOW_INSTALL_DIR,
SWALLOW_VERSION. SWALLOW_RELEASE_URL replaces the GitHub download location, for a mirror.
EOF
}

# The whole script runs inside main so that `curl ... | bash` has read all of it before any
# command can consume standard input; commands that might read it get /dev/null instead.
main() {
  local version="${SWALLOW_VERSION:-@SWALLOW_VERSION@}"
  local install_dir="${SWALLOW_INSTALL_DIR:-/opt/swallow}"
  local address="${SWALLOW_ADDRESS:-}"
  local release_url="${SWALLOW_RELEASE_URL:-https://github.com/maple52046/swallow/releases/download}"
  local os work bundle base production action installed
  local -a missing=()

  while (($# > 0)); do
    case "$1" in
      --address) address="${2:?--address needs an IP address or host name}"; shift 2 ;;
      --dir) install_dir="${2:?--dir needs a directory}"; shift 2 ;;
      --version) version="${2:?--version needs a release version}"; shift 2 ;;
      -h|--help) usage; return 0 ;;
      *) die "unknown option: $1 (see --help)" ;;
    esac
  done

  version="${version#v}"
  [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] ||
    die "pass --version: '${version}' is not a release version"
  [[ "${EUID}" -eq 0 ]] || die 'run as root, for example: curl -fsSL <install.sh URL> | sudo bash'
  [[ "$(uname -m)" == x86_64 ]] || die 'swallow needs an x86_64 (amd64) host'
  os="$( (. /etc/os-release && printf '%s %s|%s' "${ID:-}" "${VERSION_ID:-}" "${PRETTY_NAME:-}") 2>/dev/null || true)"
  [[ "${os%%|*}" == "ubuntu 24.04" ]] || die "swallow needs Ubuntu 24.04; this host is ${os#*|}"

  command -v curl >/dev/null 2>&1 || missing+=(curl)
  command -v zstd >/dev/null 2>&1 || missing+=(zstd)
  if ((${#missing[@]} > 0)); then
    log "installing ${missing[*]}"
    apt-get update -q </dev/null
    DEBIAN_FRONTEND=noninteractive apt-get install -y -q ca-certificates "${missing[@]}" </dev/null
  fi

  work="$(mktemp -d)"
  # shellcheck disable=SC2064 # expand now: the EXIT trap runs after main's locals are gone
  trap "rm -rf -- $(printf '%q' "${work}")" EXIT
  bundle="swallow-compose-${version}.tar.zst"
  base="${release_url}/v${version}"
  log "downloading swallow ${version}"
  curl -fsSL --retry 3 -o "${work}/${bundle}" "${base}/${bundle}" || die "cannot download ${base}/${bundle}"
  curl -fsSL --retry 3 -o "${work}/SHA256SUMS" "${base}/SHA256SUMS" || die "cannot download ${base}/SHA256SUMS"
  (cd "${work}" && awk -v name="${bundle}" '$2 == name' SHA256SUMS | sha256sum --check --strict --status) ||
    die "${bundle} does not match the release's SHA256SUMS"

  production="${install_dir}/production"
  installed="$(sed -n 's/^SWALLOW_RELEASE_VERSION=//p' "${production}/state/installed" 2>/dev/null || true)"
  if [[ -n "${installed}" && "${installed}" != "${version}" ]]; then
    action=upgrade
    log "upgrading ${installed} to ${version} in ${install_dir}"
  else
    action=install
    log "installing ${version} in ${install_dir}"
  fi
  install -d -m 0755 "${install_dir}"
  tar --zstd -xf "${work}/${bundle}" -C "${install_dir}" --no-same-owner

  [[ -z "${address}" ]] || export SWALLOW_ADDRESS="${address}"
  "${production}/swallowctl" "${action}" --release-manifest "${install_dir}/release-manifest.json" </dev/null
}

main "$@"
