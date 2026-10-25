# Brief S2 — go-fabric-server: the bridge half (`/bridge`), the S2 controller commands that need no new facade, by_design, and the first release's follow-ups

Date 2026-10-25. Concept v8 (`go-fabric/docs/concept-v8-fabric-server.md`
C.1, C.3, D row S2, E) — with one correction this brief makes and the
concept adopts afterwards: the kit's event model is richer than C.3's
`command`/`result`, see §1.2. Repository `SukramJ/go-fabric-server` at
`v0.1.0` (`main` = `f1b2be0c`; local clone
`/home/markus/Dokumente/GitHub/go-fabric-server`), its `CONCEPT.md`,
`docs/protocol.md`, `docs/reports/S1.md` (Differences, Decisions, Left
out, Questions — the facade gaps Q1–Q14 are **not** yours; go-fabric closes
them in its bundle CJ; until then the seven commands stay stubs). go-fabric
at `v0.11.0` (or the newer tag the launcher names; `v0.12.0` lands during
the bundle — bump when it exists). go-fabric's `CLAUDE.md` (token rules),
`docs/guides/host-view.md`, `docs/guides/embed-in-a-daemon.md`,
`docs/guides/testing-in-process.md` (the pacing model after CG: a test
moves time through `Site.Step`/`Eventually` only). matter.js only via
`git -C /home/markus/Dokumente/GitHub/matter.js show 862ad75f:<path>`.
Branch `s2/bridge` in a worktree `/home/markus/Dokumente/GitHub/go-fabric-server/.claude/worktrees/s2`,
conventional commits, one pull request, no auto-merge; the owner merges
and tags `v0.2.0`. English. Never add attribution lines.

**Model:** Opus. **Hard rules as S1:** facades only (the import guard;
the device half may import the `<cluster>def` packages, `device`,
`node`, `contract`, `schema`, `parity` — not `cluster/spec` itself beyond
what the def packages re-export, not `endpoint`, `bridge`, `im`); no
Matter knowledge of your own (names and ids from the snapshot through the
S1 generator); a facade gap is reported under Questions and worked around
only by a stub that names it; compatibility of `/ws` untouched.

## 1 The bridge half (`/bridge`)

- **1.1 Transport and envelope.** The same WebSocket server, path `/bridge`,
  the same `protocol` envelope and `Conn`, the same token rule; schema
  number of the bridge half starts at 1 (`bridge_schema_version` in the
  `server_info` of `/bridge`); one bridge client at a time per daemon (a
  second connection is refused with `details` naming the holder) — the
  daemon owns one node, and its topology has one author.
