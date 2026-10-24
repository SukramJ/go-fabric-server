# The protocol

go-fabric-server speaks one WebSocket/JSON protocol in two halves. The
controller half at `/ws` is described here; the bridge half at `/bridge`
arrives with S2.

## Compatibility statement

`/ws` is wire-compatible with matterjs-server's WebSocket API at schema 14
(matterjs-server 1.5.5, commit `a7922e85`), the contract of
python-matter-server that Home Assistant's Matter integration speaks:

- the same envelope, error codes, event names and shapes;
- `server_info` reports `schema_version` 14 and `min_supported_schema_version` 11;
- every command of matterjs-server's dispatch is answered: the S1 set as
  matterjs-server answers it, the rest with `InvalidCommand` (9) and a
  `details` that names the bundle bringing it (`planned for S2|S3|never`) or
  the go-fabric facade route it waits for.

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
| 6, 10, 11, 100-103 | VersionMismatch, UpdateCheckError, UpdateError, IcdMultiAdmin, OtaUploadError, Camera… | defined, not raised by S1 |

`reason` (commissioning): `not-discovered`, `passcode-mismatch`,
`invalid-payload`, `timeout`, `other`. `attestation-rejected` is defined but
reported as `other` until go-fabric names an attestation rejection.

## Commands (S1)

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
