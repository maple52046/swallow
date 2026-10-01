# shellcheck shell=bash
# Sourced (never executed) by swallowctl, local-maas.sh, and bootstrap.sh so each reads the
# installation's .env the same way whether swallowctl runs it or an operator reruns it.

# load_installation_env exports the .env values that the caller's environment does not
# already set, matching Docker Compose precedence (shell environment over .env). Values are
# taken literally: .env lines are KEY=VALUE without quotes.
load_installation_env() {
  local env_file="$1" line key
  [[ -f "${env_file}" ]] || return 0
  while IFS= read -r line || [[ -n "${line}" ]]; do
    [[ "${line}" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || continue
    key="${BASH_REMATCH[1]}"
    [[ -n "${!key+x}" ]] || export "${key}=${BASH_REMATCH[2]}"
  done <"${env_file}"
}
