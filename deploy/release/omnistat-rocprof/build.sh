#!/usr/bin/env bash
# Build Omnistat's ROCProfiler-SDK extension once per ROCm minor in matrix.json (decision 079) and
# check each asset before anything is published:
#   1. the builder image (builder.Dockerfile) builds it at glibc 2.28, imports it, and audits it;
#   2. a supported host image (check.Dockerfile, Ubuntu 24.04 with that ROCm minor) imports it with
#      the OS's own python3.12.
# Both run without network access. The output, in out/omnistat-rocprof/<omnistat>-r<revision>/, is one
# asset per ROCm minor, SHA256SUMS, and declarations.json: the file artifacts the Slurm module declares
# once upload.sh has published them (upload first, then declare). Building does not publish.
#
# Usage: deploy/release/omnistat-rocprof/build.sh [ROCM_MINOR...]   (default: every minor in matrix.json)
# Prerequisites: an x86_64 host with docker (Docker Engine, or nerdctl with BuildKit running), curl, jq,
# and sha256sum; the build pulls the pinned images and AMD's packages, and needs no GPU.
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd -- "${here}/../../.." && pwd)"
matrix="${here}/matrix.json"

log() { printf '[omnistat-rocprof] %s\n' "$*"; }
die() { printf '[omnistat-rocprof] %s\n' "$*" >&2; exit 1; }

[[ "$(uname -m)" == x86_64 ]] || die 'build on an x86_64 host: the builder images are linux/amd64'
for tool in docker curl jq sha256sum; do
  command -v "${tool}" >/dev/null 2>&1 || die "missing prerequisite: ${tool}"
done

field() { jq -er "$1" "${matrix}"; }
omnistat="$(field .omnistat.version)"
revision="$(field .revision)"
tag="omnistat-rocprof-${omnistat}-r${revision}"
out="${root}/out/omnistat-rocprof/${omnistat}-r${revision}"
mkdir -p "${out}"

if (($# > 0)); then
  minors=("$@")
else
  mapfile -t minors < <(jq -r '.rocm[].version' "${matrix}")
fi

source_archive="${out}/omnistat-${omnistat}.tar.gz"
if [[ ! -f "${source_archive}" ]] || ! echo "$(field .omnistat.sha256)  ${source_archive}" | sha256sum -c --status; then
  log "downloading the Omnistat ${omnistat} source"
  curl -fsSL --retry 3 -o "${source_archive}.part" "$(field .omnistat.url)"
  echo "$(field .omnistat.sha256)  ${source_archive}.part" | sha256sum -c --status ||
    die "the Omnistat source at $(field .omnistat.url) is not the sha256 matrix.json pins"
  mv "${source_archive}.part" "${source_archive}"
fi

for minor in "${minors[@]}"; do
  entry="$(jq -ec --arg minor "${minor}" '.rocm[] | select(.version == $minor)' "${matrix}")" ||
    die "matrix.json has no ROCm ${minor}"
  packages="$(jq -r .packages <<<"${entry}")"
  rocprofiler_sdk="$(jq -r .rocprofilerSdk <<<"${entry}")"
  asset="omnistat-rocprof-${omnistat}-rocm${minor}-$(field .builder.abi)-x86_64.tar.gz"
  common=(
    --build-arg "ROCM_VERSION=${minor}" --build-arg "ROCM_PACKAGES=${packages}"
    --build-arg "AMD_GPG_URL=$(field .amdPackages.signingKey.url)"
    --build-arg "AMD_GPG_FINGERPRINT=$(field .amdPackages.signingKey.fingerprint)"
  )

  log "building the builder for ROCm ${minor}"
  docker build -f "${here}/builder.Dockerfile" -t "swallow-omnistat-rocprof-builder:${omnistat}-rocm${minor}" \
    --build-arg "BUILDER_IMAGE=$(field .builder.image)" --build-arg "AMD_EL8_REPO=$(field .amdPackages.el8)" \
    --build-arg "BUILDER_PYTHON=$(field .builder.python)" "${common[@]}" "${here}"
  log "building ${asset}"
  docker run --rm --network none --user "$(id -u):$(id -g)" -v "${out}:/out" \
    -e "BUILDER_PYTHON=$(field .builder.python)" "swallow-omnistat-rocprof-builder:${omnistat}-rocm${minor}" \
    bash /opt/builder/build-extension.sh "/out/$(basename "${source_archive}")" "${omnistat}" "${minor}" \
    "${rocprofiler_sdk}" "$(field .builder.glibc)" "/out/${asset}"

  log "checking ${asset} on $(field .check.os) with ROCm ${minor}"
  docker build -f "${here}/check.Dockerfile" -t "swallow-omnistat-rocprof-check:${omnistat}-rocm${minor}" \
    --build-arg "CHECK_IMAGE=$(field .check.image)" --build-arg "AMD_UBUNTU_REPO=$(field .amdPackages.ubuntu2404)" \
    --build-arg "CHECK_PYTHON=$(field .check.python)" "${common[@]}" "${here}"
  docker run --rm --network none -v "${out}/${asset}:/asset.tar.gz:ro" \
    "swallow-omnistat-rocprof-check:${omnistat}-rocm${minor}" \
    sh /opt/check/check-asset.sh /asset.tar.gz "${minor}" "$(field .check.python)" ||
    die "${asset} does not import on $(field .check.os) with ROCm ${minor}"
done

log 'writing SHA256SUMS and declarations.json'
(
  cd "${out}"
  shopt -s nullglob
  assets=(omnistat-rocprof-"${omnistat}"-rocm*-x86_64.tar.gz)
  ((${#assets[@]} > 0)) || die "no asset in ${out}"
  sha256sum "${assets[@]}" > SHA256SUMS
  for file in "${assets[@]}"; do
    minor="${file#omnistat-rocprof-"${omnistat}"-rocm}"
    minor="${minor%%-*}"
    jq -n --arg path "omnistat-rocprof/${omnistat}-r${revision}/${file}" \
      --arg url "https://github.com/$(field .repository)/releases/download/${tag}/${file}" \
      --arg sha256 "$(sha256sum "${file}" | cut -d' ' -f1)" --arg rocm "${minor}" \
      --slurpfile audit <(tar -xzOf "${file}" ./audit.json) \
      '{path: $path, url: $url, sha256: $sha256, rocm: $rocm,
        rocprofilerSdk: $audit[0].rocprofilerSdk, builderGlibc: $audit[0].builderGlibc}'
  done | jq -s . > declarations.json
)
log "built ${tag} in ${out}; publish it with upload.sh, then declare declarations.json in the Slurm module"
