# SPDX-License-Identifier: Apache-2.0
# Home Assistant add-on Dockerfile (the local-build fallback; the preferred path
# is the pre-built image `image:` in addon/config.yaml names, built by
# .github/workflows/addon-image.yml).
#
# The Go sources are at the repository root: build with the root as context,
#
#   docker build --build-arg BUILD_FROM=ghcr.io/home-assistant/amd64-base:latest \
#     -f addon/Dockerfile -t go-fabric-server-addon .

# BUILD_FROM (the per-arch HA base image) is the runtime stage's FROM; an ARG
# used in a FROM is declared before the first FROM.
ARG BUILD_FROM

# ---------- Stage 1: build the Go binary ----------
FROM golang:1.27.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=addon
ARG COMMIT=none
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=0
RUN go build -trimpath \
      -ldflags="-s -w \
        -X github.com/SukramJ/go-fabric-server/internal/version.Version=${VERSION} \
        -X github.com/SukramJ/go-fabric-server/internal/version.Commit=${COMMIT} \
        -X github.com/SukramJ/go-fabric-server/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/fabric-server ./cmd/fabric-server

# ---------- Stage 2: Home Assistant add-on runtime ----------
ARG BUILD_FROM
FROM ${BUILD_FROM}
COPY --from=builder /out/fabric-server /usr/bin/fabric-server
COPY script/run.sh /run.sh
RUN chmod +x /run.sh
ARG BUILD_ARCH=amd64
ARG BUILD_VERSION=0.3.0
LABEL \
  io.hass.version="${BUILD_VERSION}" \
  io.hass.type="addon" \
  io.hass.arch="${BUILD_ARCH}"
CMD ["/run.sh"]
