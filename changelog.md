# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- The add-on icon and logo (`addon/icon.svg`, `addon/logo.svg`, rendered to
  `icon.png` and `logo.png` with rsvg-convert): the server and the fabric's
  three threads with their node, go-fabric-ui's motif as go-ha2fabric's
  icon uses it. No Connectivity Standards Alliance mark.
- Option labels for the add-on's configuration page in English and German
  (`addon/translations/en.yaml`, `de.yaml`): the page shows "API port" and
  "Bridge node extensions" rather than the raw keys.

## [0.4.6] - 2026-10-30

Dependency release: go-fabric `v0.15.4`, go-fabric-ui `v0.2.8`.

### Changed

- A `/bridge` device whose type or mandated cluster the specification
  level (`--bridge-spec-level`) does not know is withheld instead of
  refusing the whole declaration, as go-fabric's reference daemon does: the
  declaration succeeds, the device serves no endpoint, the node records it
  as withheld (the `surface` report lists it), the daemon logs the warning
  `fabricserver.bridge.withheld` once, and `declare`'s answer gains
  `withheld: [{key, device_type, spec_level}]`, absent when none. The Go
  client's `DeclareResult` gains `Withheld`.
- The Python compatibility job's test dependencies: pytest `8.4.2` →
  `9.0.3` (Dependabot alert 1).
- go-fabric `v0.15.3` → `v0.15.4`: `node.Observers.OnSurfaceChanged` announces
  an endpoint whose served surface an assembly changed; a withheld key's
  record leaves `Node.Surface().Withheld` on `Remove` and `Forget`; two test
  flakes (#306). Nothing composed changes.
- go-fabric-ui `v0.2.7` → `v0.2.8`: `surface_changed` comes from the node's
  observer, so a reassembly the host runs on its own reaches the stream too;
  the status page's warning count text is neutral; the Surface page shows the
  root endpoint.

## [0.4.5] - 2026-10-30

Dependency release: go-fabric `v0.15.3`, go-fabric-ui `v0.2.7`.

### Added

- The bridge node's surface policies (go-fabric's `node.Config.Surface`,
  its ADR 0030): `--bridge-extensions id[,id…]` enables features go-fabric
  implements ahead of matter.js by register ID (Thermostat's
  `MatterScheduleConfiguration` is `BD-chip-Thermostat-Schedules`),
  `--bridge-spec-level` sets the specification level and
  `--bridge-profile name[,name…]` the controller profiles. Each has its
  `FABRIC_SERVER_` variable; an unknown value stops the start with the
  known ones. Every declared device is built under the node's policies.
- The `/bridge` command `surface`: the node's surface report, go-fabric's
  `surface.Report` JSON unchanged (policies, level, profiles, extensions,
  the effective surface, decisions, withheld elements, changes).
- Add-on options `surface_extensions`, `surface_level` and
  `surface_profiles`, passed as the three flags.

### Changed

- A `/bridge` declaration that selects an ahead-of-matter.js feature the
  node does not enable is still `InvalidArguments`, and its details now
  name the register IDs and the `--bridge-extensions` value that enables
  them; a device type the specification level withholds names the level
  and `--bridge-spec-level`.
- go-fabric `v0.15.2` → `v0.15.3`: `surface.Warn`, the verdict of a policy that
  keeps an element and flags it; a controller profile's Reported or Inherited
  row is a `Warn` decision. Nothing composed changes.
- go-fabric-ui `v0.2.6` → `v0.2.7`: the node's surface report gets its own page
  (`GET /node/surface`: effective surface per endpoint, decisions, withheld
  elements, announced changes), a Surface tab in the shell, the stream event
  `surface_changed` and a marker on the Endpoints page for an endpoint whose
  surface changed; the status page counts warnings by the `Warn` verdict.

## [0.4.4] - 2026-10-30

Dependency release: go-fabric `v0.15.2`, go-fabric-ui `v0.2.6`.

### Changed

- go-fabric `v0.15.1` → `v0.15.2`: `surface.Report.Extensions` lists the enabled ahead-of-matter.js register IDs in the node's surface report, for go-fabric-ui's status page (its 0.2.6). Nothing composed changes. `protocol/schema` regenerated: only the go-fabric version stamp moves.
- go-fabric-ui `v0.2.5` → `v0.2.6`: the node status page shows the surface
  policies the node composes under — the specification level with its
  origin, the controller profiles, the enabled extensions — and, when
  non-zero, the counts of withheld elements, profile warnings and
  announced surface changes (`GET /node/status` gains `surface`).

## [0.4.3] - 2026-10-30

