#!/usr/bin/env bash
# Publish the GPU probe image build.sh made (decision 083, decision 079). A published tag is never
# replaced: a changed probe or base is a new tag, declared in the Slurm module only after it is
# pushed. Publish from the release machine only.
#
# Usage: deploy/release/gpu-probe/upload.sh
# Prerequisites: docker logged in to ghcr.io with a token that may push the image.
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/maple52046/swallow-gpu-probe:1.0.0-rocm7.2.4}"

log() { printf '[gpu-probe] %s\n' "$*"; }
die() { printf '[gpu-probe] %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || die 'missing prerequisite: docker'
docker image inspect "${IMAGE}" >/dev/null 2>&1 || die "build ${IMAGE} first (build.sh)"
if docker manifest inspect "${IMAGE}" >/dev/null 2>&1; then
  die "${IMAGE} is already published; a new probe is a new tag"
fi
docker push "${IMAGE}"
log "published ${IMAGE}"
