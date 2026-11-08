#!/usr/bin/env bash
# Check that every file artifact the modules declare answers at its declared URL (decision 079).
# publish.sh runs it before building anything: a release whose modules declare a file nobody has
# published yet would stop every Site that uses its Local Repository (the Local Repository cannot fetch
# the file and is never ready) and fail every Site that fetches from the upstreams. It asks each URL
# for its headers, following redirects, and asks for its first byte where a server refuses HEAD; it
# downloads no file and does not check sha256, which the Local Repository and the hosts do.
#
# Usage: deploy/release/check-declared-files.sh [MODULES_DIR]   (default: api-server/modules)
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
modules="${1:-${root}/api-server/modules}"

log() { printf '[declared-files] %s\n' "$*"; }
for tool in curl jq; do
  command -v "${tool}" >/dev/null 2>&1 || { log "missing prerequisite: ${tool}" >&2; exit 1; }
done

mapfile -d '' manifests < <(find "${modules}" -name module.json -print0 | sort -z)
((${#manifests[@]} > 0)) || { log "no module.json under ${modules}" >&2; exit 1; }

checked=0
missing=0
while IFS=$'\t' read -r manifest path url; do
  checked=$((checked + 1))
  if curl -fsSIL --retry 2 --max-time 60 -o /dev/null "${url}" ||
    curl -fsSL --retry 2 --max-time 60 -r 0-0 -o /dev/null "${url}"; then
    continue
  fi
  log "${manifest#"${root}/"} declares ${path} at ${url}, which does not answer" >&2
  missing=$((missing + 1))
done < <(jq -r '.artifacts.files[]? | [input_filename, .path, .url] | @tsv' "${manifests[@]}")

if ((missing > 0)); then
  log "${missing} of ${checked} declared files do not answer; publish them (or fix the declaration) first" >&2
  exit 1
fi
log "every declared file answers (${checked} checked)"
