# Report S — go-fabric-server on go-fabric `controller/topology`

Brief: `notes/briefs/2026-11-03-S-topology-on-go-fabric.md`. Branch
`s/topology-on-go-fabric`, cut from main 4091845. No release, no version
bump: the changelog's `[Unreleased]` only.

## §1 Dependency bumps and migrations

- go-fabric `v0.15.6` → `v0.16.1-0.20261103161943-af164eb2aa1e` (main at af164eb2).
- go-fabric-ui `v0.2.9` → `v0.3.1-0.20261103135546-2d326bdcf180`. That is the pseudo-version Go gives 2d326bdc, which follows the last tag, v0.3.0; the commit is v0.4.1 by the go-fabric-ui changelog.
- The build list moves with them (MVS, from go-fabric): sqlite 1.61.0, libc 1.79.0, go-humanize 1.1.0 and go-strftime 1.1.0. `grandcat/zeroconf` and `cenkalti/backoff` are no longer in the build list. `THIRD-PARTY-NOTICES.md` is updated to match.
- The ble modules stay at `ble/bluez v0.12.1` and `ble/esphome v0.13.10`.
- `make generate` regenerated `protocol/schema` (`GoFabricVersion` only). `bridgeapi/clusters_gen.go` did not change.

Migrations:

- `node.Observers`: the hooks return nothing (`bridgeapi/api.go` `Observers`).
- `Node.PairingCodes()` → `Node.SetupPayload()` `.QR`/`.Manual`. Changed in `bridgeapi/commands.go` `cmdPairingCodes` and in four tests: `controllerapi/{attestation,harness}_test.go`, `internal/daemon/{controllerui,proxy}_test.go`.
- `Node.CommissioningWindow()` → `Node.Window()` `.Open`/`.Own` (`bridgeapi/commands.go` `cmdNodeInfo`).
- `device.Options.Surface` is now a value, not `&policies` (`bridgeapi/declare.go`).
- `node.New(cfg)` → `node.New(ctx, cfg)`. The `contextcheck` nolint is dropped (`internal/daemon/run.go`).
- `slices.Concat(device.AheadRows(), node.AheadRows())` → `node.AheadRows()`, and the `device` import is dropped (`internal/daemon/surface.go`).
- `fabrictest.Device.Bridge` → `Device.BLE()`, the ready `bletest.Peripheral` (`internal/daemon/proxy_test.go` `proxyCentral`).
- `node.Network.WiFiCommissioning` → `fabrictest.UseWiFiCommissioning(cfg, drv)` (`internal/daemon/proxy_test.go`).
- `node_info` lint: gocritic `rangeValCopy` on `node.Topology().Devices`, whose element is now 176 bytes. The loop is indexed now (`bridgeapi/commands.go`).
- go-fabric-ui forced no migration: `labelled` embeds the `ControllerSource` and so inherits `NetworkTopology`.
- The stale `go-fabric-ui v0.2.1` strings in `web.go` and `controllerui.go` now read `v0.4.1`.

## §2 The service in the daemon

- `internal/daemon/run.go`:
  - `var changes topology.Changes; changes.Install(&cfg)` runs after `pageHooks.Wrap` and before `controller.New`.
  - `topo := topology.New(ctrl, topology.Config{Changes: &changes})` is built whether or not `--web` is set.
  - The deferred shutdown closes in this order: `topo.Close()`, then `pageHooks.Close()`, then `ctrl.Close`.
- `controllerSource(ctrl, topo, api, hooks, log)` passes `ControllerOptions{…, Topology: topo}`, so the pages' `/controller/topology` answers 200. `TestControllerPages` checks this: the commissioned node appears as a `peer`.
- **Shape (decision):** `controllerapi.Server.Start(ctx, b Backend, topo *topology.Service)`.
  - `Backend` is unchanged, a go-fabric controller.
  - `topo` is required: `nil` is an error, like a nil backend, so there is one shape and no half-wired mode.
  - The service is built after `controller.New` and `Options` is fixed at `New`, so it cannot be an option.
  - Start registers the `OnChanged` subscriber and the test-node source. `Shutdown` releases both before it flushes `server_shutdown`.
  - The three test harnesses build the service the same way: `controllerapi`, `bridgeapi` and `internal/daemon`.

## §3 `get_network_topology`

