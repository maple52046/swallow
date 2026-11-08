# check=skip=InvalidDefaultArgInFrom
# A supported host for the extension's second import check (decision 079): the pinned Ubuntu 24.04
# image with its own python3.12 and one ROCm minor's ROCProfiler-SDK from AMD's Core SDK packages, as a
# Slurm compute node has them. build.sh passes every argument from matrix.json and runs the check in
# it without network access.
ARG CHECK_IMAGE
FROM ${CHECK_IMAGE}

ARG ROCM_VERSION
ARG ROCM_PACKAGES
ARG AMD_GPG_URL
ARG AMD_GPG_FINGERPRINT
ARG AMD_UBUNTU_REPO
ARG CHECK_PYTHON

# The same AMD key signs the repository's InRelease; a key with another fingerprint stops the build
# before apt trusts it.
RUN set -eu; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl gnupg "${CHECK_PYTHON}"; \
    install -d -m 0755 /etc/apt/keyrings; \
    curl -fsSL --retry 3 -o /etc/apt/keyrings/amd-rocm.asc "${AMD_GPG_URL}"; \
    fingerprint="$(gpg --show-keys --with-colons /etc/apt/keyrings/amd-rocm.asc | awk -F: '$1 == "fpr" { print $10; exit }')"; \
    [ "${fingerprint}" = "${AMD_GPG_FINGERPRINT}" ] || { echo "AMD's signing key is ${fingerprint}, not ${AMD_GPG_FINGERPRINT}" >&2; exit 1; }; \
    echo "deb [signed-by=/etc/apt/keyrings/amd-rocm.asc] ${AMD_UBUNTU_REPO} stable main" > /etc/apt/sources.list.d/amd-rocm.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends "amdrocm-profiler-base${ROCM_VERSION}=${ROCM_PACKAGES}"; \
    rm -rf /var/lib/apt/lists/*

COPY check-asset.sh /opt/check/
