# go-fabric-server — concept

The English form of go-fabric's concept v8
([`docs/concept-v8-fabric-server.md`](https://github.com/SukramJ/go-fabric/blob/main/docs/concept-v8-fabric-server.md),
accepted by the owner on 2026-10-24), parts B to E, as far as they concern
this repository. Where this file and the German concept differ, the concept
decides and this file is corrected.

## Decisions it rests on (concept v8 §0)

- **Both roles from the start (0.3).** go-fabric-server is (a) a
  **controller server** wire-compatible with matterjs-server's WebSocket API
  (schema 14, the python-matter-server contract), so that Home Assistant's
  Matter integration uses it as a drop-in; and (b) a **device-side server**
  with a remote API (declaration, state deltas, commands) for hosts that are
  not written in Go. Camera/WebRTC is excluded.
- **Order (0.5):** the server core first (protocol, daemon, in-process tests),
  then go-hamapping, then go-ha2fabric. Go programs embed go-fabric in process
  (0.2); the server is the door for everyone else.

## B — Architecture

### B.1 Roles and boundaries

| Module | Role | Boundary |
| --- | --- | --- |
| go-fabric | The Matter knowledge: wire, clusters, device kit, `node` facade, controller facade. A library. | Imports no host; `contract/` is stdlib-only. |
| **go-fabric-server** | The daemon: WebSocket/JSON API in both roles, node cache, store, operation (image, systemd, release). | Imports `node`, `device`, `controller`, `client/*` as the controller exposes them, `contract`, `parity`, `schema`, the `<cluster>def` packages, `fabrictest` in tests — **nothing below** (a guard like go-fabric's `script/archcheck`). No cluster knowledge of its own. |
| go-hamapping | Home Assistant component vocabulary → device type and declaration. A Go module. | No `node`, no network. |
| go-ha2fabric | The Home Assistant add-on: HA WebSocket in, go-fabric in process out. | No server. |

### B.2 Inside go-fabric-server

```
cmd/fabric-server   flags and environment, the composition root (one process, either role or both)
protocol/           the envelope, error codes, schema numbers, the JSON codec (uint64 native), the backpressure send path
protocol/schema/    generated from go-fabric's parity/schema.json: names ↔ ids of clusters, attributes, commands,
                    events, datatypes, device types; the JSON Schema of the values (an artefact for clients)
controllerapi/      the matterjs-server-compatible half: dispatch table, node cache, events
bridgeapi/          the device-side half: declaration, deltas, commands, events (S2)
client/             a Go client of both halves (for tests; go-ha2fabric does not use it)
deploy/             Dockerfile (distroless, non-root), systemd unit, release matrix
```

A process runs one role, the other, or both (`--controller`, `--bridge`);
the two are separate composition roots in go-fabric (`controller.New`,
`node.New`) and share only the store file and the logger.

## C — The protocol

### C.1 Common to both halves

- **One envelope:** request `{message_id, command, args}`, response
  `{message_id, result}` or `{message_id, error_code, details, status?,
  cluster_status?, reason?}`, event `{event, data}` — exactly matterjs-server's
  (compatibility); `/bridge` uses the same form.
- **Numbers:** 64-bit ids as JSON numbers (compatibility); Go encodes `uint64`
  natively, the decoder uses `json.Number`.
- **Schema number:** on `/ws` `schema_version` 14 and
  `min_supported_schema_version` 11 (the Home Assistant contract); a command
  not implemented yet answers `InvalidCommand` (9) with `details` naming the
  bundle that brings it. A `capabilities` list in `server_info` (clients ignore
  unknown keys) names the implemented commands. `/bridge` has its own number
  from 1.
- **Authentication (our addition):** an optional bearer token (`--token`,
  header `Authorization` or query `?token=`); the Unix socket is the default
  when `--listen` is not given; without a token only loopback. Compatible
  clients without a token keep working over loopback or the Unix socket.
- **Backpressure:** matterjs-server's model, because it is right (direct, then
  a queue with a window; `node_updated` coalesced; reports droppable;
  structural events never; overflow closes), with metrics.
- **Observability:** go-fabric's slog key convention, `expvar`, `GET /health`,
  `GET /metrics` (expvar), `GET /schema` (the generated JSON Schema with its
  go-fabric version).

### C.2 The controller half (`/ws`, compatible)

- **Core (bundle S1):** `server_info`, `start_listening`, `get_nodes`,
  `get_node`, `commission_with_code`, `commission_on_network`,
  `parse_onboarding_payload`, `discover_commissionable_nodes`/`discover`,
  `read_attribute`, `read`, `write_attribute`, `device_command`,
  `batched_device_commands`, `subscribe`, `unsubscribe`,
  `enable/disable_auto_subscription`, `remove_node`, `interview_node`,
  `ping_node`, `get_node_ip_addresses`, `open_commissioning_window`,
  `set_/remove_wifi_credentials`, `set_/remove_thread_dataset`,
  `get_all_credentials`, `set_default_fabric_label`, `get_fabric_label`,
  `get_matter_fabrics`, `remove_matter_fabric`, `diagnostics`,
  `get_/set_loglevel`; all node, attribute, endpoint, subscription and server
  events. Where go-fabric's controller facade lacks a route, the command
  answers `InvalidCommand` naming what it needs, and the gap goes to go-fabric
  as a facade fix — never a workaround here.
- **S2:** `set_acl_entry`, `set_node_binding`, `get_vendor_names` (no new
  facade needed). **With go-fabric's M5 (v0.12.0):** the four ICD commands.
  **With go-fabric's phase 25:** `check_node_update`, `update_node`,
  `initiate_ota_upload` with `POST /ota-upload`. `import_test_node` is not a
  pure cache operation and moves to S3.
- **With M6 (S3):** the Thread commands (`get_thread_border_routers`,
  `get_thread_diagnostics`, `get_network_topology`).
- **Never:** camera/WebRTC, a BLE proxy endpoint.
- **Values:** `attributes` keyed `"endpoint/cluster/attribute"` in decimal;
  matterjs-server's conversion rules (read map §2.2-2.5). Planned additions,
  ignorable by old clients: `attribute_names` in the node data and a
  `get_schema` command.

### C.3 The device-side half (`/bridge`, S2)

The host's commands `declare` (the topology document, names not ids, one
`Aggregator.Batch`), `set` (state deltas through the typed `Server.Set`),
`host` (name, addresses, reachability, label), `remove` (keeps the
endpoint number), `forget` (frees it), `result` (the answer
to a `write` or `command` event, with correlation and deadline),
`pairing_codes`, `node_info`, `start_listening`.

