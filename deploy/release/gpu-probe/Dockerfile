# The GPU probe image (decision 083): the official ROCm development image, whose ROCm covers every
# Instinct gfx target, with swallow-gpu-probe built for gfx90a (MI200), gfx942 (MI300), and gfx950
# (MI350). A Slurm deployment's gate runs it in the Platform's container runtime with only the job's
# GPU devices, so a host needs only the amdgpu driver. ROCM_BASE pins the base by tag; build.sh
# passes the digest it resolved.
ARG ROCM_BASE=docker.io/rocm/dev-ubuntu-24.04:7.2.4
FROM ${ROCM_BASE}
ARG GFX_TARGETS="gfx90a gfx942 gfx950"
COPY probe.hip /usr/src/swallow-gpu-probe/probe.hip
RUN set -eux; \
    offload=""; for target in ${GFX_TARGETS}; do offload="${offload} --offload-arch=${target}"; done; \
    /opt/rocm/bin/hipcc -O2 ${offload} -o /usr/local/bin/swallow-gpu-probe /usr/src/swallow-gpu-probe/probe.hip
LABEL org.opencontainers.image.title="swallow-gpu-probe" \
      org.opencontainers.image.description="HIP probe for swallow's Slurm GPU acceptance, built for gfx90a, gfx942, and gfx950" \
      org.opencontainers.image.source="https://github.com/maple52046/swallow"
ENTRYPOINT ["/usr/local/bin/swallow-gpu-probe"]
CMD ["--expect=-1", "--runs=1"]
