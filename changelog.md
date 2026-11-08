# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Erase a device node from the pages (#50): the fabrics page's Erase
  (admin, go-fabric-ui's `POST …/maintenance/erase`) removes every fabric,
  erases the node's store and endpoint records and brings the node back in
  place — same key, port, discriminator and passcode, a new UniqueID,
  uncommissioned — for the `--bridge-*` node and each registry node. The
  bridge client receives `server_shutdown` and dials the same path back.
  A failed erase or a new node that fails to start keeps the daemon up and
  shows as the warning `node_erase_failed` or `node_restart_failed` on
  every status page; Erase again retries.

### Changed

- go-fabric-ui `v0.6.1` → `v0.7.1` (contracts 0.7.0 and 0.7.1, on go-fabric
  `v0.18.0` as before; no new module). What the server sees: the node
  pages' erase (`Options.Erase`, `NodeEntry.Erase`, `Status.erase`), the
  re-announce route and the status page's attestation error, boot reason
  and session parameters, all served from `*node.Node` as is; the update
  manager's events now from go-fabric-ui's adapter; `fabricui.NodeSwitch`,
  the source that follows a node replaced by an erase; the commissioning's
  Bluetooth LE statement on the Commission page (go-fabric-ui#49). The
  pages preselect the `default` credential entry (the one an id-less
  `set_wifi_credentials` or `set_thread_dataset` stores):
  `controllerapi.Credential.Default`, mapped to go-fabric-ui's
  `Credential.Default`. `protocol/schema` unchanged.

### Fixed

- `commission_with_code` says why it uses or skips Bluetooth LE (#48):
  `fabricserver.commission.ble` (`network`, `wifi_id`/`thread_id` as named
  or `default`; never an SSID or a dataset) when BLE is handed to
  go-fabric, `fabricserver.commission.ble_skipped` with `reason`
  `network_only`, `code_without_ble` or `no_network` (plus a `hint`) when
  the daemon has a central and BLE was left out — the reason names of
  go-fabric-ui 0.7.1's Commission page. The rule itself is unchanged
  (`by_design.md` B20). `docs/protocol.md` names `--bluetooth-proxy`
  beside `--bluetooth-adapter` for the BLE route.

## [0.6.1] - 2026-11-07

Dependency release: go-fabric `v0.18.0`, go-fabric-ui `v0.6.1`.

### Changed

- go-fabric `v0.17.0` → `v0.18.0` (milestone 10 phases 36–39, the matter.js
  pin lift 862ad75f → db021da0). `protocol/schema` regenerated from the
  `v0.18.0` snapshot: the go-fabric version and the matter.js source commit
  move, no table changes. What this server sees of the breaks: none of the
  removed symbols was used — `controller.OwnNode`, `node/controllernode` and
  `OTAConfig.StartNode`/`Listen`/`Advertiser`/`ProductID` (the controller's
  second node; the OTA provider is the controller itself at
  `controller.OTAProviderEndpoint`, `softwareupdate.Config.ProviderNodeID`
  is RootNodeID), `CommissionOptions.CaseAuthenticatedTags` and
  `Record.CaseAuthenticatedTags` (no CAT field on the wire), and
  `transport/tcp`'s `MaxPayloadSize` (not set here; a session now sends at
  most the MAX_TCP_MESSAGE_SIZE its peer announced). The daemon never
  configured an OTA provider, so the own-node migration has nothing to move
  in `fabric-server.db`; `check_node_update`, `update_node` and
  `initiate_ota_upload` stay stubs. The controller side of cameras
  (`Config.Camera`, `Controller.CameraRequestor`) and groups
  (`Controller.Groups`, `SetGroupKey`, `Config.GroupTransport`) is new in
  go-fabric and not composed: the `camera_*` commands stay "not planned",
  and no protocol field changes meaning.
- go-fabric-ui `v0.6.0` → `v0.6.1`: on go-fabric `v0.18.0`; no removed
  symbol was used there.
- golang.org/x/net `v0.61.0` → `v0.62.0` (indirect, required by go-fabric
  `v0.18.0`).

## [0.6.0] - 2026-11-06

Device nodes created in the pages from a registry, on go-fabric-ui v0.6.0 (`--bridge-node` and `--node-default` removed), and the add-on's list options passed in full, so ESPHome Bluetooth proxies work.

### Added

- Device nodes created in the pages (#42, go-fabric-ui contract 0.6.0):
  the first node stays the configuration of the `--bridge-*` flags; every
  further node is registry state in `nodes.json` under `--storage-path`
  (`{"nodes": [{"key", "label", "listen", "discriminator", "passcode",
  "created_at"}]}`, mode 0600), which an admin creates, renames and deletes
  through the node selector (`POST /api/v1/nodes`, `PUT
  /api/v1/nodes/<key>/label`, `DELETE /api/v1/nodes/<key>`). The daemon
  allocates the key (a slug of the label), the next free UDP port after the
  default node's, a random discriminator no other node holds and a random
  valid passcode, starts the node and serves `/bridge/<key>` at once;
  `server_info` names the node as before. Deleting a node is final: its
  client receives `server_shutdown`, `/bridge/<key>` answers 404, the node
  leaves every fabric (each controller that paired it loses it) and stops,
  and its store `bridge-<key>.db` and its entry are deleted. Every registry
  node starts with the daemon. `--node-limit` (default 8) caps the
  registry. Each node's status page shows its configuration block,
  `source` `configuration` (the `--bridge-*` flags) or `registry`, never the
  passcode.

### Changed

- go-fabric-ui `v0.5.0` → `v0.6.0` (contract 0.6.0: the node registry,
  per-node selection, the configuration block).

### Removed

- `--bridge-node` and `--node-default` (0.5.0), and `Config.BridgeNodes`
  and `Config.NodeDefault`: one source for the node list — the first node
  is configuration, every further node registry state. A daemon started
  with either flag, or with `FABRIC_SERVER_BRIDGE_NODE` or
  `FABRIC_SERVER_NODE_DEFAULT` set, refuses with a message naming the
  pages. Migration: start with `--web` and `--web-admin` (or
  `--web-admin-source`) and create each further node in the pages' node
  selector ("Create node…"); it gets a new port, discriminator and passcode
  and is paired again. The old nodes' stores (`bridge-<key>.db`) are left on
  disk, never adopted, and can be deleted.

### Fixed

- Add-on: the last entry of every list option was dropped, so a single
  `bluetooth_proxies` entry never reached the daemon (`capabilities.ble`
  stayed `false`) and a single `surface_extensions` or `surface_profiles`
  entry was passed as an empty value (#45, broken since 0.4.1). `run.sh`
  read bashio's list output with `while read`, which loses a last line that
  has no newline after it. The add-on now logs how many Bluetooth proxies it
  passes, or that none are configured.

## [0.5.0] - 2026-11-05

The milestone-9 release of the daemon: several device nodes per daemon, the operation and ping ports of go-fabric-ui 0.5.0, `logring`, on go-fabric v0.17.0 and go-fabric-ui v0.5.0.

### Added

- Several device nodes per daemon: each `--bridge-node
  key,listen=ADDR[,discriminator=N][,passcode=N][,vendor-id=N][,product-id=N][,label=TEXT]`
  runs one more Matter node beside the `--bridge-*` one (key `default`),
  with its own port, discriminator, passcode, fabrics, mDNS advertisement
  and store `bridge-<key>.db`; a client feeds it at `/bridge/<key>`
  (`/bridge` is the default node, `--node-default`), `server_info` on
  `/bridge` names the node (`node`), and the Go client dials one with
  `client.Options.Node`. A daemon configured as before runs its one node
  unchanged. The pages list every node in their node selector, the
  default first.
- The pages' Operation page (go-fabric-ui contract 0.5.0): the log level,
  the log ring with its stream (go-fabric's `logring` in front of the
  stderr handler) and the version and uptime. Its level and `/ws`
  `get_loglevel`/`set_loglevel` are one source; `file_loglevel` stays
  `null`.
- The controller pages ping a node (go-fabric-ui's adapter, one Matter
  read); the node pages' selection says why there is
  none: "the server keeps no host item model; selection is the client's".
- `/ws` `ping_node` takes `details: true` (an extension) and answers one
  result per address — `[{ip, port, reachable, round_trip_ms?, error?}]`;
  the Go client's `PingNodeAddresses`.

### Changed

- go-fabric `v0.16.1-0.20261103161943-af164eb2aa1e` →
  `v0.16.1-0.20261104200735-c2dc83da3d2a` (main at `c2dc83da`, `logring`)
  and go-fabric-ui `v0.3.1-0.20261103135546-2d326bdcf180` →
  `v0.3.1-0.20261104202028-11bd64b23b6a` (main at `11bd64b`, contract
  0.5.0, wave U5a): pseudo-versions until both modules tag, the next release lifts
  them to the tags; `protocol/schema` regenerated (`GoFabricVersion`
  only).
- `/health`'s `device_count` sums the devices of every device node.
- `/ws` `ping_node` probes every address of the node's record with
  go-fabric's `client.Node.PingAddress` (go-fabric #398), in parallel and
  up to `attempts` times each, and answers each IP with its own outcome;
  before, one read through the session answered every address alike. A
  node without a session falls back to that session-opening read, whose
  outcome answers every address, as matterjs-server's ICMP ping needs no
  session.
- go-fabric `v0.16.1-0.20261104200735-c2dc83da3d2a` →
  `v0.16.1-0.20261105055107-8feaa508792d` (main at `8feaa508`,
  `Node.PingAddress`); `protocol/schema` regenerated (`GoFabricVersion`
  only).
- go-fabric `v0.16.1-0.20261105055107-8feaa508792d` → `v0.17.0` (the tag
  of milestones 8 and 9; nothing moved since `8feaa508` but the TBRM golden
  and the harness image); `protocol/schema` regenerated (`GoFabricVersion`
  only).
- go-fabric-ui `v0.3.1-0.20261104202028-11bd64b23b6a` → `v0.5.0` (the tag of
  milestone 9: U4 device details, U5 operation; contract 0.5.0).

- The add-on's side panel is titled **Fabric Controller** (was `Fabric`,
  `panel_title` in `addon/config.yaml`), the role the add-on plays for Home
  Assistant's Matter integration; go-ha2fabric's panel is `Fabric Bridge`.
- The add-on image workflow refuses a tag whose version differs from
  `addon/config.yaml` or `internal/version`, as the release workflow does;
  a misplaced tag no longer publishes an add-on image under a version its
  manifest contradicts.
- go-fabric `v0.15.6` → `v0.16.1-0.20261103161943-af164eb2aa1e` (main at
  `af164eb2`, `controller/topology` with `Service.AddSource`) and
  go-fabric-ui `v0.2.9` → `v0.3.1-0.20261103135546-2d326bdcf180` (main at
  `2d326bdc`, its changelog's v0.4.1, contract 0.3.1): pseudo-versions
  until both modules tag, the next release lifts them to the tags. The
  build list follows them: `modernc.org/sqlite` 1.61.0, `modernc.org/libc`
  1.79.0, `go-humanize` 1.1.0, `go-strftime` 1.1.0; `grandcat/zeroconf`
  and `cenkalti/backoff` leave it (go-fabric's own DNS-SD responder).
  `protocol/schema` regenerated (`GoFabricVersion` only).
- The bridge half's `node_info` always carries `commissioning_window`
  (`open`, `own` from go-fabric's `Node.Window`), `false`/`false` before
  the node runs, where it was absent while the node had no window state.
- The topology is go-fabric's `controller/topology`: `get_network_topology`,
  `network_topology_updated` and the controller pages' `/controller/topology`
  answer one service, rebuilt 2 s after the last node change (debounced)
  and every 60 s, its `refresh` reading the diagnostics of the available
  Thread and Wi-Fi nodes four at a time within 30 s, as matterjs-server;
  the server's own build, its per-cache-pass update and its hash gate
  are gone. The imported test nodes join the graph as a
  `topology.Source`; they carry no `ip_addresses` now. The wire shape is
  otherwise unchanged.
- **Breaking for a host of `controllerapi`:** `Server.Start(ctx, backend)`
  is `Start(ctx, backend, topo)`, `topo` the `*topology.Service` built
  over the same controller (`topology.Changes.Install` on its
  configuration before `controller.New`, `topology.New` after); the host
  closes it after `Shutdown`.

## [0.4.9] - 2026-11-01

### Changed

- The add-on's entry in Home Assistant's sidebar is titled "Fabric" (the
  manifest's `panel_title`), as the go-*2mqtt add-ons name theirs; the
  add-on itself keeps its name.

## [0.4.8] - 2026-11-01

Dependency release: go-fabric `v0.15.6`, go-fabric-ui `v0.2.9`.

- go-fabric `v0.15.4` → `v0.15.6`: the camera's signals to a controller's
  requestor (`camera.Provider.Offer`, `Answer`, `IceCandidates`, `End`;
  go-fabric #351) and `camera.ErrPayloadTooLarge` (#356); a port's
  `contract.StatusError` answers its own status instead of FAILURE through
  the kit servers (#356); the colour conversions give the same values on
  arm64 as on amd64 (the add-on's aarch64 image); `bridge.ConnectPeer`; the
  device-side CASE initiator's address resolution logged at Debug; test and
  fuzz fixes. `protocol/schema` regenerated from the `v0.15.6` snapshot;
  nothing composed changes.
- go-fabric-ui `v0.2.8` → `v0.2.9`: the response schemas of the surface
  report are open to unknown fields again, so the web UI's client no longer
  rejects the whole `GET /node/surface` answer when go-fabric adds a field
  (go-fabric-ui #27); oapi-codegen runtime `v1.8.0`.

## [0.4.7] - 2026-10-31

### Added

- The add-on icon and logo (`addon/icon.svg`, `addon/logo.svg`, rendered to
  `icon.png` and `logo.png` with rsvg-convert): the server and the fabric's
  three threads with their node, go-fabric-ui's motif as go-ha2fabric's
  icon uses it. No Connectivity Standards Alliance mark.
- Option labels for the add-on's configuration page in English and German
  (`addon/translations/en.yaml`, `de.yaml`): the page shows "API port" and
  "Bridge node extensions" rather than the raw keys.

### Fixed

- `/ws` and `/bridge` ping every client every 30 seconds, so proxies and
  remote access relays no longer close idle connections; a client that
  answers neither a ping nor sends a message for three pings in a row is
  disconnected, a ping sent while the connection queues does not count
  (matterjs-server #1217, `WebSocketConnection.ts` at `0bd06f22`). The
  closes count in `ws.closed_overflow`.
- `set_default_fabric_label` with the label the server already holds, as
  Home Assistant sends on every connect, no longer pushes the label to
  every node again (matterjs-server #1227).
- A device with a test Certification Declaration, such as a go-ha2fabric
  bridge or a development kit, is commissioned again: the attestation
  policy is now matterjs-server's (`ControllerCommandHandler.ts:305-314`,
  `:2041-2052`), which refuses a test certificate the trust store does not
  accept (with the hint to enable `--enable-test-net-dcl`, the add-on's
  `enable_test_net_dcl`) and any error-level finding, and proceeds on info
  and warning findings. Before, go-fabric's `Reject` policy refused every
  finding, the test Certification Declaration included, so such a device
  failed with "Device attestation produced 1 finding(s) and was rejected by
  policy" even with the test roots trusted. Report S3's decision D4 was
  wrong about matterjs-server.

### Changed

- The matterjs-server pin of the `/ws` contract: `a7922e85` (1.5.5) →
  `0bd06f22` (1.5.6-alpha.0-20261031-362f4cc). The API documentation is
  unchanged between the two; the line citations into
  `ControllerCommandHandler.ts` follow the two insertions of #1227.

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
