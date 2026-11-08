# 080. Host toolchains are requirements a host meets, components converge probe-first, and hosts reach the Local Repository through their own configuration

- Status: Accepted
- Date: 2026-11-08
- 現行規格：[module-authoring.md](../development/module-authoring.md) §4 每支 playbook 都要遵守的規則；[local-repository.md](../development/local-repository.md) §8.2 `artifact_sources`；[platform-deployment.md](../development/platform-deployment.md) §6.3 Slurm reference（Omnistat）

## Context

swallow built Omnistat's environment with the host's `python3 -m venv` and pip. Every compute node
therefore installed `python3-venv`, `python3-dev`, and `build-essential`, pip resolved Omnistat's build
dependencies (cmake, ninja, and others) in an isolated build without pinning them, and the role passed
the Local Repository's Python index to pip itself, although `artifact_sources` already points pip at
it through `/etc/pip.conf`.

Hardware counters need the ROCProfiler-SDK extension, which loads only in the CPython minor it was
built for, and swallow builds it for CPython 3.12 ([decision 079](079-release-built-host-artifacts.md)
(release-built host artifacts)). An earlier plan pinned a uv version and a bundled CPython build
(python-build-standalone); the user wants any uv and any CPython 3.12 instead, wants deployments to
know as little as possible about the Local Repository, and wants a host that already has what is
needed (an OS image with Omnistat or the toolchain baked in, or a host a previous run converged) to be
used as it is.

## Decision

1. **A toolchain is a requirement, not a pinned install.** A role that needs Python requires any
   CPython 3.12 and any uv that runs. The shared role `python_toolchain` looks for them in a fixed
   order (`/usr/bin/python3.12`, `/opt/swallow/python/3.12/bin/python3.12`, then `python3.12` on the
   `PATH`; `/usr/local/bin/uv`, `/opt/swallow/bin/uv`, then `uv` on the `PATH`) and supplies only what
   is missing:
   - CPython 3.12 comes from the OS package (on Ubuntu 24.04 it is the system `python3`, which Ansible
     already needs). An OS without the package stops the run with `[python312_unavailable]`. Supplying
     a CPython of swallow's own is deferred until a supported OS lacks the package.
   - uv comes from a file artifact, from the Site's choice (its Local Repository or the declared
     release URL, same sha256). The pin is on the supply side only: any uv a host already has is used.
2. **Components converge probe-first.** A role first probes whether the host already has a usable
   component, and installs nothing when it does. For Omnistat the probe checks that its environment
   is CPython 3.12, every locked dependency is installed at its locked version, Omnistat is the
   expected version, and swallow's patches are applied; with hardware counters it also loads the
   extension and samples the counter in a separate process. Only a failing probe brings in the
   toolchain and rebuilds the environment. The probe, not the presence of files, decides what is
   usable.
3. **What a component installs stays locked.** Omnistat and its dependencies, its build dependencies
   included, install from a hash-locked requirements file, as wheels only; Omnistat itself installs
   without build isolation, so nothing unpinned reaches the environment. This is the verified
   combination of [decision 063](063-omnistat-slurm-observability.md) (omnistat slurm observability);
   only the toolchain is unpinned.
4. **A pre-baked image contract.** swallow uses, and does not overwrite, what an image provides in
   these layouts:
   - Omnistat in `/opt/omnistat/<version>`, with `/opt/omnistat/current` pointing at it: an
     environment in `venv` whose `python` is CPython 3.12 with the expected Omnistat, swallow's patches,
     and the locked dependencies, and the extension inside its `omnistat` package. It is the layout
     swallow itself installs and the path the Slurm Prolog calls.
   - The extension alone in `/opt/swallow/omnistat-rocprof/<omnistat>/rocm<X.Y>/`, which swallow
     copies instead of downloading.
   - The toolchain in the paths of point 1.

   An image whose Omnistat version differs keeps its tree: swallow installs its own version beside it
   and points `current` at that.
5. **Hosts reach the Local Repository through their own configuration.** Python tools find the Site's
   choice the way apt and pip already do: with the Local Repository, `artifact_sources` writes a
   swallow-marked `/etc/uv/uv.toml` whose default index is the Local Repository's and that forbids uv
   to download Python itself; from the upstreams it removes only that file. No other role reads the
   Python index or sets an index variable, and a content test keeps it so. Files (the uv binary, the
   extension) are still fetched by their declared path from `swallow_artifacts.files`.

## Alternatives considered

- **Pin uv and bundle a pinned CPython** (an earlier plan). Rejected by the user: any uv and any
  CPython 3.12 work, and pins on them only add upgrades.
- **uv's managed Python downloads.** Rejected: they come from GitHub, and the Local Repository serves
  only declared files, so an unpinned interpreter could not come through it.
- **uv from `pipx` installed with apt.** Not now: no pin and fully transparent, but three more OS
  packages (`python3-venv`, `python3-pip`, `pipx`) on every compute node; a candidate to replace the
  file artifact later.
- **A standard-library bootstrap that installs uv's wheel from the index.** Rejected: code swallow
  would maintain for what one file artifact does.
- **Each role passes the index** (`PIP_INDEX_URL`, `UV_INDEX_URL`). Rejected: every role would have to
  know about the Local Repository.
- **Keep `python3 -m venv` and pip with build isolation.** Rejected: compilers and headers on every
  compute node, and build dependencies that are not pinned.
- **Reinstall on every run.** Rejected: it would overwrite a pre-baked image and never converge to a
  run without changes.

## Consequences

- Compute nodes no longer get `python3-venv`, `python3-dev`, and `build-essential` for Omnistat;
  `patch` stays. Nodes that already have them keep them.
- A run on a converged host installs nothing.
- An environment pip built before this decision lacks the locked build dependencies, so it is rebuilt
  once.
- uv can change between versions; roles use only `uv venv` and `uv pip install` with long-standing
  options.
- Python packages are pull-through in the Local Repository, so the first deployment that needs them
  needs the provider to reach PyPI.
- A Slurm on an OS without a CPython 3.12 package (Ubuntu 22.04, the RHEL family) first needs the
  deferred interpreter supply.
