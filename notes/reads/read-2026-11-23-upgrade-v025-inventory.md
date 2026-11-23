# Inventory: go-fabric-server → go-fabric v0.25.0 (thingfabric.dev) + go-fabric-ui v0.13.0

Host `/home/markus/Dokumente/GitHub/go-fabric-server` @ d61a545 (go-fabric v0.22.2, ui v0.9.2, bluez v0.22.1, esphome v0.22.2,
wpasupplicant v0.22.1; Version 0.10.0). Read-only: everything was tried in a detached scratch worktree (removed again; repo
clean, `git worktree list` = main only). Tags: **[build]** = build/vet/lint/test in scratch, **[code]** read, **[inferred]**.
`gf:N` = line N of `…/scratchpad/gf-CHANGELOG.md` (= `git show v0.25.0:CHANGELOG.md`); `ui:N` = go-fabric-ui `changelog.md` @109cf14.
This is a map (CLAUDE.md "Delegate first reads"): every go-fabric / go-fabric-ui fact below needs the Fable fact-note check
before it enters the brief, `docs/protocol.md` or `docs/by_design.md`.

Not run: `make race`, `cover-check`, `fuzz-smoke`, `vuln`, `licenses`, `secrets`, `release`, `docker`, `addon-image`,
`compat-python`. Not read line by line: the ~600 non-breaking bullets of gf 0.23.0 (keyword scan by facade package + all
"Breaking" bullets + the test run instead).

## 1. Dependency / path move, generator

1. One-liner (gf:23-28) over `*.go` + `go.mod`: 66 files, 305 lines **[build]**. Then delete the four rewritten
   `thingfabric.dev/...` lines in `go.mod` (they carry v0.22.x, which does not resolve) and
   `go get thingfabric.dev/go-fabric@v0.25.0 thingfabric.dev/go-fabric/ble/bluez@v0.25.0 thingfabric.dev/go-fabric/ble/esphome@v0.25.0 thingfabric.dev/go-fabric/wifi/wpasupplicant@v0.25.0 github.com/SukramJ/go-fabric-ui@v0.13.0`, `go mod tidy`, `make fmt`.
   Result **[build]**: old path leaves the graph entirely; indirect bumps oapi-codegen/runtime 1.8.0→1.9.0, x/crypto 0.58→0.59,
   x/sys 0.49→0.50, modernc sqlite 1.61.0→1.61.1.
2. go-fabric-ui keeps its path `github.com/SukramJ/go-fabric-ui`; v0.13.0 requires `thingfabric.dev/go-fabric v0.25.0` (its go.mod:5-7).
3. Strings the one-liner does rewrite, but check: `internal/version/version.go:30`, `internal/gen/snapshot.go:139`
   (`d.Path == …` build-info lookup; unrewritten → `GoFabric()` "unknown" / generator loses the version),
   `internal/gen/bridge.go:24` `goFabricModule`, `internal/arch/arch_test.go:23` `goFabric` (unrewritten → the guard matches
   nothing and passes everything silently).
4. **Not** covered by the one-liner: `test/compat/run.sh:37-38` (`go list -m … github.com/SukramJ/go-fabric`, `go install
   …/examples/reference-bridge@$gofabric`) — `make compat-python` with `COMPAT_BRIDGE=1` breaks until changed by hand.
   Docs/links: `THIRD-PARTY-NOTICES.md:4,17,23,29,33,39` (+ wpasupplicant block; versions, source URLs →
   `github.com/thingfabric/go-fabric`), `README.md` (3), `docs/operations.md` (7), `docs/protocol.md` (1), `CONCEPT.md` (1),
   `CLAUDE.md:7` (link) and its dependency paragraph. `internal/daemon/run.go:685` comment names the old bluez path (rewritten).
   Makefile, Dockerfiles, workflows, `.golangci.yaml`: own module path only — no change.
