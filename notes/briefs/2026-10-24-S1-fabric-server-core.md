# Brief S1 — go-fabric-server: the repository, the protocol core and the controller half (matterjs-server-compatible)

Date 2026-10-24. Concept v8 (`go-fabric/docs/concept-v8-fabric-server.md`,
German: §0 decisions, B.2 layout, C.1/C.2 protocol, D wave S1, E what we do
better), the read map `read-v8-matterjs-server.md` (copy to
`notes/reads/read-v8-matterjs-server.md` in the first commit; 4 919 lines
with verbatim excerpts of matterjs-server at `a7922e85` — the wire contract
this half is compatible with; the clone is at
`/tmp/claude-1000/-home-markus-Dokumente-GitHub-go-fabric/574f85ea-24a8-44a6-b25c-462f723d7a1b/scratchpad/dl-matterjs-server/`,
**read only, never run**), go-fabric at the tag the brief names (`v0.11.0`
or later: the launcher says which), go-fabric's `CLAUDE.md` (token rules,
model rules), concept v7 B.4 (the repository standard, incl. "allow
auto-merge"). Repository `SukramJ/go-fabric-server` (the owner creates it
empty before you start; module `github.com/SukramJ/go-fabric-server`, Go
1.27.3, Apache-2.0 + CLA as the sibling repositories go-ha2fabric and
go-hamapping — copy their root files and workflows, replace the name).
Branch `s1/core` from the empty `main`, commits in conventional-commit
form (`feat(protocol): …`), one pull request, no auto-merge; the owner
merges and tags `v0.1.0`. English everywhere. Never add attribution lines.

**Model:** Opus. **Hard rules:** (1) go-fabric is used through its facades
only — `node`, `device`, `controller`, `controller/client`, `client/*` as
`controller` exposes them, `contract`, `parity`, `schema`, the
`<cluster>def` packages, `fabrictest` in tests; an import below them fails
a guard you write (`script/archcheck`-style, a test in `internal/arch`).
(2) No Matter knowledge of your own: cluster/attribute/command names and
ids, field names, types come from `parity/schema.json` through a
generator; a hand-written table of ids is a defect. (3) A gap in go-fabric's
controller facade is **reported, not worked around**: write what you
need (symbol, why) under Questions in the report and continue with the
nearest facade route or a skip; the owner routes the change into
go-fabric. (4) Compatibility first: on `/ws` the frames are byte-for-byte
the shapes of the map (§1.1-1.6, §2); where the map shows two shapes
(the `write_attribute` result casing, map 1498), follow the documented
one and note it. (5) Camera/WebRTC, Thread topology, BLE proxy, test
nodes, OTA upload: not in S1 (answer `InvalidCommand` with a `details`
that names the bundle that brings it).

## 1 Repository and layout