- `cmdGetNetworkTopology` answers `svc.Refresh(ctx)` when `refresh` is set and `svc.Topology()` otherwise. If the refresh fails, the error is logged and the graph as it stands is answered.
- `wireTopology(topology.Topology) networkTopology` is a pure mapping into the unchanged wire types:
  - `node_id` and `available` only for `KindMatter`; `is_bridge` only when true.
  - `hex16` ext address and ext PAN id when non-zero; `rloc16` from `*uint16`.
  - `lqi` only when `Network == thread`; `rssi` from `*int8`.
  - `ip_addresses` without the zone, deduplicated and string-sorted.
- Removed:
  - the server's own build: `refreshDiagnostics`, `topology()`, `buildTopology`, `topologySource`, `lqiStrength`, `rssiStrength`, `networkTypeOf`, `threadRole(s)`, `threadExtAddress`, `enumValue`, `neighbors`, `bssidOf`, `wifiSSID`;
  - the cache-pass trigger at `cache.go:307`;
  - `topoMu`/`topoSig`.
- `topologyChanged` is the `OnChanged` subscriber. It keeps the per-connection opt-in and `SendCoalescable("topology", …)`, and sends nothing once the server is closing.
- **Test placement (decision):** `TestBuildTopology` became `TestWireTopology` in `controllerapi/topology_internal_test.go`.
  - The same five dump-attribute nodes go through `testNodeImage`, then `topology.Build`, then `wireTopology`.
  - Every expected wire string of the old test is unchanged. Node 3 additionally carries zoned and duplicate addresses, which come out stripped and sorted.
  - So the old build and go-fabric's give the same wire output for these cases. go-fabric's own `Build` tests hold its rules.
- `TestNetworkTopologyCommand`, `compat_internal_test.go` and `test_python_client.py` are unchanged and pass.

## §4 Test nodes as a source

- `testNodeSource{s}.Images()` is in `controllerapi/testnodes.go`. It decodes each test node through `testNodeImage` (`cmd_topology.go`) into the typed states:
  - NetworkCommissioning: `Networks`, plus the FeatureMap.
  - GeneralDiagnostics: `NetworkInterfaces` `Type`/`HardwareAddress`.
  - ThreadNetworkDiagnostics: `RoutingRole`, `NetworkName`, `ExtendedPanId`, `NeighborTable` (ExtAddress, Rloc16, Lqi clamped to 255, AverageRssi/LastRssi nullable, RxOnWhenIdle, IsChild) and `Rloc16`.
  - WiFiNetworkDiagnostics: `Bssid`, `ChannelNumber`, `Rssi`.
- The decoding uses the schema-aware helpers that were kept: `member`, `entries`, `uintOf`, `intOf`, plus a new `bytesOf` and `int8Of`.
- Test nodes carry no `Addresses`.
- The source is registered once at `Start` and removed at `Shutdown`. Import and removal call `svc.Changed()` after `s.mu` is released (`topologyNodesChanged`).
- `TestTestNodeImage` covers:
  - the Thread dump of `TestNetworkTopologyCommand`: router, `Rloc16` 1024, the Thread feature;
  - a Wi-Fi dump: BSSID, RSSI, channel, SSID network, interface;
  - neighbour RSSI null handling and the LQI clamp.

## §5 Docs

- `docs/protocol.md`: the command row, the "network topology" section (the service, its timings, a refresh that fails, the shared graph with the pages, what is left out) and the "ignorable" list.
- `docs/by_design.md` B19: test nodes carry no `ip_addresses`.
- `CONCEPT.md`: what matterjs-server still does better.
- `web.go`: the handler comment.
- `changelog.md` `[Unreleased]` `### Changed`: the bumps, `node_info`'s `commissioning_window`, the topology service, and the breaking `Start` signature.

## Guards

| Guard | HEAD | Exit |
| --- | --- | --- |
| ci (build vet test lint) | d80ec05 (§1) | 2: lint, gocritic rangeValCopy `bridgeapi/commands.go:466`; fixed in 5209df6 |
| race | d80ec05 | 0 |
| cover-check | d80ec05 | 0 |
| generate-check | d80ec05 | 0 |
| version-check | d80ec05 | 0 |
| ci | c0e8da4 | 2: `internal/arch` TestImports, `controller/topology` not in the facade list; fixed in 1a5af8a |
| race | c0e8da4 | 2: the same TestImports |
| cover-check | c0e8da4 | 2: the same TestImports |
| generate-check | c0e8da4 | 0 |
| version-check | c0e8da4 | 0 |
| fuzz-smoke | c0e8da4 | 0 |
| compat-python (Python 3.13 venv, `COMPAT_BRIDGE=1`) | c0e8da4 | 0 (9 passed) |
| ci | 1a5af8a | 0 |
| race | 1a5af8a | 0 |
| cover-check | 1a5af8a | 0 (controllerapi 80.4 % / floor 76 %, bridgeapi 87.7 % / 84 %) |

