# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/JoKeks2023/housephone/bridge/internal/version.Version=${VERSION}" \
      -o /out/housephone-bridge ./cmd/housephone-bridge \
 && mkdir -p /out/data /out/config

# Home Assistant add-on (ghcr.io/jokeks2023/housephone-bridge-addon,
# `--target addon`). Runs as root: the Supervisor mounts /data owned by root.
# The add-on runs unprivileged in the Supervisor's AppArmor profile.
FROM gcr.io/distroless/static-debian12 AS addon
ARG VERSION=dev
LABEL io.hass.type="addon" io.hass.version="${VERSION}"
COPY --from=build /out/housephone-bridge /usr/local/bin/housephone-bridge
ENTRYPOINT ["/usr/local/bin/housephone-bridge"]
CMD ["serve", "-ha-options", "/data/options.json"]

# Docker Compose (default target).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/housephone-bridge /usr/local/bin/housephone-bridge
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/config /config
ENV HOUSEPHONE_CONFIG=/config/config.yaml \
    HOUSEPHONE_DATA_DIR=/data
USER nonroot:nonroot
VOLUME ["/data"]
# 8080/tcp: WebSocket (behind Cloudflare Tunnel), 5062/udp: SIP to the
# FRITZ!Box, 40000-40199/udp: RTP to the FRITZ!Box, 50000/udp: WebRTC media.
EXPOSE 8080/tcp 5062/udp 50000/udp
ENTRYPOINT ["/usr/local/bin/housephone-bridge"]
CMD ["serve"]