5. `make generate` **[build]**: needed (`internal/gen TestCommittedOutputIsCurrent` fails before). Diff is header-only:
   `protocol/schema/schema.json` and `declaration.schema.json` `x-go-fabric-version` v0.22.2→v0.25.0 and
   `x-matter-source-commit` db021da0…→b25ed960…; `tables_gen.go` `GoFabricVersion`, `SourceCommit`;
   `bridgeapi/clusters_gen.go` import paths only (264 lines). No id/name/type table change. The parity snapshot's additions
   (`attributeOrder` gf:560, `isSecret` marks and the UnitLocalization constraint gf:137) are not carried by `internal/gen`
   → no wire/schema content change; see Q9 (isSecret for redaction).
6. `/schema` and `server_info` therefore report go-fabric v0.25.0 / matter.js b25ed960 — user-visible, changelog line.

## 2. Compile breaks and SA1019

Compile breaks after step 1 **[build]** — exactly one site + its test:
| Where | Break | Fix |
| --- | --- | --- |
| `internal/daemon/noderadio.go:32,121,125` (`newPeripheral = bluez.NewPeripheral`), field `:108` | `bluez.NewPeripheral` returns `contract.GATTPeripheral` (gf:481); not assignable to `contract.BLEPeripheral` | field type `contract.GATTPeripheral` |
| `internal/daemon/noderadio_test.go:53,57` | stub signature | same; `bletest.Radio` satisfies the new port **[build]** |

SA1019 (owner rule: none, tests included) — deprecated at gf:857, "removable in v0.25.0":
| Where | Identifier | Replacement |
| --- | --- | --- |
| `internal/daemon/noderadio.go:108`, `noderadio_test.go:53,57` | `contract.BLEPeripheral` | `contract.GATTPeripheral` (gf:420) |
| `internal/daemon/noderadio.go:163` | `node.Network.BLE` | `node.Network.GATT` (gf:420, :481; `Validate` refuses both set, gf:670) |
| `internal/daemon/noderadio_test.go:222-234` (5 reads of `n.BLE`) | `node.Network.BLE` | `n.GATT` |
After these: `go build`, `go vet` clean; golangci-lint 2.14 (`staticcheck`, `tests: true`) reports **0 SA1019** **[build]**.
Comments to follow: `run.go:80`, `noderadio.go:24,99-106` (cite `bridge/ble.go StopBLE`, `peripheral_linux.go Serve:
ErrServing` — semantics moved to `RegisterService`/`Serve`, gf:481, :156; Fable re-read), `arch_test.go:82`, changelog 0.10.0 text stays.
No other deprecated identifier of gf:180-192 / :836-880 is used (softwareupdate, OTA, `spec.Hooks`, `NewRootNode`,
`SupportedFabrics`, `EndpointPersistence`, `schema.ClusterCommandLevels`: no hit) **[build + grep]**.

Lint (errcheck) **[build]** — `Device.SetReachable/Rename/Update` return `error` since gf:785:
`bridgeapi/commands.go:315` (`Update`), `:328` (`SetReachable`), `:331` (`Rename`), `bridgeapi/declare.go:178` (`SetReachable`).
These are /bridge `update`/`declare` answers: the error (store refusal of a label, `device.ErrNotBridged`) must become a
command error, not a log line → wire decision, `docs/protocol.md` (Q6).

