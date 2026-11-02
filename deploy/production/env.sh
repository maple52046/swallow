# shellcheck shell=bash
# Sourced (never executed) by swallowctl, local-maas.sh, and bootstrap.sh so each reads the
# installation's .env the same way whether swallowctl runs it or a user reruns it. install.sh
# sources the copy in the bundle it downloaded, before extracting it, to compare release versions.

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

# semver_compare prints -1, 0, or 1 as release version A precedes, equals, or follows B under
# SemVer 2.0 precedence: a pre-release precedes its release (1.0.0-rc.2 < 1.0.0), pre-release
# identifiers compare numerically when both are numbers (rc.2 < rc.10) and a number precedes
# text, and build metadata is ignored. sort -V gets the first two wrong. It fails on a value
# that is not SemVer.
semver_compare() {
  jq -nr --arg a "$1" --arg b "$2" '
    def parse:
      (sub("\\+.*$"; "") | capture("^(?<core>[0-9]+\\.[0-9]+\\.[0-9]+)(-(?<pre>[0-9A-Za-z.-]+))?$"))
        // error("not a SemVer release version: \(.)")
      | {core: (.core | split(".") | map(tonumber)),
         pre: (if .pre then .pre | split(".") | map(if test("^[0-9]+$") then tonumber else . end)
               else null end)};
    def order(x; y): if x < y then -1 elif x > y then 1 else 0 end;
    def identifier(x; y):
      if (x | type) == (y | type) then order(x; y) elif (x | type) == "number" then -1 else 1 end;
    def prerelease(x; y):
      if x == null and y == null then 0 elif x == null then 1 elif y == null then -1
      else first((range([x, y] | map(length) | min) | identifier(x[.]; y[.]) | select(. != 0)),
        order(x | length; y | length))
      end;
    ($a | parse) as $x | ($b | parse) as $y
    | if $x.core != $y.core then order($x.core; $y.core) else prerelease($x.pre; $y.pre) end'
}
