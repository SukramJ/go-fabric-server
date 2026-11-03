# Brief S — go-fabric-server on go-fabric `controller/topology` and go-fabric-ui v0.4.1

Date 2026-11-03. Owner decisions 2026-11-03 (wizard): (1) the server's
imported test nodes stay in its topology graph as a second source of
go-fabric's `topology.Service` (`Service.AddSource`, go-fabric pull request
of brief `m9-topology-sources`); (2) that go-fabric wave ran first; (3) this
wave **lands on main with pseudo-versions and is not released**: go-fabric
and go-fabric-ui are untagged until the owner's ecosystem test — no
`chore(release)` commit, no version bump, `[Unreleased]` only. Fact map:
`~/.cache/go-fabric-scratch/read-swave.md` (every `file:line` is a map —
verify what you build on). Repo `/home/markus/Dokumente/GitHub/go-fabric-server`,
worktree `../go-fabric-server-swave`, branch `s/topology-on-go-fabric`, on
main 4091845. One wave, one pull request, one hand-back. **Model:** Opus.
Pins to read: go-fabric at `af164eb2aa1e7a14fe5075d5f7a439378a61d126` (`/home/markus/Dokumente/GitHub/go-fabric`
main; `go doc github.com/SukramJ/go-fabric/controller/topology` after the
bump), go-fabric-ui at 2d326bdc (`/home/markus/Dokumente/GitHub/go-fabric-ui`
main; `fabricui/gofabric` package doc for the host wiring, `docs/embedding.md`
"The controller family"), matterjs-server 0bd06f22
(`git -C ../matterjs-server show 0bd06f22:<path>`). Both sibling checkouts are
read-only; a gap in either is an open question in the report, not a patch.

## 1 Dependency bumps and migrations (first commit, guards green before §2)

- `go.mod`: `go get github.com/SukramJ/go-fabric@af164eb2aa1e7a14fe5075d5f7a439378a61d126` (pseudo-version
  `v0.16.1-0.<date>-af164eb2aa1e7a14fe5075d5f7a439378a61d126…`) and
  `go get github.com/SukramJ/go-fabric-ui@2d326bdc` (pseudo-version of v0.4.1);
  `go mod tidy`. The ble modules stay (`ble/bluez v0.12.1`, `ble/esphome
  v0.13.10` are their newest tags).
- Migrations: read go-fabric `CHANGELOG.md` from `## [Unreleased]` to
  `## [0.15.6]` (lines 11-203 at c91e5b7f; the `### Changed`/`### Removed`
  sections of `[Unreleased]` and `[0.16.0]` name every API move with its
  migration line) and go-fabric-ui `changelog.md` `[0.3.0]`–`[0.4.1]`
  (`### Changed` at :35, :68, :81; contract 0.2.0 → 0.3.1, `fabricui.Options`
  and `gofabric.ControllerOptions` moves). Apply each migration the build or
  `make lint` demands; each one is a line in the report (what moved, where).
- `make generate` (`internal/gen`: `protocol/schema` and
  `bridgeapi/clusters_gen.go` from go-fabric) — commit what it regenerates in
  this commit; `make generate-check` green.
- `changelog.md` `[Unreleased]` `### Changed`: one line per bump, naming the
  pseudo-versions and that the next release lifts them to tags.
- The stale strings `go-fabric-ui v0.2.1` in `internal/daemon/web.go:20` and
  `internal/daemon/controllerui.go:19` → `v0.4.1`.

## 2 The topology service in the daemon (`internal/daemon/run.go`)

