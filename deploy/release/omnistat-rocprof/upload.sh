#!/usr/bin/env bash
# Publish the rocprof extension assets build.sh made (decision 079) as their own GitHub Release,
# omnistat-rocprof-<omnistat>-r<revision>, at the commit that built them. It is a prerelease that is
# never marked latest, so swallow's own releases stay the repository's latest. A published revision is
# never replaced: an existing release refuses the upload, and a rebuild is a new revision in
# matrix.json. After the upload, declare declarations.json in the Slurm module (upload first, then
# declare). Publish from the release machine only.
#
# Usage: deploy/release/omnistat-rocprof/upload.sh
# Prerequisites: gh logged in with a token that can create releases, jq, and sha256sum.
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd -- "${here}/../../.." && pwd)"
matrix="${here}/matrix.json"

log() { printf '[omnistat-rocprof] %s\n' "$*"; }
die() { printf '[omnistat-rocprof] %s\n' "$*" >&2; exit 1; }

for tool in gh git jq sha256sum; do
  command -v "${tool}" >/dev/null 2>&1 || die "missing prerequisite: ${tool}"
done
[[ -z "$(git -C "${root}" status --porcelain -- deploy/release/omnistat-rocprof)" ]] ||
  die 'deploy/release/omnistat-rocprof has uncommitted changes; publish what is committed'

omnistat="$(jq -er .omnistat.version "${matrix}")"
revision="$(jq -er .revision "${matrix}")"
repository="$(jq -er .repository "${matrix}")"
tag="omnistat-rocprof-${omnistat}-r${revision}"
out="${root}/out/omnistat-rocprof/${omnistat}-r${revision}"

[[ -f "${out}/SHA256SUMS" && -f "${out}/declarations.json" ]] || die "no build in ${out}; run build.sh first"
(cd "${out}" && sha256sum -c --status SHA256SUMS) || die "${out} does not match its SHA256SUMS; rebuild it"
mapfile -t assets < <(cut -d' ' -f3 "${out}/SHA256SUMS")
expected="$(jq -r '.rocm | length' "${matrix}")"
((${#assets[@]} == expected)) ||
  die "${out} has ${#assets[@]} assets but matrix.json lists ${expected} ROCm minors; build every one"
if gh release view "${tag}" --repo "${repository}" >/dev/null 2>&1; then
  die "${tag} is already published; a revision is never replaced (raise revision in matrix.json)"
fi

notes="$(mktemp)"
trap 'rm -f "${notes}"' EXIT
{
  echo "Omnistat ${omnistat}'s ROCProfiler-SDK extension (rocprofiler_sdk_extension, cp312), built by"
  echo "swallow's release tooling at glibc $(jq -r .builder.glibc "${matrix}") (decision 079). One asset per ROCm minor:"
  echo
  echo '| ROCm | ROCProfiler-SDK | Status |'
  echo '| --- | --- | --- |'
  jq -r '.rocm[] | "| \(.version) | \(.rocprofilerSdk) | \(.status)\(if .verified != "" then ": " + .verified else "" end) |"' "${matrix}"
} > "${notes}"

log "publishing ${tag} to ${repository}"
gh release create "${tag}" --repo "${repository}" --target "$(git -C "${root}" rev-parse HEAD)" \
  --title "Omnistat ${omnistat} rocprof extension, revision ${revision}" --notes-file "${notes}" \
  --prerelease --latest=false \
  "${assets[@]/#/${out}/}" "${out}/SHA256SUMS"
log "published ${tag}; declare ${out}/declarations.json in the Slurm module next"
