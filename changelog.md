# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.1] - 2026-10-25

The release of 0.2.0: its tag pointed at the commit before the version bump, so the release workflow refused it; this version carries the same code with the constant and the changelog in step.

## [0.2.0] - 2026-10-25

Bundle S2 of go-fabric's concept v8, on go-fabric v0.11.0.

### Added

- The bridge half at `/bridge` (`--bridge`): a bridge node whose devices a
  client declares by name (`declare`, one Aggregator batch, validated through
  go-fabric's typed `Patch` structs), changes (`set`, `host`, `remove`) and
  answers (`result`); the events `changed`, the opt-in `write` and `command`
  with correlation and deadline, `result_timeout`, `fabric_added`,
  `fabric_removed`, `commissioning_window`, `subscriptions`,
  `server_shutdown`; one bridge client at a time; its own `server_info`
  with `bridge_schema_version` 1. `GET /schema/declaration` serves the JSON
  Schema of the declaration. Flags `--bridge-listen`, `--bridge-passcode`,
  `--bridge-discriminator`, `--bridge-vendor-id`, `--bridge-product-id`,
  `--bridge-label`, `--bridge-result-timeout`.
- The controller half answers `set_acl_entry`, `set_node_binding` and
  `get_vendor_names` (the production DCL's vendor records).
- The Go client's `DialBridge`; a Python example client
  (`examples/bridge-python/`).
- `docs/by_design.md`: the deliberate differences B1–B13.

### Changed

- The ICD commands' `details` name go-fabric v0.12.0, the OTA commands'
  go-fabric phase 25; `import_test_node` stays planned for S3.

### Fixed

- A derived cluster's inherited commands are requests again
  (RvcOperationalState's and OvenCavityOperationalState's Pause, Stop,
  Start, Resume): `device_command` found no such command.

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
