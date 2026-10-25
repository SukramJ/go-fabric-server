# The protocol

go-fabric-server speaks one WebSocket/JSON protocol in two halves: the
controller half at `/ws`, compatible with matterjs-server, and the bridge
half at `/bridge`, our own, for a host that declares devices
([the bridge half](#the-bridge-half-bridge)).

## Compatibility statement

`/ws` is wire-compatible with matterjs-server's WebSocket API at schema 14
(matterjs-server 1.5.5, commit `a7922e85`), the contract of
python-matter-server that Home Assistant's Matter integration speaks:

- the same envelope, error codes, event names and shapes;
- `server_info` reports `schema_version` 14 and `min_supported_schema_version` 11;
- every command of matterjs-server's dispatch is answered: the S1 set as
  matterjs-server answers it, the rest with `InvalidCommand` (9) and a
  `details` that names what brings it (`planned for S3|never`, or the
  go-fabric release it waits for: `go-fabric v0.12.0 (milestone 5, …)`,
  `go-fabric phase 25 (…)`) or the go-fabric facade route it needs.

The test `controllerapi/compat_internal_test.go` holds the server's types
against matterjs-server's documented examples
(`protocol/testdata/compat/`).

## Envelope

| Frame | Shape |
| --- | --- |
| request | `{"message_id": "…", "command": "…", "args": {…}}` |
| success | `{"message_id": "…", "result": …}` |
| failure | `{"message_id": "…", "error_code": n, "details": "…", "status"?: n, "cluster_status"?: n, "reason"?: "…"}` |
| event | `{"event": "…", "data": …}` |

- On connect the server sends the bare `server_info` object, before any
  response.
- `args` may be absent or `null` (the empty set); any other non-object is
  refused with `InvalidArguments` (8) naming the command. Unknown argument
  keys are ignored, as python-matter-server ignores them.
- A numeric `message_id` is answered under its digits.
- Events reach a connection only after its `start_listening` was answered;
  `subscription_report` and `subscription_ended` reach the owning connection
  whether or not it listens; `server_info_updated` and `server_shutdown`
  reach every connection.

### Numbers

64-bit values (node ids, fabric ids, event numbers) are plain JSON integers
at full width (`18446744069414584320`). The server decodes every number
exactly; send ids as integer literals, never in exponent form.

### Error codes

| Code | Name | Raised by this server for |
| --- | --- | --- |
| 0 | UnknownError | a device's status answer (with `status`, `cluster_status`), any unclassified failure |
| 1 | NodeCommissionFailed | a failed commissioning, with `reason` |
| 2 | NodeInterviewFailed | `interview_node` failing |
| 3 | NodeNotReady | a node not commissioned, disabled or closed |
| 4 | NodeNotResolving | a node not reached (timeout, network failure) |
| 5 | NodeNotExists | a `node_id` the controller does not hold |
| 7 | SDKStackError | `read_attribute` without any value |
| 8 | InvalidArguments | malformed or missing arguments; `reason: "invalid-payload"` for a pairing code of the wrong form |
| 9 | InvalidCommand | an unknown or not yet implemented command |
| 6, 10, 11, 100-103 | VersionMismatch, UpdateCheckError, UpdateError, IcdMultiAdmin, OtaUploadError, Camera… | defined, not raised yet |
| 200 | BridgeClientConnected | `/bridge` only: a second bridge client (our own code) |

`reason` (commissioning): `not-discovered`, `passcode-mismatch`,
`invalid-payload`, `timeout`, `other`. `attestation-rejected` is defined but
reported as `other` until go-fabric names an attestation rejection.

## Commands

| Command | Status | Notes |
| --- | --- | --- |
| `server_info` | implemented | plus `capabilities` |
| `start_listening` | implemented | the dump, then events |
| `get_nodes`, `get_node` | implemented | |
| `commission_with_code` | implemented | QR or manual code; the stored Wi-Fi/Thread credentials are not used yet (devices on the network) |
| `commission_on_network` | needs go-fabric | discovery route on the controller facade; works with a backend that has one |
| `parse_onboarding_payload` | needs go-fabric | an exported payload decoder |
| `discover_commissionable_nodes`, `discover` | needs go-fabric | discovery route |
| `read_attribute` | implemented | wildcards, batches of 9, unfiltered unless `fabric_filtered` |
| `read` | implemented | strict paths, `attribute_status`, `event_status`, `data_versions` |
| `write_attribute` | implemented | `*` endpoint, `timed_request_timeout_ms` |
| `device_command` | implemented | `command_name` in any case, payload by field names; no group ids |
| `batched_device_commands` | needs go-fabric | a multi-command invoke |
| `subscribe`, `unsubscribe` | implemented | only with `--disable-auto-subscription` |
| `enable_auto_subscription`, `disable_auto_subscription` | needs go-fabric | per-node switching of the server's subscription |
| `remove_node` | implemented | `client.Registry.Decommission` |
| `interview_node` | implemented | a full read without version filters |
| `ping_node` | implemented | a Matter read instead of ICMP (see below) |
| `get_node_ip_addresses` | implemented | from the node's record |
| `open_commissioning_window` | needs go-fabric | an enhanced commissioning window route |
| `set_/remove_wifi_credentials`, `set_/remove_thread_dataset`, `get_all_credentials` | implemented | secrets are write-only; Thread summaries carry the id only |
| `set_default_fabric_label`, `get_fabric_label` | implemented | stored; reaches the fabric on its first run only (see below) |
| `get_matter_fabrics`, `remove_matter_fabric` | implemented | |
| `diagnostics` | implemented | `{info, nodes, events}`, the last 25 node events |
| `get_loglevel`, `set_loglevel` | implemented | console level; `file_loglevel` is `null` |
| `set_acl_entry` | implemented (S2) | the whole ACL of our fabric in one write; a subject naming the node itself is left out; result `[{path: {endpoint_id, cluster_id, attribute_id}, status}]` |
| `set_node_binding` | implemented (S2) | the whole Binding list of `endpoint`; the same result shape |
| `get_vendor_names` | implemented (S2) | the production DCL's vendor records, kept for a day; `filter_vendors` narrows them |
| `get_icd_state`, `register_icd`, `resync_icd`, `unregister_icd` | waits for go-fabric v0.12.0 | the ICD client of go-fabric's milestone 5 |
| `check_node_update`, `update_node`, `initiate_ota_upload` | waits for go-fabric phase 25 | the OTA provider on the controller |
| `import_test_node` | planned for S3 | not a pure cache operation: every node command would route test nodes apart |

"needs go-fabric" commands answer `InvalidCommand` with `details` naming the
missing route and are absent from `capabilities`.

## Node data and values

The node data is matterjs-server's `MatterNodeData`: `node_id`,
`date_commissioned` and `last_interview` (local time,
`YYYY-MM-DDTHH:mm:ss.SSS000`), `interview_version` 6, `available` (the node's
connection state is Connected), `is_bridge` (an Aggregator in any endpoint's
DeviceTypeList), `attributes`, `attribute_subscriptions` (always `[]`),
`matter_version` (from SpecificationVersion, else estimated from
DataModelRevision) and `endpoint_parents`.

Values follow matterjs-server's conversion:

- attribute values are tag-based: a struct is an object keyed by field id;
- command responses and event data are keyed by the camelCase field name;
- bytes are base64, enums and bitmaps numbers, epoch times the raw value;
- a cluster or attribute outside go-fabric's model is sent as decoded from
  the TLV (structs keyed by tag), and its FeatureMap as `{}`.

The JSON Schema of the values is served at `GET /schema`
(`protocol/schema/schema.json`, generated with the go-fabric version in
`x-go-fabric-version`).

## Events

`node_added`, `node_updated`, `node_removed`, `attribute_updated`
(`[node_id, "e/c/a", value]`, `null` for a removed attribute),
`endpoint_added`, `endpoint_removed`, `node_event`, `subscription_report`,
`subscription_ended`, `server_info_updated`, `server_shutdown` — matterjs-server's
shapes. The values of a node's first report after a start or a commissioning
are not sent as `attribute_updated`; the node becomes available in a
`node_updated`.

## Delivery to slow clients

The send path is matterjs-server's: frames go to the socket at once until the
bytes in flight pass 1 MiB; then events wait in an ordered outbox drained
with a window. `node_updated` and `attribute_updated` coalesce (the latest
per node, per path); past the cap (1000, or 20 per node) the oldest
`node_event` and `subscription_report` frames are dropped; `node_added`,
`node_removed`, `endpoint_added`, `endpoint_removed` and
`subscription_ended` are never dropped, and more of them than the cap close
the connection, as does a write without progress for 5 minutes. Responses
are never queued. Metrics: `ws.connections`, `ws.queued_bytes`,
`ws.dropped`, `ws.closed_overflow` (expvar, prefix `fabricserver.`).

## Extensions

Ignorable by a client that does not know them:

- `capabilities` in `server_info`: the commands this server answers;
- `GET /schema`: the JSON Schema of the values;
- `GET /health`: `{version, node_count, healthy}`;
- the bearer token (see operations).

## Deliberate differences from matterjs-server

- `ping_node` answers `{ip: reachable}` from a Matter read of the node, not
  from ICMP echoes: the daemon runs without raw sockets.
- A missing or non-numeric `node_id` is `InvalidArguments` for every command
  (matterjs-server answers `UnknownError` for some), as python-matter-server
  does. A `node_id` of a class that can never name a node is
  `NodeNotExists`, not `InvalidArguments`: the node id classes are go-fabric's
  to name.
- Path ids are checked against their width (endpoint ≤ 0xFFFE, cluster and
  element ≤ 0xFFFFFFFE), not against matter.js's id classes.
- Name-based struct keys are the matter.js property names only;
  matterjs-server additionally writes chip's acronym-preserving alias where
  it differs.
- `write_attribute` changes the node data with the device's next report, not
  at once.
- `get_vendor_names` answers from the DCL only: matterjs-server merges a
  static vendor table of its own under the ledger's, which this server does
  not carry (no Matter tables of its own), so without the ledger the command
  fails with `UnknownError`.
- `set_acl_entry` and `set_node_binding` take our fabric's index on the node
  from its `CurrentFabricIndex` in the node data; without one the entries go
  without it and the node fills in the accessing fabric.

`docs/by_design.md` keeps each deliberate difference with its reason.

# The bridge half (`/bridge`)

The door for a host that is not written in Go: it declares devices by name,
sends state deltas and answers what a controller asks of them. The daemon
runs one bridge node (`--bridge`, see operations) and the half drives
go-fabric's device kit one-to-one: a declaration is `device.New` with the
typed `<cluster>def.Declare` of each cluster, a delta is the typed server's
`Set`, a write veto is `OnWrite`, a host-answered command is
`Handle<Command>`, a change is `OnChanged` with its `contract.Change.Origin`.
A Go host embeds go-fabric instead (go-fabric's `docs/guides/host-view.md`).

## Connection

- The same envelope, codec and send path as `/ws`, the same token rule.
- On connect the bare `server_info`: `{bridge_schema_version: 1,
  sdk_version, capabilities}`. The bridge half's schema number counts on its
  own, from 1.
- **One bridge client at a time.** The node has one author. A second
  connection gets one error frame, `{"error_code": 200, "details": "the
  bridge half has a client: bridge1 from 127.0.0.1:… since …"}`, and the
  close status 4409. The next client is served once the holder is gone.
- Events reach the client after its `start_listening` was answered;
  `server_shutdown` reaches it either way.

## Commands (host → node)

| Command | Args | Result |
| --- | --- | --- |
| `server_info` | — | the `server_info` object |
| `start_listening` | — | `{devices: [{key, type, endpoint, reachable, clusters: {<Cluster>: <state>}}]}`, then events |
| `declare` | the declaration document (below) | `{devices: [{key, action, endpoint, error?}], removed: [key]}` |
| `set` | `{device, cluster, patch}` | `{}` |
| `host` | `{device, name?, device_address?, channel_address?, reachable?, label?}` | `{}` |
| `remove` | `{device}` | `{}` |
| `result` | `{correlation, ok? \| error: {status?, cluster_status?}, response?}` | `{}` |
| `pairing_codes` | — | `{qr_code, manual_code}` |
| `node_info` | — | `{aggregator_endpoint, devices: [{key, endpoint, device_type, name, reachable}], fabrics: [...], commissioning_window: {open, own}, subscriptions}` |

### `declare`

Names, not ids; every name is matched without regard to case:

```json
{"devices": [
  {"key": "zendure-1", "type": "ElectricalSensor", "name": "Zendure", "vendor": "Zendure",
   "device_address": "SN1", "reachable": true,
   "clusters": {
     "PowerTopology": {"features": ["NodeTopology"]},
     "ElectricalPowerMeasurement": {"features": ["AlternatingCurrent"],
       "initial": {"powerMode": 2, "numberOfMeasurementTypes": 1, "activePower": 1234, "accuracy": [...]}},
     "PowerSource": {"features": ["Battery"], "initial": {"order": 0, "batPercentRemaining": 180}}}},
  {"key": "light-1", "type": "OnOffLight", "name": "Lamp",
   "clusters": {"OnOff": {"initial": {"onOff": false}, "handle_writes": false, "handle_commands": ["Toggle"]}}}
]}
```

- `key` is the host's stable key: the device keeps its endpoint number under
  it across restarts. `type` is a device type of the snapshot that
  go-fabric's kit builds.
- Per cluster: `initial` (a patch, below), `features` (titles or codes of
  the cluster's features), `handle_writes`, `handle_commands`. A cluster the
  type does not mandate is mounted by being declared.
- The document is the whole topology and is applied as one
  `Aggregator.Batch`: a new key is added, a changed device replaced, an
  unchanged one (the same declaration) left as it is, a key the document no
  longer names removed (`{"devices": []}` removes every device); the batch
  marks the topology complete, so the node forgets the endpoint numbers of
  removed keys.
- Validation at the door: a malformed document is `InvalidArguments` naming
  the path (`devices[0].clusters.OnOff.initial.nope: the OnOff cluster has no
  attribute "nope"`); nothing is applied. The kit's own refusal of a device
  (a cluster the type does not allow, a missing mandatory server, a feature
  selection it refuses) is the error too, with the kit's message
  (`devices[1] (light-1): device: configuration refused: …`).
- `GET /schema/declaration` serves the JSON Schema of the document
  (`protocol/schema/declaration.schema.json`, generated from go-fabric's
  snapshot and its typed cluster packages): `$defs/patch.<Cluster>` is a
  patch, `$defs/cluster.<Cluster>` a cluster entry.

### Patches

A patch is an object of attributes by property name (`activePower`) —
decoded into the cluster's generated `<cluster>def.Patch`, so the kit's
types decide: integers by width, enums and bitmaps as numbers, bytes
base64, structs by member name, lists as arrays. `null` sets a nullable
attribute to null (the Patch's `Null` list); an attribute or struct member
the cluster does not have is refused with its path. `set` applies a patch
through the typed server's `Set`: every value is checked, and all of them
are stored or none; the controllers' subscriptions report the change.

### `host`

`name`, `device_address`, `channel_address` are the host's statements
(`device.Device.Update`): a name is served while no label is written and
is not persisted. `label` renames the device persistently
(`Device.Rename`), as a controller's write of NodeLabel does. `reachable`
is `Device.SetReachable`.

## Events (node → host)

| Event | Data |
| --- | --- |
| `changed` | `{device, endpoint, cluster, state, attributes, origin}` after every change the host did not make with `set`: `origin` is `controller_write`, `command` (a command the kit answered from state, and what the kit does after it, such as a transition) or `unknown` |
| `write` | `{correlation, device, cluster, attribute, value, deadline, timeout_ms}` — only for a cluster declared with `handle_writes: true` |
| `command` | `{correlation, device, cluster, command, fields, deadline, timeout_ms}` — only for a command named in `handle_commands` |
| `result_timeout` | `{correlation, device, cluster, attribute \| command}`: the deadline passed without a result |
| `fabric_added`, `fabric_removed` | `{fabric_index, fabric_id, node_id, vendor_id, label, compressed_fabric_id}` |
| `commissioning_window` | `{open, own, commissioning_mode, discriminator, window_status}` |
| `subscriptions` | `{count}` when the node's subscription count moved (read every 5 s) |
| `server_shutdown` | `{}` |

`state` is the cluster's served attributes by property name, `attributes`
the names of those that changed.

### Writes and commands the host answers

Without opt-in the kit applies a controller's write itself and answers a
command from its state; the host sees `changed`. With
`handle_writes: true` the kit asks the host before it stores a write
(`OnWrite`): the `write` event carries the value written; `result` with
`ok` stores it, with `error` refuses it. With `handle_commands` the host
answers the named commands in place of the kit (`Handle<Command>`): the
`command` event carries the request fields by name, `result` answers with
an optional `response` (the response fields by name). A command the host
answers changes no state by itself: the host `set`s what the command
changed.

Correlation and deadline are mandatory: the host answers before
`deadline` (`--bridge-result-timeout`, 5 s by default). A result past the
deadline, a second result and an unknown correlation are refused with
`InvalidArguments`; at the deadline the controller is answered and the host
gets `result_timeout`; the endpoint stays. Without a listening client a
`write` or `command` is refused at once, and a client that disconnects
answers its pending events.

**The status of a refusal.** go-fabric's facade has no error that carries
an IM status, so every refusal — the host's `error` with any `status`, a
missed deadline, no host — reaches the controller as FAILURE (report S2,
Questions Q15). The `status` and `cluster_status` the host gives are
logged.

**Identify.** The kit refuses a host's Identify declaration on a bridged
device, whose Identify the endpoint assembler mounts itself (report S2,
Questions Q16): `handle_commands: ["Identify"]` is the kit's refusal.
