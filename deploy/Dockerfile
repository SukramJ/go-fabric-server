# SPDX-License-Identifier: Apache-2.0
# go-fabric-server image: a static binary (CGO_ENABLED=0) on distroless,
# non-root. Multi-arch through BuildKit: the builder cross-compiles for
# TARGETPLATFORM (linux/amd64, linux/arm64, linux/arm/v7).
#
# Run it with host networking — Matter needs the host's IPv6 link and
# mDNS — and a volume for the state:
#
#   docker run -d --network host -v fabric-server:/data ghcr.io/sukramj/go-fabric-server
#
# The default listener is 127.0.0.1:5580, the URL Home Assistant's Matter
# integration connects to on the same host (ws://localhost:5580/ws). A
# listener beyond loopback needs FABRIC_SERVER_TOKEN; see docs/operations.md.

# ---------- build ----------
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} go build -trimpath \
      -ldflags="-s -w \
        -X github.com/SukramJ/go-fabric-server/internal/version.Version=${VERSION} \
        -X github.com/SukramJ/go-fabric-server/internal/version.Commit=${COMMIT} \
        -X github.com/SukramJ/go-fabric-server/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/fabric-server ./cmd/fabric-server && \
    mkdir -p /out/data

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/fabric-server /usr/local/bin/fabric-server
COPY --from=builder --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
VOLUME ["/data"]
ENV FABRIC_SERVER_STORAGE_PATH=/data \
    FABRIC_SERVER_LISTEN=127.0.0.1:5580
EXPOSE 5580
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/usr/local/bin/fabric-server", "--health-check", "http://127.0.0.1:5580/health"]
ENTRYPOINT ["/usr/local/bin/fabric-server"]