Dependency release: go-fabric `v0.15.1`, go-fabric-ui `v0.2.5`.

### Changed

- go-fabric `v0.14.0` → `v0.15.1`. The daemon builds and its tests pass
  without a code change. go-fabric v0.15.0 serves each endpoint's surface
  (device types, clusters, features, the global lists) as a value under
  node-level policies (its ADR 0030): a specification level, opt-ins for
  the features it implements ahead of matter.js, and controller profiles,
  all in `node.Config.Surface`, which this daemon leaves at its zero
  value. Through go-fabric, for the bridge half: when an endpoint comes
  back after a restart with a different surface, the node raises its and
  the root's ConfigurationVersion in one change and bumps the changed
  clusters' data versions, so a paired controller reloads it. **A
  `/bridge` declaration that selects a feature go-fabric implements ahead
  of matter.js** — Thermostat's `MatterScheduleConfiguration` or its
  events feature, TimeSynchronization's time-zone, NTP client, NTP server
  and time-sync-client features — **is now refused** by go-fabric
  (`surface.ErrExtensionNotEnabled`) because the daemon does not yet
  expose the opt-in; every other feature selection is unchanged. The
  opt-in in the daemon's configuration is a follow-up. v0.15.1: a bridged
  endpoint whose device has no name keeps its Descriptor; three race-job
  test flakes fixed in go-fabric.
- go-fabric-ui `v0.2.4` → `v0.2.5`: the selection page hides items whose
  verdict is unmappable until the toggle above the list shows them.
- `protocol/schema` regenerated from go-fabric `v0.15.1`'s snapshot: only
  the go-fabric version stamp moves (`x-go-fabric-version`,
  `GoFabricVersion`); no id, name or type changes.

## [0.4.2] - 2026-10-29

Dependency release: go-fabric `v0.14.0` (its `ble/esphome` at `v0.13.10`),
go-fabric-ui `v0.2.4`.

### Changed

- go-fabric `v0.13.5` → `v0.14.0`. go-fabric v0.13.7 removed
  `controller/thread`; the Thread dataset codec of `set_thread_dataset`,
  `get_all_credentials` and the pages' Thread credentials is now
  `github.com/SukramJ/go-fabric/thread`, every identifier unchanged, and
  the import guard allows `thread` in its place. What a dataset must be
  to be accepted does not change. Through go-fabric, for this daemon:
  the controller rotates its operational key on every start (a new key
  pair and NOC under the same fabric, saved over the stored record);
  `commission_with_code` commissions every matching device it finds in
  parallel and keeps the first to establish PASE, and with a BLE backend
  searches DNS-SD as well; single invokes within one batch window share
  one InvokeRequest when the device accepts several paths; the bridge
  half's node answers TimeSynchronization's SetUTCTime with
  connectedhomeip's statuses instead of accepting every request (the
  clock is still not set). The camera, Thread Border Router, Joint
  Fabric, TLS and AmbientContextSensing servers it adds are not composed
  by this daemon.
- `/bridge`: a declaration of a `VideoDoorbell` device is accepted; it was
  refused as a type go-fabric's device kit does not build. go-fabric
  v0.13.12 builds the composite, and v0.14.0 builds every device type of
  the snapshot, so no declared type is refused for that reason any more.
- go-fabric `ble/esphome` from a pseudo-version of 2026-10-27 to its tag
  `v0.13.10`. `ble/bluez` stays at `v0.12.1`, its latest tag.
- go-fabric-ui `v0.2.1` → `v0.2.4`: the pages take Home Assistant's theme
  from the first frame (v0.2.2), build-tooling updates (v0.2.3), the
  go-fabric `v0.14.0` build and the exported `NodeObservers` (v0.2.4),
  which this daemon does not use.
- `protocol/schema` regenerated from go-fabric `v0.14.0`'s snapshot: only
  the go-fabric version stamp moves (`x-go-fabric-version`,
  `GoFabricVersion`); no id, name or type changes.

## [0.4.1] - 2026-10-27

On go-fabric v0.13.5 and its nested module `ble/esphome` (brief S5).

### Added

- `--bluetooth-proxy host[:port][,key]`, repeatable: `commission_with_code` and the pages commission BLE devices through ESPHome Bluetooth proxies (go-fabric `ble/esphome`), so the host needs no Bluetooth adapter or D-Bus. Not together with `--bluetooth-adapter` (a start-up error); a proxy that refuses (wrong or missing key, no `bluetooth_proxy: active: true`) or none answering stops the start. A pairing that fails at a proxy (another API client took it, usually Home Assistant) reports the module's error text as it is.
- The add-on's option `bluetooth_proxies` (a list, empty by default), passed as repeated `--bluetooth-proxy`; the add-on still has no `host_dbus`.