- **1.2 Model.** Read the kit first: `device.New(type, options…)` with
  typed declarations `<cluster>def.Declare{Initial: Patch{…}}`;
  `Device.Update(HostStatement)` (name, addresses, reachable),
  `SetReachable`, `Rename`; per cluster `Server.Set(Patch)`,
  `Server.OnChanged(fn(State, contract.Change))` with `Change.Origin`
  (`OriginHostSet`, `OriginControllerWrite`, `OriginCommand`),
  `Server.OnWrite(fn(ctx, next State, attrID) error)` (the host may refuse
  a controller's write), `Server.Handle<Command>(fn(ctx, req) (resp,
  error))` (the host answers a command the server does not answer from
  state). The bridge half maps this one-to-one:
  - Host → node: `declare` (the topology document, §1.3 → one
    `Aggregator.Batch`), `set` (`{device, cluster, patch}` → `Server.Set`),
    `host` (`{device, name?, reachable?, addresses?}` → `Update`/
    `SetReachable`/`Rename`), `result` (`{correlation, ok | error:{status,
    cluster_status?}, response?}` answering a `write` or `command` event
    within its `deadline`), `pairing_codes`, `node_info`, `start_listening`
    (dump of the current declaration state, then events), `remove`
    (`{device}` → `Aggregator.Remove`).
  - Node → host (events): `changed` (`{device, cluster, state, attributes,
    origin}` after every change the host did not make itself — a
    controller's write or command the server applied, or a transition the
    kit drives), `write` (`{correlation, device, cluster, attribute, value,
    deadline}` — only when the host **opted in** per cluster in the
    declaration with `"handle_writes": true`; otherwise the server applies
    the write and the host sees `changed`), `command` (`{correlation,
    device, cluster, command, fields, deadline}` — only for commands the
    host opted into with `"handle_commands": ["Identify", …]`; a command
    the kit answers from state needs no host), `fabric_added/removed`,
    `commissioning_window`, `subscriptions` (count), `server_shutdown`.
  - A missed `result` past `deadline` answers the controller `Timeout`
    (or the kit's default for a write) and the event `result_timeout` tells
    the host; the endpoint stays.
- **1.3 The declaration document.** Names, not ids: `{"devices":[{"key":
  "zendure-1","type":"ElectricalSensor","name":"…","clusters":{"ElectricalPowerMeasurement":{"initial":{"activePower":1234},"handle_writes":false},"PowerSource":{"initial":{"batPercentRemaining":180}}}}]}`.
  Validation at the door: the S1 generator gains the device-type table
  (name ↔ id, the required/optional cluster lists are the kit's business)
  and a JSON Schema per cluster `Patch`; decoding goes **through the
  generated typed `Patch` structs** (`encoding/json` into
  `<cluster>def.Patch` — field names match case-insensitively; a generated
  switch `patchFor(clusterName) any` and `declareFor(clusterName, patch)
  device.Option` over all def packages the generator emits from the
  snapshot's cluster list; nullable fields via the `Null` list; a key the
  schema does not know is an error naming the path). The kit's own refusal
  (`device.New` errors: a cluster the type does not allow, a missing
  mandatory server, conformance) is returned as the `declare` error with
  the kit's message — do not pre-empt the kit.
- **1.4 Tests.** In-process with `fabrictest`: the daemon's node on a site
  (the device half can use `node.Config` fully — the site's `AddDevice`
  takes a `Topology`; read whether a daemon-built `node.New` can join a site
  or whether the test drives the `/bridge` handler against a
  `fabrictest.Device` — say which), a site controller commissions it, the
  Go client declares one ElectricalSensor + PowerSource (the CE cookbook's
  shape) and one OnOffLight with `handle_commands: ["Identify"]`; the
  controller reads the values, writes `OnOff` (host sees `changed` with
  `OriginControllerWrite`), invokes `Identify` (host gets `command`,
  answers `result`), the host `set`s a new power value (controller's
  subscription sees it), `host` reachability flips, `remove` erases, a
  late `result` yields `result_timeout`; every error path. A Python
  example client (`examples/bridge-python/`, ≤ 80 lines, stdlib +
  `websockets`, not run in CI, documented) shows the non-Go use.

## 2 Controller-half commands that need no new facade

`set_acl_entry`, `set_node_binding` (writes to the ACL and Binding
clusters through `client.Node.Write` as the reference CLI's `acl`/`bind`
commands do — read `examples/reference-controller/cli`), `get_vendor_names`
(`controller/dcl` vendor table, read how the CLI resolves a vendor name),
`import_test_node` only if it is a pure cache operation (else S3). The
ICD commands wait for go-fabric's M5 bundle (`v0.12.0`); `check_node_update`/
`update_node`/`initiate_ota_upload` wait for phase 25 — keep their stubs
and move their `details` to the right bundle name.

## 3 Housekeeping of the first release

- `docs/by_design.md` in the server repo: B1–B9 of the S1 report as
  entries (one paragraph each, with the map/clone line), plus whatever S2
  adds (the one-client rule of `/bridge`; opt-in `write`/`command`).
- `docs/protocol.md` gains the bridge half; `CONCEPT.md` gets the §1.2
  correction (events from the kit's model: `changed`, opt-in `write` and
  `command`); `docs/operations.md` the `/bridge` flags.
- `server_info` of `/ws` keeps `capabilities`; `/bridge` has its own
  `server_info`.
- Release notes section in `changelog.md` under `[Unreleased]`.

## 4 Guards and report

The repo's `make ci`, `make race` twice, `cover-check`, `fuzz-smoke` (the
declaration decoder is a fuzz target), the security job locally, the
import guard, the generator staleness test, `docker build`, `make release`
dry run. Report `docs/reports/S2.md` (and the PR body): **Bridge half**
(messages, the declaration decoder, the opt-in events, the one-client
rule), **Commands added**, **Tests**, **Guards** with HEAD, **Differences**,
**Decisions**, **Left out**, **Questions** (facade gaps with the go-fabric
symbol), **by_design** entries written. Copy this brief to
`notes/briefs/2026-10-25-S2-fabric-server-bridge.md` in the first commit.
