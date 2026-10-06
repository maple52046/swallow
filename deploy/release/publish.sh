#!/usr/bin/env bash
# Publish a swallow release from a workstation; the project has no CI. It runs the release and
# documentation checks, checks the official runtime images pinned in runtime-images.env, builds
# the api, dashboard, and cli images and pushes them as <IMAGE_REPO>:<component>-<VERSION>, and
# assembles the artifacts with assemble-release.sh under out/release/<VERSION>/. Only those three
# images go to IMAGE_REPO. Released tags are never overwritten.
#
# Usage: deploy/release/publish.sh VERSION [--allow-dirty] [--github-release]
#   VERSION           SemVer without "v", for example 0.1.0
#   --allow-dirty     publish a working tree with uncommitted changes (for rehearsals only;
#                     the manifest still records HEAD)
#   --github-release  also create GitHub Release vVERSION at HEAD with the artifacts (needs gh)
#
# Prerequisites: an x86_64 host. Every image targets linux/amd64 only and is built natively with
# plain `docker build` and `docker push`, so buildx and emulation are not used; the docker CLI may
# be Docker Engine or nerdctl with BuildKit running. It must be logged in to the registry with a
# token that can write packages (`docker login ghcr.io -u <user>`); crane reads the same login.
# Also git, jq, zstd, python3 (the documentation check), and crane
# (`go install github.com/google/go-containerregistry/cmd/crane@v0.22.1`).
# Environment: IMAGE_REPO (default ghcr.io/maple52046/swallow).
set -euo pipefail

version="${1:-}"
shift || true
allow_dirty=false
github_release=false
while (($# > 0)); do
  case "$1" in
    --allow-dirty) allow_dirty=true; shift ;;
    --github-release) github_release=true; shift ;;
    *) printf 'unknown option: %s\n' "$1" >&2; exit 2 ;;
  esac
done

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
release_dir="${root}/deploy/release"
image_repo="${IMAGE_REPO:-ghcr.io/maple52046/swallow}"

log() { printf '[publish] %s\n' "$*"; }
die() { printf '[publish] %s\n' "$*" >&2; exit 1; }

[[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] ||
  die 'usage: publish.sh VERSION [--allow-dirty] [--github-release]   (VERSION is SemVer without "v")'
[[ "$(uname -m)" == x86_64 ]] ||
  die 'publish from an x86_64 host: the images are built natively for linux/amd64, without buildx or emulation'
for tool in docker git jq zstd python3; do
  command -v "${tool}" >/dev/null 2>&1 || die "missing prerequisite: ${tool}"
done
command -v crane >/dev/null 2>&1 ||
  die 'missing prerequisite: crane (go install github.com/google/go-containerregistry/cmd/crane@v0.22.1, then put Go'\''s bin directory on PATH)'
if [[ "${github_release}" == true ]]; then
  command -v gh >/dev/null 2>&1 || die 'missing prerequisite for --github-release: gh'
fi

if [[ "${allow_dirty}" != true && -n "$(git -C "${root}" status --porcelain)" ]]; then
  die 'the working tree has uncommitted changes; commit them or pass --allow-dirty for a rehearsal'
fi
commit="$(git -C "${root}" rev-parse HEAD)"
built_at="$(git -C "${root}" show -s --format=%cI HEAD)"

# Without CI these are the release gate: script syntax, the manifest contract, pinned Python
# dependencies, and documentation links. Run the component test suites before publishing too.
log 'checking the release contract and documentation'
(cd "${root}" && "${release_dir}/validate.sh" && python3 scripts/check-docs.py)

for component in api dashboard cli; do
  if crane digest "${image_repo}:${component}-${version}" >/dev/null 2>&1; then
    die "${image_repo}:${component}-${version} already exists; a released version is never overwritten"
  fi
done

# shellcheck source=runtime-images.env
source "${release_dir}/runtime-images.env"
log 'checking the official runtime images'
for image in "${MONGO_IMAGE:-}" "${POSTGRES_IMAGE:-}" "${TEMPORAL_SERVER_IMAGE:-}"; do
  [[ "${image}" =~ ^[a-z0-9.-]+\.[a-z]+/[^@]+@sha256:[0-9a-f]{64}$ ]] ||
    die "runtime-images.env must pin each image by registry, name, and digest: '${image}'"
  crane digest --platform linux/amd64 "${image}" >/dev/null ||
    die "${image} does not resolve to a linux/amd64 image"
done

# With the buildx plugin installed, Docker's `docker build` runs through buildx and would attach a
# provenance attestation, turning each image into a two-entry index; the release pins one plain
# linux/amd64 manifest per component. nerdctl does not read this variable.
export BUILDX_NO_DEFAULT_ATTESTATIONS=1

declare -A dockerfiles=([api]=api-server.Dockerfile [dashboard]=dashboard.Dockerfile [cli]=cli.Dockerfile)
declare -A digests=()
for component in api dashboard cli; do
  tag="${image_repo}:${component}-${version}"
  log "building ${tag}"
  docker build \
    -f "${root}/deploy/production/${dockerfiles[${component}]}" \
    --build-arg "VERSION=${version}" --build-arg "COMMIT=${commit}" --build-arg "BUILT_AT=${built_at}" \
    -t "${tag}" "${root}" ||
    die "building ${tag} failed; publishing needs a native linux/amd64 builder (Docker Engine, or nerdctl with BuildKit running), not buildx or emulation"
  log "pushing ${tag}"
  docker push "${tag}"
  digests[${component}]="$(crane digest "${tag}")"
done

out="${root}/out/release/${version}"
log "assembling the release in ${out}"
RELEASE_VERSION="${version}" OUT_DIR="${out}" IMAGE_REPO="${image_repo}" \
  API_DIGEST="${digests[api]}" DASHBOARD_DIGEST="${digests[dashboard]}" CLI_DIGEST="${digests[cli]}" \
  MONGO_IMAGE="${MONGO_IMAGE}" POSTGRES_IMAGE="${POSTGRES_IMAGE}" \
  TEMPORAL_SERVER_IMAGE="${TEMPORAL_SERVER_IMAGE}" \
  "${release_dir}/assemble-release.sh" "${commit}"

if [[ "${github_release}" == true ]]; then
  log "creating GitHub Release v${version}"
  (
    cd "${out}"
    gh release create "v${version}" --target "${commit}" --title "swallow ${version}" --generate-notes \
      "swallow-compose-${version}.tar.zst" install.sh swallow-linux-amd64 release-manifest.json \
      offline-media-manifest.json SHA256SUMS
  )
fi

cat <<EOF

swallow ${version} is published to ${image_repo} (commit ${commit}).
Artifacts: ${out}
  swallow-compose-${version}.tar.zst   the single-VM installation (production/, testing/, manifest)
  install.sh                           the one-command installer and upgrader for this version
  swallow-linux-amd64                  the CLI
  release-manifest.json, offline-media-manifest.json, SHA256SUMS

Make sure the GHCR package is public (or installations must docker login first).
EOF