Tests failing after the above + `make generate` **[build]** (everything else green: controllerapi, protocol, arch, gen):
| Test | Cause | Kind |
| --- | --- | --- |
| `bridgeapi TestSurfaceWithheldReplaced` (`surface_test.go:194`) | `Batch.Add` now checks the duplicate key **before** the withheld record (go-fabric `node/aggregator.go:445-452` @v0.25.0 vs `:415-425` @v0.22.2; "never for a key it serves", R2-P6-store-2414) → `bridgeapi/commands.go:129-140` `replace` (add withheld, then Remove) gets `ErrDuplicateKey` | **product bug on /bridge**: re-declaring a served key as a withheld type fails. Fix: `Remove` first, then `add`; passes in scratch **[build]**; endpoint-number retention to be re-read (Fable) |
| `bridgeapi TestSurfaceExtensionEnabled` (`surface_test.go:26-28,70`) | fixture `scheduleTypeFeatures:0`; ScheduleTypes entries must support presets or setpoints (gf:148) | fixture; also a refusal a /bridge client now sees |
| `internal/daemon TestWebAdmin` (`daemon_test.go:346,351,361`) | `httptest.NewRequest` Host `example.com` → 403 `host_not_allowed` (ui:338) | test: loopback/IP Host |
| `internal/daemon TestRunRegistry` (`daemon_test.go:229`) | page write without `X-Fabric-UI: 1` in host mode (ui:331) | test helper `pageWrite` (`multinode_test.go:426`), `controllerui_test.go:155` |
| `internal/daemon TestTwoNodes` (`multinode_test.go:260`) | `config.store` hidden without admin rights (ui:322-330) | test reads as admin |

## 3. Behaviour changes that touch this server

