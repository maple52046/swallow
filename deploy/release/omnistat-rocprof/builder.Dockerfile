# check=skip=InvalidDefaultArgInFrom
# The builder of Omnistat's ROCProfiler-SDK extension for one ROCm minor (decision 079): the pinned
# manylinux_2_28 image (glibc 2.28, the lowest glibc the ROCm minors swallow builds for support) with
# that minor's ROCProfiler-SDK and HSA runtime from AMD's EL8 Core SDK packages, and the build's
# Python requirements as verified wheels, so build-extension.sh then runs without network access.
# build.sh passes every argument from matrix.json.
#
# The build uses AlmaLinux 8.10's own python3.12, not the image's /opt/python/cp312-cp312: those are
# built without libpython, and Omnistat's CMakeLists asks for Python's Development component, which
# needs it. Both are CPython 3.12 with the same extension ABI (cpython-312-x86_64-linux-gnu); audit.json
# records the exact version the package gave.
ARG BUILDER_IMAGE
FROM ${BUILDER_IMAGE}

ARG ROCM_VERSION
ARG ROCM_PACKAGES
ARG AMD_GPG_URL
ARG AMD_GPG_FINGERPRINT
ARG AMD_EL8_REPO
ARG BUILDER_PYTHON

# AMD signs the repository metadata and the packages with the key whose primary fingerprint
# matrix.json pins; a key with another fingerprint stops the build before dnf trusts it.
RUN set -eu; \
    curl -fsSL --retry 3 -o /etc/pki/rpm-gpg/RPM-GPG-KEY-amd-rocm "${AMD_GPG_URL}"; \
    fingerprint="$(gpg --show-keys --with-colons /etc/pki/rpm-gpg/RPM-GPG-KEY-amd-rocm | awk -F: '$1 == "fpr" { print $10; exit }')"; \
    [ "${fingerprint}" = "${AMD_GPG_FINGERPRINT}" ] || { echo "AMD's signing key is ${fingerprint}, not ${AMD_GPG_FINGERPRINT}" >&2; exit 1; }; \
    rpm --import /etc/pki/rpm-gpg/RPM-GPG-KEY-amd-rocm; \
    printf '%s\n' '[amd-rocm-core]' 'name=AMD ROCm Core SDK (EL8)' "baseurl=${AMD_EL8_REPO}" 'enabled=1' \
      'repo_gpgcheck=1' 'gpgcheck=1' 'gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-amd-rocm' \
      > /etc/yum.repos.d/amd-rocm-core.repo; \
    dnf install -y --setopt=install_weak_deps=False \
      "amdrocm-profiler-base${ROCM_VERSION}-${ROCM_PACKAGES}" "amdrocm-runtime-devel${ROCM_VERSION}-${ROCM_PACKAGES}" \
      python3.12 python3.12-devel python3.12-pip; \
    dnf clean all

COPY builder-requirements.txt build-extension.sh audit.py /opt/builder/
RUN "${BUILDER_PYTHON}" -m pip download --quiet --no-cache-dir --no-deps --require-hashes --only-binary :all: \
      -r /opt/builder/builder-requirements.txt -d /opt/builder/wheels
