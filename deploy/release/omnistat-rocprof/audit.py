"""Audit the native files of a rocprof extension asset and gate its publication (decision 079).

Runs in the builder, with the interpreter the extension was built for. For each ELF file it records
the highest GLIBC_, GLIBCXX_, and CXXABI_ symbol versions it requires and the libraries it needs
(readelf), and for the build the builder's own glibc, its CPython version, and the ROCProfiler-SDK
version the module was built against. It writes the result as audit.json and
exits 1 when an asset could not load where swallow says it does: a file requires a newer glibc than
the builder has, the builder is not the glibc matrix.json pins, or the SDK is not the version the
compatibility table lists.

Usage: audit.py --builder-glibc 2.28 --rocm /opt/rocm/core-10.0 --rocprofiler-sdk 1.3.5 \
           --out audit.json FILE...
"""

import argparse
import json
import os
import platform
import re
import subprocess
import sys

# A versioned symbol requirement in readelf's .gnu.version_r section.
REQUIREMENT = re.compile(r"Name: (GLIBC|GLIBCXX|CXXABI)_([0-9][0-9.]*)\b")
# A dynamic NEEDED entry.
NEEDED = re.compile(r"\(NEEDED\)\s+Shared library: \[([^\]]+)\]")
# The ROCProfiler-SDK version macros of rocprofiler-sdk/version.h.
SDK_VERSION = re.compile(r"#define\s+ROCPROFILER_VERSION_(MAJOR|MINOR|PATCH)\s+(\d+)")


def version_key(version):
    """Order dotted versions numerically (2.9 < 2.17)."""
    return tuple(int(part) for part in version.split("."))


def highest(versions):
    """Return the highest of the dotted versions, or "" for none."""
    return max(versions, key=version_key) if versions else ""


def audit_file(path):
    """Read one ELF file's symbol version requirements and needed libraries."""
    versions = subprocess.run(["readelf", "-V", "-W", path], check=True, capture_output=True, text=True).stdout
    dynamic = subprocess.run(["readelf", "-d", "-W", path], check=True, capture_output=True, text=True).stdout
    required = {"GLIBC": [], "GLIBCXX": [], "CXXABI": []}
    for prefix, version in REQUIREMENT.findall(versions):
        required[prefix].append(version)
    return {
        "glibc": highest(required["GLIBC"]),
        "glibcxx": highest(required["GLIBCXX"]),
        "cxxabi": highest(required["CXXABI"]),
        "needed": sorted(set(NEEDED.findall(dynamic))),
    }


def sdk_version(rocm):
    """Read the ROCProfiler-SDK version a ROCm installation's headers declare, or ""."""
    header = os.path.join(rocm, "include", "rocprofiler-sdk", "version.h")
    try:
        with open(header, encoding="utf-8") as source:
            parts = dict(SDK_VERSION.findall(source.read()))
    except OSError:
        return ""
    if not all(key in parts for key in ("MAJOR", "MINOR", "PATCH")):
        return ""
    return f"{parts['MAJOR']}.{parts['MINOR']}.{parts['PATCH']}"


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--builder-glibc", required=True, help="the glibc matrix.json pins for the builder")
    parser.add_argument("--rocm", required=True, help="the ROCm installation the extension was built against")
    parser.add_argument("--rocprofiler-sdk", required=True, help="the ROCProfiler-SDK version the table lists")
    parser.add_argument("--out", required=True, help="where to write audit.json")
    parser.add_argument("files", nargs="+", help="the ELF files of the asset")
    args = parser.parse_args()

    builder_glibc = os.confstr("CS_GNU_LIBC_VERSION").split()[-1]
    files = {os.path.basename(path): audit_file(path) for path in args.files}
    report = {
        "builderGlibc": builder_glibc,
        "builderPython": platform.python_version(),
        "rocprofilerSdk": sdk_version(args.rocm),
        "maxGlibc": highest([entry["glibc"] for entry in files.values() if entry["glibc"]]),
        "maxGlibcxx": highest([entry["glibcxx"] for entry in files.values() if entry["glibcxx"]]),
        "files": files,
    }
    with open(args.out, "w", encoding="utf-8") as out:
        json.dump(report, out, indent=2, sort_keys=True)
        out.write("\n")

    failures = []
    if builder_glibc != args.builder_glibc:
        failures.append(f"the builder has glibc {builder_glibc}, but matrix.json pins {args.builder_glibc}")
    for name, entry in files.items():
        if entry["glibc"] and version_key(entry["glibc"]) > version_key(builder_glibc):
            failures.append(f"{name} requires GLIBC_{entry['glibc']}, newer than the builder's glibc {builder_glibc}")
    if report["rocprofilerSdk"] != args.rocprofiler_sdk:
        failures.append(
            f"the extension was built against ROCProfiler-SDK {report['rocprofilerSdk'] or 'of unknown version'}, "
            f"but the compatibility table lists {args.rocprofiler_sdk}"
        )
    for failure in failures:
        print(f"[audit] {failure}", file=sys.stderr)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
