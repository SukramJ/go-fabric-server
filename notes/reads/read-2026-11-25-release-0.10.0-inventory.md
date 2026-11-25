# Read 2026-11-25: release 0.10.0 inventory (go-fabric v0.26.0, go-fabric-ui v0.14.1)

**Model:** Opus (inventory read; a map, not citations; every go-fabric fact below
is to be re-read at the tag before it enters a brief, `docs/protocol.md` or a
comment, per CLAUDE.md).
**Base:** go-fabric-server `main` 7217eaa (pins go-fabric v0.25.0, nested
modules v0.25.0, go-fabric-ui v0.14.0). go-fabric tag `v0.26.0` (93aae7575,
nested `ble/bluez`, `ble/esphome`, `wifi/wpasupplicant` also tagged v0.26.0).
go-fabric-ui tag `v0.14.1` (0764225). Citations: `GF:` = go-fabric at v0.26.0,
`UI:` = go-fabric-ui at v0.14.1, bare paths = this repo at 7217eaa.

## 0. Trial bump (scratch copy, not this tree)

In a copy under the session scratchpad: `go get` go-fabric + the three nested
modules @v0.26.0 and go-fabric-ui @v0.14.1, `go mod tidy`:

- `go build ./...`, `go vet ./...`: clean. No compile break.
- `staticcheck -checks SA1019`: exactly one hit —
  `controllerapi/cmd_acl.go:222:12: controller/dcl.NewClient is deprecated`
  (`golangci-lint`'s staticcheck is enabled, `.golangci.yaml:26`, so `make lint`
  goes red on the bump until it is migrated).
- `go test ./...`: all packages green except `internal/gen`
  `TestCommittedOutputIsCurrent` (generated files stale). `go run ./internal/gen`
  changes only the version strings (`schema.json` `x-go-fabric-version`,
  `tables_gen.go` `GoFabricVersion`): the parity snapshot / matter.js commit did
  not move, so `/ws` did not move → `make compat-python` can be skipped with that
  reason.
- No store schema change in 0.26.0 (no migration named in `GF:CHANGELOG.md:13-181`).

## 1. go-fabric v0.26.0 (and S14 left-outs)

