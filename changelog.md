# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.3.0] - 2026-10-25

On go-fabric v0.12.1 (the controller facade routes of its bundle CJ).

### Added

- `/ws`: `parse_onboarding_payload`, `batched_device_commands` (one InvokeRequest, refused above the node's MaxPathsPerInvoke), `enable_auto_subscription`/`disable_auto_subscription` (with `--disable-auto-subscription`), `open_commissioning_window` (an enhanced window with its pairing codes), `discover`/`discover_commissionable_nodes` and `commission_on_network` on the controller's own discovery.
- `/ws`: the ICD commands `get_icd_state`, `register_icd`, `resync_icd`, `unregister_icd` (go-fabric's ICD client; a multi-admin refusal is error 100 with matterjs-server's JSON details).
- `/ws`: `get_network_topology` and the `network_topology_updated` event (Thread links from the neighbor tables, Wi-Fi access points, the nodes' addresses as `ip_addresses`).
- `/ws`: `import_test_node` — the nodes of a diagnostics dump as test nodes, answered from memory as matterjs-server answers them.
- `commission_with_code` reports an attestation the policy rejects with reason `attestation-rejected`, and an undecodable code with `Invalid pairing code: …` (error 8, `invalid-payload`); with `--bluetooth-adapter` and a stored credential it commissions over BLE (the flag refuses to start until go-fabric releases its BlueZ central on v0.12).
- `get_all_credentials` names the Thread dataset's `networkName` and `extPanId`.
- `set_default_fabric_label` pushes the label to every connected node (and at start, when the fabric's label differs).
- `get_vendor_names` answers from go-fabric's embedded vendor snapshot when the ledger is offline, the ledger's names over it otherwise.
- `/bridge`: a host's refusal with a status reaches the controller with that status, a missed deadline as TIMEOUT (go-fabric's `contract.StatusError`); a bridged device's Identify takes the host's handlers.
- The node pages: `--web`, `--web-admin`, `--web-admin-source` serve go-fabric-ui v0.1.1's pages of the bridge node.
- `--enable-test-net-dcl` (matterjs-server's flag: the CSA test roots).
- The Home Assistant add-on (`addon/`, `repository.yaml`, the add-on image workflow) and `make version-check`, `make addon-image`.
- The CI job `compat-python`: matter-python-client 1.4.0 (Home Assistant's pin) against the daemon and a commissioned reference bridge. Docs: `docs/home-assistant.md`, `docs/addon.md`, `docs/compat.md`.

### Changed

- Values go through go-fabric's JSON route (`client.EncodeJSON`/`DecodeJSON`); `internal/wire` and its import-guard exception are gone. A command's mandatory field left out now takes matter.js's fallback instead of being refused; a 4-byte float renders as its shortest decimal (`0.1`).
- `set_thread_dataset` refuses a dataset that is not MeshCoP TLVs; the messages of a bad dataset are matterjs-server's.
- `discover` listens 3 seconds (matterjs-server's window), not 5.
- The daemon opens its database with `controller.OpenDB` and the production trust with `controller.ProductionTrust`, and gives every node its own hooks (`controller.Config.HooksFor`): `node_event` is forwarded in production.
- The InvalidCommand details of the commands not answered name the go-fabric work they wait for, or "not planned".

### Removed

- The bridge node's pairing codes from the log (they carry the passcode); `pairing_codes` on `/bridge` and the node pages show them.
- `modernc.org/sqlite` as a direct dependency (go-fabric's opener carries it).

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