Store / persistence
| Change | Server side | Do |
| --- | --- | --- |
| Device store schema v7→v12 (gf:513, :635, :647, :693, :142) | every node: `node.Store{Path}` `run.go:483`; no own backend, no pinned `Migrations()` | nothing in code; `Upgrade` at start. **One-way** for operators (old binary on a v12 file not tested). Changelog + `docs/operations.md` + `addon/CHANGELOG.md` backup warning |
| Controller registry `sqlstore` v1→v3 (gf:483) + controller store v12 | `controller.OpenDB` `run.go:277`, `controller.New` `:319` | same: one-way, backup warning for `fabric-server.db` |
| Controller image quota per node, paths outside the node's structure dropped, stored image cut at load (gf:541, :352 `Config.MaxImageBytesPerNode`; gf:124) | `/ws` node images: `controllerapi/cache.go:66-…`, `start_listening`/`get_nodes` | visible on `/ws`: a large node's attributes can stop growing (`matter.controller.client.image_quota_reached`). Decide default vs flag; `docs/protocol.md` note (Q7) |
| `controller.Config.Sealer` opt-in (gf:304); seals CA, fabric keys, resumption, kept onboarding codes | `run.go:285` sets none | Q4 (ties to #70) |

Controller `/ws`
| Change | Server side | Do |
| --- | --- | --- |
| `Node.PeerAddress` answers for a node that failed to load; "commissioned" is `IsCommissioned` (gf:109) | `controllerapi/cache.go:67-70` reads `ok` as commissioned; called unguarded from `api.go:254` (guarded at `cache.go:367`) | guard with `IsCommissioned` or decide to list load-failed nodes — wire-visible (`get_nodes`), compare matterjs-server |
| `Registry.Decommission` on a load-failed node fails `ErrLoadFailed`, keeps data (gf:110) | `remove_node` `cmd_node.go:94`; `s.node` (`cmd_node.go:36`) already answers NodeNotExists for such a node | none expected; confirm no path to delete a load-failed node is needed (Q8) |
| `PingAddress` refuses non-record addresses (gf:805) | `ping_node` `cmd_node.go:235` passes `n.Record().Addresses` | none |
| `Node.Stop` reports `SubscriptionStatusChanged(false)` (gf:806); sustained-subscription re-establish backoff 15 s… (gf:779); per-session report bound counts memory (gf:775); CASE responder cap 5/peer (gf:540) | `controllerapi/events.go`, `cmd_subscribe.go` | no code; possible extra availability events on `/ws` → run `make compat-python` |
| Fabric label ≤ 32 **bytes** (gf:759); string bounds in UTF-8 bytes (gf:106) | `set_default_fabric_label` `cmd_server.go:268-280`, `--default-fabric-label` | check `normalizeLabel` cuts/validates bytes; error text |
| `attestation.Data.VendorID/ProductID` `*uint16`, `attestation.Error` not comparable (gf:114-115); CD mismatch and >900-byte elements are policy findings (gf:116-117); DAC findings (gf:760) | `controllerapi/attestation.go`, `cmd_commission.go` | compiles; commissioning outcomes can differ (policy decides) → Fable read of the commissioning path (CLAUDE.md row) |
| Commissioner: disarm before PASE close, fail-safe end only if the device answers, race stagger (gf:546, :151, :545, :761) | `commission_with_code`, `commission_on_network` | none; compat run |
| `controller/camera`: `ErrClosed` after `Close` (gf:810), signals bounded 1024/1 MiB → `EndSignalOverflow` (gf:510), concurrent `StartStream` one session (gf:939) | `cmd_camera.go:127,215,246,528`, `camera_errors.go:22-50`, `camera_events.go` | #73 item; map `EndSignalOverflow` in the observer if it switches on end reasons |
| `logring` Any attrs kept as strings (gf:701); topology/diag escape control chars (gf:808); topology duplicate ext-address rule (gf:809) | `internal/daemon/operation.go`, `cmd_topology.go` | none |
| DCL: public addresses only without own HTTPClient, redirect rules, bounds (gf:644, :555, :766) | `get_vendor_names`, attestation store `run.go:600-620` | none; a DCL mirror on a private address would stop working — operations note |

Bridge `/bridge`
| Change | Server side | Do |
| --- | --- | --- |
| `Batch.Add` duplicate check before withheld | §2 | fix `commands.go:129-140` |
| Controller write that changes nothing: no DataVersion, no report, host not told (gf:620-621) | `handle_writes` events `bridgeapi/hooks.go` | a /bridge client no longer gets a write event for an unchanged value → `docs/protocol.md` |
| `device.New` refusals: required Initial for bare Fan/ModeSelect/RVC/Camera/… (gf:665), optional command declared without handler (gf:487), BridgedDeviceBasicInformation bounds on name/vendor (gf:787), struct conformance of Initial/Set (gf:731), thermostat schedules (gf:148) | `bridgeapi/declare.go:160-176` → InvalidArguments with the kit's text | declarations that passed at 0.10.0 can be refused; changelog "Changed" + protocol note; `examples/` and `client/` declarations re-checked (tests green apart from §2) |
| BooleanState serves ChangeEvent by default (gf:563); WindowCovering unknown position = null (gf:564); bridged SerialNumber/NodeLabel only when stated (gf:723) | what controllers read from declared devices | protocol/changelog note |
| mDNS `DT`/`_T` inferred from the first bridged device when `ProductDescription.DeviceType` is zero (gf:562) | `run.go:478` leaves it zero | Q5 (set Aggregator) |
| `LocationCapability` zero = IndoorOutdoor (gf:559); BootReason Unspecified (gf:570) | `run.go:473-477` | Q5 |
| Fresh salt per window, `Commissioning.Salt` unused (gf:817) | `run.go:432-435,476` | #73 |
| `Config.Validate`: BasicInformation strings by constraint (gf:794) | `run.go:480-481` NodeLabel/SerialNumber | #73 |
| `Forget` erases stored UserLabel/nonvolatile values (gf:532, :797); reassembly that removes an endpoint ends no subscription (gf:566); `Stop` waits for a `Start` only until ctx (gf:515) and expires an armed fail-safe (gf:793); min MaxInterval 2 s (gf:565) | declare's `bt.Forget` `commands.go:191`; `nodeLifecycle.erase` (`nodes.go`) | no code; erase budget path re-read against gf:515 |

BLE / Wi-Fi
| Change | Server side | Do |
| --- | --- | --- |
| Peripheral on `contract.GATTPeripheral`: service registered before advertising, refusal is the start's error (gf:420, :481); a central between register and serve is held ≤ 5 s (gf:156) | `noderadio.go:95-106` comment, erase/replacement logic `nodes.go` | comment + behaviour re-read: a replacement node on the same radio now fails `Start` (`ErrServing` = second registration) instead of a Warn — check against `nodeLifecycle.erase` |
| `wpasupplicant.Driver` value not comparable, `*Driver` is (gf:815); node hands the driver unwrapped (gf:799) | `noderadio.go` `wifiPort` holds the pointer | none **[build]** |
| BLE scanner bounded 8192 (gf:547, :773) | `--bluetooth-adapter/-proxy` | none |

HTTP / metrics
| Change | Server side | Do |
| --- | --- | --- |
| go-fabric-ui Host check, admin gates, header | §4 | §4 |
| go-fabric's reference bridge stopped publishing expvar `cmdline`/`memstats` (gf:261: flags carry the passcode) | `internal/daemon/http.go:103` `expvar.Handler()` publishes `cmdline` — `--bridge-passcode`, `--token` given as flags — on the API listener (behind the token only when one is set); `docs/operations.md:291` | not caused by the bump, same defect class → Q10 (fix here or own issue) |
| Metric names | `expvarmetrics.New("fabricserver.")` `run.go:706` | no rename seen in gf; `docs/operations.md:291` list re-checked by Fable if new counters are to be documented |

## 4. go-fabric-ui v0.10–v0.13 host wiring

Today: `web.go:36-58` `fabricui.Options{Events, Logger, Controller, Operations, ExposureReason, Node, NodeConfig, Erase, Registry,
Warnings, Admin}` → `fabricui.Handler(o)` (`:58`); add-on wraps it in a source filter (`:60-73`, `--web-admin-source`,
`script/run.sh:56` `FABRIC_SERVER_WEB=0.0.0.0:8099`, `addon/config.yaml:33,44-45` host_network + ingress). Controller pages:
`controllerui.go:30-38` `gofabric.ControllerOf(…ControllerOptions{Hooks, Credentials, Pair, Logger, Topology})`, wrapped by
`labelled` (embeds the source → the three new methods of ui:18-25 come through, compiles **[build]**).

| Hosts item | Must do | Conf. |
| --- | --- | --- |
| Host check on every route (ui:338-347) | Add-on: `Options.AllowHost = fromAddr(r, src)` (same predicate as `webAdmin`, `web.go:97-104`). The Ingress forwards the browser's `Host` and `Origin` unchanged, peer 172.30.32.2 — sibling fact note `go-ha2fabric/notes/reads/read-2026-11-23-ingress-host-facts.md` (core 2026.10.3, Supervisor main); same `host_network` add-on shape. Without it every Ingress request is 403. Stand-alone: IP/`localhost` pass; a DNS name needs `Options.AllowedHosts` → new flag `--web-allowed-hosts` / `FABRIC_SERVER_WEB_ALLOWED_HOSTS` (Q2) | code + sibling |
| Origin on admin routes (ui:348-351) | holds with `AllowHost` when Origin = Host (fact note); owner's live Ingress test before any tag | inferred |
| `X-Fabric-UI: 1` in host mode (ui:331-337) | shipped page sends it; test helpers must (`multinode_test.go:426`, `controllerui_test.go:155`, `daemon_test.go:324-361`) | build |
| Admin-only reading routes (ui:322-330) | **product change**: `--web` without `--web-admin`/`--web-admin-source` is status-only (43 reads + controller/log streams 403); flag help `run.go:174` "read-only unless…", README, `docs/operations.md` web section, `docs/addon.md:50` to rewrite (Q1) | build |
| Strict JSON bodies (ui:352-357) | tests send `Content-Type`; re-check after header fixes | code |
| `Handler` panics / `NewHandler` returns error (ui:375), `Options.Done`, `MaxStreams` (ui:377-381) | `web.go:58` → `fabricui.NewHandler(o)`, return the error (needed once `AllowedHosts` is user input); `Done: ctx.Done()` so streams end before the 5 s `Shutdown` (`web.go:133-137`) | code |
| `Options.Events` is not the handler's hub (ui:362-366) | daemon only publishes into `ev` — check no read of it in `nodes.go`/`registry.go` | inferred |
| Unfabric'd node reopens its own window (ui:243-250) | document, or go-fabric `Commissioning.WindowOnDemand` in `run.go:473` (Q5) | code |
| `PairOptions.CommissionTimeout` 600 s (ui:95-100) | adapter path: `api.PairOptions()` template keeps a shorter own bound; compare with `/ws` `commission_*` bounds for one policy | code |
| `ErrICDMultiAdmin`, `ErrNotImplemented/ErrNotRead` only where declared (ui:106-114) | server has no own `ControllerSource` methods except `labelled.SetFabricLabel` (`controllerui.go:50-57`) → api-391 below | code |
| Onboarding inventory (ui:18-34, embedding.md:412-470) | #70, §5 | code |
| `UpdateCheckFailures` (ui:251-257) | no SoftwareUpdateSource of the server's own; adapter's — nothing | code |
| 32-bit int64 (ui:261-267) | release matrix has `linux/arm/v7` (Makefile:18): handler side fixed by the bump, nothing to do | code |
| Contract 0.9→0.13 | `docs/operations.md` still says go-fabric-ui 0.7.1/0.5.0 in comments (`web.go:19`, `controllerui.go:20`) | code |

api-391 (ui#85): concretely relevant — `labelled.SetFabricLabel` returns `fabricui.ErrConflict` for a pinned label
(`controllerui.go:54`); `setFabricLabel` declares 204/400/403/413/415/500 only (go-fabric-ui `contract/openapi.yaml:2034-2048`),
so the server is one of the "host source answers an undeclared 409" cases. Either go-fabric-ui declares 409 there (ui issue) or
the server reports the pin another way (capability). `credentialError` (`controllerui.go:95-104`) uses declared kinds — check.
test-403: the server is the host with go-fabric's real update manager behind the adapter; a fixture (image source refusing the
download) would live in `internal/daemon/controllerui_test.go`. Both are review-plan items, not blockers of this wave.

## 5. Issues

**#70 (onboarding inventory)** — now possible (go-fabric gf:307, ui v0.13.0):
- flag `--controller-keep-onboarding-codes` (`FABRIC_SERVER_…`, default off) → `controller.Config.KeepOnboardingCodes` (`run.go:285-291`)
  and `gofabric.ControllerOptions.OnboardingCodes` (`controllerui.go:31-37`); one switch or two is Q3.
- go-fabric-ui serves the three routes only with `Options.Admin` (501 in token mode, ui:31-34): in practice the inventory exists
  under `--web-admin-source` (Ingress) only. Document; do not work around.
- No logging/caching of the routes: the daemon has no HTTP middleware on `--web` (`web.go:124-132`) — state it in the report;
  `Cache-Control: no-store` is the handler's.
- `docs/operations.md`: the controller DB then holds one onboarding code per node, a secret of the class of the fabric keys;
  backups need the same protection; sealing (Q4).
- Open point "inventory over `/ws`": matterjs-server has none → would be "going ahead", recorded in `docs/protocol.md`; Q3.
- Test: `controllerui_test.go` env with `fabrictest` pairing by code, inventory read as admin, 501 in token mode.

**#73 (go-fabric #538)** — all three still open at d61a545:
- NodeLabel > 32 bytes fails `node.New` (gf:794): `run.go:480` `NodeLabel: s.Label`; `--bridge-label` `run.go:157` unbounded;
  registry label up to 64 characters (go-fabric-ui `fabricui/registry.go:28-30,107` @v0.13.0, counted in runes). Options: refuse
  at the flag + cut the NodeLabel to 32 bytes on a rune boundary for registry nodes (label stays the registry's) — Q6b.
  `Rename` path (`registry.go:260-284`, `nodes.go:145` `SetNodeLabel`) already validated to 32 bytes by go-fabric → a 33–64-char
  rename currently fails after the registry wrote it; fold into the same rule. Stored over-long labels are cut by go-fabric (gf:107).
- `camera.ErrClosed`: add a case to `cameraError` (`controllerapi/camera_errors.go:22-50`) before the `nodeError` fall-through;
  which matterjs-server code a stopped service answers is a reference fact (Fable).
- `Commissioning.Salt`: drop `bridgeSalt` (`run.go:432-435`, `:476`) — recommended, a constant that answers nothing misleads —
  or keep with a comment; check `Validate` accepts an empty Salt (Fable, gf:817 says "still validated").

## 6. internal/arch allowlist (`internal/arch/arch_test.go`)

- `:23` `goFabric` → `thingfabric.dev/go-fabric/` (one-liner). The module root import (`thingfabric.dev/go-fabric`, 3 files) is
  not matched by the trailing-slash prefix — today too; fine (root is facade), but say so in a comment.
- Unused entries that can go **[grep at d61a545]**: facade `client/interaction`, `client/peer` (no import anywhere);
  testOnly `bridge/bridgetest`, `transport/btp` (no import anywhere).
- `thread/brdiscovery` is imported by a test only (`controllerapi/topology_internal_test.go:16`) → move from `facade` to `testOnly`.
- Comment updates: `:79-83` (`node.Network.BLE` → `GATT`), `:70-74` unchanged.
- No new entry needed for the wave as scoped. A sealer (Q4) would need `store` (for `store.NewAESGCMSealer`), which is below the
  facade today → a facade-gap report to go-fabric rather than an allowlist entry.
- `exceptions` stays empty.

## 7. Commit split (one PR, branch `chore/go-fabric-v0.25.0`), guards

1. `notes/briefs/<date>-S14-gofabric-0.25.0.md` (brief; this inventory as `notes/reads/read-<date>-upgrade-v025-inventory.md`).
2. `chore(deps)`: one-liner, go.mod/go.sum, `make fmt`, `make generate` output, `test/compat/run.sh`, `arch_test.go` (path, stale
   entries), GATT port (`noderadio.go`, test), THIRD-PARTY-NOTICES. Gate: build, vet, 0 SA1019, `internal/gen` green.
3. `fix(bridge)`: `replace` order (`commands.go:129-140`), errors of `Update/SetReachable/Rename` (`commands.go:315-331`,
   `declare.go:178`), thermostat fixture, node config decisions (DeviceType, Salt, NodeLabel bound) — #73 bridge parts.
4. `fix(controller)`: `cache.go:67` commissioned check, `camera.ErrClosed` mapping (#73), fabric-label bytes.
5. `feat(web)`: `NewHandler` + error, `AllowHost` (add-on), `--web-allowed-hosts`, `Done`; test helpers (Host, `X-Fabric-UI`,
   admin reads); flag help.
6. `feat(controller)`: #70 — flag, `KeepOnboardingCodes`, `OnboardingCodes`, tests.
7. `docs`: `docs/protocol.md` (bridge refusals, unchanged-write events, image quota), `docs/operations.md` (web rights, hosts,
   store one-way + backup, inventory secret), `docs/addon.md`, `addon/DOCS.md`, README, CONCEPT/CLAUDE links,
   `docs/by_design.md` rows as decided, `changelog.md` `[Unreleased]`, `docs/reports/S14.md`.
Guards before the PR: `make ci race cover-check fuzz-smoke`, `make generate` + `git diff --exit-code`, `make vuln licenses secrets`
(go-licenses on the vanity path — re-run, not assumed), `make release` (`version-check`), `make docker addon-image`,
`make compat-python` (and `COMPAT_BRIDGE=1`: `/ws` behaviour moved under the controller) ; golangci-lint 0 SA1019; coverfloor
may need a ratchet for new web/inventory code. Then the owner's live Ingress test (Host + Origin on an admin write).
Fable reads required by CLAUDE.md: commissioning/ACL/HTTP paths (commits 4-6) and every go-fabric/ui statement of commit 7.

## 8. Owner questions (recommendation; best architecture?)

1. **Stand-alone `--web` without admin flags = status only** (ui admin gates)? Rec: keep, as go-ha2fabric decided — best
   architecture: one rule for the family, the gate lives in go-fabric-ui, no host-side bypass.
2. **`--web-allowed-hosts`** → `Options.AllowedHosts`; add-on by `AllowHost` from the proxy address? Rec: yes to both — best: the
   name check stays the handler's wherever the daemon is reached directly; the Ingress case uses what only the proxy produces.
   Not best alternative: documenting "IP/localhost only" (copies a limitation).
3. **#70 switches and `/ws`**: one flag `--controller-keep-onboarding-codes` that sets both `KeepOnboardingCodes` and the pages'
   `OnboardingCodes`, no `/ws` command in this wave? Rec: yes. Best architecture for the switch (one operator decision = one
   secret class); `/ws` inventory deferred is a scope cut, not the final shape — `/ws` has only the optional token, weaker than
   the Ingress admin gate, so adding it needs its own access decision and a `docs/protocol.md` divergence entry.
4. **Seal the controller DB (`Config.Sealer`)** when codes are kept? Rec: not in this wave; file the facade-gap report
   (`store.NewAESGCMSealer` is below the facade) and a server follow-up. Best architecture long-term is sealing on by a key file;
   doing it now via an arch exception would be a convenience seam.
5. **Bridge node identity**: `ProductDescription.DeviceType` = Aggregator explicitly; `LocationCapability` IndoorOutdoor
   accepted; window behaviour kept (no `WindowOnDemand`), documented. Rec: as go-ha2fabric (decisions 4, 5, 7). Best: a bridge's
   `DT` should not depend on the first client-declared device; the other two follow matter.js defaults.
6. **/bridge errors**: (a) `update` returning go-fabric's refusal (`Rename` store error, `ErrNotBridged`) as a command error —
   rec: yes, InvalidArguments/typed, recorded in `docs/protocol.md`; best (a silent no-op is a convenience seam). (b) NodeLabel:
   refuse `--bridge-label` > 32 bytes at start; registry label keeps 64 chars and the NodeLabel is cut to 32 bytes on a rune
   boundary — rec: yes; alternative "bound the registry to 32 bytes" is simpler but changes go-fabric-ui's contract for a
   Matter limit the page label does not need. Best: label and NodeLabel are two values with one derivation.
7. **Controller image quota** (`MaxImageBytesPerNode`): go-fabric's default, no flag, documented in `docs/protocol.md`? Rec: yes
   — best until a real node hits it; a flag before evidence is surface without need.
8. **Load-failed nodes on `/ws`**: keep hiding them (`IsCommissioned` gate) or list them as unavailable with a way to remove?
   Rec: hide in this wave (behaviour as before), open an issue to compare with matterjs-server — not the final architecture:
   an operator cannot delete such a node through `/ws` today.
9. **`isSecret` marks** of the snapshot into `protocol/schema` (for log redaction of `/ws` command args)? Rec: separate issue;
   the generator stays id/name/type-only in this wave. Forward-looking it is the right source for redaction (no own table).
10. **`GET /metrics` publishes `cmdline`** (`http.go:103`; passcode/token flags)? Rec: fix in this PR as its own `fix(http)`
    commit (publish `fabricserver.*` + memstats only, as go-fabric's reference bridge after R2-P0-sec-E03) — best: a secret
    never leaves through a diagnostics route; it is small and the wave already touches the HTTP surface. Alternative: own issue.
11. **Version**: no release → entries under `changelog.md` `[Unreleased]`, `internal/version.Version` stays 0.10.0
    (`make version-check` stays green; CLAUDE.md: the bump and the section are the release commit). Rec: this. The later release
    is 0.11.0 (user-visible: web rights, host check, store one-way, new flags). Best: the repo rule as written; a pre-bumped
    version without a tag would make binaries report a version that does not exist.
12. **api-391 / pinned fabric label 409**: ask go-fabric-ui to declare 409 on `setFabricLabel` (ui issue) rather than change the
    server? Rec: yes — the conflict is real and the contract should say it; part of the review plan, not this PR.
