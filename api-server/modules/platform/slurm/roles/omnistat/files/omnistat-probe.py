"""Report whether a Python environment is the Omnistat swallow expects (decision 080).

The omnistat role runs this with the environment's own interpreter before it installs anything: a
usable environment is used as it is, whether a previous run or an OS image built it. It checks the
interpreter and the installed distributions only, never importing Omnistat, and prints one JSON
object on stdout:

    {"site_packages": "<the directory holding the omnistat package, or empty>",
     "problems": ["<why the environment is not usable>", ...]}

An empty problems list means usable as far as packages go; the role checks swallow's patches against
site_packages itself. It exits 0 whatever it finds, so a failure to run at all is the only error.
"""

import argparse
import json
import platform
import re
import sys
from importlib import metadata

# A requirement line of a hash-locked requirements file: name==version at the start of the line.
LOCKED = re.compile(r"^([A-Za-z0-9._-]+)==([^\s\\]+)", re.MULTILINE)


def canonical(name):
    """Normalize a project name the way package indexes compare names (PEP 503)."""
    return re.sub(r"[-_.]+", "-", name).lower()


def problems_of(lock_text, omnistat_version):
    """List why the running environment is not the locked Omnistat environment."""
    problems = []
    implementation, version = platform.python_implementation(), sys.version_info[:2]
    if implementation != "CPython" or version != (3, 12):
        problems.append(f"the environment runs {implementation} {version[0]}.{version[1]}, not CPython 3.12")
    installed = {}
    for distribution in metadata.distributions():
        name = distribution.metadata["Name"]
        if name:
            installed[canonical(name)] = distribution.version
    wanted = LOCKED.findall(lock_text) + [("omnistat", omnistat_version)]
    for name, version in wanted:
        got = installed.get(canonical(name))
        if got != version:
            problems.append(f"{name} is {got or 'not installed'}, want {version}")
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--lock", required=True, help="the hash-locked requirements file")
    parser.add_argument("--omnistat", required=True, help="the Omnistat version swallow installs")
    args = parser.parse_args()
    with open(args.lock, encoding="utf-8") as lock:
        problems = problems_of(lock.read(), args.omnistat)
    try:
        site_packages = str(metadata.distribution("omnistat").locate_file(""))
    except metadata.PackageNotFoundError:
        site_packages = ""
    print(json.dumps({"site_packages": site_packages, "problems": problems}))


if __name__ == "__main__":
    main()
