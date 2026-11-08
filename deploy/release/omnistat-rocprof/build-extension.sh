#!/usr/bin/env bash
# Build one rocprof extension asset inside the builder image (decision 079); build.sh runs it without
# network access. It installs the verified build wheels and the pristine Omnistat source into a
# throwaway environment, builds the extension with Omnistat's own omnistat-build-extras against the
# ROCm minor's Core SDK, imports it, audits it (audit.py, the glibc gate), and packs the asset:
#   rocprofiler_sdk_extension.cpython-312-x86_64-linux-gnu.so   the module the omnistat package loads
#   lib/libomnistat_count.so                                     the counter tool library (user mode)
#   build_info.json                                              ROCm path and version, from Omnistat
#   audit.json                                                   symbol versions and needed libraries
#
# Usage: build-extension.sh SOURCE_ARCHIVE OMNISTAT_VERSION ROCM_VERSION ROCPROFILER_SDK BUILDER_GLIBC ASSET
#   ASSET is the path of the .tar.gz to write.
set -euo pipefail

source_archive="$1" omnistat="$2" rocm_version="$3" rocprofiler_sdk="$4" builder_glibc="$5" asset="$6"
python="${BUILDER_PYTHON:?set BUILDER_PYTHON to the CPython 3.12 of the builder image}"
rocm="/opt/rocm/core-${rocm_version}"

log() { printf '[build-extension] %s\n' "$*"; }

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
export HOME="${work}" PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1 GIT_PYTHON_REFRESH=quiet

log "building the extension of Omnistat ${omnistat} for ROCm ${rocm_version} (${rocm})"
[[ -d "${rocm}" ]] || { log "${rocm} does not exist; the builder image has no ROCm ${rocm_version}"; exit 1; }
tar -xzf "${source_archive}" -C "${work}"
"${python}" -m venv "${work}/venv"
"${work}/venv/bin/pip" install --quiet --no-index --find-links /opt/builder/wheels --require-hashes \
  -r /opt/builder/builder-requirements.txt
"${work}/venv/bin/pip" install --quiet --no-index --no-deps --no-build-isolation "${work}/omnistat-${omnistat}"

# omnistat-build-extras finds cmake on PATH, and would try to pip-install it otherwise.
PATH="${work}/venv/bin:${PATH}" "${work}/venv/bin/omnistat-build-extras" \
  --counters --no-workloads --rocm-path "${rocm}" --output-dir "${work}/lib"

package="$("${work}/venv/bin/python" -c 'import os, omnistat; print(os.path.dirname(omnistat.__file__))')"
module="$(find "${package}" -maxdepth 1 -name 'rocprofiler_sdk_extension.cpython-312-*.so' -print -quit)"
[[ -n "${module}" ]] || { log "omnistat-build-extras left no cp312 rocprofiler_sdk_extension in ${package}"; exit 1; }
[[ -f "${work}/lib/libomnistat_count.so" && -f "${work}/lib/build_info.json" ]] ||
  { log "omnistat-build-extras left no libomnistat_count.so or build_info.json in ${work}/lib"; exit 1; }

log 'importing the extension in the builder'
LD_LIBRARY_PATH="${rocm}/lib" "${work}/venv/bin/python" -c 'from omnistat import rocprofiler_sdk_extension'

stage="${work}/asset"
mkdir -p "${stage}/lib"
cp "${module}" "${stage}/"
cp "${work}/lib/libomnistat_count.so" "${stage}/lib/"
cp "${work}/lib/build_info.json" "${stage}/"
log 'auditing the asset'
"${python}" /opt/builder/audit.py --builder-glibc "${builder_glibc}" --rocm "${rocm}" \
  --rocprofiler-sdk "${rocprofiler_sdk}" --out "${stage}/audit.json" \
  "${stage}/$(basename "${module}")" "${stage}/lib/libomnistat_count.so"

tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "${stage}" -cf - . | gzip -n > "${asset}"
log "wrote ${asset}"