S14 (`notes/briefs/2026-11-23-S14-gofabric-0.25.0.md:29-47`, `docs/reports/S14.md:242-254`)
targeted v0.25.0 and deliberately left out: the Sealer (#76), the `/ws`
onboarding command (#74), load-failed nodes on `/ws` (#75), the image-quota flag,
and the atomic replace (go-fabric #585, worked around as remove-then-add). The
known issue (#587, stored fabric label > 32 bytes stops the controller) was
documented. go-fabric #584, #585, #587, #590 are all CLOSED and shipped in
v0.26.0. Everything older than 0.26.0 was covered by S14, so the table is 0.26.0.

Class: **mandatory** = deprecation (owner rule SA1019) or the bump does not pass
guards; **recommended** = security / correctness / known issue; **optional** =
new capability; **n/a** = node/bridge-internal, the server gets it with the bump
and wires nothing.

| # | Item | go-fabric (v0.26.0) | Server use (7217eaa) | Class | Effort | Recommendation |
|---|---|---|---|---|---|---|
| 1.1 | Bump go-fabric + `ble/bluez`, `ble/esphome`, `wifi/wpasupplicant` to v0.26.0 | `GF:CHANGELOG.md:14` (no source change, 42 apidiff items = dcl move), `:51` | `go.mod:7-12` | mandatory (go-fabric-ui v0.14.1 requires it, `UI:changelog.md:15`) | S | Do; `make generate` (version strings only), refresh `server_info`/`/schema` provenance text in changelog |
| 1.2 | `controller/dcl` → `dcl` (ledger client moved; old names deprecated forwarders, removable v0.28.0) | `GF:CHANGELOG.md:31` (Added `dcl`), `:65` (signatures), `:88` (Deprecated); `GF:dcl/client.go:138` `NewClient`; `GF:controller/dcl/moved.go:286` | `controllerapi/cmd_acl.go:15` import, `:222` `dcl.NewClient("")`; arch allowlist `internal/arch/arch_test.go:48-50` names `controller/dcl` | **mandatory** (owner rule, SA1019 hit) | S | Import `thingfabric.dev/go-fabric/dcl`; allowlist `"dcl"` instead of `"controller/dcl"` (keep `controller/dcl/snapshot`, it did not move: `GF:controller/dcl/snapshot/snapshot.go:117`); comment at `cmd_acl.go:204-215` |
| 1.3 | `dcl.VendorInfoService` (matter.js DclVendorInfoService) | `GF:CHANGELOG.md:31`, `GF:dcl/vendorinfo.go:19`, `:128` | own cache `controllerapi/cmd_acl.go:216-245` (`dclVendors`: snapshot + live ledger, 24 h) | optional | S–M | Later: evaluate replacing the hand-rolled cache (Fable read vs matterjs-server `WebSocketControllerHandler.ts:1695-1728`) |
| 1.4 | Facade sealer: `controller.NewAESGCMSealer`, `controller.Reseal`, `node.NewAESGCMSealer`, `node.Reseal`, `ErrUnseal`, `ErrNoSealer`, `ErrScrubIncomplete` (#584) | `GF:CHANGELOG.md:32-33`; `GF:controller/seal.go:15-34`, `GF:controller/reseal.go:39`, `GF:controller/controller.go:159-183` (`Config.Sealer`); `GF:node/facade.go:111-146`, `GF:node/config.go:799-824` (`Store.Sealer`); `GF:docs/security.md:96` | not used: `internal/daemon/run.go:317-323` (`controller.Config`, no Sealer), `:557` (`node.Store{Path}`); kept codes unsealed (`--controller-keep-onboarding-codes`) | recommended (security: kept onboarding codes, CA/IPK/op keys at rest) | L | Server #76. Caveat: the facade has no `Sealer` type name at v0.26.0 (`node/facade.go:123`, `:146`, `node/config.go:813` name `store.Sealer`, below the facade); go-fabric #601 (open) adds `type Sealer = store.Sealer` (decision 657). Implementable now only if the server never names the type; cleaner after #601 ships |
| 1.5 | Atomic replace of a served endpoint by a withheld declaration (#585) | `GF:CHANGELOG.md:151` (Replace by withheld no longer refuses; takes the endpoint off the wire, key keeps its number, returns `*surface.WithheldError`; failed validation leaves the served endpoint) | `bridgeapi/commands.go:131-166` (`replace`: Remove + Add + Forget-on-failure, the `gone` path), comment cites v0.25.0 and #585 at `:134-143` | recommended (simpler, removes the `gone` half-state, correctness) | M | Collapse `replace` to `bt.Replace` + `errors.As(WithheldError)`; drop `gone`/`removedGone` (`:229-238`); tests in `bridgeapi`; `docs/protocol.md` `declare` text |
| 1.6 | `Batch` judged at commit (end state); errors may now come from `Batch` at commit instead of from the call; `ErrBatchDone` | `GF:CHANGELOG.md:72`, `:39` | `bridgeapi/commands.go:187-223` records per-device errors at call time; `:271-273` maps any `batchErr` to `UnknownError` for the whole `declare` after `s.devices` was already updated (`:226-262`) | recommended (correctness: a commit-time `ErrDuplicateKey`/`WithheldError` would answer UnknownError while the half's map says applied) | M | Re-check `cmdDeclare` against commit-time errors together with 1.5; tests pass today (trial bump) but no test exercises a commit-time refusal |
| 1.7 | Stored fabric label > 32 bytes cut at load, warned, saved back (#587); `FabricConfig.Logger` | `GF:CHANGELOG.md:149`, `:37`; the controller passes its logger via `NewFabricAuthority(authority, log)` (`GF:controller/controller.go:502-512`) | Known issue text in `changelog.md:13-16`, `addon/CHANGELOG.md` (Unreleased), `addon/DOCS.md:58-60`, `docs/addon.md:59-65`, `docs/operations.md:247-249`; server.json cut `controllerapi/settings.go:59-140` stays | **recommended — release blocker removed by the bump** | S | Bump; delete the known-issue paragraphs; say "a stored label over 32 bytes is cut at load (go-fabric, warn `matter.controller.fabric...`)" |
| 1.8 | `controller/camera.New`: one Service per controller (`ErrServiceInUse`) | `GF:CHANGELOG.md:58`, `:29` | `internal/daemon/run.go:378` (one `camera.New` per controller) | n/a | – | Nothing to do; no second New |
| 1.9 | `SubscribeRequest.Retries` (sustained retry schedule); `DefaultSustainRetries` deprecated | `GF:CHANGELOG.md:34`, `:89` | `controllerapi/cmd_subscribe.go:181-189` (`client.SubscribeRequest`, embeds `interaction.SubscribeRequest`, `GF:controller/client/interact.go:47-48`); no `DefaultSustainRetries` | optional | S | Leave default (= matter.js default); no flag unless a need appears |
| 1.10 | `ICDClient.Register` returns the store error (registration stays in memory) | `GF:CHANGELOG.md:59` | `controllerapi/cmd_icd.go:144` | recommended (check) | S | Confirm the error mapping/answer of `register_icd` when the store fails; probably no change |
| 1.11 | Load-failed client node carries `HooksFor` hooks; `Registry.Forget` drops a load-failed node | `GF:CHANGELOG.md:147`; `GF:controller/client/registry.go:502-508` (Forget), `GF:controller/client/fabricops.go:288-294` (Decommission refuses `ErrLoadFailed`) | `controllerapi/cache.go:65-69` (`IsCommissioned` gate), `controllerapi/cmd_node.go:82-94` (`remove_node` → `Decommission`) | optional (enables #75) | M | See §3, #75 |
| 1.12 | Security fixes in node/bridge (DiagnosticLogs per-fabric, CASE install from the step value, `Redact` fail-closed) | `GF:CHANGELOG.md:19-21` | bridge nodes `internal/daemon/run.go:540-600` | recommended (automatic with bump) | – | Bump; list in the changelog's Security section |
| 1.13 | `wifi/wpasupplicant`: no credential left behind on an unanswered `AddNetwork`; `ble/bluez`: re-register after adapter loss | `GF:CHANGELOG.md:77`, `:168`, `:156` | `internal/daemon/noderadio.go:44` (wpasupplicant), bluez node peripheral (S13) | recommended (automatic with nested bump) | – | Bump; one changelog line |
| 1.14 | Camera PeerGone (`Manager.SetOnPeerGone`, `Provider.PeerGone`), `ProviderConfig.MaxSessions[PerFabric]`, `WebRTCEndReasonEnum` (#590) | `GF:CHANGELOG.md:41-44`; wired inside node (`GF:node/webrtc_provider_peers.go:77-78`), bound via `GF:device/defaults_camera.go:151` | the server serves no camera provider (`/bridge` has no camera port; controller side is `controller/camera`) | n/a | – | Nothing |
| 1.15 | `Messages.SetLogger`, `AccessControl.OnAttributeChange`, GroupKeyManagement change notify, `ThreadNetworkCommissioning.OnAttributeChange`, `ScenesConfig.ListFabrics` (Fabrics deprecated), `IsFailSafeArmed` deprecated (Wi-Fi/Thread NC) | `GF:CHANGELOG.md:26`, `:38`, `:45-46`, `:86-87` | server imports no `cluster/core` (import census); node wires them (`GF:node/root.go:267`, …) | n/a | – | Nothing; SA1019 run confirms no deprecated use |
| 1.16 | `sigma.Established`, `bridge.CaseAdapter.SetOnEstablished` (old callback deprecated) | `GF:CHANGELOG.md:47`, `:90` | server imports no `bridge`/`secure/*`; node wires it (`GF:node/security.go:1234`) | n/a | – | Nothing |
| 1.17 | `diagevent.ForgetFabric`, Detail keys | `GF:CHANGELOG.md:28`, `:36` | not imported; `bridge` calls it (`GF:bridge/bridge.go:1969`, `:2125`) | n/a | – | Nothing |
| 1.18 | `mdns.Service.Validate` refuses labels > 63 octets / names > 255 | `GF:CHANGELOG.md:129` | bridge node instance names from registry keys/labels (`internal/daemon/registry.go`) | recommended (check) | S | Confirm a registry node key/label cannot produce an instance name that now fails `Publish` (start would fail or advertise nothing) |
| 1.19 | `endpoint.Spec.Instance`, `contract.StatefulServer`, `transport/tcp.NoteSessionsEnded` | `GF:CHANGELOG.md:27`, `:35`, `:40` | not used | n/a / optional | – | Nothing (Aggregator sets Instance itself) |

## 2. go-fabric-ui v0.13.0 → v0.14.1

The server pins v0.14.0 already (M move, #80) and adopted 0.13.0 in S14
(`OnboardingCodes`, `AllowedHosts`, `AllowHost`, admin header). 0.14.0 and
0.14.1 carry no route, field or Options change (`UI:changelog.md:8-25`,
`:27-70`; `git diff --stat v0.13.0 v0.14.1` on `docs/embedding.md`,
`docs/provider.md`, `fabricui/options.go`, `fabricui/gofabric/gofabric.go`:
path renames only).

| # | Item | go-fabric-ui (v0.14.1) | Server use | Class | Effort | Recommendation |
|---|---|---|---|---|---|---|
| 2.1 | v0.14.1 = go-fabric v0.26.0 minimum, contract 0.14.0 unchanged | `UI:changelog.md:8-25` | `go.mod:8` v0.14.0 | mandatory with 1.1 | S | Bump together with go-fabric |
| 2.2 | Problem `type` URIs `https://thingfabric.dev/go-fabric-ui/problems#problem-<code>` | `UI:changelog.md:45-52`, `:63-65` | no code compares `type` (grep); already in `changelog.md` [Unreleased] Changed | done | – | Nothing more |
| 2.3 | Onboarding inventory (0.13.0) | `UI:changelog.md:72-118`, `UI:docs/embedding.md:412` | wired in S14 (`internal/daemon/controllerui.go`, `run.go:304-323`) — pages only, `/ws` is #74 | done (pages) | – | `/ws` side is §3 #74 |
| 2.4 | `Options.Counters` (host counters on `/node/status`) | `UI:fabricui/options.go:337-342` | not set (`internal/daemon/web.go:49-53`) | optional | S | Could publish `fabricserver.ws.dropped` etc.; later |
| 2.5 | `Options.FrameAncestors` | `UI:fabricui/options.go:287-300` | not set | optional | S | Only for framing outside Ingress (an HA dashboard iframe on another origin); later, on demand |
| 2.6 | `Options.MaxStreams`, `ConfiguredWindow`, `AllowEmptyTopology`, `Prefix` | `UI:fabricui/options.go:265`, `:281`, `:286`, `:333` | not set (defaults) | optional | – | Keep defaults (S14 decision 5 kept the window behaviour) |

No new port or capability in 0.13.0 → 0.14.1 that the server leaves unserved,
apart from the `/ws` inventory (#74).

## 3. Open server issues

| Issue | What | Doable now? | Blocker / dependency | Effort | Recommendation |
|---|---|---|---|---|---|
| #74 `/ws` onboarding code inventory | expose kept codes on `/ws` | technically yes (go-fabric controller API exists since 0.25.0; the pages use it) | owner access decision (`/ws` has only the optional token, weaker than the Ingress admin gate); a deliberate matterjs-server difference for `docs/protocol.md`; better after #76 so codes are sealed | M | Later (0.11.0), after #76; needs a Fable read + owner decision |
| #75 load-failed nodes on `/ws` | list them as unavailable, allow removal | yes: `Registry.Forget` drops a load-failed node (`GF:controller/client/registry.go:502-508`), Decommission refuses it (`fabricops.go:288-294`), hooks carried since 0.26.0 (`GF:CHANGELOG.md:147`) | Fable read of matterjs-server at its pin (are such nodes listed `available:false`?); `remove_node` → Forget for a load-failed node | M | Should (small user value: an operator can delete a broken node); or later if the release is to stay small |
| #76 seal controller DB + node stores | key file flag, add-on option, `Config.Sealer`, `node.Store.Sealer`, errors, rotation, docs | yes (go-fabric #584 shipped, §1.4) | facade has no `Sealer` type name until go-fabric #601 (decision 657); add-on key location (everything in `/data` is in the same HA backup as the DB); rotation UX (`controller.Reseal`/`node.Reseal` with no controller open → offline subcommand) | L | Later (own bundle), ideally on the go-fabric release that carries #601; keep "unsealed" wording in docs for 0.10.0 |
| #78 flaky `TestCommissionReadCommandAndEvents` | test waits on timing | yes | none | S–M | Should: a red CI on the release PR/tag costs more than the fix |

## 4. Release mechanics (first release under thingfabric)

State: last tag and GitHub release **v0.9.3** (2026-11-12, images under
`ghcr.io/sukramj/…`, go-fabric 0.22.2). **v0.10.0 was never tagged**:
`internal/version` (`Version = "0.10.0"`), `addon/config.yaml:7` (`"0.10.0"`) and a
`changelog.md:120` section `## [0.10.0] - 2026-11-13` exist (S13, commit 6e4db23,
also on go-fabric 0.22.2), and `[Unreleased]` (`changelog.md:7-118`) sits above
it. Under the org `thingfabric` the only container package is `go-fabric-chip`
(public); `go-fabric-server` and `go-fabric-server-addon-{amd64,aarch64,armv7}`
do not exist there yet.

What the pipeline needs:

- `make release` = `version-check` (`Makefile:115-119`: internal/version ==
  addon `version:` and a `## [X.Y.Z]` heading in `changelog.md`) + cross builds +
  `script/extract-release-notes.sh`, which prints only the `## [X.Y.Z]` section
  up to the next `## [` and a compare link to the next lower section
  (`script/extract-release-notes.sh:17-37`). **Tagging v0.10.0 now would pass
  every check but publish the 2026-11-13 notes only** — without the move, the
  bump, the onboarding codes, the security fix. The release commit must fold
  `[Unreleased]` into `[0.10.0]` (new date) or bump to 0.11.0.
- `release.yml`: tag `v*`, owner `thingfabric` gate, tag == internal/version,
  `make release`, prepends `.github/SPONSORING.md`, GitHub release with archives
  + SHA256SUMS, then multi-arch `ghcr.io/thingfabric/go-fabric-server:{version,latest}`.
- `addon-image.yml`: tag `v*` (no owner gate), tag == addon `version` ==
  internal/version, pushes `ghcr.io/thingfabric/go-fabric-server-addon-{arch}:{version,latest}`
  (`provenance: false`).
- **GHCR first push:** the four packages are created on the tag's first run.
  They must be **public** for the Supervisor and `docker pull` without login;
  check (and if needed set) visibility per package right after the run, as for
  `go-fabric-chip`. Until then the add-on update fails to pull.
- Add-on: `addon/config.yaml` version and `image` already right; fold
  `addon/CHANGELOG.md` "## Unreleased" + "## 0.10.0" into one "## 0.10.0".
  Existing installs added the old repository URL; GitHub redirects the moved
  repo, so the Supervisor likely sees the new manifest (new image path) — worth
  one live check; the changelog tells users to re-add the URL anyway.
- Version texts that name the wrong predecessor: "upgrading from 0.10.0" / "no
  way back to 0.10.0" / "set under 0.10.0" — users come from **0.9.3**
  (0.10.0 never shipped; same go-fabric 0.22.2, so the upgrade facts hold for
  0.9.3): `changelog.md:9-16`, `addon/CHANGELOG.md` Unreleased,
  `addon/DOCS.md:58-60`, `docs/addon.md:59-65`, `docs/operations.md:240-249`.
  With 1.7 the known-issue paragraphs go entirely.
- `docs/operations.md:320` (`ghcr.io/thingfabric/go-fabric-server:0.1.0`) and
  `:329` (`…:0.8.0`): tags that never existed under thingfabric → `0.10.0`
  (or a `<version>` placeholder that the release bump never has to touch).
- `README.md:14` status line ("v0.10.0; main on go-fabric v0.25.0 … unreleased")
  → released state.
- `docs/operations.md:240-245` cites go-fabric `CHANGELOG.md` lines "at v0.25.0"
  and `:366` `CHANGELOG.md:644 at v0.25.0`; ~49 `v0.25.0` citations in code/docs
  stay valid as citations at that tag; refresh only those whose behaviour changed
  (`bridgeapi/commands.go:134-143`, `controllerapi/settings.go:119`, the known
  issue).
- Guards before the PR (CLAUDE.md): `make ci race cover-check fuzz-smoke`,
  `make vuln licenses secrets`, `make release` (with `version-check`),
  `make docker`, `make addon-image`; `compat-python` skippable (§0).
- Release commit = `internal/version.Version` + changelog section in one commit
  (CLAUDE.md Rules); Version is already 0.10.0, so for 0.10.0 only the changelog
  fold + date.

## 5. Proposed release scope

**Must (0.10.0 cannot ship without):**
1. Bump go-fabric + nested modules to v0.26.0, go-fabric-ui to v0.14.1;
   `make generate` (version strings) — §1.1, §2.1.
2. `controller/dcl` → `dcl` incl. arch allowlist — §1.2 (owner rule, lint red otherwise).
3. Remove the #587 known issue everywhere; state the load-time cut — §1.7.
4. Release paperwork: fold `[Unreleased]` into `[0.10.0]` (new date), predecessor
   texts → 0.9.3, `addon/CHANGELOG.md` folded, `docs/operations.md:320,329` tags,
   README status — §4.
5. After the tag: make the four new GHCR packages public; one live add-on update check.

**Should (same bundle, small, correctness):**
6. Atomic replace (#585) + commit-time `Batch` errors in `declare` — §1.5, §1.6 (M).
7. #78 flaky test fixed to wait on the condition (S–M).
8. Checks without expected code change: `register_icd` store error (§1.10), mDNS
   name bounds of registry nodes (§1.18).

**Later (own bundles, 0.11.0+):**
9. #76 sealing (L) — on the go-fabric release with #601's `node.Sealer` alias.
10. #74 `/ws` onboarding inventory (M) — after #76, with an owner access decision.
11. #75 load-failed nodes on `/ws` (M) — Fable read of matterjs-server first.
12. Optional: `dcl.VendorInfoService` (§1.3), `Options.Counters` (§2.4),
    `SubscribeRequest.Retries` (§1.9).

## 6. Questions for the owner

1. **Version number:** release as **0.10.0** (fold `[Unreleased]` into the never
   tagged 0.10.0, new date) or as 0.11.0 with 0.10.0 left as an untagged section?
   Recommendation: **0.10.0** — no 0.10.0 artefact exists, a 0.11.0 would leave a
   changelog section and a compare link (`v0.10.0...v0.11.0`) to a tag that never
   existed, and every "from 0.10.0" text is wrong either way.
2. **Scope:** must-only (fast, low risk) or must + should (#585/Batch rework, #78)?
   Recommendation: **must + should** — the Replace rework removes the `gone`
   half-state the S14 workaround introduced and is the best architecture now that
   go-fabric has the atomic step; #78 protects the release CI.
3. **#75 in this release?** Recommendation: **no**, own small bundle after the
   release (needs a Fable read of matterjs-server and a `docs/protocol.md` entry).
4. **#76 sealing timing:** now on v0.26.0 (without a facade `Sealer` name), or on
   the go-fabric release that carries #601's alias? Recommendation: **wait for
   #601**, then one bundle with the key-file flag, add-on option and an offline
   reseal; and decide the add-on key location then (a key in `/data` shares the
   HA backup with the DB).
5. **GHCR:** who sets the four new packages public after the first tag run (owner
   by hand in the org's package settings, as for `go-fabric-chip`)?
   Recommendation: owner, right after the run, before announcing.