Per v8 B.2: `cmd/fabric-server`, `protocol/`, `protocol/schema/` (generated),
`controllerapi/`, `bridgeapi/` (S2: an empty package with a doc.go naming
S2), `internal/wire/`, `internal/arch/` (the import guard),
`client/` (Go client), `deploy/` (Dockerfile distroless non-root, systemd
unit with the hardening of v7 B.2, release matrix linux/{amd64,arm64,arm/v7}
in `release.yml` — a real release pipeline this time, the sibling
repositories' `release-on-tag.yml` is the template with `make release`),
`docs/` (`protocol.md`: the envelope, the compatibility statement, the
extensions; `operations.md`: flags, files, ports, token; `CONCEPT.md` at
the root in English from concept v8 Teil B–E, server parts). Dependencies:
go-fabric, `github.com/coder/websocket` (MIT; pure Go, context-aware) —
each a decision noted in `THIRD-PARTY-NOTICES.md`; nothing else without a
line under Questions. Guards as the siblings (`make ci` = build vet test
lint; `make race`; `cover-check` with floors; `fuzz-smoke` on the JSON
decoders; the security job real: govulncheck, go-licenses, gitleaks).

## 2 `protocol/` — the envelope and the codec

- Types: `Request{MessageID string; Command string; Args json.RawMessage}`
  (absent/null args = empty object; non-object args → error 8 naming the
  command, map 50-54), `Result{MessageID; Result any}`, `Error{MessageID;
  ErrorCode int; Details string; Status *int; ClusterStatus *int; Reason
  *string}`, `Event{Event string; Data any}`; error codes 0–11 and 100–103
  as constants with the map's names (map 196-237). JSON: numbers encoded
  as plain decimals also for `uint64`; decoding with `UseNumber`; a
  helper for the id fields that may exceed 2^53. One encoder, one decoder,
  fuzz-tested.
- `Conn`: one per WebSocket, the matterjs-server send model (map
  3459-3478): direct mode until `bufferedAmount` (coder/websocket has no
  bufferedAmount — implement the window with an outbox and a drain
  goroutine measuring bytes in flight) crosses the high-water mark
  (1 MiB), then an ordered queue with a window; `SendReliable` (responses,
  server_info), `SendOrdered(frame, droppable bool)`, `SendCoalescable(key,
  build)` (latest wins per key, used for `node_updated` per node); caps
  (base 1000 + 20 per node); on overflow drop the oldest droppable
  (`node_event`, `subscription_report`), never structural events, close
  the connection when the undroppable queue exceeds the cap or a send
  stalls past the watchdog (5 min); metrics through `contract.Metrics`
  (`ws.connections`, `ws.queued_bytes`, `ws.dropped`, `ws.closed_overflow`).
- Schema numbers: `SchemaVersion = 14`, `MinSupportedSchemaVersion = 11`
  for `/ws`; the `server_info` object carries additionally
  `capabilities: []string` (the implemented commands) and `server: "go-fabric-server/<version> (go-fabric/<version>)"`
  in `sdk_version`'s form; old clients ignore the extra key.
- Auth and transport: `--listen` (tcp addr or `unix:/path`), default
  `unix:$XDG_RUNTIME_DIR/fabric-server.sock` when unset; `--token`:
  required on a TCP listener that is not loopback (refuse to start
  otherwise), checked as `Authorization: Bearer` or `?token=`; loopback
  and unix need none. `GET /health` (from `node.Health`/controller state),
  `GET /metrics` (expvar), `GET /schema` (the generated JSON schema with
  the go-fabric version).

## 3 `protocol/schema/` — generated from go-fabric's snapshot

A generator (`go run ./internal/gen`, committed output, a test that fails
when stale — the clustergen pattern) reads `parity.Schema()` (or the
embedded bytes `parity/schema.json` through the imported module — read
`go-fabric/parity` for the exported accessor) and emits: cluster id ↔ name,
attribute id ↔ name per cluster, command id ↔ name and field names/types
per command, event names, struct field names (camelCase as the snapshot
gives them), enum/bitmap value names, device type id ↔ name; and a JSON
Schema document (draft 2020-12) of node data values per cluster for
`GET /schema`. The tables are what `internal/wire` uses to render values
name-based for known clusters and tag-based for unknown ones (map
2382-2390 is the rule, incl. `FeatureMap` of an unknown cluster as `{}`).

## 4 `controllerapi/` — the compatible half, S1 command set

Dispatch on `command` (map 730-791 is the full list; S1 implements, each
with its argument struct from map 793-1318 and its result shape from map
1319-2089): `server_info`, `start_listening` (dump of all nodes, then the
per-connection `listening` flag — events only after the response was
sent, map 3826-3835), `get_nodes` (`only_available`), `get_node`,
`commission_with_code` (QR or manual code; `network_only`),
`commission_on_network`, `parse_onboarding_payload`,
`discover_commissionable_nodes`/`discover`, `read_attribute` (path with
wildcards, map 2316-2370), `read`, `write_attribute`, `device_command`
(`endpoint_id`, `cluster_id`, `command_name`, `payload` by field names,
`timed_request_timeout_ms`), `batched_device_commands`, `subscribe`/
`unsubscribe`/`enable_auto_subscription`/`disable_auto_subscription`
(schema 14, map 2914-3178), `remove_node`, `interview_node`, `ping_node`,
`get_node_ip_addresses`, `open_commissioning_window`,
`set_wifi_credentials`/`remove_wifi_credentials`, `set_thread_dataset`/
`remove_thread_dataset`, `get_all_credentials`, `set_default_fabric_label`,
`get_fabric_label`, `get_matter_fabrics`, `remove_matter_fabric`,
`diagnostics`, `get_loglevel`/`set_loglevel`. Everything else:
`InvalidCommand` (9) with `details: "not implemented in go-fabric-server
<version>; planned for <S2|S3|never>"`.

- **The controller.** `controller.New(ctx, cfg)` with the store under
  `--storage-path` (SQLite through go-fabric's store; the CA and fabric
  persist as the facade persists them), `Controller.Pair` for
  `commission_with_code`, `Registry()`/`Nodes()`, `client.Node.Read/Write/
  Invoke/Subscribe`, `Registry.Decommission` for `remove_node`. Read
  `go-fabric/controller/controller.go` and `controller/client/*.go`
  exported surface first (the map §6 lists `node` only; `grep -n "^func (c
  \*Controller)\|^func (n \*Node) [A-Z]\|^func (r \*Registry) [A-Z]"`), and
  `examples/reference-controller/cli` for how the reference CLI does each
  of these — the CLI is the second consumer of the same facade, copy its
  routes, not its code.
- **Node cache.** `MatterNodeData` per node (map 2270-2297): `attributes`
  keyed `"endpoint/cluster/attribute"` decimal, values via `internal/wire`;
  `available` from the node's connection state; `is_bridge` from the
  Aggregator device type on endpoint 1 (as matterjs-server decides it —
  grep `is_bridge` in the clone); `interview_version`, `date_commissioned`,
  `last_interview` from the registry record; `matter_version` from
  BasicInformation; `endpoint_parents` from the Descriptor PartsLists.
  Filled from the sustained subscription's image (`client.Node` keeps the
  attribute image — read `controller/client/node.go` `Endpoints`/
  `Endpoint`), updated per report; `attribute_updated` events as the
  triple; `node_added/updated/removed`, `endpoint_added/removed`,
  `node_event`, `subscription_report/ended`, `server_info_updated`,
  `server_shutdown` as the map's shapes (§1.5-1.6).
