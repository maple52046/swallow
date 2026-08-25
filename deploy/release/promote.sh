#!/usr/bin/env bash
set -euo pipefail

manifest=out/release/release-manifest.json
[[ "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] ||
  { printf 'release tag must be SemVer (vX.Y.Z)\n' >&2; exit 1; }
version="${VERSION#v}"
commit="$(jq -er '.commit' "${manifest}")"
[[ "${commit}" == "${GITHUB_SHA}" ]] ||
  { printf 'candidate commit does not match tag\n' >&2; exit 1; }

api="$(jq -er '.images.api' "${manifest}")"
dashboard="$(jq -er '.images.dashboard' "${manifest}")"
crane tag "${api}" "${version}"
crane tag "${dashboard}" "${version}"
cosign sign --yes "${api}"
cosign sign --yes "${dashboard}"

oci_archive="swallow-oci-${version}.tar"
native_bundle="swallow-native-${version}.tar.zst"
compose_bundle="swallow-compose-${version}.tar.zst"
mv out/release/swallow-oci-candidate.tar "out/release/${oci_archive}"
jq --arg version "${version}" --arg oci "${oci_archive}" \
  --arg native "${native_bundle}" --arg compose "${compose_bundle}" \
  '.version=$version |
   .artifacts.ociArchive=$oci |
   .artifacts.nativeBundle=$native |
   .artifacts.composeBundle=$compose' "${manifest}" >"${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"

tar -C out/release -caf "out/release/${native_bundle}" native release-manifest.json
tar -C out/release -caf "out/release/${compose_bundle}" production testing release-manifest.json
(
  cd out/release
  sha256sum release-manifest.json "${oci_archive}" "${native_bundle}" "${compose_bundle}" \
    api.spdx.json dashboard.spdx.json offline-media-manifest.json >SHA256SUMS
)
cosign sign-blob --yes --bundle out/release/SHA256SUMS.sigstore.json out/release/SHA256SUMS