- Build the service regardless of `--web` (the WS command needs it):
  between `cfg.HooksFor = pageHooks.Wrap(...)` (`run.go:247-251`) and
  `controller.New` (`:252`): `var changes topology.Changes;
  changes.Install(&cfg)` — after the Wrap, so both wrap the same chain;
  after `controller.New`: `topo := topology.New(ctrl, topology.Config{Changes:
  &changes})` (defaults are matterjs-server's); in the deferred shutdown
  (`:256-263`) `topo.Close()` before `pageHooks.Close()` and `ctrl.Close`.
- `controllerSource` (`controllerui.go:25-33`) takes the service:
  `ControllerOptions{..., Topology: topo}` — the pages' `/controller/topology`
  answers 200 from then on (go-fabric-ui's adapter maps it; `Hooks.Close`
  releases the adapter's `OnChanged` subscription).
- `controllerapi.Server` reaches the service: `Start(ctx, ctrl)` stays
  the `Backend`; the service is a second argument or an option (`api.Start(ctx,
  ctrl, topo)` or `controllerOptions(...).Topology`) — choose the one shape
  that keeps `Backend` (`controllerapi/backend.go:19-29`) a go-fabric
  controller and lets the tests' `newEnv` build a service over the
  fabrictest controller the same way. Say which in the report.

## 3 `get_network_topology` on go-fabric (`controllerapi/cmd_topology.go`)

- `cmdGetNetworkTopology` (`:106-117`): `refresh` → `svc.Refresh(ctx)`
  (its error is logged, the snapshot is still answered — matterjs-server's
  refresh is best-effort, `NetworkTopologyService.ts:196-205`), else
  `svc.Topology()`; the result mapped by a pure `wireTopology(topology.Topology)
  networkTopology` into the existing wire types (`:49-83`, unchanged on the
  wire — the compat fixtures `protocol/testdata/compat/get_network_topology*`
  and `python/network_topology_updated.event.json` must still round-trip):
  `collected_at` = `CollectedAt.UnixMilli()`; `node_id`, `available` only for
  `KindMatter` (pointers), `is_bridge` only when true; `ext_address`/
  `ext_pan_id` as `hex16` when non-zero; `rloc16` from `*uint16`; `network_type`,
  `kind`, `role`, `strength`, `network` are the same strings; `ip_addresses`
  = `Addresses` as strings with the zone stripped (`netip.Addr.WithZone("")`),
  string-sorted, as `cmd_node.go:129-146` renders a node's; `lqi` only on
  Thread directions (`Network == NetworkThread`; go-fabric's `Direction.LQI`
  is 0 on Wi-Fi), `rssi` from `*int8`.
- `refreshDiagnostics` (`:122-137`), `topology()` (`:139-164`),
  `buildTopology` (`:166-`) and the helpers it alone uses (`lqiStrength`,
  `rssiStrength`, `neighbors`, `bssidOf`, `wifiSSID`, …, `:332-557`) go —
  except what §4 keeps for the test-node images. `cache.go:307`'s
  `s.topologyUpdated()` after a cache pass goes (the service rebuilds on the
  node hooks); the `topologyUpdated` sender (`:559-592`) becomes the
  `svc.OnChanged` subscriber: it keeps the per-connection opt-in
  (`conn.topology`, `conn.go:35-38`) and sends
  `network_topology_updated` with `SendCoalescable("topology", …)`; the
  server's own hash gate (`topoSig`) goes — the service's `OnChanged`
  already emits only a changed snapshot (`hashTopology`,
  `NetworkTopologyService.ts:530-539`). The first `OnChanged` snapshot goes
  to opted-in connections like any other (none exist before the first
  `get_network_topology`, so nothing is sent).
- `TestBuildTopology` (its cases become `wireTopology` cases on a
  `topology.Topology` literal, or go where go-fabric's `Build` tests cover
  them — say which), `TestNetworkTopologyCommand` (`s3_test.go:191-215`)
  unchanged in intent: it must still pass as written (a commissioned node,
  `refresh: true`, one `matter` node with its decimal id; an imported Thread
  test node → `network_topology_updated` with `"role":"router"`,
  `"rloc16":1024`). `compat_internal_test.go:164-165` and
  `test/compat/test_python_client.py:185` unchanged.

## 4 Test nodes as a `topology.Source` (`controllerapi/testnodes.go`)

- `type testNodeSource struct{ s *Server }` with `Images() []topology.Image`:
  one image per `testNodeListLocked()` entry — `NodeID`, `Available`,
  `IsBridge` from the dump, `NetworkFeatures` from `0/49/65532`,
  `ThreadNetworkDiagnostics` (`RoutingRole` `0/53/1`, `NetworkName` `0/53/2`,
  `ExtendedPanId` `0/53/4`, `NeighborTable` `0/53/7` with `ExtAddress`,
  `Rloc16`, `Lqi`, `AverageRssi`/`LastRssi`, `RxOnWhenIdle`, `IsChild`,
  `Rloc16` `0/53/64`), `GeneralDiagnostics.NetworkInterfaces` `0/51/0`
  (`Type`, `HardwareAddress`), `WiFiNetworkDiagnostics` (`Bssid` `0/54/0`,
  `ChannelNumber` `0/54/3`, `Rssi` `0/54/4`), `NetworkCommissioning.Networks`
  `0/49/1` — decoded from the dump's JSON (`map[string]json.RawMessage`) with
  the schema-aware helpers `cmd_topology.go` already has (`member`,
  `entries`, `enumValue`, `intOf`, `threadExtAddress` — keep what the
  decoder needs, drop the rest); `Addresses`: none (go-fabric's
  `netip.Addr` cannot hold the dump's placeholder `testNodeAddresses`;
  `ip_addresses` is then absent on test nodes — a wire difference from
  today, allowed: no fixture pins it; say so in the changelog line).
- Registered once at `Start` (`svc.AddSource(testNodeSource{s})`), the
  remove function called at stop; `cmdImportTestNode` and the test-node
  removal call `svc.Changed()` where they called `s.topologyUpdated()`
  (`testnodes.go:109`, `:251`).
- A unit test: a dump with the three attributes of `TestNetworkTopologyCommand`
  → one image with `RoutingRole` router, `Rloc16` 1024, Thread feature; a
  Wi-Fi dump → BSSID and RSSI.

## 5 Docs

`docs/` pages naming `get_network_topology` or the topology (grep), the
`by_design` note B19 (`ip_addresses`) if the test-node change touches it,
`internal/daemon/web.go` comment; `changelog.md` `[Unreleased]` `### Changed`
("the topology is go-fabric's `controller/topology`: debounced 2 s,
periodic 60 s, refresh concurrency 4 as matterjs-server; test nodes carry no
`ip_addresses`"). No version bump (decision 3).

## 6 Guards

`make ci` (build vet test lint), `make race`, `make cover-check`,
`make generate-check`, `make version-check` (unchanged versions must still
agree), `make compat-python` (`PYTHON` venv per the target's doc, `COMPAT_BRIDGE=1`
if the environment allows; say if it could not run), `make fuzz-smoke`,
all in the background under `flock ~/.cache/go-fabric-scratch/guards.lock`
with the summary log `~/.cache/go-fabric-scratch/swave/summary.log` (HEAD
before and after, one `exit <name> <code>` line per guard); a guard's log
only on failure, with grep. Commits: conventional, English, no attribution
or co-author lines; `git var GIT_AUTHOR_IDENT` must show
`SukramJ <sukramj@icloud.com>` before each commit; commit with
`-c user.name=SukramJ -c user.email=sukramj@icloud.com`.

## 7 Out of scope

The release (owner tags first), the border-router registry, route-table
fields, `host_name`, any go-fabric or go-fabric-ui change, the add-on.

## Hand-back

Push, open the pull request (no auto-merge, no labels, body = report
`docs/reports/S-topology.md` or the repo's report place), hand back: per §,
migrations applied (one line each), guards table (guard, HEAD, exit),
differences from the brief with reasons, decisions (§2 shape, §3 test
placement), left out, open questions.
