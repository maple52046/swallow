# Development image for the api-server component (api-server).
#
# The image ships only the toolchain; the source tree is bind-mounted at runtime
# so that editing a file on the host never requires an image rebuild.

# air needs a newer compiler than the project does, so it is built separately
# and copied in. Keeping the final stage on 1.25 means the project is compiled
# by the same Go version its go.mod declares.
FROM golang:1.26-bookworm AS air-build
ARG AIR_VERSION=v1.67.4
RUN go install "github.com/air-verse/air@${AIR_VERSION}"

# iPXE for Boot ISOs (decision 049), built exactly as in the production image: pinned commit, no
# embedded script; each Boot ISO is packaged with genfsimg at runtime.
FROM golang:1.25-bookworm AS ipxe
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

FROM golang:1.25-bookworm

ARG UID=1001
ARG GID=1001

ENV GOPATH=/go \
    GOCACHE=/go/cache \
    GOMODCACHE=/go/pkg/mod \
    CGO_ENABLED=0 \
    GOFLAGS=-buildvcs=false

COPY --from=air-build /go/bin/air /usr/local/bin/air

# Match the production execution environment: the lock is copied before the source bind
# mount replaces /app, and the venv is available to the non-root developer user.
COPY api-server/automation/requirements.txt /tmp/ansible-requirements.txt
# mtools, xorriso, isolinux, and syslinux-common are what genfsimg needs to package a Boot ISO.
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client python3-venv mtools xorriso isolinux syslinux-common \
 && python3 -m venv /opt/ansible \
 && /opt/ansible/bin/pip install --no-cache-dir -r /tmp/ansible-requirements.txt \
 && rm -rf /var/lib/apt/lists/* /tmp/ansible-requirements.txt
ENV PATH="/opt/ansible/bin:${PATH}"
COPY --from=ipxe /out/ /usr/share/swallow/ipxe/

# Run as the host developer's UID/GID so that anything written into the
# bind-mounted source tree or the cache volumes stays owned by them, not root.
# The job artifact directory is created here so the named volume mounted over it
# inherits the developer's ownership on first init; otherwise the embedded runner
# cannot write run artifacts as the non-root user.
RUN groupadd -g "${GID}" dev \
 && useradd -u "${UID}" -g "${GID}" -m -s /bin/bash dev \
 && mkdir -p "${GOCACHE}" "${GOMODCACHE}" /tmp/air /var/lib/swallow/jobs /var/lib/swallow/boot-media \
 && chown -R "${UID}:${GID}" /go /tmp/air /var/lib/swallow/jobs /var/lib/swallow/boot-media

USER dev
WORKDIR /app

CMD ["air", "-c", "/etc/swallow/air.toml"]
