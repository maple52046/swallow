ARG GO_IMAGE=golang:1.25-bookworm@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48
ARG EXECUTION_ENVIRONMENT=python:3.13-slim-bookworm@sha256:cbd229d7b9bd041012af758ce5ffd807a6518d76493ce9035d208f1db9ceff1e

# iPXE for Boot ISOs (decision 049): ipxe.lkrn (BIOS) and ipxe.efi (x86_64 UEFI) are compiled
# once, with no embedded script, from a pinned commit; each Boot ISO is packaged at runtime with
# iPXE's own genfsimg, which adds the rendered script as autoexec.ipxe, so the runtime image needs
# no compiler. The commit is the v2.0.0 tag's; the build fails if the tag ever moves.
FROM ${GO_IMAGE} AS ipxe
ARG IPXE_VERSION=v2.0.0
ARG IPXE_COMMIT=12798ec29aa8a64d8675c4378b99f5fe28447afb
RUN apt-get update \
 && apt-get install -y --no-install-recommends liblzma-dev \
 && rm -rf /var/lib/apt/lists/* \
 && git clone --depth 1 --branch "${IPXE_VERSION}" https://github.com/ipxe/ipxe.git /src/ipxe \
 && test "$(git -C /src/ipxe rev-parse HEAD)" = "${IPXE_COMMIT}" \
 && make -C /src/ipxe/src -j"$(nproc)" bin/ipxe.lkrn bin-x86_64-efi/ipxe.efi \
 && install -d /out \
 && install -m 0644 /src/ipxe/src/bin/ipxe.lkrn /src/ipxe/src/bin-x86_64-efi/ipxe.efi /out/ \
 && install -m 0755 /src/ipxe/src/util/genfsimg /out/genfsimg \
 && printf '%s\n' "${IPXE_VERSION}" > /out/VERSION

FROM ${GO_IMAGE} AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
WORKDIR /src
COPY api-server/go.mod api-server/go.sum ./
RUN go mod download
COPY api-server/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -ldflags="-s -w -X github.com/maple52046/swallow/internal/version.Version=${VERSION} -X github.com/maple52046/swallow/internal/version.Commit=${COMMIT} -X github.com/maple52046/swallow/internal/version.BuiltAt=${BUILT_AT}" \
    -o /out/swallow-api ./cmd/swallow-api

FROM ${EXECUTION_ENVIRONMENT}
LABEL org.opencontainers.image.source="https://github.com/maple52046/swallow" \
      org.opencontainers.image.description="swallow API, worker, and Ansible executor"
USER 0
COPY api-server/automation/requirements.txt /tmp/swallow-ansible-requirements.txt
# mtools, xorriso, isolinux, and syslinux-common are what genfsimg needs to package a Boot ISO.
# The Boot ISO directory is created owned by the service user so a named volume mounted over it
# starts writable.
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client tar mtools xorriso isolinux syslinux-common \
 && python3 -m pip install --no-cache-dir --requirement /tmp/swallow-ansible-requirements.txt \
 && rm -rf /var/lib/apt/lists/* /tmp/swallow-ansible-requirements.txt \
 && groupadd --gid 10001 swallow \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /sbin/nologin swallow \
 && install -d -o 10001 -g 10001 -m 0750 /var/lib/swallow/jobs \
 && install -d -o 10001 -g 10001 -m 0755 /var/lib/swallow/boot-media \
 && install -d -o 10001 -g 10001 -m 0700 /run/swallow/jobs
COPY --from=ipxe /out/ /usr/share/swallow/ipxe/
COPY --from=build /out/swallow-api /usr/local/bin/swallow-api
COPY --chown=10001:10001 api-server/automation /opt/swallow/automation
ENV SWALLOW_API_PLAYBOOK_MANIFEST=/opt/swallow/automation/manifest.json \
    SWALLOW_API_PLAYBOOK_DIR=/opt/swallow/automation/playbooks \
    SWALLOW_API_JOB_RUNTIME_DIR=/run/swallow/jobs \
    SWALLOW_API_JOB_ARTIFACT_DIR=/var/lib/swallow/jobs \
    HOME=/tmp
USER 10001:10001
WORKDIR /opt/swallow
EXPOSE 30051
ENTRYPOINT ["/usr/local/bin/swallow-api"]
CMD ["api"]
