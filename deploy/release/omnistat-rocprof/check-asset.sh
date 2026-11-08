#!/bin/sh
# Import a rocprof extension asset the way a supported host loads it (decision 079): the host OS's own
# CPython 3.12, with the ROCm minor's libraries on LD_LIBRARY_PATH as the omnistat unit sets them, and
# check that the asset's build_info.json names that ROCm minor. Runs in the check image (check.Dockerfile).
#
# Usage: check-asset.sh ASSET ROCM_MINOR PYTHON
set -eu

asset="$1" minor="$2" python="$3"
dir="$(mktemp -d)"
tar -xzf "${asset}" -C "${dir}"
cd "${dir}"
LD_LIBRARY_PATH="/opt/rocm/core-${minor}/lib" "${python}" -c 'import rocprofiler_sdk_extension'
"${python}" - "${minor}" <<'EOF'
import json
import sys

minor = sys.argv[1]
with open("build_info.json", encoding="utf-8") as source:
    built = json.load(source).get("rocm_version") or ""
if not built.startswith(minor + "."):
    sys.exit(f"build_info.json names ROCm {built or 'no version'}, not ROCm {minor}")
EOF
echo "[check-asset] ${asset} imports with ${python} and ROCm ${minor}"