- **Errors.** go-fabric errors mapped: a Matter status from the device →
  `status`/`cluster_status` on the error (map 73-81); commissioning
  failures → `NodeCommissionFailed` with `reason` from the map's list
  (`not-discovered`, `passcode-mismatch`, `invalid-payload`,
  `attestation-rejected`, `timeout`, `other`); unknown node →
  `NodeNotExists`; not reachable → `NodeNotResolving`; bad args →
  `InvalidArguments`.

## 5 `client/` and tests

- A Go client (`client.Dial(ctx, url, opts)`, typed methods for the S1
  commands, an event channel, auto-`start_listening`) used by the tests
  and by nothing else yet.
- **In-process tests** with go-fabric's `fabrictest`: a `Site`, the
  server's controller built so that it lives on the site's network and
  clock (read `fabrictest.AddController` and `controller.Config`: if
  `controller.New` cannot take the site's listener/resolver/clock, that is
  a facade gap — report it under Questions and test through
  `fabrictest.AddController`'s pieces meanwhile, saying so), a site device
  (an OnOffLight from go-fabric's examples/host-view pattern), the Go
  client against the server: `server_info` shape, `commission_with_code`
  with the device's QR payload, `start_listening` dump with the node,
  `read_attribute "1/6/0"`, `device_command` OnOff `On`,
  `attribute_updated` event for `1/6/0` after the host toggles,
  `write_attribute`, `subscribe`, `open_commissioning_window`,
  `remove_node`, every error path (unknown command, bad args, unknown
  node, token missing/wrong on TCP). Each command of §4 has at least one
  test. The backpressure model has a unit test with a slow fake socket
  (drops, coalescing, close on overflow).
- **Compatibility fixtures:** the shapes under `python_client/` and
  `packages/ws-client/src/models/model.ts` of the clone are the oracle;
  copy the JSON examples of `docs/websockets_api.md` into
  `protocol/testdata/compat/*.json` (with the clone's commit in a README
  there) and assert the Go types round-trip them byte-for-byte modulo key
  order.

## 6 Report and guards

Guards: the repo's `make ci`, `make race` twice, `make cover-check`,
`make fuzz-smoke`, the security job locally (`make vuln licenses secrets`),
`go test ./internal/arch/...` (the import guard), the generator's
staleness test, `docker build` of `deploy/Dockerfile`, the release matrix
`make release` dry run. Report `docs/reports/S1.md` (and as the PR body):
**Protocol** (envelope, codec, backpressure, auth), **Schema** (what the
generator emits, from which go-fabric version), **Commands** (table:
command → facade route → test → status implemented/stub), **Node cache**,
**Tests** (in-process, compat fixtures), **Guards** with HEAD,
**Differences** from the brief, **Decisions**, **Left out**, **Questions**
(every facade gap with the symbol you needed), by_design candidates
(deviations from matterjs-server's wire behaviour, each with the map
line). Copy this brief to `notes/briefs/2026-10-24-S1-fabric-server-core.md`
in the first commit.
