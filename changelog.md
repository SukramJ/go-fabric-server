# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-25

Bundle S1 of go-fabric's concept v8, on go-fabric v0.11.0.

### Added

- The controller half at `/ws`, wire-compatible with matterjs-server's
  WebSocket API at schema 14 (minimum 11): `server_info` on connect,
  `start_listening`, the node, attribute, command, subscription, commissioning,
  credential, fabric and log-level commands of the S1 set, the node, attribute,
  endpoint, subscription and server events. Commands without a go-fabric
  facade route yet answer `InvalidCommand` naming the route; commands of later
  bundles answer `InvalidCommand` naming the bundle.
- `capabilities` in `server_info`; `GET /health`, `GET /metrics` (expvar) and
  `GET /schema` (the JSON Schema of the node data values, generated from
  go-fabric's snapshot of the matter.js model).
- The protocol package: the envelope, one JSON codec with exact 64-bit
  numbers, matterjs-server's backpressure send path with metrics.
- Authentication: a Unix socket by default, an optional bearer token,
  required on a TCP listener beyond loopback.
- A Go client (`client/`), the daemon `fabric-server`, a distroless image,
  a hardened systemd unit, the release matrix linux/amd64, linux/arm64,
  linux/arm/v7.