generate-check, version-check, fuzz-smoke and compat-python were not re-run at 1a5af8a. The two commits after c0e8da4 only add an arch allow-list entry and index two loops, and touch neither the schema, the versions, the decoders nor the wire.

## Differences from the brief

- go-fabric-ui's pseudo-version is `v0.3.1-0.…-2d326bdcf180`, not a `v0.4.1-…` form. Go derives it from the last tag, v0.3.0.
- `internal/arch/arch_test.go` gains `controller/topology` in the facade allow-list, with a positive case. The brief's wiring needs the import, and the guard failed without it.
- `node_info` now always carries `commissioning_window`. `Node.Window()` returns a zero snapshot rather than nil, so absence can no longer be told apart. This is in the changelog.
- matterjs-server adds its test-node source lazily, on the first `get_network_topology` (`WebSocketControllerHandler.ts:2220-2230`). Here it is registered at `Start`, so test nodes are in the graph, and in the pages' graph, from their import on. The brief prescribes registering at Start.
- `THIRD-PARTY-NOTICES.md` drops zeroconf and backoff, which left the build list.

## Left out

The release and version bump, the border-router registry, route-table fields, `host_name`, the add-on, and any go-fabric or go-fabric-ui change.

## Open questions

- go-fabric `Changes.Install` schedules a rebuild on every `Changed` hook. matterjs-server does so only for an attribute change of its topology clusters (`NetworkTopologyService.ts:161-165`, `TOPOLOGY_CLUSTERS`). The 2 s debounce keeps the effect small, but it is a go-fabric-side difference to classify there.
- The pages' graph shows the commissioned fabrictest device as `state: unreachable` right after commissioning (seen in `TestControllerPages` output). Is this go-fabric-ui's mapping of `Available` from the connection state, or the fabrictest site? It is not asserted here.

## Cited lines (re-read at the pins)

matterjs-server 0bd06f22:

- `packages/ws-controller/src/server/WebSocketControllerHandler.ts:695-707`: `sendCoalescable("network_topology", …)`, latest wins.
- `WebSocketControllerHandler.ts:2220-2230`: `#handleGetNetworkTopology`, `addNodeSource(#testNodeSource)`, `refresh()` / `getTopology()`.
- `packages/ws-controller/src/controller/NetworkTopologyService.ts:72-81`: `TopologyNodeSource`, derivation inputs only.
- `NetworkTopologyService.ts:133-136`: the defaults 2000 / 60000 / 30000 / 4.
- `NetworkTopologyService.ts:161-174`: the observers and the periodic timer.
- `NetworkTopologyService.ts:188-194`: `addNodeSource`.
- `NetworkTopologyService.ts:196-205`: refresh is best effort.
- `NetworkTopologyService.ts:279-285`: `#emitIfChanged`.
- `NetworkTopologyService.ts:307`: the sources joined into `#build`.
- `NetworkTopologyService.ts:530-539`: `hashTopology`.
- `packages/ws-controller/src/controller/TestNodeCommandHandler.ts:228-241`: the placeholder addresses.

go-fabric af164eb2:

- `controller/topology` (`go doc`): `Changes.Install`, `Config` defaults, `New`, `AddSource` (compared with `==`), `Changed`, `OnChanged` (first snapshot included, must not call Refresh), `Refresh` (ctx error, shared flight), `Topology`, `Close`, `Image`, `Build`.
- `service.go:204-216` `images()` calls `Source.Images` outside the service lock.
- `node/observers.go:238-244` `Node.Window`.
- `CHANGELOG.md` `[Unreleased]` and `[0.16.0]`: the migration lines.

go-fabric-ui 2d326bdc:

- `fabricui/gofabric/gofabric.go:55-79` `ControllerOptions.Topology`; `:98-108` `ControllerOf`.
- `docs/embedding.md:125`: the `/controller/topology` wiring.
- `changelog.md` `[0.3.0]`–`[0.4.1]`.
