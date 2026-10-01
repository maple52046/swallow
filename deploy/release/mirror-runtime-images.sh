#!/usr/bin/env bash
# Copy the approved upstream runtime images into the swallow image repository so an
# installation pulls every image from one registry. Only MIRROR_PLATFORM (default linux/amd64,
# the only installation target) is copied, so the mirrored digest differs from the upstream
# index digest; the digest recorded is the one read back from the mirror after the copy.
#
# Inputs (environment): IMAGE_REPO plus UPSTREAM_MONGO_IMAGE, UPSTREAM_TEMPORAL_POSTGRES_IMAGE,
# UPSTREAM_TEMPORAL_SERVER_IMAGE, UPSTREAM_TEMPORAL_UI_IMAGE, each an exact @sha256 reference
# (deploy/release/runtime-images.env pins them).
# Argument: the version part of the tag, e.g. "1.2.3".
# Output: `<key>=<repo>@<digest>` lines appended to $MIRROR_OUTPUT (or stdout without it).
set -euo pipefail

version="${1:?tag version required, e.g. 1.2.3}"
repo="${IMAGE_REPO:?set IMAGE_REPO, e.g. ghcr.io/<owner>/swallow}"
platform="${MIRROR_PLATFORM:-linux/amd64}"
output="${MIRROR_OUTPUT:-/dev/stdout}"

mirror() {
  local key="$1" component="$2" source="$3" target
  [[ "${source}" =~ @sha256:[0-9a-f]{64}$ ]] ||
    { printf '%s is not digest pinned: %s\n' "${component}" "${source}" >&2; exit 1; }
  target="${repo}:${component}-${version}"
  printf 'mirroring %s (%s) -> %s\n' "${source}" "${platform}" "${target}" >&2
  crane copy --platform "${platform}" "${source}" "${target}" >&2
  printf '%s=%s@%s\n' "${key}" "${repo}" "$(crane digest "${target}")" >>"${output}"
}

mirror mongo mongo "${UPSTREAM_MONGO_IMAGE:?}"
mirror temporal_postgres temporal-postgres "${UPSTREAM_TEMPORAL_POSTGRES_IMAGE:?}"
mirror temporal_server temporal-server "${UPSTREAM_TEMPORAL_SERVER_IMAGE:?}"
mirror temporal_ui temporal-ui "${UPSTREAM_TEMPORAL_UI_IMAGE:?}"
