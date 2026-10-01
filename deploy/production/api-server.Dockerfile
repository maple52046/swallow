ARG GO_IMAGE=golang:1.25-bookworm@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48
ARG EXECUTION_ENVIRONMENT=python:3.13-slim-bookworm@sha256:cbd229d7b9bd041012af758ce5ffd807a6518d76493ce9035d208f1db9ceff1e

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
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client tar \
 && python3 -m pip install --no-cache-dir --requirement /tmp/swallow-ansible-requirements.txt \
 && rm -rf /var/lib/apt/lists/* /tmp/swallow-ansible-requirements.txt \
 && groupadd --gid 10001 swallow \
 && useradd --uid 10001 --gid 10001 --no-create-home --shell /sbin/nologin swallow \
 && install -d -o 10001 -g 10001 -m 0750 /var/lib/swallow/jobs \
 && install -d -o 10001 -g 10001 -m 0700 /run/swallow/jobs
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
