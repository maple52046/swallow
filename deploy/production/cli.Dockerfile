ARG GO_IMAGE=golang:1.25-bookworm@sha256:e401dae1bf814e29204a8cb7915682e1780951e609ca0dd8865ee1937f510c48

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY cli/go.mod cli/go.sum ./
RUN go mod download
COPY cli/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
    -o /out/swallow ./cmd/swallow \
 && install -d -o 65532 -g 65532 -m 0700 /out/home

# The CLI is a static binary; the release also extracts /swallow as the host binary.
# HOME holds the CLI profile, so mount a volume there to keep a login across runs.
FROM scratch
LABEL org.opencontainers.image.source="https://github.com/maple52046/swallow" \
      org.opencontainers.image.description="swallow operator CLI"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/swallow /swallow
COPY --from=build --chown=65532:65532 /out/home /home/swallow
ENV HOME=/home/swallow
USER 65532:65532
ENTRYPOINT ["/swallow"]
