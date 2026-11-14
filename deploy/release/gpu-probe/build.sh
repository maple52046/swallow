#!/usr/bin/env bash
# Build the GPU probe image the Slurm module declares (decision 083, decision 079): IMAGE below, from the
# ROCm development image pinned by ROCM_BASE, with the probe built for every Instinct gfx target.
# Building does not publish; upload.sh pushes it, and only then may the module declare a new tag
# (upload first, then declare).
#
# Usage: deploy/release/gpu-probe/build.sh
# Prerequisites: an x86_64 host with docker; the build pulls the ROCm image and needs no GPU.
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# The image and its base: change both with the Slurm module's artifacts.images and GPUProbeImage.
IMAGE="${IMAGE:-ghcr.io/maple52046/swallow-gpu-probe:1.0.0-rocm7.2.4}"
ROCM_BASE="${ROCM_BASE:-docker.io/rocm/dev-ubuntu-24.04:7.2.4}"

log() { printf '[gpu-probe] %s\n' "$*"; }
die() { printf '[gpu-probe] %s\n' "$*" >&2; exit 1; }

[[ "$(uname -m)" == x86_64 ]] || die 'build on an x86_64 host: the ROCm image is linux/amd64'
command -v docker >/dev/null 2>&1 || die 'missing prerequisite: docker'

log "building ${IMAGE} from ${ROCM_BASE}"
docker build --platform linux/amd64 --build-arg "ROCM_BASE=${ROCM_BASE}" -t "${IMAGE}" "${here}"
log "checking that the probe runs and reports a container without GPUs"
report="$(docker run --rm "${IMAGE}" --expect=0 --runs=1)" || die "the probe failed without GPUs: ${report}"
log "${report}"
log "built ${IMAGE}; publish it with deploy/release/gpu-probe/upload.sh"
