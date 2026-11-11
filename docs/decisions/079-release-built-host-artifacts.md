# 079. swallow builds the host binaries no upstream publishes at release time, once per ROCm minor, and declares them as file artifacts

- Status: Accepted
- Date: 2026-11-08
- 現行規格：[installation.md](../development/installation.md) §4 Release artifacts 與 image provenance；[local-repository.md](../development/local-repository.md) §5.1 模組宣告

## Context

Omnistat collects GPU hardware counters (`VALUBusy`, decision 063) only through its ROCProfiler-SDK
Python extension, a nanobind CPython module that links `rocprofiler-sdk` and `hsa-runtime64`. Omnistat
publishes no binary of it: upstream builds it on the host (`omnistat-build-extras`), which needs a C++
compiler, CMake, the CPython headers, and ROCm's development headers on every compute node, and turns
a deployment into a build that can fail on each node.

Three facts constrain where and how often it is built:

- A native module links the glibc of the machine that builds it, and glibc is only backward
  compatible: a module built against an older glibc loads on a newer one, not the other way round.
- The module is built without nanobind's stable ABI, so it loads only in the CPython minor it was
  built for.
- A module's `module.json` is compiled into `swallow-api`, and the release is published from a clean
  tree, so a file artifact's URL and sha256 are committed before the release is built. The build is
  not reproducible, so the sha256 can only be the sha256 of the file that was actually published.
  A Site that uses its Local Repository refuses every deployment while one declared file is missing
  ([decision 078](078-local-repository.md) (local repository)), so a declaration whose file is not
  published yet stops those Sites.

## Decision

1. **A host binary no upstream publishes is built by swallow's release tooling, not on the hosts.**
   The Omnistat extension is the first. It is built once per ROCm minor (`X.Y`), never once per ROCm
   and OS, in one pinned builder whose glibc is the lowest of the operating systems that ROCm minor
   officially supports: today the PyPA `manylinux_2_28` image (glibc 2.28, AlmaLinux 8), with ROCm's
   EL8 packages. A host uses the asset of its own ROCm minor when its glibc is not older than the
   builder's. When a ROCm minor drops every glibc 2.28 OS, its builder moves to the next floor
   (glibc 2.34).
2. **One CPython ABI.** The extension is built for CPython 3.12 (`cp312`) only, because swallow builds
   Omnistat's environment with CPython 3.12 ([decision 080](080-host-toolchains-probe-first.md)
   (host toolchains probe first)). Each ROCm minor gives one asset,
   `omnistat-rocprof-<omnistat>-rocm<X.Y>-cp312-x86_64.tar.gz`, holding the module, the
   `build_info.json` Omnistat's own builder writes, and an `audit.json` with the module's highest
   `GLIBC_` and `GLIBCXX_` symbol versions, its `NEEDED` libraries, and the builder's glibc.
3. **An asset is published only after its gates pass**: its highest `GLIBC_` symbol version is not
   newer than the builder's glibc, the module imports in the builder, and it imports with the
   CPython 3.12 of a supported host OS (Ubuntu 24.04's `python3.12` with that ROCm minor's runtime).
4. **Assets have their own release tag**, `omnistat-rocprof-<omnistat>-r<N>`, independent of swallow's
   releases: a new ROCm minor or Omnistat version is a new revision `N`, and a published URL never
   changes. The compatibility table (each ROCm minor with its ROCProfiler-SDK version and whether it
   was verified on hardware or only built and checked) lives with the release tooling.
5. **An asset is an ordinary file artifact.** The Slurm module declares each asset's URL and sha256 in
   `artifacts.files`, and a host fetches it from where the Site's Local Repository mode sends it: its
   Local Repository, or the declared release URL in `upstream` mode (and in `cache` mode until the
   Local Repository holds it), with the same sha256 either way. A host
   that neither source can serve keeps its exporter without counters (reason `artifact_unavailable`,
   decision 063); the deployment does not fail.
6. **Upload first, then declare.** The change that declares an asset is merged only after the asset
   is published, and publishing a swallow release first checks that every file URL its modules
   declare answers, refusing to publish otherwise.

## Alternatives considered

- **Build the extension on each host while deploying** (upstream's way). Rejected: every compute node
  would need a compiler, CPython headers, and ROCm's development packages, each node would build the
  same module, and a build failure would become a deployment failure.
- **One asset per ROCm version and OS.** Rejected by the user: glibc's backward compatibility makes
  one build at the lowest glibc enough, and more builds only add work.
- **Publish the assets with every swallow release, under the release's tag.** Rejected: the
  declaration is committed before the release exists, so its URL answers 404 until the upload, and
  every Site that uses its Local Repository (the development Site among them) refuses deployments in
  between; the assets would also be rebuilt and change sha256 with every release.
- **One module per CPython minor in each asset.** Not needed once swallow fixes Omnistat's environment
  to CPython 3.12 (decision 080); each further minor would multiply the builds.
- **Patch upstream to build with nanobind's stable ABI.** Rejected: a patch swallow maintains is the
  exception ([decision 064](064-upstream-patches-are-the-exception.md) (upstream patches are the
  exception)), and nanobind's stable ABI needs CPython 3.12 or later anyway.
- **Serve the asset only from the Local Repository.** Rejected by the user: production installations
  start in `upstream` mode, so counters would be off on most Sites.

## Consequences

- Supporting another ROCm minor is a build in the release tooling, an upload, and a declaration; hosts
  need no compiler.
- A published asset outlives the swallow releases that declare it, so its URL is stable.
- The release machine needs the pinned builder image and AMD's EL8 package repository, and a check
  that every declared file URL answers.
- An asset cannot be rebuilt in place: a rebuild has a new sha256, so it is a new revision.
