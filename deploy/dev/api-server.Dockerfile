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
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client python3-venv \
 && python3 -m venv /opt/ansible \
 && /opt/ansible/bin/pip install --no-cache-dir -r /tmp/ansible-requirements.txt \
 && rm -rf /var/lib/apt/lists/* /tmp/ansible-requirements.txt
ENV PATH="/opt/ansible/bin:${PATH}"

# Run as the host developer's UID/GID so that anything written into the
# bind-mounted source tree or the cache volumes stays owned by them, not root.
# The job artifact directory is created here so the named volume mounted over it
# inherits the developer's ownership on first init; otherwise the embedded runner
# cannot write run artifacts as the non-root user.
RUN groupadd -g "${GID}" dev \
 && useradd -u "${UID}" -g "${GID}" -m -s /bin/bash dev \
 && mkdir -p "${GOCACHE}" "${GOMODCACHE}" /tmp/air /var/lib/swallow/jobs \
 && chown -R "${UID}:${GID}" /go /tmp/air /var/lib/swallow/jobs

USER dev
WORKDIR /app

CMD ["air", "-c", "/etc/swallow/air.toml"]