### Fixed

- `server_info.bluetooth_enabled` is true when a BLE backend is configured (matterjs-server's `ble.enable`); it was always false, also with `--bluetooth-adapter`.

### Changed

- The BLE central is closed before the controller on shutdown.

## [0.4.0] - 2026-10-27

On go-fabric-ui v0.2.1 (the controller family of v0.2.0, building on 32-bit targets; go-fabric stays v0.13.3; brief S4).

### Added

- `--web` serves go-fabric-ui's controller pages beside the node pages, over the daemon's controller (`fabricui/gofabric.ControllerOf`): the fabric, the commissioned nodes with their attributes, commands, ACL, bindings, ICD and fabrics, commissioning by code or from a discovery, decommissioning, the fabric label, the Wi-Fi and Thread credentials and software updates. `--controller=false` shows the family as disabled. The admin rule of `--web-admin` and `--web-admin-source` covers the controller's admin routes too; read-only by default.
- The pages and `/ws` share the controller half's state: one credential list (`server.json`; an entry set on either side is listed on the other by name only, a dataset set on the pages must be MeshCoP TLVs, every change broadcasts `server_info`), one fabric label (a change on the pages is stored as `set_default_fabric_label` stores it; refused with 409 under `--default-fabric-label`) and one pairing template (the attestation policy, `--disable-auto-subscription`, the BLE central of `--bluetooth-adapter`).
- `controllerapi.Server` gains the host side of that state: `Credentials`, `SetWiFiCredential`, `SetThreadDataset`, `RemoveCredential`, `WiFiCredential`, `ThreadDataset`, `SetFabricLabel` and `PairOptions`, with `ErrInvalidCredential`, `ErrUnknownCredential` and `ErrLabelPinned`.

### Changed

- The add-on's Ingress shows the controller pages, and the bridge node's pages with **bridge** on.
- With `--web`, go-fabric-ui's node hooks are chained after the controller half's (`gofabric.Hooks.Wrap`) and closed (`Hooks.Close`) before the controller closes.

## [0.3.2] - 2026-10-26

### Added

- `/bridge` `forget` (`{"device": "<key>"}`, answering `{"device": "<key>"}`): removes the device if it is still declared and frees the endpoint number stored under its key (`Aggregator.Forget`), so a later `declare` of the key gets a fresh number. `remove` keeps the number, so a later `declare` of the key gets it back; `declare` is unchanged and still forgets a key the document no longer names. `forget` is idempotent and may name a key the daemon has never seen in this run (one `remove` dropped before a restart); only an empty key is `InvalidArguments`. The client package gains `Bridge.Forget`.

## [0.3.1] - 2026-10-26

On go-fabric v0.13.3, go-fabric-ui v0.1.2 and go-fabric's BlueZ central
`github.com/SukramJ/go-fabric/ble/bluez` v0.12.1 (brief F1 §2).

### Added

- `--bluetooth-adapter` works: the daemon opens go-fabric's BlueZ central on the adapter (`0` is `hci0`) and `commission_with_code` commissions over BLE with a stored Wi-Fi or Thread credential. The start fails when the adapter cannot be opened (no BlueZ, no system-bus rights to `org.bluez`).

### Changed

- `/bridge` `declare`: a key the document no longer names is forgotten (`Batch.Forget`), so its endpoint number is freed at once; go-fabric v0.13.0 removed `Batch.MarkComplete`, which freed it before. `remove` keeps the number stored under the key, so a later `declare` of the key gets it back (until v0.3.0 the next `declare` freed it).
- The node pages (go-fabric-ui v0.1.2) observe the bridge node themselves; the daemon no longer wires `Events.Observers` into the node's config. `GET /node/status` gains `window_kind` and `window_deadline`, `GET /node/endpoints` the clusters' names and revisions.
- `GET /schema/declaration` and `protocol/schema` regenerated from go-fabric v0.13.3's snapshot: the device kit builds Camera, SnapshotCamera, AudioDoorbell, Intercom, Chime, CameraController, ThreadBorderRouter, NetworkInfrastructureManager and JointFabricAdministrator, so `declare` no longer refuses them as types the kit does not build; the camera, WebRTC, push AV, zone, Thread border router and directory, Thread network diagnostics, Joint Fabric and TLS clusters have typed packages and are declarable (AccessControl, OperationalCredentials and GroupKeyManagement stay the only ones that are not).

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