The events follow the device kit's model (brief S2 §1.2, correcting the
command-and-result model this section first had): `changed` after every
change the host did not make itself — a controller's write or a command the
kit applied, with the change's origin; `write` and `command` only for what
the host opted into per cluster in the declaration (`handle_writes`,
`handle_commands`: the kit's `OnWrite` veto and `Handle<Command>`), else the
kit answers from its state; `result_timeout` when a deadline passed;
`fabric_added/removed`, `commissioning_window`, `subscriptions` (the count),
`server_shutdown`. Deadlines and correlation are mandatory; a missed one
answers the controller and the endpoint stays. Every `declare`/`set` is
decoded through go-fabric's generated typed `Patch` structs, and the
generated JSON Schema of the declaration documents it. One bridge client at a
time: the node has one author.

## D — Bundles

| Bundle | Content | Proof |
| --- | --- | --- |
| S1 | The repository, this concept, `protocol/`, the `protocol/schema` generator, the controller core (C.2), node cache, events, backpressure, token and Unix socket, `GET /health\|/metrics\|/schema`, the Go client, `deploy/`, in-process tests with `fabrictest`, compatibility fixtures of matterjs-server | Home Assistant's Matter integration against the server (owner, manual); `v0.1.0` |
| S2 | `bridgeapi` (C.3) with its Go client, a 50-line Python example client, the S2 commands of the controller half as far as M5 delivered | a Python script makes a device visible in Apple Home (owner, manual); `v0.2.0` |
| S3 | The Thread commands, `import_test_node`, the dashboard question | `v0.3.0` |

## E — Better than others, better than before, is the split right

### E.1 Better than matterjs-server (the reference)

| Point | matterjs-server | go-fabric-server |
| --- | --- | --- |
| Runtime | Node ≥ 22, an npm tree | one static Go binary, distroless, `CGO_ENABLED=0` |
| 64-bit numbers | its own BigInt JSON serializer | `uint64` native, `json.Number` |
| Names | numeric ids only | ids for compatibility, names on `/bridge`, `GET /schema` |
| Schema | a number and a changelog | a number plus a machine-readable JSON Schema, `capabilities` in `server_info` |
| Security | no authentication, bind address | token, Unix-socket default, loopback without token |
| Correctness | good tests | go-fabric's guards underneath; the server tests in process, without a network |
| Operation | a dashboard in the package | a protocol free of UI; expvar, health, the slog table |
| Backpressure | good (coalesce, never-drop, close) | taken over, made visible with metrics |
| Role | controller only | controller and device side in one daemon |

What matterjs-server does better for now: camera/WebRTC, Thread topology
graphs, migrating old python-matter-server data, years of interop
experience with Home Assistant.

### E.2 Better than before

The controller facade (`controller.New`, go-fabric ADR 0020) gets its first
external consumer: as loom tested the device facade, the server tests the
controller facade, and its gaps become facade fixes in go-fabric. Packaging
moves from the library into the product, where a daemon gives it a reason.

### E.3 Is the split right

Rejected: everything in go-fabric's `cmd/` (a library's stability window
against a daemon's release cadence); the server as the only door, also for Go
hosts (Go programs would pay a protocol for an import); one daemon per role
(the roles already share a process in `fabrictest`; one daemon with flags is
the smaller surface). Inside the server: `protocol` knows no Matter,
`controllerapi`/`bridgeapi` know go-fabric's facades and the protocol,
values go through go-fabric's JSON route (S3); nothing reaches below the facades, and a
guard (`internal/arch`) holds that.

## F — Risks

- Home Assistant compatibility is empirical: the fixtures cover shapes, not
  HA's use; the manual check against HA is the owner's step per bundle.
- Controller facade gaps couple the server's cadence to go-fabric's.
- A go-fabric pin change changes the generated schema; the schema carries the
  go-fabric version.
