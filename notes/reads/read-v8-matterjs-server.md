# Read map v8: matterjs-server and go-fabric surface

Repository: https://github.com/matter-js/matterjs-server (shallow clone at /tmp/claude-1000/-home-markus-Dokumente-GitHub-go-fabric/574f85ea-24a8-44a6-b25c-462f723d7a1b/scratchpad/dl-matterjs-server)
HEAD: a7922e85f4b94a5db94a1dbee1a02c685c2ec71d
Commit date: 2026-10-24T13:06:34Z
version.txt: 1.5.5
go-fabric (local main): a0b842b6d2b485eedec8a22486e47b2e94f0da19 2026-10-24T17:56:11Z

Paths below are relative to the clone root unless prefixed /home/markus. Excerpts are verbatim; [file:a-b] gives the line range.


## 1. Wire protocol

### 1.1 Transport and connect (docs/websockets_api.md:1-75)
[docs/websockets_api.md:1-75]
~~~~
# WebSocket API Documentation

This document describes the WebSocket API for the Matter.js server. The server listens on `ws://localhost:5580/ws` by default.

## Connection

On connection, the server immediately sends a `server_info` message with fabric and capability information:

```json
{
  "fabric_id": 1234567890,
  "compressed_fabric_id": 9876543210,
  "schema_version": 14,
  "min_supported_schema_version": 11,
  "sdk_version": "matter-server/1.1.7 (matter.js/0.17.5-alpha)",
  "wifi_credentials_set": true,
  "thread_credentials_set": false,
  "bluetooth_enabled": true
}
```

## Request/Response Format

All commands follow this request format:

```json
{
  "message_id": "unique-id",
  "command": "command_name",
  "args": { ... }
}
```

`args` may be left out, or sent as `null`, and means an empty argument set either way: a command whose
arguments are all optional answers such a request, and a command with a required argument refuses it
the way it refuses that argument being absent. An `args` that is anything else than a JSON object — a
string, a number, a boolean or an array — is refused with error 8 (`InvalidArguments`) naming the
command.

Successful responses:
```json
{
  "message_id": "unique-id",
  "result": { ... }
}
```

Error responses:
```json
{
  "message_id": "unique-id",
  "error_code": 0,
  "details": "Error description"
}
```

Optional fields of an error response (schema 14):

- `status` and `cluster_status`: when the failure is a Matter status that matter.js reports as received from the
  device, the numeric Matter status code and, if the device sent one, the cluster-specific status code. This is
  always the case for a `device_command` the device rejected, for example with `UNSUPPORTED_COMMAND` (0x81), and for
  any command whose whole interaction the device answered with a status. matter.js does not mark the status of a
  command the server sends on its own behalf (for example to open a commissioning window or remove a fabric), so
  those failures carry no `status`. Neither do arguments the server rejects before anything is sent. `error_code`
  stays as it was.
- `reason`: why commissioning failed, with every `NodeCommissionFailed` error of `commission_with_code` and
  `commission_on_network`, and with a pairing code that cannot be decoded. Other argument errors carry no `reason`.
  - `not-discovered`: no matching device was found.
  - `passcode-mismatch`: the device rejected the pairing code.
  - `invalid-payload`: the pairing code could not be decoded; `error_code` is `InvalidArguments`.
  - `attestation-rejected`: the device certificates were not accepted.
  - `timeout`: a device was found but not reached in time, an attempt to reach it timed out, the device stopped
    answering, or commissioning took too long.
  - `other`: anything else, for example an attempt that failed with an error other than a timeout.

~~~~

### 1.2 Server-info message fields as typed (packages/ws-client/src/models/model.ts:1527-1544) and as built (WebSocketControllerHandler.ts:1165-1186)
[packages/ws-client/src/models/model.ts:1527-1544]
~~~~
export interface ServerInfoMessage {
    fabric_id: number | bigint;
    compressed_fabric_id: number | bigint;
    /** The fabric index. Note: Only available with OHF Matter Server, not Python Matter Server. */
    fabric_index?: number;
    schema_version: number;
    min_supported_schema_version: number;
    sdk_version: string;
    wifi_credentials_set: boolean;
    wifi_ssid?: string;
    thread_credentials_set: boolean;
    bluetooth_enabled: boolean;
    /** True when BLE proxy mode is enabled (server exposes `/ble` WebSocket endpoint for a remote proxy client). Note: Only available with OHF Matter Server, not Python Matter Server. */
    ble_proxy_enabled?: boolean;
    /** The controller's own operational (CASE) node id. Note: Only available with OHF Matter Server, not Python Matter Server. */
    controller_node_id?: number | bigint;
}

~~~~
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1165-1186]
~~~~
    async #getServerInfo(): Promise<ServerInfoMessage> {
        await this.#commandHandler.start();
        const {
            fabricId: fabric_id,
            compressedFabricId: compressed_fabric_id,
            fabricIndex: fabric_index,
        } = await this.#commandHandler.getCommissionerFabricData();
        return {
            fabric_id,
            compressed_fabric_id,
            fabric_index,
            schema_version: SCHEMA_VERSION,
            min_supported_schema_version: MIN_SUPPORTED_SCHEMA_VERSION,
            sdk_version: `matter-server/${this.#serverVersion} (matter.js/${MATTER_VERSION})`,
            wifi_credentials_set: !!(this.#config.wifiSsid && this.#config.wifiCredentials),
            wifi_ssid: this.#config.wifiSsid && this.#config.wifiCredentials ? this.#config.wifiSsid : undefined,
            thread_credentials_set: !!this.#config.threadDataset,
            bluetooth_enabled: this.#commandHandler.bleEnabled,
            ble_proxy_enabled: this.#commandHandler.bleProxyEnabled,
            controller_node_id: this.#commandHandler.getCommissionerNodeId(),
        };
    }
~~~~
Schema constants: packages/ws-controller/src/server/WebSocketControllerHandler.ts:114 `const SCHEMA_VERSION = 14;`, packages/ws-controller/src/server/WebSocketControllerHandler.ts:119 `const MIN_SUPPORTED_SCHEMA_VERSION = 11;`
On connect: server_info is sent after the handlers are wired: packages/ws-controller/src/server/WebSocketControllerHandler.ts:862-866 (`this.#getServerInfo().then(response => connection.sendReliable(toBigIntAwareJson(response)))`). The frame is a bare ServerInfoMessage object without `event` or `message_id` wrapper.
`start_listening` and `server_info` also return the same object / node list as a normal result (dispatch packages/ws-controller/src/server/WebSocketControllerHandler.ts:950-953 and packages/ws-controller/src/server/WebSocketControllerHandler.ts:1117-1119).

### 1.3 Envelopes as typed (packages/ws-client/src/models/model.ts)
[packages/ws-client/src/models/model.ts:1516-1520]
~~~~
export interface CommandMessage {
    message_id: string;
    command: keyof APICommands;
    args?: APICommands[keyof APICommands]["requestArgs"];
}
~~~~
[packages/ws-client/src/models/model.ts:1715-1717]
~~~~
export interface ResultMessageBase {
    message_id: string;
}
~~~~
[packages/ws-client/src/models/model.ts:1765-1777]
~~~~
export interface ErrorResultMessage extends ResultMessageBase {
    error_code: number;
    details?: string;
    /** The Matter status received from the device, when matter.js reports the failure as one; see the API docs. */
    status?: number;
    cluster_status?: number;
    reason?: CommissionFailureReason;
}

export interface SuccessResultMessage extends ResultMessageBase {
    result: unknown;
}

~~~~
Error response as built: packages/ws-controller/src/server/WebSocketControllerHandler.ts:1148-1163
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1148-1163]
~~~~
        } catch (err) {
            logger.error(`[${connId}] WebSocket error response (${command})`, messageId, err);
            const errorCode = err instanceof ServerError ? err.code : ServerErrorCode.UnknownError;
            const status = statusResponseOf(err);
            return {
                response: {
                    message_id: messageId ?? "",
                    error_code: errorCode,
                    details: (err as Error).message,
                    ...(status !== undefined && { status: status.code }),
                    ...(status?.clusterCode !== undefined && { cluster_status: status.clusterCode }),
                    ...(err instanceof ServerError && err.reason !== undefined && { reason: err.reason }),
                },
            };
        }
    }
~~~~
Error enum: packages/ws-controller/src/types/WebSocketMessageTypes.ts:73-110 (ServerErrorCode); docs table docs/websockets_api.md:2379-2392 (codes 0-11, 100-106)
[packages/ws-controller/src/types/WebSocketMessageTypes.ts:73-110]
~~~~

/**
 * Error codes matching Python Matter Server for API compatibility.
 * @see https://github.com/matter-js/python-matter-server/blob/main/matter_server/common/errors.py
 */
export enum ServerErrorCode {
    /** Generic/unknown error */
    UnknownError = 0,
    /** Node commissioning failed */
    NodeCommissionFailed = 1,
    /** Node interview failed */
    NodeInterviewFailed = 2,
    /** Node is not ready (offline or not yet interviewed) */
    NodeNotReady = 3,
    /** Node not resolving (CASE session establishment failed) */
    NodeNotResolving = 4,
    /** Node does not exist */
    NodeNotExists = 5,
    /** SDK version mismatch */
    VersionMismatch = 6,
    /** SDK/Stack error */
    SDKStackError = 7,
    /** Invalid command arguments */
    InvalidArguments = 8,
    /** Invalid/unknown command */
    InvalidCommand = 9,
    /** OTA update check failed */
    UpdateCheckError = 10,
    /** OTA update failed */
    UpdateError = 11,
    /** OHF extension (not python-matter-server): value must equal ICD_MULTI_ADMIN_ERROR_CODE in ws-client model.ts. */
    IcdMultiAdmin = 100,
    /** OHF extension (not python-matter-server): OTA firmware image upload failed (corrupt file / store failure). */
    OtaUploadError = 101,
    /** OHF extension: the camera, the offer or the request rules the stream out; `details.reason` says which. */
    CameraStreamIncompatible = 102,
    /** OHF extension: the camera refused the allocation for lack of capacity. */
    CameraResourceExhausted = 103,
~~~~
Docs table of codes (docs/websockets_api.md:2379-2392, lines for 100-106 abbreviated here to code and name):
## Error Codes

Error codes match the [Python Matter Server](https://github.com/matter-js/python-matter-server) for API compatibility.

| Code | Name | Description |
|------|------|-------------|
| 0 | UnknownError | Generic/unknown error |
| 1 | NodeCommissionFailed | Node commissioning failed |
| 2 | NodeInterviewFailed | Node interview failed |
| 3 | NodeNotReady | Node is not ready (offline or not yet interviewed) |
| 4 | NodeNotResolving | Node not resolving (CASE session establishment failed) |
| 5 | NodeNotExists | Node does not exist |
| 6 | VersionMismatch | SDK version mismatch |
| 7 | SDKStackError | SDK/Stack error |
| 8 | InvalidArguments | Invalid command arguments |
| 9 | InvalidCommand | Invalid/unknown command |
| 10 | UpdateCheckError | OTA update check failed |
| 11 | UpdateError | OTA update failed |
| 100 | IcdMultiAdmin | OHF extension (not in Python Matter Server). ICD registration rejected because other-vendor admi
| 101 | OtaUploadError | OHF extension (not in Python Matter Server). `initiate_ota_upload` or `POST /ota-upload/<upload
| 102 | CameraStreamIncompatible | OHF extension. The camera, the offer or the request rules the stream out; `reason` sa
| 103 | CameraResourceExhausted | OHF extension. The camera refused the allocation for lack of capacity. `details` is a 
Request parse: packages/ws-controller/src/server/WebSocketControllerHandler.ts:896-911 (parseBigIntAwareJson; `{ message_id: string; command: string; args: any }`); unknown command -> ServerError.invalidCommand at packages/ws-controller/src/server/WebSocketControllerHandler.ts:1127-1128 (`Unknown command: ${command}`, ServerErrorCode.InvalidCommand=9 at WebSocketMessageTypes.ts:75-108).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:896-915]
~~~~

        // Stop per-connection watchdog timers deterministically; the close handlers also dispose,
        // but that races the socket close events.
        for (const connection of this.#connections) connection.dispose();
        this.#connections.clear();
        for (const { subscription } of this.#interactionSubscriptions.values()) subscription.close();
        this.#interactionSubscriptions.clear();

        const wss = this.#wss;
        // Wait for the WebSocket server to close properly
        return new Promise<void>((resolve, reject) => {
            wss.close(err => {
                if (err) {
                    reject(err);
                } else {
                    resolve();
                }
            });
        });
    }
~~~~

### 1.4 Event envelope (docs/websockets_api.md:2002-2012)
[docs/websockets_api.md:2002-2012]
~~~~
## Events

Events are sent to clients that have called `start_listening`. Events have this format:

```json
{
  "event": "event_name",
  "data": { ... }
}
```

~~~~
Event envelope built at packages/ws-controller/src/server/WebSocketControllerHandler.ts:1131 region: `toBigIntAwareJson({ event, data })` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:1195 in #broadcastEvent) and per-connection: `toBigIntAwareJson({ event: eventName, data: nodeDetails })` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:460-468).

### 1.5 Event names
Typed union: packages/ws-client/src/models/model.ts APIEvents at 1546; EventTypes union ~1690-1714:
1:[packages/ws-client/src/models/model.ts:1546-1714]
4:    node_added: {
5:        data: MatterNodeData;
7:    node_updated: {
8:        data: MatterNodeData;
10:    node_removed: {
11:        data: number | bigint;
13:    node_event: {
14:        data: MatterNodeEvent;
16:    attribute_updated: {
17:        data: [node_id: number | bigint, attribute_path: string, value: unknown];
19:    server_shutdown: {
20:        data: Record<string, never>;
22:    endpoint_added: {
23:        data: { node_id: number | bigint; endpoint_id: number };
25:    endpoint_removed: {
26:        data: { node_id: number | bigint; endpoint_id: number };
28:    server_info_updated: {
29:        data: ServerInfoMessage;
31:    thread_diagnostics_updated: {
32:        data: ThreadDiagnosticsBatch;
34:    network_topology_updated: {
35:        data: NetworkTopology;
42:    webrtc_callback: {
43:        data: WebRtcCallbackData;
45:    subscription_report: {
46:        data: SubscriptionReportData;
48:    subscription_ended: {
49:        data: SubscriptionEndedData;
61:    camera_session_ended: {
62:        data: CameraSessionEndedData;
69:    camera_stream_evicted: {
70:        data: CameraStreamEvictedData;
79:    data: MatterNodeData;
83:    data: MatterNodeData;
87:    data: number | bigint;
91:    data: MatterNodeEvent;
95:    data: [number | bigint, string, unknown];
99:    data: Record<string, never>;
103:    data: { node_id: number | bigint; endpoint_id: number };
107:    data: { node_id: number | bigint; endpoint_id: number };
111:    data: ServerInfoMessage;
115:    data: ThreadDiagnosticsBatch;
119:    data: NetworkTopology;
123:    data: WebRtcCallbackData;
138:    data: SubscriptionReportData;
143:    data: SubscriptionEndedData;
147:    data: CameraSessionEndedData;
151:    data: CameraStreamEvictedData;
Event heading list in docs (docs/websockets_api.md line: name):
2024:**node_added** - A new node was commissioned or imported
2045:**node_updated** - A node's structure, availability or set of attributes changed. After a server restart, a node is
2054:**node_removed** - A node was decommissioned
2065:**attribute_updated** - An attribute value changed. Values from a node's first report, after a server restart or af
2078:**endpoint_added** - An endpoint was added to a node (bridges)
2090:**endpoint_removed** - An endpoint was removed from a node
2104:**node_event** - A Matter event occurred (e.g., button press, switch position)
2125:**subscription_report** *(schema 14+)* - A report after the priming report of a subscription opened with `subscribe
2149:**subscription_ended** *(schema 14+)* - A subscription opened with `subscribe` ended without `unsubscribe`
2162:**server_info_updated** - Server configuration changed (e.g., credentials set)
2180:**server_shutdown** - Server is shutting down
2189:**thread_diagnostics_updated** - A Thread network's diagnostics batch changed (schema 12)
2211:**network_topology_updated** *(schema 13+)* - The derived network graph changed
2218:**webrtc_callback** *(schema 12+)* - A camera signalled for a WebRTC session
2271:**camera_session_ended** *(schema 14+)* - A session this connection opened ended without its own command
2315:**camera_stream_evicted** *(schema 14+)* - The server deallocated a stream to make room for a request
Server-side emit sites (grep 'event:' / #broadcastEvent / sendOrdered):
455:                eventName: "node_added" | "node_updated",
472:            const sendNodeFullDetails = (eventName: "node_added" | "node_updated", nodeId: NodeId) => {
475:                if (eventName === "node_updated") {
497:                    sendNodeFullDetails("node_updated", nodeId);
508:                sendNodeFullDetails("node_updated", nodeId);
515:                    case "node_added":
519:                        sendNodeFullDetails("node_added", nodeId);
521:                    case "node_updated":
528:                    case "node_removed":
562:                        return toBigIntAwareJson({ event: "attribute_updated", data: [nodeId, pathStr, value] });
588:                        connection.sendOrdered(toBigIntAwareJson({ event: "node_event", data: nodeEvent }));
599:                sendNodeDetailsEvent("node_added", nodeId);
617:                sendNodeDetailsEvent("node_updated", nodeId);
621:                sendNodeDetailsEvent("node_updated", nodeId);
625:                sendNodeDetailsEvent("node_removed", nodeId);
635:                    toBigIntAwareJson({ event: "endpoint_added", data: { node_id: nodeId, endpoint_id: endpointId } }),
647:                        event: "endpoint_removed",
658:                connection.sendOrdered(toBigIntAwareJson({ event: "node_added", data: testNode }), {
666:                connection.sendOrdered(toBigIntAwareJson({ event: "node_removed", data: nodeId }), {
680:                            event: "thread_diagnostics_updated",
702:                            toBigIntAwareJson({ event: "network_topology_updated", data: topology }),
736:                            connection.sendReliable(toBigIntAwareJson({ event: "camera_session_ended", data }));
748:                                event: "camera_stream_evicted",
766:                    connection.sendReliable(toBigIntAwareJson({ event: "webrtc_callback", data }));
884:        const shutdownMessage = toBigIntAwareJson({ event: "server_shutdown", data: {} });
1199:            event === "node_updated" && data !== null && typeof data === "object" && "node_id" in data
1217:        this.#broadcastEvent("server_info_updated", serverInfo);
1536:                        "subscription_report",
1553:                        "subscription_ended",
1588:    #sendSubscriptionEvent<E extends "subscription_report" | "subscription_ended">(
2094:            this.#broadcastEvent("node_updated", testNode);
2102:        this.#broadcastEvent("node_updated", this.#collectNodeDetails(nodeId));
2128:        this.#broadcastEvent("node_updated", this.#collectNodeDetails(nodeId));
2145:        this.#broadcastEvent("node_updated", this.#collectNodeDetails(nodeId));

### 1.6 Event verbatim shapes (docs/websockets_api.md:2013-2330)
[docs/websockets_api.md:2013-2330]
~~~~
### Delivery to slow clients

When a connection does not read fast enough, the server queues the events for it. When the queue is full, the oldest
queued `node_event` and `subscription_report` frames are dropped. `node_added`, `node_removed`, `endpoint_added` and
`endpoint_removed` are never dropped and keep their order; a `node_updated` that carries an added endpoint is sent
before its `endpoint_added`. A queued `node_updated` holds the state at send time, so it can lack an endpoint that
was added and removed again meanwhile. If more of these four are waiting than the queue holds, the server closes the
connection, and the client gets the current state again when it reconnects and calls `start_listening`.

### Node Events

**node_added** - A new node was commissioned or imported

```json
{
  "event": "node_added",
  "data": {
    "node_id": 1,
    "date_commissioned": "2024-01-01T00:00:00.000000",
    "last_interview": "2024-01-01T12:00:00.000000",
    "interview_version": 6,
    "available": true,
    "is_bridge": false,
    "attributes": { ... },
    "attribute_subscriptions": [],
    "endpoint_parents": { "1": 0, "2": 1 }
  }
}
```

`endpoint_parents` *(schema 14+)* maps endpoints in `attributes` other than 0 to their direct parent endpoint; a top-level endpoint has parent `0`. An endpoint whose parent the server does not know has no entry, and the map is `{}` when `attributes` is empty. A server that sends the field sends it with every message that carries node data; a test node imported from a dump without the field has none. The PartsList of the Descriptor cluster does not say this by itself: Root Node, Aggregator and Bridged Node endpoints may list every descendant, not only their children. The field states only where an endpoint sits; whether it is a bridged device or a part of a composed device is for the client to decide from the device types.

**node_updated** - A node's structure, availability or set of attributes changed. After a server restart, a node is unavailable until the device's first report has been applied, and then becomes available in a `node_updated` carrying the reported values. Other `node_updated` events with `available: false` can arrive while it waits. A node whose first report arrived before the server registered it is available at once, without such an update. After commissioning, a node can be sent once more when its first report has been applied

```json
{
  "event": "node_updated",
  "data": { ... }
}
```

**node_removed** - A node was decommissioned

```json
{
  "event": "node_removed",
  "data": 1
}
```

### Attribute Events

**attribute_updated** - An attribute value changed. Values from a node's first report, after a server restart or after commissioning, are not sent this way. A removed attribute is sent with the value `null`, the same as a `null` value of a nullable attribute, also when the server only finds the removal while it rebuilds the node data (after a first report, a reconnect from unavailable or a structure change). Later `node_updated` events no longer contain it. Attributes of an endpoint the device no longer has are not sent this way

```json
{
  "event": "attribute_updated",
  "data": [1, "1/6/0", true]
}
```

Format: `[node_id, "endpoint/cluster/attribute", value]`

### Endpoint Events

**endpoint_added** - An endpoint was added to a node (bridges)

```json
{
  "event": "endpoint_added",
  "data": {
    "node_id": 1,
    "endpoint_id": 3
  }
}
```

**endpoint_removed** - An endpoint was removed from a node

```json
{
  "event": "endpoint_removed",
  "data": {
    "node_id": 1,
    "endpoint_id": 3
  }
}
```

### Matter Events

**node_event** - A Matter event occurred (e.g., button press, switch position)

```json
{
  "event": "node_event",
  "data": {
    "node_id": 1,
    "endpoint_id": 1,
    "cluster_id": 59,
    "event_id": 1,
    "event_number": 12345,
    "priority": 1,
    "timestamp": 1704067200000,
    "timestamp_type": 1,
    "data": { "newPosition": 1 }
  }
}
```

### Subscription Events

**subscription_report** *(schema 14+)* - A report after the priming report of a subscription opened with `subscribe`

Sent only to the connection that opened the subscription, also when it has not called `start_listening`.

```json
{
  "event": "subscription_report",
  "data": {
    "node_id": 1,
    "subscription_id": 2864434397,
    "attributes": { "1/6/0": true },
    "events": []
  }
}
```

`attributes`, `events`, `attribute_status`, `event_status` and `data_versions` have the shapes of the `read` result.

Like `node_event`, `subscription_report` is queued under backpressure: when a connection does not read fast enough
and its outgoing queue passes the limit, the oldest queued node events and reports are dropped.
`subscription_ended` is never dropped; it is sent ahead of reports still queued. A report for a subscription can
therefore arrive after its `unsubscribe` response or after its `subscription_ended`. Ignore reports for
subscriptions you no longer track.

**subscription_ended** *(schema 14+)* - A subscription opened with `subscribe` ended without `unsubscribe`

Sent only to the owning connection, for example when the node was removed or the device stopped reporting.

```json
{
  "event": "subscription_ended",
  "data": { "node_id": 1, "subscription_id": 2864434397 }
}
```

### Server Events

**server_info_updated** - Server configuration changed (e.g., credentials set)

```json
{
  "event": "server_info_updated",
  "data": {
    "fabric_id": 1234567890,
    "compressed_fabric_id": 9876543210,
    "schema_version": 14,
    "min_supported_schema_version": 11,
    "sdk_version": "matter-server/1.1.7 (matter.js/0.17.5-alpha)",
    "wifi_credentials_set": true,
    "thread_credentials_set": true,
    "bluetooth_enabled": true
  }
}
```

**server_shutdown** - Server is shutting down

```json
{
  "event": "server_shutdown",
  "data": {}
}
```

**thread_diagnostics_updated** - A Thread network's diagnostics batch changed (schema 12)

Streamed as diagnostics are collected from Border Routers. On first collection for a network the
batch arrives incomplete and is refined over the ~20 s window (a `partialReason` marks incomplete /
failed batches; it is absent once complete). **Delivered only to connections that have issued a
Thread request** (`get_thread_diagnostics` / `get_thread_border_routers`) during their lifetime, so
older clients that don't understand the event never receive it.

```json
{
  "event": "thread_diagnostics_updated",
  "data": {
    "extPanIdHex": "1122334455667788",
    "networkName": "MyThreadNet",
    "collectedAt": 1730000000000,
    "source": "meshcop",
    "nodes": [],
    "partialReason": "in_progress"
  }
}
```

**network_topology_updated** *(schema 13+)* - The derived network graph changed

Carries the same `NetworkTopology` payload as `get_network_topology`, debounced and latest-wins
coalesced, plus a slow periodic refresh so sleepy-device drift is eventually reflected.
**Delivered only to connections that have issued `get_network_topology`** during their lifetime, so
pre-schema-13 clients never receive it.

**webrtc_callback** *(schema 12+)* - A camera signalled for a WebRTC session

The camera's half of the WebRTC signalling: its offer, its answer, its ICE candidates and its end of
the session, each carrying `event_type`, `webrtc_session_id`, `node_id`, `endpoint_id`, `fabric_index`
and a per-type `data`, which is `null` on a callback that carried no payload. An event of a session
started with a `client_session_key` also carries that key as `client_session_key`.

```json
{
  "event": "webrtc_callback",
  "data": {
    "event_type": "offer",
    "webrtc_session_id": 3,
    "node_id": 1,
    "endpoint_id": 1,
    "fabric_index": 1,
    "data": {
      "sdp": "v=0\r\n...",
      "ice_servers": [{ "urls": ["stun:stun.example.org:3478"] }],
      "ice_transport_policy": "all"
    }
  }
}
```

| `event_type` | `data` |
|---|---|
| `offer` | `{ sdp, ice_servers?, ice_transport_policy? }`. Answer it with `camera_provide_answer` |
| `answer` | `{ sdp }` |
| `ice_candidates` | `{ ice_candidates: [{ candidate, sdpMid, sdpMLineIndex }] }`. Send yours with `camera_provide_ice_candidates` |
| `end` | `{ reason }`, the cluster's `WebRTCEndReasonEnum` as a number |

**Delivered only to connections that have issued a WebRTC command** — `camera_start_stream`,
`camera_provide_answer`, `camera_provide_ice_candidates` or `send_webrtc_provider_command` — during
their lifetime, so a client that never asked for a session never receives an event type it does not
know.

Among those connections it is **routed to the one that owns the session**: a session opened by
`camera_start_stream` reaches the connection that opened it and no other, for every event type,
including the camera's `end`. Its events wait until the `camera_start_stream` response is sent. Such a
client does not need the `end` event: `camera_session_ended` reports every end of its session, the
camera's own included.

A session this server holds no record of reaches every opted-in connection instead: nothing names an
owner for it, and withholding the signalling would strand a session no client could complete. Three
cases reach that. A session opened on the raw `send_webrtc_provider_command` route registers nothing
with the camera subsystem, so its signalling is broadcast — including to a client driving a managed
session on the same camera. An event for a session whose record has already been dropped is broadcast
too. So is an event that arrives in the window between the server registering a session with its local
WebRTC requestor and recording it — the requestor answers `NOT_FOUND` for every signalling command
naming a session it has not registered, the camera's offer included, so nothing arrives before that
registration.

**camera_session_ended** *(schema 14+)* - A session this connection opened ended without its own command

```json
{
  "event": "camera_session_ended",
  "data": { "node_id": 1, "endpoint_id": 1, "webrtc_session_id": 3, "cause": "peer", "reason": 2 }
}
```

A client that opens sessions with `camera_start_stream` can listen here alone to learn that a session ended. The event is sent for every end the client did not ask for itself, the camera's own `End` included. The camera's `End` still arrives as a `webrtc_callback` `end` event as well, for clients of the raw route. A session started with a `client_session_key` carries that key as `client_session_key`; a session the camera lost because it gave the id to a new session carries the old session's key.

| `cause` | What happened |
|---|---|
| `peer` | The camera sent `End`. `reason` is its `WebRTCEndReasonEnum` value, present when the camera stated one. |
| `other_connection` | Another connection sent `camera_stop_stream`, or `EndSession` through `device_command`. Any connection may end any of this server's sessions: the camera's own `PeerNodeID` and fabric check decides that, not this server's records. |
| `camera_restarted` | The camera restarted. Either the camera reported a `StartUp` event and then, within 30 seconds, the first change of that endpoint's `CurrentSessions` no longer lists the session, or the camera gave the session's id to a new session. A `CurrentSessions` the server reads from its own copy of the camera's state, without a change from the camera, does not count. A camera hands out only an id it no longer holds: the Matter SDK camera numbers sessions from 0 again after a reboot. An id can also come back after the camera's counter wrapped, so this cause can name a session that ended without a restart. From then on the id's signalling goes to whoever opened the new session. |
| `camera_dropped` | A report of the camera's `CurrentSessions` no longer lists the session, without an `End` and not counted as a restart (see `camera_restarted`). |
| `node_removed` | The node was removed from the fabric. |

The server reads `CurrentSessions` from the node's subscription. It ends a session there only after a report listed the session and a later report no longer does, because a report built before the camera answered the offer does not list the new session yet. The attribute has no N (non-volatile) quality (§11.5.5), so it is not persistent data (§7.12.1) and a camera can start with an empty list after a reboot. A node that goes unavailable does not end its sessions: media can still flow, and the browser detects a failed connection itself. Without a subscription (`--disable-auto-subscription`) the server sees no reports, so neither cause comes from a report: only a reissued id ends a session, as `camera_restarted`.

Delivery:

- **Only to connections that have issued a camera command** — any `camera_*` command. `send_webrtc_provider_command` does not count: it exists since schema 12, so issuing it does not show that a client knows these events. A client that drives sessions only through `send_webrtc_provider_command` and wants these events sends any `camera_*` command once, for example `camera_get_capabilities`. A pre-schema-14 client never receives an event type it does not know.
- Among those, **to the connection that opened the session**.
- Never to the connection whose `camera_stop_stream` or `device_command` `EndSession` ended it. That connection has the answer to its own command, and a second report of the same fact would give it two messages with no order between them.
- A session this server holds no record of — one opened on the raw `send_webrtc_provider_command` route — is announced to every other such connection instead. Nothing names an owner, so withholding it would tell nobody; `webrtc_callback` broadcasts such a session's signalling for the same reason.

Three endings send nothing, because the client already knows or cannot be told:

| Ending | Why no event |
|---|---|
| The client's own `camera_stop_stream`, or its own `EndSession` on the `device_command` route | That command's response is the answer |
| The owning connection closed | The only connection this concerns is the one that went away |
| The server is shutting down | Every socket is closed before the sessions are ended; the `server_shutdown` event is what a client sees |

When `EndSession` fails:

- With anything but the camera's `NOT_FOUND`: the session is still open and is not announced. The next stop, disconnect or shutdown reaches it again. When a report or a peer `End` already showed the session gone, it ends with that cause (`camera_dropped`, `camera_restarted` or `peer`) and is announced to its owner.
- With `NOT_FOUND`: the camera states it holds no such session. The server's records go, and the owner is told even though the command failed. When a report or a peer `End` already showed the session gone, the event names that cause, also to a requester that is the owner, because its command did not end the session.
- For an id no record named, nothing is announced, because there was no session on either side to report.

`send_webrtc_provider_command` cannot send `EndSession` at all.

**camera_stream_evicted** *(schema 14+)* - The server deallocated a stream to make room for a request

```json
{
  "event": "camera_stream_evicted",
  "data": { "node_id": 1, "endpoint_id": 1, "kind": "video", "stream_id": 7 }
}
```

- `kind` is `"video"` or `"snapshot"`, the two kinds the make-room step can take. An audio stream holds neither an encoder nor a share of the camera's encoded pixel rate.
- The id is gone for good. Where the server allocates a replacement for the same range, the camera issues a new id, and a client still holding the old one has to allocate again.
- Sent to every connection that has issued a camera command, including the one the room was made for: no record says which connection holds a stream.
- For a video stream, that caller also reads the same ids in its `camera_start_stream` response under `video.evicted_stream_ids`. For a snapshot stream this event is the only report, because that field carries video stream ids.

When the server takes a stream is described under **Making room** in `camera_start_stream`.

~~~~

### 1.7 Command dispatch table (switch in packages/ws-controller/src/server/WebSocketControllerHandler.ts:947-1130)
Format: command | dispatch line | handler expression | doc heading line in docs/websockets_api.md
(handler = first result expression after the case label; doc = line of the command heading in docs/websockets_api.md)
- start_listening | WebSocketControllerHandler.ts:950 | result = this.#handleStartListening(args); | docs:162
- set_default_fabric_label | WebSocketControllerHandler.ts:954 | result = await this.#handleSetDefaultFabricLabel(args, connection); | docs:275
- get_fabric_label | WebSocketControllerHandler.ts:957 | result = this.#handleGetFabricLabel(args); | docs:293
- commission_with_code | WebSocketControllerHandler.ts:960 | result = await this.#handleCommissionWithCode(args); | docs:307
- commission_on_network | WebSocketControllerHandler.ts:963 | result = await this.#handleCommissionOnNetwork(args); | docs:342
- parse_onboarding_payload | WebSocketControllerHandler.ts:966 | result = this.#handleParseOnboardingPayload(args); | docs:369
- get_icd_state | WebSocketControllerHandler.ts:969 | result = await this.#handleGetIcdState(args); | docs:1148
- register_icd | WebSocketControllerHandler.ts:972 | result = await this.#handleRegisterIcd(args); | docs:1176
- resync_icd | WebSocketControllerHandler.ts:975 | result = await this.#handleResyncIcd(args); | docs:1211
- unregister_icd | WebSocketControllerHandler.ts:978 | result = await this.#handleUnregisterIcd(args); | docs:1194
- get_node | WebSocketControllerHandler.ts:981 | result = await this.#handleGetNode(args); | docs:185
- get_nodes | WebSocketControllerHandler.ts:984 | result = this.#handleGetNodes(args); | docs:173
- get_node_ip_addresses | WebSocketControllerHandler.ts:987 | result = await this.#handleGetNodeIpAddresses(args); | docs:983
- read_attribute | WebSocketControllerHandler.ts:990 | result = await this.#handleReadAttribute(args); | docs:533
- read | WebSocketControllerHandler.ts:993 | result = await this.#commandHandler.read( | docs:676
- subscribe | WebSocketControllerHandler.ts:998 | ({ result, afterResponse } = await this.#handleSubscribe(args, connection)); | docs:730
- unsubscribe | WebSocketControllerHandler.ts:1001 | result = this.#handleUnsubscribe(args, connection); | docs:795
- enable_auto_subscription | WebSocketControllerHandler.ts:1004 | result = await this.#handleSetAutoSubscription(args, true); | docs:812
- disable_auto_subscription | WebSocketControllerHandler.ts:1007 | result = await this.#handleSetAutoSubscription(args, false); | docs:812
- get_vendor_names | WebSocketControllerHandler.ts:1010 | result = await this.#handleGetVendorNames(args); | docs:1932
- device_command | WebSocketControllerHandler.ts:1013 | result = await this.#handleDeviceCommand(args, ownerId, connection); | docs:866
- batched_device_commands | WebSocketControllerHandler.ts:1016 | result = await this.#handleBatchedDeviceCommands(args); | docs:916
- send_webrtc_provider_command | WebSocketControllerHandler.ts:1019 | result = await this.#handleSendWebRtcProviderCommand(args); | docs:1868
- camera_get_capabilities | WebSocketControllerHandler.ts:1022 | result = await this.#handleCameraGetCapabilities(args); | docs:1263
- camera_start_stream | WebSocketControllerHandler.ts:1025 | ({ result, afterResponse } = await this.#handleCameraStartStream(args, ownerId, startingSessions)); | docs:1372
- camera_stop_stream | WebSocketControllerHandler.ts:1028 | result = await this.#handleCameraStopStream(args, ownerId); | docs:1690
- camera_snapshot | WebSocketControllerHandler.ts:1031 | result = await this.#handleCameraSnapshot(args); | docs:1722
- camera_release_stream | WebSocketControllerHandler.ts:1034 | result = await this.#handleCameraReleaseStream(args); | docs:1807
- camera_provide_answer | WebSocketControllerHandler.ts:1037 | result = await this.#handleCameraSignallingCommand(args, command); | docs:1634
- camera_provide_ice_candidates | WebSocketControllerHandler.ts:1040 | result = await this.#handleCameraProvideIceCandidates(args, ownerId, startingSessions); | docs:1660
- write_attribute | WebSocketControllerHandler.ts:1043 | result = await this.#handleWriteAttribute(args); | docs:573
- interview_node | WebSocketControllerHandler.ts:1046 | result = await this.#handleInterviewNode(args); | docs:958
- ping_node | WebSocketControllerHandler.ts:1049 | result = await this.#handlePingNode(args); | docs:970
- diagnostics | WebSocketControllerHandler.ts:1052 | result = { | docs:89
- remove_node | WebSocketControllerHandler.ts:1059 | result = await this.#handleRemoveNode(args); | docs:997
- set_wifi_credentials | WebSocketControllerHandler.ts:1062 | result = await this.#handleSetWifiCredentials(args); | docs:221
- set_thread_dataset | WebSocketControllerHandler.ts:1065 | result = await this.#handleSetThreadDataset(args); | docs:238
- remove_wifi_credentials | WebSocketControllerHandler.ts:1068 | result = await this.#handleRemoveWifiCredentials(args); | docs:266
- remove_thread_dataset | WebSocketControllerHandler.ts:1071 | result = await this.#handleRemoveThreadDataset(args); | docs:266
- get_all_credentials | WebSocketControllerHandler.ts:1074 | result = await this.#handleGetAllCredentials(); | docs:256
- get_thread_border_routers | WebSocketControllerHandler.ts:1077 | result = this.#controller.borderRouters.list(); | docs:472
- get_thread_diagnostics | WebSocketControllerHandler.ts:1081 | result = await this.#handleGetThreadDiagnostics(args); | docs:478
- get_network_topology | WebSocketControllerHandler.ts:1084 | result = await this.#handleGetNetworkTopology(args); | docs:506
- open_commissioning_window | WebSocketControllerHandler.ts:1087 | result = await this.#handleOpenCommissioningWindow(args); | docs:417
- discover_commissionable_nodes | WebSocketControllerHandler.ts:1090 | result = await this.#handleDiscoverCommissionableNodes(args); | docs:197
- get_matter_fabrics | WebSocketControllerHandler.ts:1093 | result = await this.#handleGetMatterFabrics(args); | docs:1011
- remove_matter_fabric | WebSocketControllerHandler.ts:1096 | result = await this.#handleRemoveMatterFabric(args); | docs:1023
- set_acl_entry | WebSocketControllerHandler.ts:1099 | result = await this.#handleSetAclEntry(args); | docs:1038
- set_node_binding | WebSocketControllerHandler.ts:1102 | result = await this.#handleSetNodeBinding(args); | docs:1070
- import_test_node | WebSocketControllerHandler.ts:1105 | result = await this.#handleImportTestNode(args); | docs:1946
- check_node_update | WebSocketControllerHandler.ts:1108 | result = await this.#handleCheckNodeUpdate(args); | docs:1095
- update_node | WebSocketControllerHandler.ts:1111 | result = await this.#handleUpdateNode(args); | docs:1107
- initiate_ota_upload | WebSocketControllerHandler.ts:1114 | result = await this.#controller.otaUploads.initiateOtaUpload(peerAddress); | docs:1120
- server_info | WebSocketControllerHandler.ts:1117 | result = await this.#getServerInfo(); | docs:80
- discover | WebSocketControllerHandler.ts:1120 | result = await this.#handleDiscoverCommissionableNodes({}); | docs:197
- get_loglevel | WebSocketControllerHandler.ts:1123 | result = this.#handleGetLogLevel(); | docs:98
- set_loglevel | WebSocketControllerHandler.ts:1126 | result = this.#handleSetLogLevel(args); | docs:122
Note: discover_commissionable_nodes and discover also at packages/ws-controller/src/server/WebSocketControllerHandler.ts:1090 and 1120; set_loglevel/get_loglevel at 1123-1126; default (InvalidCommand) at 1127.
Stub: docs/websockets_api.md:2471-2475 lists subscribe_attribute as answering InvalidCommand; grep for the string in ws-controller/ws-client src gives no case (not in dispatch).

### 1.8 Command arguments, typed (packages/ws-client/src/models/model.ts:977-1497, APICommands) verbatim
[packages/ws-client/src/models/model.ts:977-1497]
~~~~
export interface APICommands {
    start_listening: {
        requestArgs: Record<string, never>;
        response: Array<MatterNodeData>;
    };
    diagnostics: {
        requestArgs: Record<string, never>;
        response: {
            info: ServerInfoMessage;
            nodes: Array<MatterNodeData>;
            events: Array<MatterNodeEvent>;
        };
    };
    server_info: {
        requestArgs: Record<string, never>;
        response: ServerInfoMessage;
    };
    get_nodes: {
        requestArgs: { only_available?: boolean };
        response: Array<MatterNodeData>;
    };
    get_node: {
        requestArgs: { node_id: number | bigint };
        response: MatterNodeData;
    };
    commission_with_code: {
        requestArgs: {
            code: string;
            network_only?: boolean;
            wifi_credentials_id?: string;
            thread_dataset_id?: string;
            /**
             * Schema 14. Seconds to find the device and establish PASE with it, at most 172800 (48 hours, the longest a device
             * announces for commissioning); matter.js searches 3 minutes without it.
             */
            discovery_timeout_s?: number;
        };
        response: MatterNodeData;
    };
    commission_on_network: {
        requestArgs: {
            setup_pin_code: number;
            /** Discovery filter type: 0=None, 1=ShortDiscriminator, 2=LongDiscriminator, 3=VendorId, 4=DeviceType */
            filter_type?: number;
            /** Filter value (discriminator, vendor ID or device type depending on filter_type) */
            filter?: number;
            /** Direct IP address for commissioning */
            ip_addr?: string;
            /** Schema 14. Same as `discovery_timeout_s` of `commission_with_code`. */
            discovery_timeout_s?: number;
        };
        response: MatterNodeData;
    };
    /** Schema 14. Decodes a QR code payload or a manual pairing code without contacting any device. */
    parse_onboarding_payload: {
        requestArgs: { code: string };
        response: OnboardingPayloadResult;
    };
    set_wifi_credentials: {
        requestArgs: { ssid: string; credentials: string; id?: string };
        response: Record<string, never>;
    };
    set_thread_dataset: {
        requestArgs: { dataset: string; id?: string };
        response: Record<string, never>;
    };
    remove_wifi_credentials: {
        requestArgs: { id?: string };
        response: Record<string, never>;
    };
    remove_thread_dataset: {
        requestArgs: { id?: string };
        response: Record<string, never>;
    };
    /** List stored WiFi/Thread credential summaries (no secrets). @since schema 12 */
    get_all_credentials: {
        requestArgs: Record<string, never>;
        response: AllCredentialsSummary;
    };
    /** mDNS-discovered Thread Border Routers (passive). @since schema 12 */
    get_thread_border_routers: {
        requestArgs: Record<string, never>;
        response: BorderRouterEntry[];
    };
    /**
     * Per-Thread-network diagnostics. `ext_pan_id` selects one network (returns the batch, or `null`
     * when nothing is cached / diagnostics are disabled); omitted returns an array of all known
     * networks. `force` bypasses the cache. Also streamed via the `thread_diagnostics_updated` event.
     * @since schema 12
     */
    get_thread_diagnostics: {
        requestArgs: { ext_pan_id?: string; force?: boolean };
        response: ThreadDiagnosticsBatch | ThreadDiagnosticsBatch[] | null;
    };
    /**
     * Full Matter network topology (Thread mesh + Wi-Fi). `refresh` re-reads the Thread
     * neighbor/route tables (and Wi-Fi diagnostics) from online nodes before building the
     * snapshot; omitted/false builds from the current attribute cache. Issuing this command
     * also opts the connection in to the `network_topology_updated` event. @since schema 13
     */
    get_network_topology: {
        requestArgs: { refresh?: boolean };
        response: NetworkTopology;
    };
    open_commissioning_window: {
        requestArgs: {
            node_id: number | bigint;
            timeout?: number;
            iteration?: number;
            /**
             * Commissioning window type: 0=Enhanced, 1=Basic.
             * Currently ignored by the server (always uses Enhanced).
             */
            option?: 0 | 1;
            /** Discriminator value (null for random) */
            discriminator?: number | null;
        };
        response: CommissioningParameters;
    };
    discover: {
        requestArgs: Record<string, never>;
        response: CommissionableNodeData[];
    };
    interview_node: {
        requestArgs: { node_id: number | bigint };
        response: null;
    };
    get_icd_state: {
        requestArgs: { node_id: number | bigint };
        response: IcdStateData;
    };
    register_icd: {
        requestArgs: {
            node_id: number | bigint;
            allow_multi_admin?: boolean;
            ignored_vendors?: number[];
        };
        response: IcdStateData;
    };
    resync_icd: {
        requestArgs: { node_id: number | bigint };
        response: null;
    };
    unregister_icd: {
        requestArgs: { node_id: number | bigint; force?: boolean };
        response: IcdStateData;
    };
    device_command: {
        requestArgs: {
            node_id: number | bigint;
            /**
             * Required, except when `node_id` is a Group Node ID: then leave it out.
             */
            endpoint_id?: number | null;
            cluster_id: number;
            command_name: string;
            payload: unknown;
            /** Response type hint passed by the client SDK (currently unused by server) */
            response_type: unknown;
            timed_request_timeout_ms?: number | null;
            interaction_timeout_ms?: number | null;
        };
        /** The command's response payload, or `null` when it has none or was groupcast (no node answers). */
        response: unknown;
    };
    /**
     * `ProvideOffer` and `SolicitOffer` start a session and answer with its id; `ProvideAnswer` and
     * `ProvideIceCandidates` act on an existing session and answer `null`.
     *
     * For clients that allocate their own streams. For sessions from `camera_start_stream`, use
     * `camera_provide_answer`, `camera_provide_ice_candidates` and `camera_stop_stream`.
     */
    send_webrtc_provider_command: {
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            command_name: WebRtcProviderCommandName;
            payload: Record<string, unknown>;
        };
        response: unknown;
    };
    /** Read-only: what the camera reports and what is allocated on it. */
    camera_get_capabilities: {
        requestArgs: { node_id: number | bigint; endpoint_id: number };
        response: CameraCapabilitiesResult;
    };
    /** Sends `ProvideOffer` when `sdp` is set, `SolicitOffer` otherwise. `video: false` or `audio: false` leaves out that track. */
    camera_start_stream: {
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            /**
             * `LiveView`, `Recording` or `Analysis`, as listed in `camera_get_capabilities`
             * `supported_stream_usages` (case-insensitive). `Internal` is refused.
             */
            stream_usage: string;
            sdp?: string;
            video?: CameraVideoHints | false;
            audio?: CameraAudioHints | false;
            /** Up to 10 entries. */
            ice_servers?: CameraIceServer[];
            /** 1 to 16 characters. */
            ice_transport_policy?: string;
            metadata_enabled?: boolean;
            /**
             * Whether the server may deallocate an unused stream to make room. Defaults to true;
             * evicted ids are in `video.evicted_stream_ids`. Set false to keep other controllers'
             * streams; the request can then still succeed with `video.degraded` true.
             */
            allow_eviction?: boolean;
            /**
             * Defaults to false: the server rewrites the offer's sending audio sections so the camera is not
             * asked to play audio. `true` forwards the offer unchanged and requires `sdp`.
             */
            talkback?: boolean;
            /**
             * 1 to 128 characters, unique among this connection's sessions that are starting or open. Echoed
             * on every `webrtc_callback` and `camera_session_ended` event of the session, and accepted by
             * `camera_stop_stream` and `camera_provide_ice_candidates` in place of `webrtc_session_id`, also
             * while this command is still running.
             */
            client_session_key?: string;
        };
        response: CameraStartStreamResult;
    };
    /**
     * Ends the WebRTC session; its streams stay allocated. Also works for sessions from before a server
     * restart. `ended` is false when the camera answers `NOT_FOUND`: an unknown id or another
     * controller's session. Other failures throw.
     */
    camera_stop_stream: {
        /**
         * A `client_session_key` whose `camera_start_stream` is still running cancels that start: it fails
         * with error 7, and `ended` says whether the camera had opened a session that the server then ended.
         */
        requestArgs: { node_id: number | bigint; endpoint_id: number } & CameraSessionReference;
        response: { ended: boolean };
    };
    /**
     * Sends the SDP answer (`ProvideAnswer`) after `camera_start_stream` without `sdp`. The camera's
     * offer arrives as a `webrtc_callback` `offer` event. An unknown session id fails with `NOT_FOUND`.
     */
    camera_provide_answer: {
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            webrtc_session_id: number;
            sdp: string;
        };
        response: null;
    };
    /**
     * Sends ICE candidates to an existing session (`ProvideIceCandidates`). Entries have the same shape
     * as in a `webrtc_callback` `ice_candidates` event.
     */
    camera_provide_ice_candidates: {
        /**
         * A `client_session_key` whose `camera_start_stream` is still running queues the candidates; they
         * are sent once that command has answered, and this command answers when they were sent.
         */
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            /** At least one entry. */
            ice_candidates: WebRtcIceCandidate[];
        } & CameraSessionReference;
        response: null;
    };
    camera_snapshot: {
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            /** Ceiling: a capability larger than this in either dimension is not used. */
            max_resolution?: { width: number; height: number };
            /**
             * The size wanted, such as a thumbnail's. The server uses the smallest capability that reaches
             * it in every stated dimension (within `max_resolution`), or the largest allowed one when none
             * does, and asks the camera for the target fitted into that capability's aspect ratio, so the
             * camera can scale the image down. A camera that cannot scale returns the capability's own
             * size. `degraded` compares each side the target states with the smaller of the target and that
             * capability. Absent: the largest allowed capability.
             */
            resolution?: CameraResolutionTarget;
            /** Image codec name from `camera_get_capabilities`, e.g. "JPEG". */
            codec?: string;
            /**
             * Whether the camera burns its manufacturer watermark into the image. `true` fails with
             * error 102 on a camera without the `Watermark` feature. When unset on a camera with the
             * feature, no watermark is requested and snapshot streams with one are not used. The camera
             * may apply the video stream's setting instead; `camera_get_capabilities` shows the result.
             */
            watermark_enabled?: boolean;
            /** Whether the camera burns an on-screen display into the image. @see watermark_enabled */
            osd_enabled?: boolean;
        };
        response: CameraSnapshotResult;
    };
    /** Deallocates a stream, whoever allocated it. Fails with error 104 while the stream is in use. */
    camera_release_stream: {
        requestArgs: {
            node_id: number | bigint;
            endpoint_id: number;
            kind: "video" | "audio" | "snapshot";
            stream_id: number;
        };
        response: null;
    };
    remove_node: {
        requestArgs: { node_id: number | bigint };
        response: null;
    };
    get_vendor_names: {
        requestArgs: { filter_vendors?: Array<number> };
        response: { [key: string]: string };
    };
    /**
     * Schema 14. Paths use the `read_attribute` format; event paths are "endpoint/cluster/event". Only available with
     * the server running with auto subscription disabled.
     */
    subscribe: {
        requestArgs: Omit<InteractionReadArgs, "event_paths"> & {
            event_paths?: Array<{ path: string; is_urgent?: boolean }>;
            /** Seconds. */
            min_interval: number;
            /** Seconds. */
            max_interval: number;
            /** Defaults to false. */
            keep_subscriptions?: boolean;
        };
        /** Carries the priming report; `subscription_report` carries only the later ones. */
        response: InteractionReportData & { subscription_id: number };
    };
    /**
     * Schema 14. Only the connection that opened the subscription can end it. Only available with the server running
     * with auto subscription disabled.
     */
    unsubscribe: {
        /** A subscription id is only unique per node, so the node is part of it. */
        requestArgs: { node_id: number | bigint; subscription_id: number };
        response: null;
    };
    /**
     * Schema 14. Switches the server's own subscription to the node on, until the server restarts. Only available with
     * the server running with auto subscription disabled. Ends the node's subscriptions opened with `subscribe`. Resolves
     * once the subscription is active; fails when matter.js finds the device unreachable meanwhile, when the
     * subscription is not active within 4 minutes 15 seconds, or when the node is removed or the server shuts down
     * meanwhile, and the node then stays switched on. Also fails when `disable_auto_subscription` switches the node off
     * meanwhile.
     */
    enable_auto_subscription: {
        requestArgs: { node_id: number | bigint };
        response: null;
    };
    /** Schema 14. Only available with the server running with auto subscription disabled. */
    disable_auto_subscription: {
        requestArgs: { node_id: number | bigint };
        response: null;
    };
    /** Schema 14. */
    read: {
        requestArgs: InteractionReadArgs;
        response: InteractionReportData;
    };
    read_attribute: {
        requestArgs: {
            node_id: number | bigint;
            /** Single attribute path or array of paths. Supports wildcards (*) for cluster and attribute IDs. */
            attribute_path: string | string[];
            /** Filter by fabric (default: false) */
            fabric_filtered?: boolean;
        };
        response: AttributesData;
    };
    /**
     * Schema 14. Sends all commands to the node in one InvokeRequest, in the given order, never split and never combined
     * with other commands. Not for groups.
     */
    batched_device_commands: {
        requestArgs: {
            node_id: number | bigint;
            /** At most as many commands as the node accepts per invoke (its MaxPathsPerInvoke). */
            commands: Array<{ endpoint_id: number; cluster_id: number; command_name: string; payload?: unknown }>;
            /** Sends the batch after one Timed Request with this timeout (0 to 65535). */
            timed_request_timeout_ms?: number | null;
            /** Time the device may take to process the batch. */
            interaction_timeout_ms?: number | null;
        };
        /** One entry per command, in request order; a command's failure does not fail the request. */
        response: BatchedCommandResult[];
    };
    write_attribute: {
        requestArgs: {
            node_id: number | bigint;
            /**
             * `endpoint/cluster/attribute`. `*` as the endpoint writes every endpoint of the node that has the attribute
             * (schema 14), and is required when `node_id` is a Group Node ID.
             */
            attribute_path: string;
            value: unknown;
            /** Schema 14. Sends the write as a timed write with this timeout (0 to 65535); unicast only. */
            timed_request_timeout_ms?: number | null;
        };
        /**
         * The node's write status per path it answered for: one entry for a specific endpoint, one per endpoint reached
         * for a wildcard endpoint. `null` for a groupcast (no node answers).
         */
        response: Array<{
            Path: { EndpointId: number; ClusterId: number; AttributeId: number };
            Status: number;
            ClusterStatus?: number;
        }> | null;
    };
    ping_node: {
        requestArgs: { node_id: number | bigint; attempts?: number };
        response: NodePingResult;
    };
    import_test_node: {
        requestArgs: { dump: string };
        response: null;
    };
    get_node_ip_addresses: {
        requestArgs: { node_id: number | bigint; prefer_cache?: boolean; scoped?: boolean };
        response: Array<string>;
    };
    check_node_update: {
        requestArgs: { node_id: number | bigint };
        response: MatterSoftwareVersion | null;
    };
    update_node: {
        requestArgs: { node_id: number | bigint; software_version: number | string };
        response: MatterSoftwareVersion | null;
    };
    initiate_ota_upload: {
        requestArgs: Record<string, never>;
        response: OtaUploadTicket;
    };
    discover_commissionable_nodes: {
        requestArgs: Record<string, never>;
        response: CommissionableNodeData[];
    };
    get_matter_fabrics: {
        requestArgs: { node_id: number | bigint };
        response: MatterFabricData[];
    };
    remove_matter_fabric: {
        requestArgs: { node_id: number | bigint; fabric_index: number };
        response: Record<string, never>;
    };
    set_acl_entry: {
        requestArgs: { node_id: number | bigint; entry: AccessControlEntry[] };
        response: AttributeWriteResult[] | null;
    };
    set_node_binding: {
        requestArgs: { node_id: number | bigint; endpoint: number; bindings: BindingTarget[] };
        response: AttributeWriteResult[] | null;
    };
    set_default_fabric_label: {
        requestArgs: { label: string | null };
        response: null;
    };
    get_fabric_label: {
        requestArgs: Record<string, never>;
        response: { fabric_label: string | null };
    };
    get_loglevel: {
        requestArgs: Record<string, never>;
        response: LogLevelResponse;
    };
    set_loglevel: {
        requestArgs: { console_loglevel?: SettableLogLevelString; file_loglevel?: SettableLogLevelString };
        response: LogLevelResponse;
    };
}

/** Utility type to extract request args for a command */
export type ArgsOf<R extends keyof APICommands> = APICommands[R]["requestArgs"];

/** Utility type to extract response type for a command */
export type ResponseOf<R extends keyof APICommands> = APICommands[R]["response"];

/**
 * Log levels the server reports (`get_loglevel`/`set_loglevel` response): the
 * Python Matter Server names plus `notice`.
 */
export type LogLevelString = "critical" | "error" | "warning" | "notice" | "info" | "debug";

/**
 * Log levels `set_loglevel` accepts as input: the reported names plus the
 * matter.js aliases `fatal` (= `critical`) and `warn` (= `warning`).
 */
export type SettableLogLevelString = LogLevelString | "fatal" | "warn";

/** Response for get_loglevel and set_loglevel commands */
export interface LogLevelResponse {
    console_loglevel: LogLevelString;
    file_loglevel: LogLevelString | null;
}

/** Access Control Entry for set_acl_entry command */
export interface AccessControlEntry {
    privilege: number;
    auth_mode: number;
    subjects: Array<number | bigint> | null;
    targets: AccessControlTarget[] | null;
}

export interface AccessControlTarget {
    cluster: number | null;
    endpoint: number | null;
    device_type: number | null;
}

/** Binding target for set_node_binding command */
export interface BindingTarget {
    node: number | bigint | null;
    group: number | null;
    endpoint: number | null;
    cluster: number | null;
}

/** Attribute write result for set_acl_entry and set_node_binding responses */
~~~~

### 1.9 Command docs verbatim (args and examples), docs/websockets_api.md
[docs/websockets_api.md:76-530]
~~~~
## Commands

### Server Information

**server_info** - Get server information

```json
{
  "message_id": "1",
  "command": "server_info"
}
```

**diagnostics** - Get server diagnostics (info, nodes, and recent events)

```json
{
  "message_id": "1",
  "command": "diagnostics"
}
```

**get_loglevel** - Get current log levels *(Matter.js only)*

Returns the current log level for console output and optionally for file logging (if configured). This command is not available in the Python Matter Server.

```json
{
  "message_id": "1",
  "command": "get_loglevel"
}
```

Response:
```json
{
  "message_id": "1",
  "result": {
    "console_loglevel": "info",
    "file_loglevel": "debug"
  }
}
```

Note: `file_loglevel` is `null` if file logging is not configured.

**set_loglevel** - Set log levels temporarily *(Matter.js only)*

Change the log level for console and/or file logging. Changes are temporary and will be reset on the next server restart. This command is not available in the Python Matter Server.

```json
{
  "message_id": "1",
  "command": "set_loglevel",
  "args": {
    "console_loglevel": "debug",
    "file_loglevel": "info"
  }
}
```

Both arguments are optional - only provide the ones you want to change.

Log levels (from least to most verbose):
- `critical` - Only fatal errors
- `error` - Errors
- `warning` - Warnings and errors
- `notice` - Operator/user-significant lifecycle events
- `info` - Informational messages (default)
- `debug` - Debug output (verbose)

`set_loglevel` also accepts the matter.js aliases `fatal` (= `critical`) and `warn` (= `warning`).

Response returns the current levels after the change:
```json
{
  "message_id": "1",
  "result": {
    "console_loglevel": "debug",
    "file_loglevel": "info"
  }
}
```

### Listening and Node Discovery

**start_listening** - Start receiving events and get all nodes

When the `start_listening` command is issued, the server returns all existing nodes. From that moment on, all events (including node attribute changes) will be forwarded to this WebSocket connection.

```json
{
  "message_id": "1",
  "command": "start_listening"
}
```

**get_nodes** - Get all commissioned nodes

```json
{
  "message_id": "1",
  "command": "get_nodes",
  "args": {
    "only_available": false
  }
}
```

**get_node** - Get a single node by ID

```json
{
  "message_id": "1",
  "command": "get_node",
  "args": {
    "node_id": 1
  }
}
```

**discover** / **discover_commissionable_nodes** - Discover commissionable devices on the network

```json
{
  "message_id": "1",
  "command": "discover"
}
```

### Credentials

WiFi and Thread credentials are stored as **named lists** (schema 12). Each entry has an `id`; the
reserved `default` entry is what pre-12 callers use when they omit `id`, so older clients keep
working unchanged. Storing multiple networks lets commissioning pick which one to use (see
`commission_with_code`), and — for Thread — lets the server query diagnostics from any Border Router
whose network you hold credentials for.

Secrets are **write-only**: they are stored but never returned. `get_all_credentials` returns only
summaries. On `set_*`, a value is required; a WiFi password may be omitted **only** to keep the
already-stored secret for an **unchanged** SSID (re-saving an entry without resending the password).
A blank password on a new/changed SSID, or an empty Thread dataset, is rejected. To clear an entry —
including the reserved `default`, which cannot be deleted from the list — use `remove_wifi_credentials`
/ `remove_thread_dataset` (this zeroes both the SSID and the secret).

**set_wifi_credentials** - Set WiFi credentials for commissioning

Inform the controller about the WiFi credentials it needs to send when commissioning a new device.
Pass an optional `id` to address a named entry (omit for the `default` entry).

```json
{
  "message_id": "1",
  "command": "set_wifi_credentials",
  "args": {
    "ssid": "wifi-name-here",
    "credentials": "wifi-password-here",
    "id": "Guest"
  }
}
```

**set_thread_dataset** - Set Thread credentials for commissioning

Inform the controller about the Thread credentials it needs to use when commissioning a new device.
The dataset must be a non-empty hex-encoded operational dataset. Pass an optional `id` for a named
entry. A dataset carrying `pskc` + `networkKey` additionally enables **MeshCoP diagnostics** for that
Thread network (see Thread Network Diagnostics below).

```json
{
  "message_id": "1",
  "command": "set_thread_dataset",
  "args": {
    "dataset": "hex-encoded-operational-dataset",
    "id": "MyThreadNet"
  }
}
```

**get_all_credentials** - List stored credential summaries (schema 12)

Returns `{ wifi: [{ id, ssid }], thread: [{ id, networkName, extPanId }] }` — summaries only, never
secrets. The `default` entry is always present (treat it as unset unless `server_info`'s
`wifi_credentials_set` / `thread_credentials_set` is true).

```json
{ "message_id": "1", "command": "get_all_credentials" }
```

**remove_wifi_credentials** / **remove_thread_dataset** - Clear a stored entry (schema 12 for a named `id`)

Removes a named entry, or clears the reserved `default` (zeroing its SSID + secret). Omit `id` for the
`default` entry.

```json
{ "message_id": "1", "command": "remove_thread_dataset", "args": { "id": "MyThreadNet" } }
```

**set_default_fabric_label** - Set the default fabric label

The answer comes once the server holds the new label. The server then tells the connected nodes in the background, a few at a time. A node that is not reached keeps its old label until the server starts again and connects to it.

Ignored when the server is started with `--default-fabric-label` (env `DEFAULT_FABRIC_LABEL`): the request succeeds but the pinned label is kept. Read the effective value with `get_fabric_label`.

Also ignored per session across connections: the first WebSocket connection to issue `set_default_fabric_label` owns the label for the lifetime of that connection. Other connections' `set_default_fabric_label` requests then succeed but are ignored (and logged) until the owning connection disconnects, at which point the next connection to issue the command claims ownership. This stops two clients (e.g. two Home Assistant instances) from overwriting each other's label.

```json
{
  "message_id": "1",
  "command": "set_default_fabric_label",
  "args": {
    "label": "Home"
  }
}
```

**get_fabric_label** - Get the currently configured fabric label (schema 12+)

```json
{
  "message_id": "1",
  "command": "get_fabric_label",
  "args": {}
}
```

Response: `{ "fabric_label": "HomeAssistant" }`

### Commissioning

**commission_with_code** - Commission a new device using QR code or manual pairing code

For WiFi or Thread based devices, the credentials need to be set upfront, otherwise commissioning will fail. Supports both QR-code syntax (MT:...) and manual pairing code.

The controller will use Bluetooth for commissioning wireless devices. If Bluetooth is not available, commissioning will only work for devices already on the network (set `network_only: true`).

Using QR code:
```json
{
  "message_id": "1",
  "command": "commission_with_code",
  "args": {
    "code": "MT:Y.ABCDEFG123456789"
  }
}
```

Using manual pairing code (network only):
```json
{
  "message_id": "1",
  "command": "commission_with_code",
  "args": {
    "code": "35325335079",
    "network_only": true
  }
}
```

`discovery_timeout_s` (optional, schema 14) limits, in seconds, how long the server has to find the device and to set
up the PASE session with it. Allowed are numbers above 0 up to 172800 (48 hours, the longest a device announces for
commissioning); `null` counts as not given. Without it matter.js searches for 3 minutes. The commissioning steps after PASE are not
limited. A device that is found but not reached in time fails with `NodeCommissionFailed` and `reason` `timeout`; a device
never found, with `reason` `not-discovered`.

**commission_on_network** - Commission a device already on the network

Commission using setup PIN code with optional filtering by discriminator, vendor ID, product or device type.

```json
{
  "message_id": "1",
  "command": "commission_on_network",
  "args": {
    "setup_pin_code": 20202021,
    "filter_type": 2,
    "filter": 3840,
    "ip_addr": "192.168.1.100"
  }
}
```

Filter types:
- `0` - No filter (discover any)
- `1` - Short discriminator
- `2` - Long discriminator
- `3` - Vendor ID
- `4` - Device type

`discovery_timeout_s` works as for `commission_with_code`. The attempt at `ip_addr` keeps the PASE limit of matter.js;
the timeout applies to the discovery that follows when that attempt fails.

**parse_onboarding_payload** *(schema 14+)* - Decode a QR code payload or a manual pairing code

Decodes the code the way `commission_with_code` reads it, without contacting any device and without changing any
state.

```json
{
  "message_id": "1",
  "command": "parse_onboarding_payload",
  "args": { "code": "MT:Y.K90AFN00KA0648G00" }
}
```

- `code` (required, string): a QR code payload (starting with `MT:`) or an 11- or 21-digit manual pairing code.

Response for a QR code, one entry per payload, so a code that concatenates several payloads returns several:

```json
{
  "type": "qr",
  "payloads": [
    {
      "version": 0,
      "vendor_id": 65521,
      "product_id": 32768,
      "flow_type": 0,
      "discovery_capabilities": 4,
      "discriminator": 3840,
      "setup_pin_code": 20202021
    }
  ]
}
```

`vendor_id` and `product_id` are left out where the payload says 0 (unspecified). `flow_type` is 0 standard, 1
user-intent, 2 custom. `discovery_capabilities` is the bitmask as encoded in the payload. TLV data appended to the
payload is not returned.

Response for a manual pairing code; `vendor_id` and `product_id` only for the 21-digit form, and left out where it says 0:

```json
{ "type": "manual", "short_discriminator": 15, "setup_pin_code": 20202021 }
```

A code that cannot be decoded fails with `InvalidArguments` (8) and `reason` `invalid-payload`, as in
`commission_with_code`. A missing or non-string `code` fails with `InvalidArguments` without `reason`. An unexpected
error while decoding fails with `SDKStackError` (7) without `reason`.

**open_commissioning_window** - Open commissioning window to share a device

Open a commissioning window to allow another controller to commission a device already on this controller.

```json
{
  "message_id": "1",
  "command": "open_commissioning_window",
  "args": {
    "node_id": 1,
    "timeout": 300
  }
}
```

Response includes pairing codes and, *(Matter.js only)*, the window's structured fields, so a client can hand the window to another ecosystem without decoding the QR code. `discriminator` is the long (12-bit) discriminator the device advertises while the window is open, `commissioning_timeout` is the window duration in seconds. Detect these fields by their presence, not by `schema_version`. The `discriminator` request argument is ignored; a random discriminator is used:
```json
{
  "message_id": "1",
  "result": {
    "setup_pin_code": 12345678,
    "setup_manual_code": "35325335079",
    "setup_qr_code": "MT:Y.ABCDEFG123456789",
    "discriminator": 3840,
    "vendor_id": 65521,
    "product_id": 32768,
    "commissioning_timeout": 300
  }
}
```

### Thread Network Diagnostics

Read-only diagnostics for the Thread networks around the controller (schema 12). This is separate
from Matter-over-Thread commissioning and can be turned off entirely with `--disable-thread-diagnostics`
(env `DISABLE_THREAD_DIAGNOSTICS`) without affecting commissioning.

**How it works**

- The server passively discovers Thread **Border Routers** via mDNS (`_meshcop._udp`) and lists them
  with `get_thread_border_routers` — no credentials required.
- To collect per-node **diagnostics** for a network, the server needs a way in:
  - **MeshCoP (CoAP/DTLS)** — used when you've stored a Thread dataset carrying `pskc` + `networkKey`
    for that network (via `set_thread_dataset`). Stored datasets are registered at startup and
    whenever you set them.
  - **OTBR REST** — used automatically when a discovered Border Router exposes the OpenThread REST API.
  - When both are available the server prefers MeshCoP (CoAP) — it is much faster than the REST
    collection path. A network with neither yields a partial result with reason `no_credentials`.
- **First query is slow, then cached.** Diagnostics are collected over a streaming window (about
  20 seconds): a first partial batch resolves after ~5 s and more nodes fill in until the window
  closes (~20 s). So when a client opens the Thread panel for the first time in a session, expect the
  mesh to **populate progressively over ~20 s**, arriving via `thread_diagnostics_updated` events.
  Results are then **cached (~1 h)** and returned instantly on subsequent queries; pass `force: true`
  to bypass the cache and re-collect.

**get_thread_border_routers** - List discovered Thread Border Routers (passive, no credentials)

```json
{ "message_id": "1", "command": "get_thread_border_routers" }
```

**get_thread_diagnostics** - Fetch per-Thread-network diagnostics

- With `ext_pan_id`: awaits a collection and returns the batch, or `null` when nothing is cached /
  diagnostics are disabled.
- Without `ext_pan_id`: returns the **current cache** for all known networks (an array, possibly empty)
  **immediately**, and kicks off a background refresh whose fresh batches arrive via the
  `thread_diagnostics_updated` event. Use the `ext_pan_id` form when you need synchronously-fresh data
  for one network.
- `force: true` bypasses the cache and re-collects.

```json
{
  "message_id": "1",
  "command": "get_thread_diagnostics",
  "args": { "ext_pan_id": "1122334455667788", "force": false }
}
```

Issuing either Thread command opts the connection in to `thread_diagnostics_updated` events (see
Events); a client that never queries Thread data never receives them.

With `--disable-auto-subscription` the server does not discover Border Routers on its own: the first
`get_thread_border_routers`, `get_thread_diagnostics` or `get_network_topology` starts the discovery, so that first
call can return no Border Routers or diagnostics yet. A `get_thread_diagnostics` with `ext_pan_id` that starts the
discovery returns `null` instead of reporting the Border Router as unreachable; repeat it once discovery found it.

### Network Topology

**get_network_topology** *(schema 13+)* - Return the whole Matter network as a graph

Derives the Thread mesh and the Wi-Fi star from the nodes' own diagnostics, plus the discovered
Border Routers. Optional `refresh: true` re-reads the diagnostics from every online node first
(seconds, real radio traffic — user-initiated only, never polled). Issuing the command also opts
the connection in to `network_topology_updated` events (see Events).

```json
{
  "message_id": "1",
  "command": "get_network_topology",
  "args": { "refresh": false }
}
```

The response is a `NetworkTopology` (`{ collected_at, nodes[], connections[] }`); see
[the schema changelog](websocket-api-schema-changelog.md) for the node kinds, link directions and
strength values, and `packages/ws-client/src/models/model.ts` for the exact wire shape.

### Attribute Operations

> [!NOTE]
> When the server runs with `--disable-auto-subscription`, it does not subscribe to nodes, so there are no
> automatic `attribute_updated` or node events from device reports. You are responsible for subscribing as needed,
> or for switching the server's subscription on per node with `enable_auto_subscription` (see
~~~~

[docs/websockets_api.md:956-1230]
~~~~
### Node Management

**interview_node** - Re-interview a node (refresh its data)

```json
{
  "message_id": "1",
  "command": "interview_node",
  "args": {
    "node_id": 1
  }
}
```

**ping_node** - Ping a node to check connectivity

```json
{
  "message_id": "1",
  "command": "ping_node",
  "args": {
    "node_id": 1,
    "attempts": 3
  }
}
```

**get_node_ip_addresses** - Get IP addresses for a node

```json
{
  "message_id": "1",
  "command": "get_node_ip_addresses",
  "args": {
    "node_id": 1,
    "prefer_cache": false,
    "scoped": false
  }
}
```

**remove_node** - Remove/decommission a node

```json
{
  "message_id": "1",
  "command": "remove_node",
  "args": {
    "node_id": 1
  }
}
```

### Fabric Management

**get_matter_fabrics** - Get all fabrics on a node

```json
{
  "message_id": "1",
  "command": "get_matter_fabrics",
  "args": {
    "node_id": 1
  }
}
```

**remove_matter_fabric** - Remove a fabric from a node

```json
{
  "message_id": "1",
  "command": "remove_matter_fabric",
  "args": {
    "node_id": 1,
    "fabric_index": 2
  }
}
```

### ACL and Bindings

**set_acl_entry** - Set ACL entries on a node

Replaces the ACL entries for the controller's fabric on the target node. The server automatically determines the fabric index.

```json
{
  "message_id": "1",
  "command": "set_acl_entry",
  "args": {
    "node_id": 1,
    "entry": [
      {
        "privilege": 5,
        "auth_mode": 2,
        "subjects": [112233],
        "targets": null
      }
    ]
  }
}
```

Entry fields:
- `privilege`: 1=View, 3=Operate, 4=Manage, 5=Administer
- `auth_mode`: 1=PASE, 2=CASE, 3=Group
- `subjects`: Array of NodeIds or GroupIds (or null)
- `targets`: Optional target restrictions (or null) - each target has `cluster`, `endpoint`, `device_type` fields

Response: one entry, `[{ "path": { "endpoint_id": 0, "cluster_id": 31, "attribute_id": 0 }, "status": 0 }]`, with the
status the node answered. Every call sends the write to the node, also when the entries equal the current ones. A
subject that names the node itself is left out, because a node always has access to itself.

**set_node_binding** - Set bindings on a node endpoint

```json
{
  "message_id": "1",
  "command": "set_node_binding",
  "args": {
    "node_id": 1,
    "endpoint": 1,
    "bindings": [
      {
        "node": 2,
        "endpoint": 1,
        "cluster": 6
      }
    ]
  }
}
```

Response: one entry for the Binding attribute of `endpoint`, with the status the node answered. Every call sends the
write to the node, also when the bindings equal the current ones.

### Firmware Updates

**check_node_update** - Check for available firmware updates

```json
{
  "message_id": "1",
  "command": "check_node_update",
  "args": {
    "node_id": 1
  }
}
```

**update_node** - Apply a firmware update

```json
{
  "message_id": "1",
  "command": "update_node",
  "args": {
    "node_id": 1,
    "software_version": 2
  }
}
```

**initiate_ota_upload** *(schema 13+)* - Reserve an id for uploading a local `.ota` firmware file

Returns an `upload_id` that must be POSTed to `/ota-upload/<upload_id>` (see HTTP Endpoints
below) within `expires_in` seconds, from the same client that reserved it. Reserving an id also
claims one of the server's limited in-flight upload slots, so a client must follow through or let
the reservation expire before retrying. Requires OTA support to be enabled (no `--disable-ota`).

```json
{
  "message_id": "1",
  "command": "initiate_ota_upload",
  "args": {}
}
```

```json
{
  "upload_id": "3f9a1c2e8b7d4a10f6c9e0b2d4a17853",
  "expires_in": 60,
  "max_size": 67108864
}
```

### ICD Management

Manage this controller's Intermittently Connected Device (ICD) Check-In registration with a peer node.
Read the details about ICD devices and consequences of changing the ICD mode carefully before applying any changes.

**get_icd_state** - Get ICD state for a node

```json
{
  "message_id": "1",
  "command": "get_icd_state",
  "args": {
    "node_id": 1
  }
}
```

Response (`IcdStateData`):

```json
{
  "supported": true,
  "lit_supported": true,
  "registered": true,
  "operating_mode": "LIT",
  "awake": false,
  "available": true,
  "next_expected_checkin": 1735689600000
}
```

If the node has no ICD Management cluster, `supported` is `false` and all other fields are `false`/`null`.

**register_icd** - Register this controller as an ICD Check-In client

```json
{
  "message_id": "1",
  "command": "register_icd",
  "args": {
    "node_id": 1,
    "allow_multi_admin": false,
    "ignored_vendors": [4874]
  }
}
```

Response: `IcdStateData` (see `get_icd_state`).

Fails with error code `100 IcdMultiAdmin` if the peer has other-vendor administrators and `allow_multi_admin` is not set.

**unregister_icd** - Drop this controller's ICD Check-In registration

If other ecosystems are still registered the device might stay in the LIT mode, and connecting to the device might take a long time, and the server might automatically re-register after the next connection.

```json
{
  "message_id": "1",
  "command": "unregister_icd",
  "args": {
    "node_id": 1,
    "force": false
  }
}
```

Response: `IcdStateData` (see `get_icd_state`). `force` skips the peer round-trip (for an unreachable peer) and only clears local state.

**resync_icd** - Drop the local ICD registration and reconnect

This should be the last resort to try to get an ICD device in LIT mode connected again. Before doing this try to restart the device. It can take up to the "maximum IdleMode Time" of the device before it reconnects.

```json
{
  "message_id": "1",
  "command": "resync_icd",
  "args": {
    "node_id": 1
  }
}
```

A LIT peer re-registers automatically once the server's own subscription to it is active. With
`--disable-auto-subscription` the server only drops the local registration and does not reconnect, unless
`enable_auto_subscription` switched the server's own subscription to the node on. For a node without it, the device
registers again once `enable_auto_subscription` makes that subscription active; `register_icd` fails until then. A
subscription opened with `subscribe` does not count.

~~~~

[docs/websockets_api.md:1930-1960]
~~~~
### Vendor Information

**get_vendor_names** - Get vendor names by ID

```json
{
  "message_id": "1",
  "command": "get_vendor_names",
  "args": {
    "filter_vendors": [4874, 65521]
  }
}
```

### Test Nodes

**import_test_node** - Import test node(s) from a diagnostic dump

Import nodes from Home Assistant diagnostic dumps for testing purposes. Test nodes have node IDs >= 0xFFFFFFFE00000000.

```json
{
  "message_id": "1",
  "command": "import_test_node",
  "args": {
    "dump": "{\"data\":{\"node\":{...}}}"
  }
}
```

## HTTP Endpoints
~~~~

Camera commands (doc sections docs/websockets_api.md:1231-1929; arguments typed in model.ts above): camera_get_capabilities:1263, camera_start_stream:1372, camera_provide_answer:1634, camera_provide_ice_candidates:1660, camera_stop_stream:1690, camera_snapshot:1722, camera_release_stream:1807, send_webrtc_provider_command:1868; argument rules 1833-1867.
[docs/websockets_api.md:1833-1867]
~~~~
#### Rules for all camera commands

##### Argument rules

- `args` must be an object when stated. A message that leaves it out or sends `null` has an empty argument set, so it fails with error 8 for the missing `node_id`. A string, number, boolean or array is refused with error 8 naming the command.
- An unknown argument key is refused with error 8, not dropped: a dropped key would answer a request the client did not make. The refusal names the key and lists the keys the command takes.
- `node_id` must name one node: an operational node id, or a test node id. A group id or another special id (Matter Core specification § 2.5.5, Table 4) is refused with error 8, because a camera command talks to one camera.
- Send a large `node_id` as an integer literal or a bigint, not in exponent form: a JSON number above 2^53 cannot hold every integer, so the server refuses it with error 8 instead of addressing a different node.
- `endpoint_id` is a valid endpoint number (0 to 65534).

##### Names

Codecs, stream usages and talkback modes are names, not numbers. All names are matched case-insensitively.

- Codecs: `H264`, `H265`, `H266`, `AV1` (video), `OPUS`, `AAC` (audio), `JPEG`, `HEIC` (snapshot). The codec set is open: a codec the cluster enum does not define is reported as its decimal digits and accepted back in that spelling.
- Stream usages: `Internal`, `Recording`, `Analysis`, `LiveView`. They are closed for requests only. `camera_start_stream` refuses `Internal`, which marks a stream allocated for the device's own use, and any name not in this list. A session may still run on an existing `Internal` stream (§11.1.3.1.1). `camera_get_capabilities` still reports a usage the enum does not define as its decimal digits; a stream carrying it cannot be requested back.
- `two_way_talk_support`: `NotSupported`, `HalfDuplex`, `FullDuplex`. A value the enum does not define is reported as its decimal digits. It has no request side: talkback is asked for with `camera_start_stream`'s `talkback` argument and an offer whose audio can send, not by a hint (see **The offer's media sections** under `camera_start_stream`).

A client can send these reported values back in a request (the key name can differ):

| Reported by `camera_get_capabilities` | Send back as |
|---|---|
| `video.codecs` | `camera_start_stream`'s `video.codecs` |
| `audio.codecs` | `camera_start_stream`'s `audio.codecs` |
| `audio.channels` | `camera_start_stream`'s `audio.channel_count`, at most the reported number |
| `audio.sample_rates` | `camera_start_stream`'s `audio.sample_rate`, one of the reported rates |
| `limits.supported_stream_usages` | `camera_start_stream`'s `stream_usage`, any name but `Internal` |
| `snapshot.capabilities[].image_codec` | `camera_snapshot`'s `codec` |

Everything else the command reports is a fact about the camera, not a value to send back. `audio.bit_depths` has no hint: `AudioStreamAllocate` takes one bit depth and the server picks it from that list. A key a hint object does not take is refused with error 8 instead of being ignored, so a bound is never dropped without the caller hearing about it.

##### Resolutions

Every `resolution`, `min_resolution`, `max_resolution` and `sensor` on these commands is an object `{ "width": number, "height": number }`. On an argument — `camera_start_stream`'s `video.min_resolution` / `max_resolution` and `camera_snapshot`'s `max_resolution` — both fields must be an integer 1 to 65535, the range `VideoResolutionStruct` encodes them in. A negative, zero, fractional, `NaN`, infinite or larger value is refused with error 8 instead of reaching the camera.

~~~~

### 1.10 Schema version history (docs/websocket-api-schema-changelog.md headings: 14 at :13, 13 at :199, 12 at :227; min supported 11 has no section)
- Schema 11: MIN_SUPPORTED_SCHEMA_VERSION (packages/ws-controller/src/server/WebSocketControllerHandler.ts:119); no changelog entry ('Versions predating this document are not listed retroactively', changelog:8-9). WebRTC command+event first shipped under 11 without being documented (changelog:259-260).
- Schema 12 (changelog:227-272): named credential lists (get_all_credentials, optional id on set/remove wifi/thread, wifi_credentials_id/thread_dataset_id on commission_with_code); Thread diagnostics (get_thread_diagnostics, thread_diagnostics_updated, get_thread_border_routers fields); get_fabric_label, set_default_fabric_label pin/ownership semantics; WebRTC (send_webrtc_provider_command, webrtc_callback).
- Schema 13 (changelog:199-226): get_network_topology + network_topology_updated; initiate_ota_upload + POST /ota-upload/<upload_id>; error code 101 OtaUploadError; host_name/ssid/bssid presence-detectable fields.
- Schema 14 (changelog:13-198): camera API (camera_get_capabilities, camera_start_stream, camera_provide_answer, camera_provide_ice_candidates, camera_stop_stream, camera_snapshot, camera_release_stream, events camera_session_ended, camera_stream_evicted), error codes 102-106; batched_device_commands; write_attribute wildcard endpoint + ClusterStatus + Group Node ID groupcast; device_command timeouts and Group Node ID; args null/missing handling; node_id class refusal; open_commissioning_window extra fields; endpoint_parents in node data; error status/cluster_status/reason fields; parse_onboarding_payload; discovery_timeout_s; read, subscribe, unsubscribe, enable/disable_auto_subscription, subscription_report, subscription_ended, data_versions; subscribe_attribute removed from client model.
Verbatim changelog Schema 14 head (docs/websocket-api-schema-changelog.md:13-20) and the per-entry bullets:
[docs/websocket-api-schema-changelog.md:13-20]
~~~~
## Schema 14

Minimum supported: 11 (older clients keep working with the pre-14 command shapes).

Clients detect everything below via `server_info.schema_version >= 14`. The full reference, with every
argument, range and error payload, is in [websockets_api.md](websockets_api.md#camera-streaming-schema-14).

### Camera API
~~~~
[docs/websocket-api-schema-changelog.md:116-198]
~~~~
### Changes to existing commands and events

- **`send_webrtc_provider_command`**
  - Also relays `ProvideAnswer` and `ProvideIceCandidates`. `EndSession` goes through
    `camera_stop_stream`.
  - The camera's answer to an offer sent this way reaches the `answer` event with each media section's
    direction narrowed to what that offer allows (RFC 3264 §6.1), re-offers included.
  - Checks every payload field against the cluster definition and refuses a bad field with error 8.
    Newly refused: unknown keys, the same field given twice, missing mandatory fields, and a payload
    that gives both the stream list and the old single stream id.
  - `ice_servers` takes the documented `{ urls, username?, credential?, caid? }` shape. Before, that
    shape failed in the encoder.
  - Payload keys match with case and separators ignored, so `webrtc_session_id` and `sdpMLineIndex`
    are accepted.
- **`webrtc_callback`** — a session opened with `camera_start_stream` sends its events only to the
  connection that opened it. The opt-in now happens before the command runs, so the camera's first
  answer is no longer lost.
- **`device_command` with `EndSession`** also produces `camera_session_ended`.
- **`commission_on_network` `filter_type` 4** searches by the device type in `filter`; before, it
  searched for any device. Without `filter` it is refused with error 8.
- **`device_command` `interaction_timeout_ms`** is applied to the command; before, the server accepted
  it and ignored it. Anything other than an integer from 0 to 2^53-1 is refused with error 8.
- **New command `batched_device_commands`** `{ node_id, commands: [{ endpoint_id, cluster_id, command_name, payload? }],
  timed_request_timeout_ms?, interaction_timeout_ms? }` → `[{ status, cluster_status?, response? }]`, one entry per
  command in request order. Sends all commands in one InvokeRequest, never split or combined with other commands.
  Refused with `InvalidArguments` before sending: an empty list, a bad command, more commands than the node's
  MaxPathsPerInvoke, an invalid timeout, a Group Node ID or a WebRTC Transport Provider command.
- **`device_command`** refuses a command that is not in the server's model for the cluster, or fields matter.js cannot
  encode, with `InvalidArguments`; before, it failed with `UnknownError`.
- **`write_attribute`** takes `*` as the endpoint for a unicast node and writes every endpoint that has the
  attribute in one Write request; the result has one entry per endpoint the device answered for. The new
  `timed_request_timeout_ms` (0 to 65535; 0 counts as not given) sends a timed write. A wildcard cluster or
  attribute stays refused, and so does a malformed path to a node (only digits or `*` per segment, three segments).
- **`device_command` `timed_request_timeout_ms`** that is not an integer from 0 to 65535 is refused with error 8;
  before, such a value was ignored and the command was sent without a timed request.
- **`args`** — every command treats a missing or `null` `args` as no arguments, and refuses a
  non-object `args` with error 8.
- **`node_id`** — every command that targets a node refuses a `node_id` that can never name one (a
  CAT, PAKE, Unspecified or reserved id, and a Group id except on the two commands below) with error 8.
  Before, it answered `NODE_NOT_EXISTS`.
- **`write_attribute` and `device_command` to a Group Node ID** send a groupcast and answer `null`.
  The attribute path takes a wildcard endpoint. Before, a group write answered status 0, a success no
  device reported.

### Commissioning window

- **`discriminator`, `vendor_id`, `product_id`, `commissioning_timeout` in the `open_commissioning_window` result** (presence-detectable only: some schema-13 servers send them too, so check for the fields, not the schema version). `discriminator` is the long (12-bit) discriminator the device advertises while the window is open, `vendor_id`/`product_id` are the device's Vendor-ID and Product-ID from the BasicInformation cluster, `commissioning_timeout` is the window duration in seconds. They match the values encoded in the returned pairing codes; they are returned separately so clients (for example mobile apps sharing the device to another ecosystem) need no Matter QR decoder.

### Node data

- **`endpoint_parents` in node data** (every message that carries node data, for example `get_node`, `start_listening` and `node_updated`). Maps endpoints in `attributes` other than 0 to their direct parent endpoint (JSON keys are strings), `0` for a top-level endpoint. An endpoint whose parent the server does not know has no entry. A test node imported from a dump without the field has none. When the field is absent (server below schema 14, or such a test node), derive the parents from the Descriptor PartsLists as before.

### Error details

- **Optional `status` / `cluster_status` on error responses**: the Matter status the device answered with, when matter.js reports the failure as one: always for a `device_command` the device rejected, not for commands the server sends on its own behalf. `error_code` is unchanged.
- **Optional `reason` on `NodeCommissionFailed` errors of `commission_with_code` / `commission_on_network`, and on an undecodable pairing code**: `not-discovered`, `passcode-mismatch`, `invalid-payload`, `attestation-rejected`, `timeout` or `other`. A pairing code that cannot be decoded now fails with `InvalidArguments` and `reason: "invalid-payload"` instead of `UnknownError`.
- **Optional `ClusterStatus` in the `write_attribute` result**, next to `Status`, when the device sent a cluster-specific status.

### Commissioning

- **New command `parse_onboarding_payload`** `{ code }` → `{ type: "qr", payloads: [{ version, vendor_id?, product_id?,
  flow_type, discovery_capabilities, discriminator, setup_pin_code }] }` or `{ type: "manual",
  short_discriminator, setup_pin_code, vendor_id?, product_id? }`. Decodes a code as `commission_with_code` does, without
  contacting a device. An undecodable code fails with `InvalidArguments` and `reason` `invalid-payload`; an unexpected
  error while decoding fails with `SDKStackError`.
- **Optional `discovery_timeout_s` on `commission_with_code` and `commission_on_network`**: how long, in seconds, to find the device and set up PASE with it (above 0, at most 172800; `null` counts as not given). Without it matter.js searches for 3 minutes. The commissioning steps after PASE are not limited, and on `commission_on_network` the attempt at `ip_addr` keeps the matter.js PASE limit.

### Subscriptions and generic read

Meant only for the special `--disable-auto-subscription` mode, not for normal operation; the server's own subscription is much more reliable.

- **New command `read`** → `{ attributes, events, attribute_status?, event_status? }`. One Matter Read request with `attribute_paths`, `event_paths` (`[{ path }]`, `path` is `endpoint/cluster/event`), `event_min`, `fabric_filtered` (default `true`) and `data_version_filters` (`[{ path: "endpoint/cluster", data_version }]`). Without `event_min` the request has no event filter and returns all events the device holds. Refused with `InvalidArguments` before anything is sent: an argument of the wrong type, a malformed path or invalid id, no path, `event_min` or `data_version` out of range, `event_min` without `event_paths` and a non-empty `data_version_filters` without `attribute_paths`. `attributes` uses the `read_attribute` shape, `events` the `node_event` shape. `attribute_status` / `event_status` map each path the device answered with a status to `{ status, cluster_status? }` (numeric Matter status codes) and are left out when there is none.
- **New command `subscribe`** → `{ subscription_id, attributes, events, attribute_status?, event_status? }`: the id and the priming report in the `read` result shape. The `read` arguments plus `min_interval` / `max_interval` (seconds), `is_urgent` per event path and `keep_subscriptions` (default `false`, passed to the device as given; `false`, also when left out, ends all other subscriptions of this controller to the same node, including those of other connections, which arrive as `subscription_ended` at their connections). Only available when the server runs with `--disable-auto-subscription` and the node's auto subscription is off, otherwise `InvalidArguments`. `subscription_id` is the SubscriptionId the device assigned. The `read` refusals apply, and intervals outside 0 to 65535 or `max_interval` below `min_interval` are refused with `InvalidArguments` too. The subscription belongs to the connection that opened it and ends with `unsubscribe`, when that connection closes, when the node is removed or when the server shuts down.
- **`data_versions`** in the `read` and `subscribe` results and in `subscription_report`: `{ "endpoint/cluster":
  data_version }` for each cluster with a reported attribute value, left out when there is none. `subscribe` sends only
  the data version filters the caller gives, as `read` does, so the priming report holds every requested cluster the
  caller does not filter.
- **New command `unsubscribe`** (`{ node_id, subscription_id }`) → `null`. Ids are assigned per device, so the node is part of the name. Only available with `--disable-auto-subscription`. A `subscription_id` that is not an integer from 0 to 2^32-1, and a subscription this connection does not hold under that node and id, give `InvalidArguments`.
- **New commands `enable_auto_subscription` and `disable_auto_subscription`** (`{ node_id }`) → `null`. Switch the server's own subscription to one node on or off. Only available with `--disable-auto-subscription`, otherwise `InvalidArguments`; not kept across a restart. `enable_auto_subscription` returns once the server's subscription is active and fails with `NodeNotResolving` when matter.js finds the device unreachable while waiting, `NodeNotReady` when the subscription is not active within 4 minutes 15 seconds, `NodeNotExists` when the node is removed meanwhile and `UnknownError` when the server shuts down meanwhile, and the node then stays switched on; it fails with `NodeNotReady` when `disable_auto_subscription` switches the node off meanwhile, also during a `resync_icd`, and when a `resync_icd` meanwhile cannot switch the subscription back on. `disable_auto_subscription` returns once the subscription is closed. Enabling ends the node's subscriptions opened with `subscribe` (`subscription_ended`), and `subscribe` for a node with auto subscription on gives `InvalidArguments`.
- **New event `subscription_report`** → `{ node_id, subscription_id, attributes, events, attribute_status?, event_status? }`. Sent only to the connection that opened the subscription, whether or not it called `start_listening`; it carries the reports after the priming report, which the `subscribe` response carries.
- **New event `subscription_ended`** → `{ node_id, subscription_id }`. Sent to the owning connection when its subscription ends without `unsubscribe` (node removed, device stopped reporting, another subscription with `keep_subscriptions: false`); not sent for `unsubscribe`, when the connection closes or when the server shuts down (the connections are closed first; `server_shutdown` announces it). Never dropped under backpressure; reports for the id can still arrive after it.
- **`subscribe_attribute` removed from the client model.** The server never implemented it and answers `InvalidCommand`.

~~~~
[docs/websocket-api-schema-changelog.md:199-226]
~~~~
## Schema 13

Minimum supported: 11 (older clients keep working with the pre-13 command shapes).

### Network topology

- **New command `get_network_topology`** → a `NetworkTopology` (`{ collected_at, nodes[], connections[] }`) describing the whole Matter network as a graph: the Thread mesh (nodes + neighbor/route-table links, with external neighbors and mDNS-discovered Border Routers) and the Wi-Fi star (one `wifi_ap` pseudo-node per BSSID — id `ap_<BSSID>` with colons stripped, e.g. `ap_112233445566` — stations linked to it). Ethernet nodes appear unlinked. Optional `refresh` argument re-reads the Thread neighbor/route tables, routing role and network name (and Wi-Fi diagnostics) from every online node before building the snapshot (slower — seconds; best-effort, concurrency-capped, under an overall deadline, and shared between concurrent requesters); omitted/`false` builds from the current attribute cache.
  - Node kinds: `matter` (commissioned here; carries `node_id`), `border_router`, `thread_unknown` (a neighbour not commissioned on this fabric), `wifi_ap`. Thread connections keep both observed directions (`source_to_target` / `target_to_source`) so asymmetric links stay legible; the top-level `strength` is the strongest observed direction. `strength` values are `strong` / `medium` / `weak` / `none` / `unknown` — `none` means the link was observed dead (Thread pairs with no live direction are omitted entirely), `unknown` means no measurement was available (e.g. a Wi-Fi station whose RSSI can't be read) and must not be rendered as a dead link. Border Router classification depends on mDNS discovery: when the server runs with Thread diagnostics disabled, no `border_router` nodes appear and every external neighbour is reported as `thread_unknown`.
- **New event `network_topology_updated`** → a `NetworkTopology`, emitted (debounced, latest-wins coalesced) whenever the derived graph changes, plus a slow periodic refresh so sleepy-device drift is eventually reflected. **Delivered only to connections that have issued `get_network_topology`** during their lifetime — mirroring the schema-12 `thread_diagnostics_updated` opt-in so pre-schema-13 clients never receive an event type they didn't subscribe to. **Outgoing events carry no `require_schema`**; clients detect support via `server_info.schema_version >= 13`.
- **`host_name` on `border_router` nodes** (added after 1.4.0 shipped schema 13, so there is **no version signal** for it — treat it as presence-detectable only). The Border Router's mDNS host name as a *display label*, not an address: the trailing dot and a `.local` suffix are stripped (`"Cuisine.local."` -> `"Cuisine"`). Several vendors report an identical `vendor_name`/`model_name` pair for every unit they ship, which renders a multi-BR network as a row of identical labels; prefer `host_name` when present and keep the `vendor_name`/`model_name` fallback, which is permanent — the field is absent both on older servers and whenever a Border Router's `_meshcop` SRV host name was never resolved. The raw FQDN remains available verbatim via the schema-12 `get_thread_border_routers`.
- **`ssid` / `bssid` on Wi-Fi nodes** (also added after 1.4.0, presence-detectable only). Both are set on `wifi_ap` nodes and on the `matter` stations joined to them — stations previously carried no network identity at all. `bssid` is the radio address that `network_name` already carries on `wifi_ap` nodes, now under a self-describing name; `network_name` keeps its old value there so existing consumers are unaffected, and is superseded for Wi-Fi. `ssid` comes from NetworkCommissioning (`0/49/1`), whose `networkID` is the SSID only for Wi-Fi — it is the extended PAN id for Thread and the interface name for Ethernet — and some devices leave it empty, so a `bssid` fallback stays necessary. Note that one BSSID is one AP radio: a mesh reports several `wifi_ap` nodes sharing an `ssid`.

See `packages/ws-client/src/models/model.ts` for the exact `NetworkTopology` / `NetworkTopologyNode` /
`NetworkTopologyConnection` wire shapes (each field documented inline).

> This first iteration derives Thread links from the nodes' own `ThreadNetworkDiagnostics` (neighbor +
> route tables) and classifies externals against the passively-discovered Border Router registry. The
> richer MeshCoP diagnostic enrichment (route64 / childTable → router-to-router links and
> diagnostic-only mesh nodes) is a planned follow-up; the wire model already accommodates it.

### OTA firmware upload

- **New command `initiate_ota_upload`** (no arguments) → `{ upload_id, expires_in, max_size }`. Authorizes exactly one upload of a local `.ota` firmware image and claims one of the server's limited in-flight upload slots. `upload_id` is a random 32-hex string, single-use, bound to the client that reserved it, and valid for `expires_in` seconds — that window bounds when the following POST may *start*; the transfer itself is bounded by `max_size` (bytes), not by time. Fails with `OtaUploadError` (101) when OTA support is disabled (`--disable-ota`) or all slots are taken (`--ota-upload-max-in-flight`, default 5).
- **New HTTP endpoint `POST /ota-upload/<upload_id>`** on the same listener as `/ws` — not a WebSocket command, so a firmware image neither pays the ~33% base64 overhead nor has to be buffered whole in memory. Body is the raw `.ota` bytes. Answers `200` with a `MatterSoftwareVersion` (`update_source: "local"`), `400` `{ error_code, message }` for a corrupt image / unknown / expired / already-used / foreign-client id, `404` for a malformed id, `413` when the image exceeds `max_size` (`--ota-upload-max-size-mb`, default 64). The reservation and the staged file are always discarded before the response is sent, success or failure.
- **New error code `OtaUploadError` = 101** (OHF extension; python-matter-server codes stop at 11).
- Stored images are indexed by the vendor ID / product ID / software version in their header, not against a node: `check_node_update` surfaces one for any node whose vendor/product matches (its cached answer for that vendor/product is dropped on upload). *Test* images are only served when the server also runs with `--enable-test-net-dcl`, the same restriction that applies to `--ota-provider-dir`.
- **Clients detect support via `server_info.schema_version >= 13`** — the endpoint is unavailable both on older servers and when OTA is disabled.

~~~~

## 2. Data model

### 2.1 Node data held by the client (packages/ws-client/src/models/node.ts:9-33) and EndpointParentsData (model.ts:16)
[packages/ws-client/src/models/node.ts:9-33]
~~~~
/** Raw node data from server */
export interface MatterNodeData {
    node_id: number | bigint;
    date_commissioned: string;
    last_interview: string;
    interview_version: number;
    available: boolean;
    is_bridge: boolean;
    attributes: { [key: string]: unknown };
    /** Attribute subscriptions (always empty array in current protocol, matches Python Matter Server) */
    attribute_subscriptions: readonly [];
    /**
     * Matter specification version of the node (e.g., "1.2.0", "1.3.0", "1.4.0").
     * Determined from the SpecificationVersion attribute (0x15) if available,
     * otherwise estimated from DataModelRevision (0x0) on BasicInformation cluster.
     * Optional - not available in Python Matter Server.
     */
    matter_version?: string;
    /**
     * The direct parent endpoint of endpoints in `attributes` other than 0; a top-level endpoint has
     * parent 0, and an endpoint whose parent is not known has no entry. Not available in Python Matter
     * Server and older servers: derive the parents from the Descriptor PartsLists then.
     */
    endpoint_parents?: EndpointParentsData;
}
~~~~
[packages/ws-client/src/models/model.ts:14-16]
~~~~
export type AttributesData = { [key: string]: unknown };
/** The direct parent endpoint of each endpoint other than 0, keyed by endpoint number. */
export type EndpointParentsData = { [endpoint: string]: number };
~~~~
Test node id range: packages/ws-client/src/models/model.ts:1896 `export const TEST_NODE_START = 0xffff_fffe_0000_0000n;`, :1903 TEST_NODE_END = TEST_NODE_START + (1n << 32n) - 1n.
Node accessor attribute keys used by the client class MatterNode (node.ts:70-100): "0/40/5" nodeLabel, "0/40/1" vendorName, "0/40/3" productName, "0/40/15" serialNumber, "0/42/2" updateState, "0/42/3" updateStateProgress.

### 2.2 Attribute keys and path format
Attribute key = `${endpoint}/${cluster}/${attribute}`, decimal: packages/ws-controller/src/server/Converters.ts:670-672
[packages/ws-controller/src/server/Converters.ts:670-672]
~~~~
export function buildAttributePath(endpointId: number, clusterId: number, attributeId: number): string {
    return `${endpointId}/${clusterId}/${attributeId}`;
}
~~~~
Parse of paths (wildcards, 0xffff / 0xffffffff): Converters.ts:675-725
[packages/ws-controller/src/server/Converters.ts:675-725]
~~~~
function pathNumbersOf(path: string) {
    const [endpointStr, clusterStr, thirdStr] = path.split("/");
    const numberOf = (part: string | undefined, wildcard: number) => {
        const value = part !== undefined && /^\d+$/.test(part) ? parseInt(part, 10) : undefined;
        return value === wildcard ? undefined : value;
    };
    return {
        endpoint: numberOf(endpointStr, 0xffff),
        cluster: numberOf(clusterStr, 0xffffffff),
        third: numberOf(thirdStr, 0xffffffff),
    };
}

/**
 * Parse an attribute path string into its components.
 * Supports wildcards (*) for endpoint, cluster, and attribute IDs.
 * Non-numeric values are treated as wildcards and returned as undefined.
 *
 * @param path - Attribute path string in format "endpoint/cluster/attribute"
 * @returns Object with endpointId, clusterId, attributeId - each undefined if wildcard
 * @throws ServerError with InvalidArguments when a number is not a valid id of its kind
 */
export function splitAttributePath(path: string): {
    endpointId: EndpointNumber | undefined;
    clusterId: ClusterId | undefined;
    attributeId: AttributeId | undefined;
} {
    const { endpoint, cluster, third } = pathNumbersOf(path);
    return idsOf(path, () => ({
        endpointId: endpoint !== undefined ? EndpointNumber(endpoint) : undefined,
        clusterId: cluster !== undefined ? ClusterId(cluster) : undefined,
        attributeId: third !== undefined ? AttributeId(third) : undefined,
    }));
}

/** The matter.js id types reject an id out of range for its kind. */
function idsOf<T>(path: string, ids: () => T): T {
    try {
        return ids();
    } catch (error) {
        if (error instanceof ValidationError) {
            throw ServerError.invalidArguments(`Invalid id in path "${path}": ${error.message}`);
        }
        throw error;
    }
}

/**
 * True when `path` has exactly `segments` parts, each a decimal number or `*`. {@link splitAttributePath} reads any
 * other part as a wildcard, so a typo would widen the request.
 */
~~~~
Docs: docs/websockets_api.md:2331-2347 (Attribute Path Format), incl. unknown-cluster rule:
[docs/websockets_api.md:2331-2347]
~~~~
## Attribute Path Format

Attribute paths use the format: `endpoint/cluster/attribute`

- `1/6/0` - Endpoint 1, OnOff cluster (6), OnOff attribute (0)
- `0/40/1` - Endpoint 0, BasicInformation cluster (40), VendorName attribute (1)
- `*/6/*` - All endpoints, OnOff cluster, all attributes (wildcard)

A number that is not a valid id of its kind as matter.js defines it (for example cluster 32768, above the standard
cluster range that ends at 0x7fff) fails the command with `InvalidArguments`.

A value of a cluster, attribute or event that the server's data model does not know (for example a manufacturer
cluster without a custom cluster definition, or an attribute newer than the server) is sent as decoded from the TLV:
a structure becomes an object keyed by the member's tag number (`{"1": 5, "3": {"0": "a"}}`), bytes become base64,
lists become arrays. This also applies to the event data of `node_event`, which is otherwise keyed by field name.
Exception: the `FeatureMap` of an unknown cluster is sent as `{}`, because matter.js cannot decode its bits without the
cluster definition.
~~~~
Attribute value maps in node data: type `AttributesData = { [key: string]: unknown }` (model.ts:14).

### 2.3 Value encoding (server to client)
BigInt in JSON: docs/websockets_api.md:2369-2377
[docs/websockets_api.md:2369-2377]
~~~~
## BigInt Handling

Node IDs and some other fields (e.g., `fabric_id`, `compressed_fabric_id`, `event_number`, `timestamp`) may be BigInt values that exceed `Number.MAX_SAFE_INTEGER`. The server uses a custom JSON serializer that:

- Serializes BigInt values as unquoted numbers in JSON (e.g., `18446744069414584320` instead of `"18446744069414584320"`), at full 64-bit width for positive and negative values
- Because JSON has only a single numeric literal type, clients must use a parser or configuration that preserves large integer literals (or field-aware handling for known ID/counter fields) rather than relying on a drop-in `JSON.parse` replacement
- Standard JSON parsing that eagerly maps all numbers to IEEE-754 doubles may silently lose precision for these values instead of throwing an error; avoid using such parsers for Matter IDs and counters
- Non-JavaScript clients should use JSON parsing options/libraries that can keep large integers as big-integer types for these fields (for example: Python's `json` or `orjson`, which read 64-bit integers exactly — the bundled Python client uses `orjson` — Java's `BigInteger`-aware parsers, or Go's `encoding/json` with `UseNumber` combined with `math/big.Int`)
- The `@matter-server/ws-client` package handles this automatically
~~~~
Serializer/parser: packages/ws-client/src/json-utils.ts:21-52 (toBigIntAwareJson: bigint written as plain decimal digits), :60-150 (parseBigIntAwareJson: integers of 15+ digits outside safe range become bigint).
Matter model to wire conversion: packages/ws-controller/src/server/Converters.ts:405-531 (convertMatterToWebSocketTagBased / convertMatterToWebSocketNameBased / convertMatterToWebSocket). Verbatim core:
[packages/ws-controller/src/server/Converters.ts:398-430]
~~~~
    members = [...clusterModel.scope.membersOf(model)];
    byModel.set(model, members);
    return members;
}

/**
 * Uses the matter.js Model to convert the response data for read, subscribe and invoke into a tag-based response
 * including conversion of data types.
 *
 * Model classification and struct member info are cached in WeakMaps so that repeated calls
 * for the same model (e.g. across 160 nodes with identical clusters) skip the metabase traversal.
 */
export function convertMatterToWebSocketTagBased(
    value: unknown,
    model: ValueModel | undefined,
    clusterModel: ClusterModel | undefined,
): unknown {
    return convertMatterToWebSocket(value, model, clusterModel, true);
}

/**
 * Same as convertMatterToWebSocketTagBased but uses camelCase names instead of numeric tag IDs for struct keys.
 * Used for command (invoke) responses to match Python Matter Server behavior.
 */
export function convertMatterToWebSocketNameBased(
    value: unknown,
    model: ValueModel | undefined,
    clusterModel: ClusterModel | undefined,
): unknown {
    return convertMatterToWebSocket(value, model, clusterModel, false);
}

function convertMatterToWebSocket(
~~~~
[packages/ws-controller/src/server/Converters.ts:430-531]
~~~~
function convertMatterToWebSocket(
    value: unknown,
    model: ValueModel | undefined,
    clusterModel: ClusterModel | undefined,
    tagBased: boolean,
): unknown {
    if (value === null) {
        return null;
    }
    if (model === undefined) {
        if (Bytes.isBytes(value)) {
            return Bytes.toBase64(value);
        }
        if (Array.isArray(value)) {
            // Best-effort: recursively convert elements without schema
            return value.map(v => convertMatterToWebSocket(v, undefined, clusterModel, tagBased));
        }
        // matter.js decodes an unmodeled TLV structure to an object keyed by tag number
        if (isObject(value)) {
            return Object.fromEntries(
                Object.entries(value).map(([tag, member]) => [
                    tag,
                    convertMatterToWebSocket(member, undefined, clusterModel, tagBased),
                ]),
            );
        }
        if (!PRIMITIVE_TYPEOF.has(typeof value)) {
            return null;
        }
        return value;
    }

    switch (classifyModel(model)) {
        case ConvKind.Passthrough:
            return value;

        case ConvKind.EpochS:
            return typeof value === "number" ? value - MATTER_EPOCH_OFFSET_S : value;

        case ConvKind.EpochUS:
            return typeof value === "number" || typeof value === "bigint"
                ? BigInt(value) - MATTER_EPOCH_OFFSET_US
                : value;

        case ConvKind.Bytes:
            return value instanceof Uint8Array ? Bytes.toBase64(value) : value;

        case ConvKind.List: {
            if (!Array.isArray(value)) return value;
            const memberModel = listEntryModelOf(model);
            return value.map(v => convertMatterToWebSocket(v, memberModel, clusterModel, tagBased));
        }

        case ConvKind.Struct: {
            if (!isObject(value)) return value;
            const result: { [key: string]: unknown } = {};
            for (const { name, rawName, id, model: memberModel } of getStructMembers(model)) {
                if (Object.hasOwn(value, name)) {
                    const converted = convertMatterToWebSocket(value[name], memberModel, clusterModel, tagBased);
                    if (tagBased) {
                        result[id] = converted;
                    } else {
                        const wireName = matterNameToWireField(rawName, clusterModel?.name);
                        result[wireName] = converted;
                        if (wireName !== name) {
                            result[name] = converted;
                        }
                    }
                }
            }
            return result;
        }

        case ConvKind.Bitmap: {
            if (!isObject(value) || clusterModel === undefined) return value;
            let numberValue = 0;
            for (const member of getBitmapMembers(model, clusterModel)) {
                const memberTitle = member.title !== undefined ? camelize(member.title) : undefined;
                const memberValue =
                    member.name !== undefined && value[member.propertyName]
                        ? value[member.propertyName]
                        : memberTitle && value[memberTitle]
                          ? value[memberTitle]
                          : undefined;

                if (!memberValue) {
                    continue;
                }
                if (typeof memberValue !== "boolean" && typeof memberValue !== "number") {
                    throw new Error(`Invalid bitmap value for ${member.propertyName}: ${String(memberValue)}`);
                }

                const field = bitFieldOf(member);
                if (field !== undefined) {
                    numberValue |= packBitField(memberValue, field);
                }
            }
            // Bitmaps are unsigned on the wire, but |= yields a signed 32-bit result
            return numberValue >>> 0;
        }
    }
}
~~~~
Bytes: base64 (Converters.ts:440-441 `Bytes.toBase64`, :474-475 ConvKind.Bytes). Epoch-s / epoch-us shifted by MATTER_EPOCH_OFFSET (Converters.ts:464-472). Structs: tag-based (numeric id keys) for attribute data (tagBased true -> `result[id] = converted`, :489-490); name-based camelCase keys for command responses (convertMatterToWebSocketNameBased, doc comment :419-421) and events. Unknown (unmodelled) structs: object keyed by tag number (:447-452).
Date strings: getDateAsString Converters.ts:657-668 -> `YYYY-MM-DDTHH:mm:ss.SSS000` (local time, no zone).

### 2.4 Value decoding (client to server): write_attribute value, device_command payload
Generic: Converters.ts:86-130 convertWebSocketGenericToMatter (base64 string -> Bytes at :113-115; epoch-us adds MATTER_EPOCH_OFFSET_US at :122-123); tag-based structs: :135-180 convertWebSocketTagBasedToMatter (numeric keys -> camelCased member names; non-numeric names also resolved :157); command payload: :183-230 convertCommandDataToMatter; JSON-chip style: :534-655 convertWebsocketDataToMatter (hex bytes with a prefix: `Bytes.fromHex(data.slice(4))` at :600).
[packages/ws-controller/src/server/Converters.ts:86-130]
~~~~
function convertWebSocketGenericToMatter(value: unknown, model: ValueModel, clusterModel: ClusterModel): unknown {
    // Handle bitmaps - convert number to object with boolean flags
    if (typeof value === "number" && model.metabase?.metatype === "bitmap") {
        const bitmapValue: { [key: string]: boolean | number } = {};

        for (const member of getBitmapMembers(model, clusterModel)) {
            const memberName =
                member.name !== undefined && model.name !== "FeatureMap"
                    ? member.propertyName
                    : member.title !== undefined
                      ? camelize(member.title)
                      : undefined;

            if (memberName === undefined) {
                continue;
            }

            const field = bitFieldOf(member);
            if (field === undefined) {
                continue;
            }
            bitmapValue[memberName] = unpackBitField(value, field);
        }

        return bitmapValue;
    }

    // Handle bytes - convert base64 string to Uint8Array
    if (typeof value === "string" && model.metabase?.metatype === "bytes") {
        return Bytes.fromBase64(value);
    }

    // Handle epoch timestamps - convert from Unix timestamps to Matter epoch
    if (model.metabase?.metatype === "integer") {
        if (model.type === "epoch-s" && typeof value === "number") {
            return value + MATTER_EPOCH_OFFSET_S;
        } else if (model.type === "epoch-us" && (typeof value === "number" || typeof value === "bigint")) {
            return BigInt(value) + MATTER_EPOCH_OFFSET_US;
        }
    }

    // Return primitives as-is
    return value;
}

~~~~
[packages/ws-controller/src/server/Converters.ts:183-231]
~~~~
export function convertCommandDataToMatter(
    value: unknown,
    model: ValueModel | undefined,
    clusterModel: ClusterModel,
): unknown {
    if (model === undefined || value === null) {
        return value; // Return null/undefined values as-is
    }

    // Handle lists
    if (Array.isArray(value) && isList(model)) {
        const memberModel = listEntryModelOf(model);
        return value.map(v => convertCommandDataToMatter(v, memberModel, clusterModel));
    }

    // Handle structs - convert numeric keys to camelCased member names
    if (isObject(value) && model.metabase?.name === "struct") {
        const valueKeys = Object.keys(value);
        const result: { [key: string]: unknown } = {};

        const memberByName = getStructMembersByPropertyName(model);
        for (const key of valueKeys) {
            // Camelize the key to normalize PascalCase from Python CHIP SDK (e.g. DSTOffset -> dstOffset)
            const camelizedKey = camelize(key);
            const member = memberByName.get(camelizedKey);
            if (member !== undefined) {
                // Treat null for optional non-nullable fields as omitted (e.g. PINCode: null).
                // This preserves compatibility with clients that send null instead of omitting the field.
                if (value[key] === null && !member.mandatory && !member.nullable) {
                    continue;
                }
                result[camelizedKey] = convertCommandDataToMatter(value[key], member, clusterModel);
            } else {
                // Keep unknown keys as-is (fallback for unknown attributes)
                result[key] = value[key];
            }
        }
        return result;
    }

    return convertWebSocketGenericToMatter(value, model, clusterModel);
}

/**
 * Model conversion kinds, classified once per ValueModel and cached for fast dispatch.
 */
const enum ConvKind {
    Passthrough,
    EpochS,
~~~~

### 2.5 device_command encoding
Request shape typed at model.ts:1054-1066; handler packages/ws-controller/src/server/WebSocketControllerHandler.ts:1730-1850. Verbatim start of handler (field destructuring, group branch, camelize of command_name):
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1730-1790]
~~~~
    async #handleDeviceCommand(
        args: ArgsOf<"device_command">,
        requestedBy: string,
        connection: WebSocketConnection,
    ): Promise<ResponseOf<"device_command">> {
        const {
            node_id,
            endpoint_id: endpointId,
            cluster_id: clusterId,
            command_name: commandName,
            payload,
            timed_request_timeout_ms: timedInteractionTimeoutMs,
            interaction_timeout_ms: interactionTimeoutMs,
        } = args;

        const { nodeId, group } = this.#targetNodeIdOrGroup(node_id, "device_command");
        if (group) {
            if (endpointId !== undefined && endpointId !== null) {
                throw ServerError.invalidArguments(
                    "device_command to a Group Node ID takes no endpoint_id: a groupcast carries no endpoint, each node's own group table decides which of its endpoints the command reaches",
                );
            }
            // Check the stated value: a wrongly typed timeout converts to `undefined` and would pass.
            for (const [name, stated] of [
                ["timed_request_timeout_ms", timedInteractionTimeoutMs],
                ["interaction_timeout_ms", interactionTimeoutMs],
            ] as const) {
                if (stated !== undefined && stated !== null) {
                    throw ServerError.invalidArguments(
                        `${name} cannot accompany a groupcast: no node answers a group invoke, so there is no response to wait for`,
                    );
                }
            }
            await this.#commandHandler.handleGroupInvoke({
                nodeId,
                clusterId: ClusterId(clusterId),
                commandName,
                data: payload,
            });
            return null;
        }
        if (endpointId === undefined || endpointId === null) {
            throw ServerError.invalidArguments("device_command requires endpoint_id unless node_id is a Group Node ID");
        }
        const timedTimeout = timedRequestTimeoutArgument(timedInteractionTimeoutMs);
        const interactionTimeout = timeoutArgument(
            "interaction_timeout_ms",
            interactionTimeoutMs,
            MAX_INTERACTION_TIMEOUT_MS,
        );

        const camelizedCommand = camelize(commandName);
        const invoke = (): Promise<unknown> =>
            this.#handlerFor(nodeId).handleInvoke({
                nodeId,
                endpointId: EndpointNumber(endpointId),
                clusterId: ClusterId(clusterId),
                commandName: camelizedCommand,
                data: payload,
                timedInteractionTimeoutMs: timedTimeout,
                interactionTimeoutMs: interactionTimeout,
~~~~
Docs: docs/websockets_api.md:864-914 (included in 1.9). Payload keys are the matter.js camelCase field names (example `moveToLevelWithOnOff` with `level`, `transitionTime`, docs:891-905). command_name goes through `camelize(commandName)` (handler line ~1783).
Response conversion of command responses: `#convertCommandDataToWebSocket` packages/ws-controller/src/server/WebSocketControllerHandler.ts:2441-2462 (name-based).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:2441-2462]
~~~~
        clusterId: ClusterId,
        commandName: string,
        value: unknown,
        clusterData?: ClusterMapEntry,
    ) {
        if (!clusterData) {
            clusterData = ClusterMap[clusterId];
        }

        if (clusterData === undefined || clusterData.commands[commandName.toLowerCase()] === undefined) {
            logger.warn(
                `Cluster ${clusterId} does not have command ${commandName}. Do not convert data to WebSocket format`,
                value,
            );
            return {};
        }

        return convertMatterToWebSocketNameBased(
            value,
            clusterData.commands[commandName.toLowerCase()]!.responseModel,
            clusterData.model,
        );
~~~~

### 2.6 read_attribute / write_attribute
Docs excerpts in 1.9 (docs/websockets_api.md:533-621). Handler read: packages/ws-controller/src/server/WebSocketControllerHandler.ts:1494-1508; write: packages/ws-controller/src/server/WebSocketControllerHandler.ts:1653-1728.
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1494-1507]
~~~~
    async #handleReadAttribute(args: ArgsOf<"read_attribute">): Promise<ResponseOf<"read_attribute">> {
        const { node_id, attribute_path, fabric_filtered = false } = args;
        const nodeId = this.#targetNodeId(node_id, "read_attribute");

        // Normalize attribute_path to array
        const attributePaths = Array.isArray(attribute_path) ? attribute_path : [attribute_path];

        const result = await this.#handlerFor(nodeId).handleReadAttributes(nodeId, attributePaths, fabric_filtered);

        if (Object.keys(result).length === 0) {
            throw ServerError.sdkStackError("Failed to read attribute: no values returned");
        }

        return result;
~~~~
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1653-1728]
~~~~
    async #handleWriteAttribute(args: ArgsOf<"write_attribute">): Promise<ResponseOf<"write_attribute">> {
        const { node_id, attribute_path, value, timed_request_timeout_ms } = args;
        const { nodeId, group } = this.#targetNodeIdOrGroup(node_id, "write_attribute");
        // A malformed endpoint would otherwise read as the wildcard and write every endpoint of the node.
        if (!group && !isWellFormedPath(attribute_path, 3)) {
            throw ServerError.invalidArguments(
                'write_attribute needs attribute_path as endpoint/cluster/attribute, each a number or "*"',
            );
        }
        const { endpointId, clusterId, attributeId } = splitAttributePath(attribute_path);
        if (clusterId === undefined || attributeId === undefined) {
            throw ServerError.invalidArguments(WRITE_WILDCARD_REFUSAL);
        }

        if (group) {
            if (endpointId !== undefined) {
                throw ServerError.invalidArguments(
                    "write_attribute to a Group Node ID takes a wildcard endpoint in attribute_path: a groupcast carries no endpoint, each node's own group table decides which of its endpoints the write reaches",
                );
            }
            if (timed_request_timeout_ms !== undefined && timed_request_timeout_ms !== null) {
                throw ServerError.invalidArguments("timed_request_timeout_ms cannot accompany a groupcast write");
            }
            await this.#commandHandler.handleGroupWriteAttribute({ nodeId, clusterId, attributeId, value });
            return null;
        }

        const results = await this.#handlerFor(nodeId).handleWriteAttribute({
            nodeId,
            endpointId,
            clusterId,
            attributeId,
            value,
            timedInteractionTimeoutMs: timedRequestTimeoutArgument(timed_request_timeout_ms),
        });
        return results.map(({ endpointId, clusterId, attributeId, status, clusterStatus }) => ({
            Path: { EndpointId: endpointId, ClusterId: clusterId, AttributeId: attributeId },
            Status: status ?? 0,
            ...(clusterStatus !== undefined && { ClusterStatus: clusterStatus }),
        }));
    }

    async #handleGetVendorNames(args: ArgsOf<"get_vendor_names">): Promise<ResponseOf<"get_vendor_names">> {
        const { filter_vendors } = args;

        // Get vendor info from the DCL service
        const dclVendors = await this.#controller.getAllVendors();

        // Build merged result: DCL vendors override the static list but include static entries not in DCL
        const mergedVendors: { [key: string]: string } = {};

        // First add all static vendor IDs
        for (const [vendorIdStr, vendorName] of Object.entries(VendorIds)) {
            mergedVendors[vendorIdStr] = vendorName;
        }

        // Then override with DCL vendors (DCL wins)
        for (const [vendorId, vendorInfo] of dclVendors) {
            mergedVendors[vendorId] = vendorInfo.vendorName;
        }

        // If no filter, return all merged vendors
        if (!filter_vendors || !filter_vendors.length) {
            return mergedVendors;
        }

        // Filter to requested vendor IDs
        const result: { [key: string]: string } = {};
        for (const vendorId of filter_vendors) {
            const vendorName = mergedVendors[vendorId];
            if (vendorName) {
                result[vendorId] = vendorName;
            }
        }
        return result;
    }
~~~~
Result keys of read_attribute: map "endpoint/cluster/attribute" -> value (AttributesData); empty result throws sdkStackError('Failed to read attribute: no values returned') (packages/ws-controller/src/server/WebSocketControllerHandler.ts:1502-1504).

### 2.7 endpoint_parents
Docs: docs/websockets_api.md:2040-2043 (included in 1.6) and docs/websocket-api-schema-changelog.md:166 (schema 14). Server computation: packages/ws-controller/src/controller/AttributeDataCache.ts:37 (NodeSnapshot.endpointParents), :233-255 endpointParents(), :311 snapshot init, :361-370 population. Type: ws-client model.ts:16.
[packages/ws-controller/src/controller/AttributeDataCache.ts:228-260]
~~~~
     * Attribute reports patch the cached attributes between populates, so an endpoint the node added
     * since the last populate has attributes but no recorded parent; its parent is read from the node.
     * The recorded parent comes first: the node drops an endpoint before its removal is announced, and
     * until then the endpoint keeps its attributes here.
     */
    endpointParents(nodeId: NodeId, node: ClientNode): EndpointParentsData {
        const parents: EndpointParentsData = {};
        const snapshot = this.#cache.get(nodeId);
        if (snapshot === undefined) {
            return parents;
        }
        const seen = new Set<number>([0]);
        for (const path of Object.keys(snapshot.attributes)) {
            const endpointId = Number(path.slice(0, path.indexOf("/")));
            if (seen.has(endpointId)) {
                continue;
            }
            seen.add(endpointId);
            const parentId =
                snapshot.endpointParents[endpointId] ??
                (node.endpoints.has(endpointId) ? node.endpoints.for(endpointId).owner?.maybeNumber : undefined);
            if (parentId !== undefined) {
                parents[endpointId] = parentId;
            }
        }
        return parents;
    }

    /**
     * Check if a node exists in the cache.
     */
    has(nodeId: NodeId): boolean {
        return this.#cache.has(nodeId);
~~~~
[packages/ws-controller/src/controller/AttributeDataCache.ts:355-375]
~~~~
    /**
     * Collect attributes from all endpoints into a flat attribute object, and the parent of each endpoint,
     * yielding to the event loop between endpoints once the yield budget of work has passed, so a large node
     * does not block other timers, I/O, and WebSocket traffic.
     */
    async #collectSnapshot(node: ClientNode, snapshot: NodeSnapshot, context: PopulateContext): Promise<void> {
        const { attributes, endpointParents } = snapshot;
        for (const endpoint of node.endpoints) {
            const endpointId = endpoint.number;

            if (endpointId !== 0) {
                const parentId = endpoint.owner?.maybeNumber;
                if (parentId !== undefined) {
                    endpointParents[endpointId] = parentId;
                }
            }

            for (const behavior of endpoint.behaviors.active) {
                if (!ClusterBehavior.is(behavior)) {
                    continue;
                }
~~~~

### 2.8 Subscription commands (schema 14)
Typed args: model.ts subscribe/unsubscribe/enable_auto_subscription/disable_auto_subscription/read (in 1.8). Docs: docs/websockets_api.md:622-863, verbatim:
[docs/websockets_api.md:622-863]
~~~~
### Subscriptions and generic read

Schema 14. These commands give full control over the Matter Read and Subscribe requests. Most clients do not need
them: the server subscribes to every node on its own. `subscribe`, `unsubscribe`, `enable_auto_subscription` and
`disable_auto_subscription` are only available when the server runs with `--disable-auto-subscription`; otherwise
they fail with `InvalidArguments`, before anything is sent to a device. `subscribe` also fails this way for a node
whose auto subscription was switched on with `enable_auto_subscription`. `read` works in both modes.

> [!WARNING]
> These commands are meant only for the special `--disable-auto-subscription` mode, for example certification
> testing. Do not use them in normal operation: the server's own subscription is much more reliable. It keeps the
> node data and events up to date, so clients read current values from the node data, and it is set up again after a
> connection loss.

Paths:

- Attribute paths use the [attribute path format](#attribute-path-format) of `read_attribute`, wildcards included.
- Event paths use the same format with an event id: `endpoint/cluster/event`, for example `1/59/1`
  (Switch `InitialPress` on endpoint 1) or `*/59/*`.
- Data version filter paths are `endpoint/cluster` and must name both.
- Every part must be a decimal number or `*`, and a number must be a valid id of its kind as matter.js defines it (for
  example a standard cluster id below 0x8000, a standard event id below 0x100, a valid vendor prefix). Unlike `read_attribute`, which reads
  any other part as a wildcard, a malformed path is rejected with `InvalidArguments`. As in `read_attribute`, 65535 as
  endpoint and 4294967295 as cluster, attribute or event id also act as wildcards.
- `event_min` must be an integer from 0 to 2^64-1 and `data_version` one from 0 to 2^32-1, otherwise the request is
  rejected with `InvalidArguments`. Test nodes are not supported (`InvalidArguments`).
- `event_min` needs at least one event path, and a non-empty `data_version_filters` needs at least one attribute path,
  as the Matter Read Request allows EventFilters only with EventRequests and DataVersionFilters only with
  AttributeRequests. Otherwise the request is rejected with `InvalidArguments`.
- Every argument must have the documented type: lists must be lists, each `event_paths` and `data_version_filters`
  entry an object, paths strings and `fabric_filtered`, `keep_subscriptions` and, in `subscribe`, `is_urgent` `true`
  or `false`. `read` ignores `is_urgent`. An
  argument of another type is rejected with `InvalidArguments` before anything is sent to a device. An optional
  argument sent as `null` counts as left out.

Values in the results and reports have the same shape as in `read_attribute` (attributes) and `node_event`
(events). When the device answers a path with a status instead of data (for example an unsupported attribute),
the path is listed in `attribute_status` or `event_status`, keyed by the path in the request format, with the
numeric Matter status code in `status` (as in the `write_attribute` result) and the cluster-specific code in
`cluster_status` when the device sent one. Both fields are left out when there is no status.
`data_versions` holds the data version the device reported for each cluster with an attribute value in the result,
keyed by `endpoint/cluster`, the form `data_version_filters` takes; it is left out when no attribute value was
reported:

```json
{
  "attributes": { "1/6/0": true },
  "events": [],
  "attribute_status": { "1/6/9999": { "status": 134 }, "1/1234/0": { "status": 195 } },
  "event_status": { "1/69/5": { "status": 199 } },
  "data_versions": { "1/6": 2771420034 }
}
```

**read** *(schema 14+)* - Read attributes and events in one Matter Read request

```json
{
  "message_id": "1",
  "command": "read",
  "args": {
    "node_id": 1,
    "attribute_paths": ["1/6/0", "0/40/*"],
    "event_paths": [{ "path": "1/59/*" }],
    "event_min": 120,
    "fabric_filtered": true,
    "data_version_filters": [{ "path": "0/40", "data_version": 3141592 }]
  }
}
```

At least one attribute or event path is required. `fabric_filtered` defaults to `true` (unlike `read_attribute`,
which defaults to `false`). Matter has no default: FabricFiltered is a mandatory field of the request, and `true` is
the default of chip-tool and the Python controller. `event_min` becomes the EventFilter of the request; without it the
request has no event filter, also for a node with the server's own subscription, and returns every event the device
still holds on the requested paths. The request goes to the device as given: unlike `read_attribute`, it is not
split into batches and the server adds no data version filters of its own. For a LIT ICD with this controller
registered, matter.js holds the request while the device sleeps and sends it at the next check-in, as it does for
`write_attribute` and `device_command` (`subscribe` is sent at once); the request fails if the device does not check
in in time.

Side effects on all clients: matter.js merges the read attribute values into the node state, which sends the usual
`attribute_updated` events to every listening client, but only when `fabric_filtered` is `true`, the default.
matter.js applies a read only when its fabric filter matches the node's subscription settings, which the server
leaves at fabric-filtered, whether or not the node currently has the server's own subscription. Without
`--disable-auto-subscription`, the server forwards changes of a node only once its subscription has been active since
the server started, so a read of a node that has not been subscribed yet sends no `attribute_updated`. With
`--disable-auto-subscription` the server forwards changes of every node from the start. The events the read returns
are only in its response: they are not sent as `node_event`, not added to the event history, and do not change the
event number the server's own subscription resumes from.

Response:

```json
{
  "message_id": "1",
  "result": {
    "attributes": { "1/6/0": true, "0/40/1": "Vendor" },
    "events": [
      {
        "node_id": 1, "endpoint_id": 1, "cluster_id": 59, "event_id": 1, "event_number": 121,
        "priority": 1, "timestamp": 1704067200000, "timestamp_type": 1, "data": { "newPosition": 1 }
      }
    ]
  }
}
```

**subscribe** *(schema 14+)* - Open a Matter subscription for this connection

```json
{
  "message_id": "1",
  "command": "subscribe",
  "args": {
    "node_id": 1,
    "attribute_paths": ["1/6/0"],
    "event_paths": [{ "path": "1/59/*", "is_urgent": true }],
    "min_interval": 0,
    "max_interval": 60
  }
}
```

Arguments are those of `read` plus:

- `min_interval`, `max_interval` (required): the MinIntervalFloor and MaxIntervalCeiling in seconds, integers from
  0 to 65535, `max_interval` not below `min_interval`. For intermittently connected (ICD) devices matter.js sends
  a floor of 0 instead of `min_interval`.
- `is_urgent` per event path (optional): the IsUrgent flag of that event path.
- `keep_subscriptions` (optional, default `false`): the KeepSubscriptions flag, sent to the device as given
  (`false` is the default of chip-tool and the Python controller; Matter has no default). `false`, also when left
  out, ends all other subscriptions of this controller to that node, on the device and in the server, including
  those of other connections; each one that was opened with `subscribe` then arrives as `subscription_ended` at its
  connection. `true` keeps them.

Only the data version filters the caller gives are sent, as for `read`, so the priming report holds every requested
cluster the caller does not filter.

Response:

```json
{
  "message_id": "1",
  "result": {
    "subscription_id": 2864434397,
    "attributes": { "1/6/0": false },
    "events": []
  }
}
```

`subscription_id` is the SubscriptionId the device assigned. Each device assigns its own ids, so two nodes can use the
same one: a subscription is named by `node_id` and `subscription_id` together, in `unsubscribe` and in the events. The
rest of the result is the priming report, in the shape of the `read` result (`attributes`, `events`,
`attribute_status`, `event_status`, `data_versions`). Later reports follow as `subscription_report` events after this
response. All reports have the same side effects on all clients as `read`: `attribute_updated` to every listening client, only when
`fabric_filtered` is `true`, the default. Reported events reach only this subscription's reports, not `node_event` or
the event history.

A subscription belongs to the connection that opened it. It ends when the connection sends `unsubscribe`, when the
connection closes, when the node is removed, and when the server shuts down. It also ends when the device stops
reporting (no report within the max interval plus a response margin) or when another subscription of this
controller to the node is made with `keep_subscriptions` `false` or left out, from any connection. Every end the
connection did not ask for, including node removal, is announced with `subscription_ended`, except a server shutdown;
`unsubscribe` and closing the connection are not announced. A server shutdown is announced only with
`server_shutdown`: the server closes the connections before it ends their subscriptions, so no `subscription_ended`
is sent.

The server only learns that a subscription ended on the device when the local subscription times out or matter.js
closes it. A device that dropped the subscription (for example after a reboot) is noticed at the latest after the
max interval plus a response margin; until then the subscription still looks active.

**unsubscribe** *(schema 14+)* - End a subscription of this connection

```json
{
  "message_id": "1",
  "command": "unsubscribe",
  "args": { "node_id": 1, "subscription_id": 2864434397 }
}
```

Only available with `--disable-auto-subscription`; otherwise it fails with `InvalidArguments`. Returns `null`. Both
`node_id` and `subscription_id` are required; `subscription_id` must be an integer from 0 to 2^32-1, otherwise the
request fails with `InvalidArguments`. A subscription this connection does not hold under that node and id
gives an `InvalidArguments` error.
Matter has no unsubscribe message: the server stops accepting reports, and the device ends the subscription at its
next report.

**enable_auto_subscription** / **disable_auto_subscription** - Switch the server's own subscription to one node on or off

```json
{
  "message_id": "1",
  "command": "enable_auto_subscription",
  "args": { "node_id": 1 }
}
```

Only available with `--disable-auto-subscription`; otherwise they fail with `InvalidArguments` and the node is not
changed. Test nodes are not supported (`InvalidArguments`). With the server's own subscription on, a node behaves as
without `--disable-auto-subscription`: device reports send `attribute_updated` and `node_event` to every listening
client, `available` follows the connection to the device, and `register_icd` works. A LIT ICD registers on its own
once subscribed. Polling, time synchronization, fabric label pushes and automatic OTA update checks stay off. The
setting is not kept: every start switches all nodes off again. Switching a node to the state it already has changes
nothing.

The server's subscription asks the device only for events after the highest event number matter.js has received
from the node through the server's own subscription or the read after commissioning.

`enable_auto_subscription` returns `null` once the server's subscription to the node is active, so commands that
need it, such as `register_icd`, work from then on. If the node's subscription is already active, it returns at
once. It fails with an error, and the node stays switched on with matter.js trying to subscribe in the background,
when:

- matter.js finds the device unreachable while waiting: `NodeNotResolving` (4). Only a new finding counts: for a
  node that was already marked unreachable before, for example after a failed `read`, a failed try does not change
  that mark, so the command does not fail with `NodeNotResolving` and fails with `NodeNotReady` after the 4 minutes
  15 seconds below unless the subscription comes up.
- the subscription is not active within 4 minutes 15 seconds, the time matter.js allows a CASE connection while
  commissioning: `NodeNotReady` (3)
- the node is removed while waiting: `NodeNotExists` (5)
- the server shuts down while waiting: `UnknownError` (0)

It also fails with `NodeNotReady` (3) when `disable_auto_subscription` switches the node off while it waits; the
node then stays switched off, also when a `resync_icd` for the node runs at the same time. A `resync_icd` while it
waits does not make it fail, unless switching the subscription back on fails; then it fails with `NodeNotReady` (3)
at once and the node stays switched off. Send `disable_auto_subscription` to switch a node off again after a failure.
`disable_auto_subscription` returns `null` once the server's subscription is closed in the server. A node whose
device cannot be reached becomes unavailable as without `--disable-auto-subscription`.

The server's subscription is made with KeepSubscriptions `false`, so it ends every subscription opened with
`subscribe` to that node once its Subscribe request goes out, which is before a successful `enable_auto_subscription`
returns and, after a failed one, can be much later; each arrives as `subscription_ended` at its connection. While the
node's auto subscription is on, `subscribe` for it fails with `InvalidArguments`, also when it was enabled while the
`subscribe` was in progress. Do not send `enable_auto_subscription` while a `subscribe` for the same node with
`keep_subscriptions` `false` (the default) is still in progress: if that `subscribe` reaches the device after the
server's Subscribe request, the device can end the server's subscription, which the server then treats as active until
it times out. `disable_auto_subscription` ends the server's subscription; as with `unsubscribe`, the device ends it at
its next report.

~~~~
Argument parsing: packages/ws-controller/src/server/interactionArguments.ts (203 lines): readRequestOf, subscribeRequestOf, subscriptionIdOf (imported at packages/ws-controller/src/server/WebSocketControllerHandler.ts:91). Handlers: #handleSubscribe packages/ws-controller/src/server/WebSocketControllerHandler.ts:1519, #handleUnsubscribe packages/ws-controller/src/server/WebSocketControllerHandler.ts:1609, #handleSetAutoSubscription (dispatch packages/ws-controller/src/server/WebSocketControllerHandler.ts:1004,1007).
123:export function checkInteractionRequest(request: InteractionReadRequest | InteractionSubscribeRequest) {
178:export function readRequestOf(nodeId: NodeId, args: Record<string, unknown>): InteractionReadRequest {
185:export function subscribeRequestOf(nodeId: NodeId, args: Record<string, unknown>): InteractionSubscribeRequest {
198:export function subscriptionIdOf(value: unknown): number {
299:    #interactionSubscriptions = new Map<string, OwnedSubscription>();
901:        for (const { subscription } of this.#interactionSubscriptions.values()) subscription.close();
902:        this.#interactionSubscriptions.clear();
999:                    ({ result, afterResponse } = await this.#handleSubscribe(args, connection));
1002:                    result = this.#handleUnsubscribe(args, connection);
1005:                    result = await this.#handleSetAutoSubscription(args, true);
1008:                    result = await this.#handleSetAutoSubscription(args, false);
1519:    async #handleSubscribe(
1534:                    this.#sendSubscriptionEvent(
1545:                if (key === undefined || this.#interactionSubscriptions.get(key) !== entry) {
1548:                this.#interactionSubscriptions.delete(key);
1551:                    this.#sendSubscriptionEvent(

### 2.9 Cache of node data on the server (feeds get_node / start_listening)
AttributeDataCache (packages/ws-controller/src/controller/AttributeDataCache.ts, 'A node's attributes, and the parent of each endpoint', type NodeSnapshot :35-38) holds, per node, the attribute map in wire form; `Nodes` (packages/ws-controller/src/controller/Nodes.ts:21-27) holds ClientNode instances, the cache and last availability.
[packages/ws-controller/src/controller/Nodes.ts:10-30]
~~~~
import { AttributeDataCache } from "./AttributeDataCache.js";

/**
 * Manages node storage and tracks per-node availability.
 *
 * This class handles:
 * - Storage of ClientNode instances
 * - Node retrieval and existence checking
 * - Attribute data caching
 * - Connection state tracking for availability
 */
export class Nodes {
    #nodes = new Map<NodeId, ClientNode>();
    #attributeCache = new AttributeDataCache();
    /** Cached so serialization and event paths always agree on availability. */
    #lastAvailability = new Map<NodeId, boolean>();
    /**
     * Buffered endpoint_added events. python-matter-server wire contract requires
     * node_updated (carrying the new endpoint) to arrive before endpoint_added,
     * so HA can resolve node.endpoints[endpoint_id] in its callback.
     */
~~~~

## 3. Server architecture

### 3.1 Process entry
package bin: packages/matter-server/src/bin.ts:1-10 (verbatim tail):
[packages/matter-server/src/bin.ts:4-10]
~~~~
 * Copyright 2025-2026 Open Home Foundation
 * SPDX-License-Identifier: Apache-2.0
 */

// A shebang cannot portably pass --enable-source-maps. The import must stay dynamic: static imports load before this runs.
process.setSourceMapsEnabled(true);
await import("./MatterServer.js");
~~~~
Main module: packages/matter-server/src/MatterServer.ts (332 lines). Imports :8-40; storage.path var :84; ConfigStorage.create :163; WebSocketControllerHandler/handlers :230-246 ; WebServer :248; signals :330-332.
[packages/matter-server/src/MatterServer.ts:80-110]
~~~~
// matter-server is sole SIGINT/SIGTERM owner; matter.js's handler would race controller teardown.
env.vars.set("runtime.signals", false);

// Apply CLI options to environment variables
env.vars.set("storage.path", cliOptions.storagePath);
if (cliOptions.bleProxy) {
    if (cliOptions.bluetoothAdapter !== null) {
        logger.warn("--ble-proxy and --bluetooth-adapter are mutually exclusive. Using --ble-proxy.");
    }
    env.vars.set("ble.enable", true);
    logger.info("BLE proxy mode enabled");
} else if (cliOptions.bluetoothAdapter !== null) {
    env.vars.set("ble.enable", true);
    env.vars.set("ble.hci.id", cliOptions.bluetoothAdapter);
    logger.info(`Bluetooth enabled (hci-id=${cliOptions.bluetoothAdapter})`);
}
if (cliOptions.primaryInterface) {
    env.vars.set("mdns.networkInterface", cliOptions.primaryInterface);
}

const storageService = env.get(StorageService);
logger.info(
    `Using storage drivers: kv=${storageService.configuredDriver}, blob=${storageService.configuredBlobDriver}`,
);

let controller: MatterController;
let server: WebServer;
let config: ConfigStorage;
let legacyData: LegacyData;
let legacyDataWriter: LegacyDataWriter | undefined;
let fileLoggerClose: (() => Promise<void>) | undefined;
~~~~
[packages/matter-server/src/MatterServer.ts:160-182]
~~~~
        logger.info(`Found legacy data: ${parts.join(", ")}`);
    }

    config = await ConfigStorage.create(env);

    // If we found a most common fabric label in legacy data, use it as the default
    // (only applies on first migration when no fabricLabel is stored yet)
    if (
        legacyData.mostCommonFabricLabel?.length &&
        legacyData.mostCommonFabricLabel !== "HomeAssistant" &&
        config.fabricLabel === "HomeAssistant"
    ) {
        logger.info(`Setting fabric label from legacy data: "${legacyData.mostCommonFabricLabel}"`);
        await config.set({ fabricLabel: legacyData.mostCommonFabricLabel });
    }

    // A CLI-pinned fabric label overrides any persisted/legacy value and blocks later WS changes,
    // preventing two Home Assistant instances from playing fabric-label ping-pong.
    const pinnedFabricLabel = cliOptions.defaultFabricLabel?.trim();
    if (pinnedFabricLabel) {
        const label = pinnedFabricLabel.substring(0, 32);
        logger.info(`Pinning fabric label to "${label}" via --default-fabric-label`);
        await config.lockFabricLabel(label);
~~~~
[packages/matter-server/src/MatterServer.ts:230-258]
~~~~
        });
    }

    const wsHandler = new WebSocketControllerHandler(controller, config, MATTER_SERVER_VERSION);
    const handlers: WebServerHandler[] = [new HealthHandler(wsHandler), wsHandler];
    const reservedPaths = new Array<string>();
    if (!cliOptions.disableOta) {
        handlers.push(new OtaUploadHandler(controller.otaUploads));
        reservedPaths.push("/ota-upload");
    }
    if (bleProxyHandler) {
        handlers.push(bleProxyHandler);
    }
    if (!cliOptions.disableDashboard) {
        handlers.push(new StaticFileHandler(cliOptions.productionMode, reservedPaths));
    } else {
        logger.info("Dashboard disabled via CLI flag");
    }
    server = new WebServer({ listenAddresses: cliOptions.listenAddress, port: cliOptions.port }, handlers);

    if (!cliOptions.listenAddress) {
        logger.warn(
            `WebSocket server is listening on all network interfaces. Use --listen-address to restrict access. Ensure your environment (firewall, network) prevents unauthorized access.`,
        );
    }

    await server.start();
}

~~~~
[packages/matter-server/src/MatterServer.ts:328-332]
~~~~
});

process.on("SIGINT", () => void stop().catch(err => console.error(err)));
process.on("SIGTERM", () => void stop().catch(err => console.error(err)));
process.on("SIGUSR2", () => env.diagnose());
~~~~

### 3.2 HTTP and WebSocket server creation
Library: `ws` (WebSocketServer imported at packages/ws-controller/src/server/WebSocketControllerHandler.ts:29; `new WebSocketServer({ noServer: true })` at packages/ws-controller/src/server/WebSocketControllerHandler.ts:398). HTTP server: node:http `createServer()` per listen address (packages/matter-server/src/server/WebServer.ts:9,39). Path filter `/ws` at packages/ws-controller/src/server/WebSocketControllerHandler.ts:405; `/ble` at packages/ble-proxy/src/BleProxyHandler.ts:88. Default port 5580 (packages/matter-server/src/cli.ts:29 `const DEFAULT_PORT = 5580;`; docs/cli.md row --port). Handler order: HealthHandler, WebSocketControllerHandler, optional OtaUploadHandler, optional BleProxyHandler, optional StaticFileHandler (MatterServer.ts:~232-246).
[packages/matter-server/src/server/WebServer.ts:20-75]
~~~~
    constructor(config: WebServer.Config, handlers: WebServerHandler[]) {
        const { listenAddresses, port } = config;
        this.#listenAddresses = listenAddresses;
        this.#port = port;
        this.#handlers = handlers;
    }

    /** Port of the TCP listeners; the OS-chosen one when configured as 0. */
    get port() {
        return this.#port;
    }

    async start() {
        // Determine which addresses to bind to
        // null/empty means bind to all interfaces (single server with no host specified)
        const addresses = this.#listenAddresses?.length ? this.#listenAddresses : [undefined];

        // Create and start a server for each address
        for (const host of addresses) {
            const server = createServer();
            this.#servers.push(server);

            // Register all handlers with this server
            for (const handler of this.#handlers) {
                await handler.register(server);
            }

            // Fallback upgrade handler: handlers each register their own upgrade listener
            // that only claims its path (e.g. /ws, /ble) and silently ignore others, so a
            // request to an unknown path otherwise leaves the TCP socket open with no
            // response. Handlers tag the request via `req._matterHandledUpgrade = true`
            // when they claim an upgrade; this fallback runs after all listeners and 404s
            // anything that nobody claimed.
            server.on("upgrade", (req, socket) => {
                setImmediate(() => {
                    if (socket.destroyed) return;
                    if ((req as { _matterHandledUpgrade?: boolean })._matterHandledUpgrade) return;
                    try {
                        socket.write("HTTP/1.1 404 Not Found\r\nConnection: close\r\n\r\n");
                    } catch {
                        // socket already closing; destroy below
                    }
                    socket.destroy();
                });
            });

            // Start listening to this address
            await this.#startServer(server, host);
        }
    }

    async #startServer(server: HttpServer, host: string | undefined): Promise<void> {
        const socketPath = host !== undefined && isUnixSocketPath(host) ? host : undefined;

        if (socketPath !== undefined) {
            await removeStaleSocket(socketPath);
~~~~
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:384-436]
~~~~
    }

    async register(server: HttpServer) {
        logger.notice(`Starting server: matter-server/${this.#serverVersion} (matter.js/${MATTER_VERSION})`);
        // Use noServer mode with a path-filtered upgrade listener.
        // ws 8.x calls handleUpgrade unconditionally from its own upgrade listener, which
        // sends HTTP 400 for non-matching paths and destroys the socket — breaking other
        // WebSocket endpoints on the same server. By handling upgrade ourselves we only
        // call handleUpgrade when the path actually matches.
        //
        // Reuse a single WSS across all bound HTTP servers (multi-listen-address): create
        // it once, then attach a per-server upgrade listener that forwards into it. That
        // keeps `broadcast`, `unregister`, and the connection bookkeeping consistent.
        const isFirstBind = this.#wss === undefined;
        const wss = (this.#wss ??= new WebSocketServer({ noServer: true }));
        const upgradeHandler = (
            req: { url?: string; _matterHandledUpgrade?: boolean },
            socket: unknown,
            head: unknown,
        ) => {
            const path = req.url?.split("?")[0];
            if (path === "/ws") {
                req._matterHandledUpgrade = true;
                wss.handleUpgrade(
                    req as Parameters<typeof wss.handleUpgrade>[0],
                    socket as Parameters<typeof wss.handleUpgrade>[1],
                    head as Parameters<typeof wss.handleUpgrade>[2],
                    ws => wss.emit("connection", ws, req),
                );
            }
        };
        server.on("upgrade", upgradeHandler);
        this.#removeUpgradeListeners.push(() => server.removeListener("upgrade", upgradeHandler));

        if (!isFirstBind) {
            // Connection + observer wiring below is shared state; only attach once across binds.
            return;
        }

        wss.on("connection", (ws, req: { socket?: { remoteAddress?: string } }) => {
            if (this.#closed || this.#shuttingDown) {
                try {
                    ws.close(1001, "server shutting down");
                } catch {
                    // ignore
                }
                return;
            }

            const connId = nextConnectionLogTag();
            const ownerId = nextConnectionOwnerId();
            logger.info(`[${connId}] WebSocket connection established`);

~~~~
Interface WebServerHandler { register(server), unregister() }: packages/ws-controller/src/types/WebServer.ts (15 lines).
[packages/ws-controller/src/types/WebServer.ts:1-15]
~~~~
/**
 * @license
 * Copyright 2025-2026 Open Home Foundation
 * SPDX-License-Identifier: Apache-2.0
 */

import { type createServer } from "node:http";

export type HttpServer = ReturnType<typeof createServer>;

export interface WebServerHandler {
    register(server: HttpServer): Promise<void>;
    unregister(): Promise<void>;
    initiateShutdown?(): void;
}
~~~~

### 3.3 Connection handling
One class per connection for the outgoing path: packages/ws-controller/src/server/WebSocketConnection.ts:75 (`export class WebSocketConnection`, modes direct/queued, outbox, coalescing, watchdog). Defaults :55-59: DEFAULT_HIGH_WATER=1_048_576, DEFAULT_HIGH_WATER_CEILING=50*1_048_576, DEFAULT_CAP_BASE=1000, DEFAULT_CAP_PER_NODE=20, DEFAULT_WATCHDOG=Minutes(5).
[packages/ws-controller/src/server/WebSocketConnection.ts:60-74]
~~~~

/**
 * Per-connection adaptive send path with backpressure (docs/plans/ws-backpressure.md).
 *
 * In **direct** mode every send hits the socket immediately in emit order — zero overhead for the
 * common case. When a send pushes `ws.bufferedAmount` over the high-water mark the connection flips
 * to **queued** mode: frames drain from a single ordered outbox, kept-in-flight up to `highWater`
 * bytes (a window, not one frame at a time) so the pipe stays full and delivery is not serialized to
 * one round-trip per frame — which over a high-latency relay would stall a burst. Each flush callback
 * frees window space and pulls more. Attribute/node-snapshot sends coalesce latest-wins per key;
 * events are FIFO and, past the cap, dropped oldest-first, except node and endpoint additions and
 * removals, which are never dropped. An in-flight send stalled beyond the watchdog closes the dead
 * connection, and so do more of those undroppable events than the cap. The outbox empties → back to
 * direct mode.
 */
~~~~
Per-connection setup is a closure in WebSocketControllerHandler.register: connection event packages/ws-controller/src/server/WebSocketControllerHandler.ts:420; ids :431-433 (nextConnectionLogTag/nextConnectionOwnerId, packages/ws-controller/src/server/connectionIdentity.ts); opt-in flags :437-445; message handler :829-854; close/error :856-860; server_info send :862-867.
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:826-870]
~~~~
                if (CAMERA_OPT_IN_COMMANDS.has(command)) ensureCameraObservers();
            };

            ws.on("message", data => {
                this.#handleWebSocketRequest(
                    connId,
                    ownerId,
                    connection,
                    data.toString(),
                    optInToEventsFor,
                    startingSessions,
                    req?.socket?.remoteAddress,
                )
                    .then(({ response, enableListeners, afterResponse }) => {
                        if (this.#closed) return;
                        if (enableListeners) {
                            // Unlike the event opt-ins, listening starts only once start_listening has
                            // answered: its result is the node list the events then update.
                            listening = true;
                        }
                        try {
                            connection.sendReliable(toBigIntAwareJson(response));
                        } finally {
                            // Ends a hold of the command's events, which would otherwise last until disconnect.
                            afterResponse?.();
                        }
                    })
                    .catch(err => logger.error(`[${connId}] WebSocket request error`, err));
            });

            ws.on("close", onClose);
            ws.on("error", err => {
                logger.error(`[${connId}] WebSocket error`, err);
                onClose();
            });

            this.#getServerInfo()
                .then(response => {
                    logger.debug(`[${connId}] Sending server info`);
                    connection.sendReliable(toBigIntAwareJson(response));
                })
                .catch(err => logger.error(`[${connId}] WebSocket handshake error`, err));
        });

        // Initialize all nodes (populates attribute caches) and start connecting them.
~~~~

### 3.4 Routing commands to the controller
Dispatch is the switch in #handleWebSocketRequest (packages/ws-controller/src/server/WebSocketControllerHandler.ts:889-1135, table in 1.7). Per-node routing between real nodes and test nodes: `#handlerFor(nodeId)`, packages/ws-controller/src/server/WebSocketControllerHandler.ts:329-338 (TestNodeCommandHandler for ids >= TEST_NODE_START, else ControllerCommandHandler). Node id normalisation / group classification: `#targetNodeId`, `#targetNodeIdOrGroup` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:340-362; packages/ws-controller/src/util/nodeIdClasses.ts).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:326-362]
~~~~

    /**
     * Get the appropriate command handler for a node ID.
     * Returns TestNodeCommandHandler for test nodes, ControllerCommandHandler for real nodes.
     */
    #handlerFor(nodeId: number | bigint): TestNodeCommandHandler | ControllerCommandHandler {
        return TestNodeCommandHandler.isTestNodeId(nodeId) ? this.#testNodeHandler : this.#commandHandler;
    }

    /**
     * Refuses a Node ID class that can never name one node with error 8, not `NODE_NOT_EXISTS`.
     * Multicast-capable routes use {@link #targetNodeIdOrGroup}.
     */
    #targetNodeId(nodeId: number | bigint, command: string): NodeId {
        const target = NodeId(nodeId);
        const classified = nodeIdTarget(target);
        if (classified.kind !== "node") {
            throw ServerError.invalidArguments(
                `${command} cannot address ${classified.className}: node_id must name one node`,
            );
        }
        return target;
    }

    /**
     * For `write_attribute` and `device_command`, the only routes that can multicast. `group` selects the
     * multicast path, which takes no endpoint and gets no answer.
     */
    #targetNodeIdOrGroup(nodeId: number | bigint, command: string): { nodeId: NodeId; group: boolean } {
        const target = NodeId(nodeId);
        const classified = nodeIdTarget(target);
        if (classified.kind === "unusable") {
            throw ServerError.invalidArguments(
                `${command} cannot address ${classified.className}: node_id must name one node or one group`,
            );
        }
        return { nodeId: target, group: classified.kind === "group" };
~~~~
Opt-in sets for events: packages/ws-controller/src/server/WebSocketControllerHandler.ts:140-190 (THREAD_DIAGNOSTICS_OPT_IN_COMMANDS, NETWORK_TOPOLOGY_OPT_IN_COMMANDS, WEBRTC_OPT_IN_COMMANDS, CAMERA_OPT_IN_COMMANDS).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:140-192]
~~~~
}

// Issuing any of these (schema 12) proves the connection is Thread-aware, so it opts the connection
// in to `thread_diagnostics_updated` — even if the request itself errors. See the schema changelog.
const THREAD_DIAGNOSTICS_OPT_IN_COMMANDS = new Set(["get_thread_diagnostics", "get_thread_border_routers"]);

// Issuing this (schema 13) opts the connection in to `network_topology_updated`, mirroring the
// thread-diagnostics opt-in: pre-schema-13 clients never subscribed, so they must not receive it.
const NETWORK_TOPOLOGY_OPT_IN_COMMANDS = new Set(["get_network_topology"]);

// Commands that opt the connection in to `webrtc_callback`. Owned sessions are routed to their owner;
// this set decides who receives signalling for a session no record names.
const WEBRTC_OPT_IN_COMMANDS = new Set([
    "send_webrtc_provider_command",
    "camera_start_stream",
    "camera_provide_answer",
    "camera_provide_ice_candidates",
]);

// Any schema-14 camera command opts the connection in to camera_session_ended and camera_stream_evicted.
// `send_webrtc_provider_command` is schema 12 and stays out, so a schema-12 client never receives events it
// predates (python client before v1.2.0 raises AttributeError from its listen loop on an unknown event type).
// Evictions go to every opted-in connection: no record says who holds a stream.
const CAMERA_OPT_IN_COMMANDS = new Set([
    "camera_get_capabilities",
    "camera_start_stream",
    "camera_stop_stream",
    "camera_snapshot",
    "camera_release_stream",
    "camera_provide_answer",
    "camera_provide_ice_candidates",
]);

/** Any other key is refused, like unknown `payload` keys. */
const SEND_PROVIDER_ARG_KEY_SET: Record<keyof Required<ArgsOf<"send_webrtc_provider_command">>, true> = {
    node_id: true,
    endpoint_id: true,
    command_name: true,
    payload: true,
};

const SEND_PROVIDER_ARG_KEYS: readonly string[] = Object.keys(SEND_PROVIDER_ARG_KEY_SET);

// Responses the debug log omits: large ones, and open_commissioning_window, whose setup passcode
// nothing else redacts (redactSensitiveCommandFields covers request args only).
const skipMessageContentInLogFor = [
    "start_listening",
    "get_network_topology",
    "camera_snapshot",
    "open_commissioning_window",
];

/**
~~~~
Controller command interface: packages/ws-controller/src/types/CommandHandler.ts (225 lines); implementation packages/ws-controller/src/controller/ControllerCommandHandler.ts (2840 lines); controller wrapper packages/ws-controller/src/controller/MatterController.ts (818 lines).

### 3.5 Event fan-out
Two paths: (a) per-connection observers on `this.#commandHandler.events` registered after the connection is established (packages/ws-controller/src/server/WebSocketControllerHandler.ts:540-700 region), each gated by the local `listening` flag set after start_listening answers (packages/ws-controller/src/server/WebSocketControllerHandler.ts:837-843); (b) `#broadcastEvent(event, data)` to all clients (packages/ws-controller/src/server/WebSocketControllerHandler.ts:1190-1213) for server_info_updated, node_updated (some paths), server_shutdown.
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:470-560]
~~~~
                }
            };
            const sendNodeFullDetails = (eventName: "node_added" | "node_updated", nodeId: NodeId) => {
                // node_updated coalesces per node (bursts collapse to the latest snapshot under
                // backpressure); node_added must not be coalesced away, so it is an ordered send.
                if (eventName === "node_updated") {
                    connection.sendCoalescable(`node:${nodeId}`, () => buildNodeDetailsFrame(eventName, nodeId));
                } else {
                    const frame = buildNodeDetailsFrame(eventName, nodeId);
                    if (frame !== undefined) connection.sendOrdered(frame, { droppable: false });
                }
            };

            // node_updated can be triggered several times for one logical change (availability +
            // structure + a lazy populate completing all fire in the same tick). Coalesce per node so
            // each burst sends a single full snapshot rather than several large duplicates per client.
            const pendingNodeUpdated = new Set<NodeId>();
            let nodeUpdatedFlushScheduled = false;
            const flushNodeUpdated = () => {
                nodeUpdatedFlushScheduled = false;
                const nodeIds = [...pendingNodeUpdated];
                pendingNodeUpdated.clear();
                if (this.#closed || this.#shuttingDown || !listening || connectionClosed) return;
                for (const nodeId of nodeIds) {
                    // A trailing update can be queued after the node was removed; skip it instead of
                    // letting #collectNodeDetails throw nodeNotExists and log a spurious error.
                    if (!this.#commandHandler.hasNode(nodeId)) continue;
                    sendNodeFullDetails("node_updated", nodeId);
                }
            };

            // python-matter-server wire contract: the node_updated carrying a new endpoint must reach
            // the client before endpoint_added announces it, or a client that resolves the endpoint
            // against its own node model does not know it yet.
            const flushNodeUpdatedFor = (nodeId: NodeId) => {
                if (!pendingNodeUpdated.delete(nodeId)) return;
                if (this.#closed || this.#shuttingDown || !listening || connectionClosed) return;
                if (!this.#commandHandler.hasNode(nodeId)) return;
                sendNodeFullDetails("node_updated", nodeId);
            };

            const sendNodeDetailsEvent = <E extends EventTypes>(eventName: E, nodeId: NodeId) => {
                if (this.#closed || this.#shuttingDown || !listening) return;

                switch (eventName) {
                    case "node_added":
                        // node_added carries the full snapshot, so it supersedes any pending coalesced
                        // update and must not be overtaken by the deferred flush.
                        pendingNodeUpdated.delete(nodeId);
                        sendNodeFullDetails("node_added", nodeId);
                        break;
                    case "node_updated":
                        pendingNodeUpdated.add(nodeId);
                        if (!nodeUpdatedFlushScheduled) {
                            nodeUpdatedFlushScheduled = true;
                            setImmediate(flushNodeUpdated);
                        }
                        break;
                    case "node_removed":
                        pendingNodeUpdated.delete(nodeId);
                        logger.debug(
                            `[${connId}] Sending node_removed event for Node ${this.#commandHandler.formatNode(nodeId)}`,
                        );
                        connection.sendOrdered(toBigIntAwareJson({ event: eventName, data: nodeId }), {
                            droppable: false,
                        });
                        break;
                }
            };

            // Register all event listeners using ObserverGroup for easy cleanup
            observers.on(this.#commandHandler.events.attributeChanged, (nodeId, data) => {
                if (this.#closed || this.#shuttingDown || !listening) return;
                const { endpointId, clusterId, attributeId } = data.path;
                const pathStr = `${endpointId}/${clusterId}/${attributeId}`;
                // `data` is emitter-owned; snapshot the value before deferring. Coalesce latest-wins
                // per (node, path) and convert lazily so a superseded value is never converted.
                const rawValue = data.value;
                // Shared Observable: an uncaught throw here aborts the emit and starves other connections.
                try {
                    connection.sendCoalescable(`attr:${nodeId}/${pathStr}`, () => {
                        const clusterData = ClusterMap[clusterId];
                        const value = convertMatterToWebSocketTagBased(
                            rawValue,
                            clusterData?.attributes[attributeId],
                            clusterData?.model,
                        );
                        logger.debug(
                            `[${connId}] Sending attribute_updated event for Node ${this.#commandHandler.formatNode(nodeId)}`,
                            pathStr,
                            value,
~~~~
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1190-1214]
~~~~
     */
    #broadcastEvent(event: string, data: unknown) {
        if (!this.#wss || this.#closed || this.#shuttingDown) return;
        const message = toBigIntAwareJson({ event, data });
        // node_updated broadcasts carry a full node snapshot and share the per-node coalescing key
        // with the per-connection observer path, so under backpressure the two collapse to one frame
        // instead of queuing duplicates. Everything else is low-volume and reliability-critical, so
        // it bypasses the queue.
        const nodeId =
            event === "node_updated" && data !== null && typeof data === "object" && "node_id" in data
                ? (data as { node_id: number | bigint }).node_id
                : undefined;
        for (const connection of this.#connections) {
            if (nodeId !== undefined) {
                connection.sendCoalescable(`node:${nodeId}`, () => message);
            } else {
                connection.sendReliable(message);
            }
        }
    }

    /**
     * Send server_info_updated event to all connected clients.
     */
    async #broadcastServerInfoUpdated() {
~~~~
Event source objects (Observable fields): ControllerCommandHandler.ts:386-410:
[packages/ws-controller/src/controller/ControllerCommandHandler.ts:384-408]
~~~~
    readonly #autoSubscribeWaiters = new WeakMap<ClientNode, Set<(error: Error) => void>>();
    /** The setting of the last setAutoSubscription call, set before it reaches the node. */
    readonly #autoSubscribeRequested = new WeakMap<ClientNode, boolean>();
    /** In-flight invokes per dedup scope; a scope dropped by its owner takes its entries with it. */
    readonly #inFlightInvokes = new WeakMap<object, Map<string, Promise<unknown>>>();
    events = {
        started: new AsyncObservable(),
        /** A removed attribute is reported with an `undefined` value. */
        attributeChanged: new Observable<[nodeId: NodeId, data: AttributeChange]>(),
        eventChanged: new Observable<[nodeId: NodeId, data: EventChange]>(),
        nodeAdded: new Observable<[nodeId: NodeId]>(),
        nodeStateChanged: new Observable<[nodeId: NodeId, state: NodeConnectionState]>(),
        /** Emitted when node availability changes (for sending node_updated events) */
        nodeAvailabilityChanged: new Observable<[nodeId: NodeId, available: boolean]>(),
        nodeStructureChanged: new Observable<[nodeId: NodeId]>(),
        nodeDecommissioned: new Observable<[nodeId: NodeId]>(),
        nodeEndpointAdded: new Observable<[nodeId: NodeId, endpointId: EndpointNumber]>(),
        nodeEndpointRemoved: new Observable<[nodeId: NodeId, endpointId: EndpointNumber]>(),
        webRtcCallback: new Observable<[WebRtcCallbackData]>(),
        /** @see WebRtcProviderSessionWatchIo.reported */
        webRtcProviderSessionsReported: new Observable<
            [nodeId: NodeId, endpointId: EndpointNumber, present: ReadonlySet<number>, restarted: boolean]
        >(),
    };
    #peers?: PeerSet;
~~~~
Event history ring: packages/ws-controller/src/server/WebSocketControllerHandler.ts:112 EVENT_HISTORY_SIZE = 25; added packages/ws-controller/src/server/WebSocketControllerHandler.ts:366-375; read by getEventHistory() (:378) for the diagnostics command (packages/ws-controller/src/server/WebSocketControllerHandler.ts:1052-1058).

### 3.6 Node cache and persistence
Controller state lives in matter.js storage (env var storage.path = --storage-path: MatterServer.ts:84); nodes are matter.js ClientNode instances held in Nodes (packages/ws-controller/src/controller/Nodes.ts:21-27). The wire-form attribute map is a derived in-memory cache: AttributeDataCache (AttributeDataCache.ts) populated asynchronously (yield budget :31, PopulateContext :41-51) and updated per change from PeerChangeBus (packages/ws-controller/src/controller/PeerChangeBus.ts). Server config (fabric label, wifi/thread credentials): ConfigStorage (packages/ws-controller/src/server/ConfigStorage.ts:29-61, keys fabricLabel default "HomeAssistant" :58, wifiSsid/wifiCredentials/threadDataset :44-46; SENSITIVE_KEYS :29). Legacy python-matter-server files are migrated at start (packages/matter-server/src/converter, MatterServer.ts:138).
[packages/ws-controller/src/server/ConfigStorage.ts:29-62]
~~~~
const SENSITIVE_KEYS: ReadonlySet<keyof ConfigData> = new Set(["wifiCredentials", "threadDataset"]);

const SENSITIVE_LIST_KEYS = new Set(["additionalWifiCredentials", "additionalThreadCredentials"]);

function sanitizeForLog(key: string, value: unknown): string {
    if (SENSITIVE_KEYS.has(key as keyof ConfigData) || SENSITIVE_LIST_KEYS.has(key)) {
        // Fully redact sensitive values regardless of type or length.
        return "<redacted>";
    }
    return String(value);
}

interface ConfigData {
    fabricLabel: string;
    nextNodeId: number | bigint;
    wifiSsid?: string;
    wifiCredentials?: string;
    threadDataset?: string;
    peerSettingsRepairedFor?: string[];
}

export class ConfigStorage {
    #env: Environment;
    #storageService?: StorageService;
    #storage?: StorageManager;
    #configStore?: StorageContext;
    readonly #nodeIdMutex = new Mutex(this);
    readonly #data: ConfigData = {
        nextNodeId: 1,
        fabricLabel: "HomeAssistant",
        wifiSsid: undefined,
        wifiCredentials: undefined,
        threadDataset: undefined,
        peerSettingsRepairedFor: undefined,
~~~~
docs/migration.md:35-42 describes the on-disk layout (server, ota, certificates, vendors directories).

### 3.7 start_listening: dump then stream
Handler (verbatim): packages/ws-controller/src/server/WebSocketControllerHandler.ts:1220-1226. Returns all node details (`#handleGetNodes({})`, packages/ws-controller/src/server/WebSocketControllerHandler.ts:1445-1458); sets `enableListeners = true` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:950-953) and the per-connection `listening` flag turns true only after the response is sent (packages/ws-controller/src/server/WebSocketControllerHandler.ts:833-843). Observers check `!listening` and return early (e.g. packages/ws-controller/src/server/WebSocketControllerHandler.ts:492, 506, 512, 542).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1220-1226]
~~~~
    #handleStartListening(_args: ArgsOf<"start_listening">): ResponseOf<"start_listening"> {
        logger.info("WebSocket server start_listening");
        const data = this.#handleGetNodes({});
        logger.info("WebSocket server start_listening. Returned", data.length, "nodes");
        return data;
    }

~~~~
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:1445-1460]
~~~~
    #handleGetNodes(args: ArgsOf<"get_nodes">): ResponseOf<"get_nodes"> {
        const { only_available = false } = args;
        const nodeDetails = new Array<MatterNode>();
        // Include real nodes
        for (const node of this.#commandHandler.getNodeIds()) {
            const details = this.#collectNodeDetails(node);
            if (!only_available || details.available) {
                nodeDetails.push(details);
            }
        }
        // Include test nodes
        for (const testNode of this.#testNodeHandler.getNodes()) {
            if (!only_available || testNode.available) {
                nodeDetails.push(testNode);
            }
        }
~~~~

### 3.8 Authentication
No authentication code in the WebSocket path: grep for 'token', 'Authorization', 'authent' in packages/ws-controller/src/server/WebSocketControllerHandler.ts returns no match. docs/cli.md security warning (verbatim):
[docs/cli.md:59-66]
~~~~
> keeping up to seven daily backup files (≈ 7 days of history). No further cleanup is performed.

> [!WARNING]
> **Security:** By default the WebSocket server binds to **all network interfaces**, making it
> reachable by any host on your network. You are responsible for securing access according to your
> requirements and use-case. Use `--listen-address` to restrict binding to a specific interface
> (e.g. `--listen-address 127.0.0.1` for localhost-only), and use `--primary-interface` to control
> which interface Matter mDNS announcements and communication use (e.g. `--primary-interface eth0`).
~~~~
BLE proxy doc: 'No authentication is built into the protocol; rely on network-level security' (docs/ble-proxy-protocol.md:790-791). Restriction is by bind: --listen-address (IP, interface name or unix socket path; WebServer.ts:79-85 handles unix sockets).

### 3.9 Error codes
See 1.3. Mapping of thrown errors: `err instanceof ServerError ? err.code : ServerErrorCode.UnknownError` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:1146-1147); status from ReceivedStatusResponseError via `statusResponseOf` (packages/ws-controller/src/server/WebSocketControllerHandler.ts:238-250 region).
[packages/ws-controller/src/server/WebSocketControllerHandler.ts:236-262]
~~~~
}

/** The status the device answered with, if the failure is one; matter.js also reports local validation as status errors. */
function statusResponseOf(error: unknown): ReceivedStatusResponseError | undefined {
    if (error instanceof ReceivedStatusResponseError) {
        return error;
    }
    if (error instanceof AggregateError) {
        for (const inner of error.errors) {
            const status = statusResponseOf(inner);
            if (status !== undefined) {
                return status;
            }
        }
    }
    return error instanceof Error ? statusResponseOf(error.cause) : undefined;
}

/** 48 hours, the longest a device announces for commissioning (Extended Announcement, Matter Core spec 5.4.2.3.3). */
const MAX_DISCOVERY_TIMEOUT_S = 48 * 60 * 60;

function discoveryTimeoutOf(seconds: unknown): Duration | undefined {
    if (seconds === undefined || seconds === null) {
        return undefined;
    }
    if (typeof seconds !== "number" || !(seconds > 0 && seconds <= MAX_DISCOVERY_TIMEOUT_S)) {
        throw ServerError.invalidArguments(
~~~~

### 3.10 Dashboard serving
packages/dashboard (static web app built to dist/web; package README packages/dashboard/README.md). Served by StaticFileHandler (packages/matter-server/src/server/StaticFileHandler.ts, 83 lines): resolves `import.meta.resolve("@matter-server/dashboard")` (:30), static path = two dirs above main.js (:32), index.html injected with production-mode flag (:40-77), reserved prefixes passed in (`/ota-upload`) so they are not served as files (:53-56). Disabled by --disable-dashboard (MatterServer.ts:~241-246). Dashboard client connects to ws://localhost:5580/ws by default or replaces http with ws + '/ws' (packages/dashboard/src/entrypoint/main.ts:31,46-51). Health endpoint GET /health -> {version,node_count} (HealthHandler.ts:14-30; packages/ws-controller/src/server/WebSocketControllerHandler.ts:318-325).
[packages/matter-server/src/server/StaticFileHandler.ts:24-60]
~~~~
     * @param reservedPrefixes Paths another registered handler answers. Anything not reserved is
     * served (or 404'd) here, so a disabled feature's endpoint gets an answer rather than a hang.
     */
    constructor(productionMode: boolean = false, reservedPrefixes: string[] = []) {
        // Resolve the @matter-server/dashboard package entry point (dist/web/js/main.js)
        // and get its parent directory (dist/web) for serving static files
        const dashboardMain = import.meta.resolve("@matter-server/dashboard");
        // Go up from dist/web/js/ to dist/web/
        this.#staticPath = dirname(dirname(fileURLToPath(dashboardMain)));
        this.#productionMode = productionMode;
        this.#reservedPrefixes = ["/ws", "/health", ...reservedPrefixes];
    }

    async register(server: HttpServer): Promise<void> {
        const app = express();

        // Custom handler for index.html to inject the production mode flag
        app.get("/", (_req, res) => {
            res.type("html").send(this.#getIndexHtml());
        });
        app.get("/index.html", (_req, res) => {
            res.type("html").send(this.#getIndexHtml());
        });

        // Serve other static files normally
        app.use(express.static(this.#staticPath));

        // Attach to the existing server
        server.on("request", (req, res) => {
            const path = req.url?.split("?")[0] ?? "";
            if (!this.#reservedPrefixes.some(prefix => path === prefix || path.startsWith(`${prefix}/`))) {
                app(req, res);
            }
        });
    }

    /**
~~~~

### 3.11 Config flags (docs/cli.md:1-245)
[docs/cli.md:1-58]
~~~~
# Matter Server CLI

Matter Controller Server using WebSockets.

## Usage

```bash
# Start server with defaults
npm run server

# With options
npm run server -- --port 5580 --storage-path ~/.matter_server

# Bind to specific interface
npm run server -- --listen-address 192.168.1.100

# Enable BLE commissioning (Linux with bluetooth adapter)
npm run server -- --bluetooth-adapter 0
```

When the server is installed from npm, `npx matter-server` takes the same options without the extra `--`, for example `npx matter-server --port 5580`.

---

## Complete Argument Specification

| Argument                     | Type         | Default          | Required | Description                                                                                                                                                                                                                                                          |
| ---------------------------- | ------------ | ---------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| --vendorid                   | integer      | 0xFFF1 (65521)   | No       | Vendor ID for the Fabric                                                                                                                                                                                                                                             |
| --fabricid                   | integer      | (random)         | No       | Fabric ID for the Fabric (random if not specified)                                                                                                                                                                                                                   |
| --storage-path               | string       | ~/.matter_server | No       | Storage path to keep persistent data                                                                                                                                                                                                                                 |
| --port                       | integer      | 5580             | No       | TCP Port for WebSocket server. `0` binds a free port chosen by the OS (shared by all `--listen-address` TCP listeners); the bound port is in the `Webserver listening on …` log line                                                                                |
| --listen-address             | string[]     | null (bind all)  | No       | IP address(es), interface name(s) or absolute unix socket path(s) to bind WebSocket server. Repeatable — env `LISTEN_ADDRESS` (comma-separated).                                                                                                                                                                                                                 |
| --log-level                  | enum         | "info"           | No       | Global logging level                                                                                                                                                                                                                                                 |
| --log-file                   | string       | null             | No       | Log file path incl. filename, e.g. `/data/matter-server.log`                                                                                                                                                                                                         |
| --primary-interface          | string       | null             | No       | Primary network interface for link-local addresses                                                                                                                                                                                                                   |
| --default-fabric-label       | string       | null             | No       | Pin the fabric label to this value and ignore `set_default_fabric_label` WebSocket requests — env `DEFAULT_FABRIC_LABEL`. Prevents multiple clients from overwriting each other's label.                                                                             |
| --enable-test-net-dcl        | boolean flag | false            | No       | Enable test-net DCL to check for certificates and OTA-updates additionally to the production DCL                                                                                                                                                                     |
| --disable-dcl-seed           | boolean flag | false            | No       | Disable bundled offline DCL seed (PAA roots, CD signers, vendors); rely on network DCL only                                                                                                                                                                          |
| --dcl-offline                | boolean flag | false            | No       | Never contact the DCL: attestation trusts the bundled DCL seed (PAA roots, CD signers) and certificates stored by earlier runs and skips DCL revocation checks (sets from `--revocation-sets-dir` still apply), vendor names come from the seed and storage, and OTA update checks do not query the DCL. Cannot be combined with `--disable-dcl-seed` |
| --revocation-sets-dir        | string       | null             | No       | Directory with device attestation revocation sets, installed at every start before commissioning is possible — env `REVOCATION_SETS_DIR`. Reads every `*.json` file: a JSON array of entries as CHIP's `credentials/generate_revocation_set.py` writes them (`issuer_subject_key_id` hex, `issuer_name` base64 DER, `revoked_serial_numbers` hex list; `crl_signer_cert` and `crl_signer_delegator` are ignored). An invalid file is skipped with an error. Works with and without `--dcl-offline`. See [Installing Device Attestation Revocation Sets](../README.md#installing-device-attestation-revocation-sets) |
| --certificates-dir           | string       | null             | No       | Directory with PAA certificates to import at every start — env `CERTIFICATES_DIR`. Reads `*.paa.der` and `*.paa.pem` files and deletes each file after import; a file that fails stays. Imported as test certificates (trusted only with `--enable-test-net-dcl`), as production certificates when the name ends in `.prod.paa.der` or `.prod.paa.pem`. See [Importing Custom PAA Certificates](../README.md#importing-custom-paa-certificates) |
| --bluetooth-adapter          | integer      | null             | No       | Bluetooth adapter HCI ID (e.g., 0 for hci0). On Windows and macOS the ID is optional. Mutually exclusive with `--ble-proxy`.                                                                                                                                         |
| --ble-proxy                  | boolean flag | false            | No       | Expose `/ble` WebSocket endpoint for a remote BLE proxy client. Mutually exclusive with `--bluetooth-adapter` (--ble-proxy wins).                                                                                                                                    |
| --disable-ota                | boolean flag | false            | No       | Disable OTA update functionality                                                                                                                                                                                                                                     |
| --ota-provider-dir           | string       | null             | No       | Directory for OTA Provider files                                                                                                                                                                                                                                     |
| --ota-upload-max-in-flight   | integer      | 5                | No       | Maximum number of OTA firmware uploads (`POST /ota-upload/<upload_id>`, schema 13+) that may be reserved or transferring at once — env `OTA_UPLOAD_MAX_IN_FLIGHT`                                                                                                     |
| --ota-upload-max-size-mb     | integer      | 64               | No       | Maximum size in MB of an uploaded OTA firmware image — env `OTA_UPLOAD_MAX_SIZE_MB`. Uploads are staged in an `ota-uploads` subdirectory of `--ota-provider-dir` (or of `--storage-path` when that is not set) and deleted after import                               |
| --disable-dashboard          | boolean flag | false            | No       | Disable the web dashboard                                                                                                                                                                                                                                            |
| --production-mode            | boolean flag | false            | No       | Force dashboard production mode (for reverse proxy)                                                                                                                                                                                                                  |
| --disable-thread-diagnostics | boolean flag | false            | No       | Disable the Thread Network diagnostics feature (Border Router mDNS discovery, REST/CoAP probing and diagnostic queries, plus the periodic refresh of node neighbor/route tables) — env `DISABLE_THREAD_DIAGNOSTICS`. Matter-over-Thread commissioning is unaffected. |
| --thread-rest-probe-port     | integer      | 8081             | No       | TCP port of the OTBR REST API probed on discovered Thread Border Routers — env `THREAD_REST_PROBE_PORT`. Set it when the Border Router exposes its REST API on another port (e.g. `8080`). A wrong port costs probe time on every discovered Border Router before the probe gives up and the network falls back to CoAP. |
| --enable-time-sync           | boolean flag | false            | No       | Enables automatic time synchronization for devices that support it (env `ENABLE_TIME_SYNC`). Only enable when the host clock is reliably synced (e.g., NTP); pushes UTC time + time zone / DST.                                                                      |
| --custom-cluster-poll-interval | integer    | 60               | No       | Interval in seconds for polling custom cluster attributes that do not support Matter subscriptions (legacy Eve Energy devices) — env `CUSTOM_CLUSTER_POLL_INTERVAL`. Accepts 60 to 86400; raise it (e.g. `300`, `600`) to cut periodic Thread traffic on networks with many such devices, at the cost of less current energy readings. |
| --disable-auto-subscription  | boolean flag | false            | No       | Do not subscribe to nodes automatically, neither after commissioning nor on start, and stop the subscriptions of already commissioned nodes — env `DISABLE_AUTO_SUBSCRIPTION`. You are responsible for subscribing as needed (WebSocket `subscribe`/`unsubscribe` and the per-node `enable_auto_subscription`/`disable_auto_subscription` are only available with it); only recommended for very special use cases. See [Disabled auto subscription](#disabled-auto-subscription). |

> **Log rotation:** `--log-file` must be a full file path including the filename. The log is rotated
> every 24 hours, and on each startup: backups are shifted (`.6`→`.7`, …, `.1`→`.2`, current→`.1`),
~~~~
Unix-socket / listen-address and other flags are parsed in packages/matter-server/src/cli.ts (519 lines; commander-style .option chain, getCliOptions :504). Deprecated no-op flags: cli.ts:410-412 (--log-node-ids, --paa-root-cert-dir, --disable-server-interactions).

## 4. BLE proxy protocol (docs/ble-proxy-protocol.md, Version: 1.0 at :3)

| Item | Fact | Location |
|---|---|---|
| Endpoint | WebSocket `/ble` on the same host and port as `/ws` (e.g. `ws://host:5580/ble`) | docs/ble-proxy-protocol.md:15-17; packages/ble-proxy/src/BleProxyHandler.ts:88 |
| Enabled by | `--ble-proxy` (mutually exclusive with `--bluetooth-adapter`, proxy wins) | docs/cli.md (row --ble-proxy); MatterServer.ts:88-91 |
| Who connects to whom | The BLE proxy client (holds BLE hardware, e.g. Home Assistant) connects to the Matter server's `/ble`; the server sends commands, the client executes them | docs/ble-proxy-protocol.md:12-13, 23-30, 32-40 |
| Versioning | `hello` {type,version:1} client->server; `hello_response` {type,version,error?,message?}; error `unsupported_version` then close; handshake timeout 10 s | docs/ble-proxy-protocol.md:76-117 |
| Liveness | server WebSocket ping every 15 s; terminate after 45 s without pong/message | docs/ble-proxy-protocol.md:64-74 |
| Server->client command | `{"id": int, "command": str, "args"?: obj}` | docs/ble-proxy-protocol.md:123-139 |
| Client->server response | `{"id","success":true,"result"?}` or `{"id","success":false,"error","message"}` | docs/ble-proxy-protocol.md:141-170 |
| Client->server event | `{"event": str, "data": obj}` | docs/ble-proxy-protocol.md:172-186 |
| Commands | start_scan :190, stop_scan :224, connect :240, disconnect :272, discover_services :292, discover_characteristics :323, read_characteristic :357, write_characteristic :392, subscribe_characteristic :418, write_and_subscribe :447, unsubscribe_characteristic :489, request_mtu :512 | docs/ble-proxy-protocol.md |
| Events | device_discovered :538, disconnected :576, scan_stopped :592, characteristic_notification :609 | docs/ble-proxy-protocol.md |
| Binary frames | byte0 opcode, bytes1-2 connection_handle big-endian u16, rest payload; 0x01 WRITE_DATA (server->client), 0x02 NOTIFICATION (client->server), 0x03 READ_RESPONSE (client->server) | docs/ble-proxy-protocol.md:622-650 |
| Multiple clients | scan broadcast; each peripheral owned by the first client reporting it; ownership moves on disconnect; expires after 15 min | docs/ble-proxy-protocol.md:42-62 |
| Error codes | bluetooth_unavailable, already_scanning, not_scanning, device_not_found, connection_failed, already_connected, not_connected, timeout, service_not_found, characteristic_not_found, read_failed, write_failed, subscribe_failed, not_subscribed, notify_not_supported, mtu_request_failed, discovery_failed, internal_error | docs/ble-proxy-protocol.md:754-775 |
| Matter BLE UUIDs | service 0000FFF6-0000-1000-8000-00805F9B34FB; C1 18EE2EF5-263D-4559-959F-4F9C429F9D11 (write), C2 ...9D12 (notify), C3 ...9D13 (read) | docs/ble-proxy-protocol.md:777-786 |
| Auth | none built in; localhost or trusted network | docs/ble-proxy-protocol.md:788-796 |
| Implementations in clone | server side packages/ble-proxy/src (BleProxyConnection.ts, BleProxyHandler.ts, BleProxyProtocol.ts, ProxyBle.ts, ProxyBleChannel.ts, ProxyBleClient.ts, ProxyBleScanner.ts); Python client python_ble_proxy/matter_ble_proxy | directory listing |

## 5. Migration and compatibility

### 5.1 docs/migration.md (92 lines) verbatim
~~~~
# Migrating from the Python Matter Server

This server is a drop-in replacement for the [Python Matter Server](https://github.com/matter-js/python-matter-server). It reads the Python data files and migrates them on the first start.

## Before you start

> [!IMPORTANT]
> Back up your Python Matter Server data files before you start this server on them.

> [!IMPORTANT]
> Never run the Python Matter Server and this server on the same data files at the same time. Both would act as the same controller on the same fabric and send conflicting requests to the same nodes.

The Python Matter Server stores its data in two files:

- `chip.json`: the global configuration of your Matter fabric, including IDs, keys, and certificates for secure communication.
- `<long-number>.json`: the list of all nodes in the fabric and their latest data.

Prepare the host as described in the [OS requirements](os_requirements.md), especially the networking part.

## Running the server on the Python data files

### Docker

Use the same data directory and command-line arguments as for the Python Matter Server container. The [docker-compose file](../docker/matterjs-server/docker-compose.yml) maps `${HOME}/.matterjs-server` as data directory by default, so copy the Python data files there before the first start. The container of this server runs unprivileged, so the data directory needs a [permission change](docker.md#permission-issues). See [Running in Docker](docker.md) for all details.

### npm

1. Create a data directory, for example `mkdir ~/matter-data`.
2. Copy `chip.json` and `<long-number>.json` from the Python Matter Server into that directory. If the Python server runs in a Docker container (for example on Home Assistant OS, accessible via the SSH add-on):
    - Find the container with `docker ps`.
    - List the files with `docker exec -it <container-id> ls /data/`.
    - Copy them with `docker cp <container-id>:/data/chip.json ~/matter-data/` and `docker cp <container-id>:/data/<long-number>.json ~/matter-data/`.
3. Install the server as described in the [README](../README.md#manual-installation-from-npm) and start it with `npx matter-server --storage-path ~/matter-data`. If the host has several network interfaces, add `--primary-interface <interface-name>` (for example `eth0` on Linux or `en0` on macOS).

See the [CLI documentation](cli.md) for all parameters and the differences from the Python Matter Server.

## What happens on the first start

1. The server migrates `chip.json` and `<long-number>.json` into the matter.js storage format. This can take some time.
2. The server logs an error that no addresses are known for the nodes. You can ignore it: the Python data files contain no device addresses, so the server discovers them first.
3. On the first successful connection to each node, the server interviews the node completely and updates its data.

Later starts are much faster. The server reuses the last known device addresses and only requests attributes that changed while the server was stopped or the device was offline.

The server detects whether a node uses Thread or WiFi/Ethernet. For Thread devices, it limits the number of parallel requests per Thread network, so the network is not flooded with requests. With many Thread devices, it can take a while after startup until all devices are online.

## How the migration works

The server stores all data in a filesystem-based key/value store in the data directory. Do not edit files in these directories:

- `server`: all node data, one file per storage key.
- `ota`: downloaded OTA update files. To add your own OTA files, use a separate directory, see [Importing Custom OTA Firmware Files](../README.md#importing-custom-ota-firmware-files).
- `certificates`: downloaded Matter certificate chains, used to validate devices during commissioning.
- `vendors`: the downloaded list of Matter vendors.

The fabric configuration from `chip.json` is migrated into the matter.js storage.

The node data from `<long-number>.json` are converted to the matter.js format. On a first start with empty storage, the server writes them directly into the controller node storage (`nodes.peerX.endpoints.*`). If the storage already contains nodes, the server writes each node that is not yet known as `node-<NodeID>.*` files in the `server` directory and imports them into the controller node on start. Only the `nodes.peerX.endpoints.*` data are updated and used afterwards.

The migration runs once per node in `<long-number>.json`. It is skipped for a node that already has data in the `server` directory.

## Switching back to the Python Matter Server

The server keeps `<long-number>.json` up to date when nodes are added or removed, so the Python Matter Server can be started again on the same data files. Stop this server first. Nodes removed in this server are gone there too.

For nodes added in this server, no attribute data are written into `<long-number>.json`. The Python Matter Server has to interview them again, and they can show empty values until they reconnect.

## Troubleshooting

### Getting logs

Start the server with `--log-level debug` (Docker: `-e LOG_LEVEL=debug`) to get detailed logs. You can also change the log level at runtime in the dashboard under **Settings**. Attach the complete log when you report an issue.

In Home Assistant, the add-on page shows up to 10,000 lines of the log. For more, use the HA CLI:

```bash
ha apps logs core_matter_server -n 1000000 > /config/matter.log
```

Then download the file from the config folder, for example via the Samba or Studio Code Server add-on.

### Startup takes long

On the first start, every node is discovered and interviewed completely. With many Thread devices, the limit on parallel requests per Thread network makes them come online one after another. Wait until the first start has finished before you judge the startup time.

### The log says the Python Matter Server data is not migrated

The server does not migrate the Python Matter Server data when the storage directory already holds matter.js controller data that does not match it. This happens after a failed migration attempt, or when the server ran on this directory before the Python data files were copied in. The server keeps the existing data and does not keep `<long-number>.json` up to date. Restore the backup from before the first migration attempt, or start with a storage directory that holds only the Python Matter Server data files.

### Devices are not found or stay offline

Most of these issues come from the host network. Check the [OS requirements](os_requirements.md) and the [Docker troubleshooting](docker.md#troubleshooting) section.
~~~~

### 5.2 Compatibility lists in docs/websockets_api.md:2467-2546 (verbatim)
[docs/websockets_api.md:2467-2546]
~~~~
## Python Matter Server Compatibility

This API is designed to be compatible with the [Python Matter Server](https://github.com/matter-js/python-matter-server) WebSocket API.

### Stub Commands

| Command | Status | Notes |
|---------|--------|-------|
| `subscribe_attribute` | Not supported | Answers `InvalidCommand`. The server subscribes to all attributes on its own (unless it runs with `--disable-auto-subscription`); use `subscribe` (schema 14) for a subscription of your own |

### Matter.js-Only Commands

These commands are available only in the Matter.js server and not in the Python Matter Server:

| Command | Description |
|---------|-------------|
| `get_loglevel` | Get current console and file log levels |
| `set_loglevel` | Temporarily change log levels (resets on restart) |
| `get_icd_state` | Get ICD Check-In state for a node |
| `register_icd` | Register this controller as an ICD Check-In client |
| `unregister_icd` | Drop this controller's ICD Check-In registration |
| `resync_icd` | Drop the local ICD registration and reconnect (with `--disable-auto-subscription`: only drop it, unless the node's auto subscription is enabled) |
| `get_network_topology` | Return the Thread/Wi-Fi network as a graph (schema 13+) |
| `initiate_ota_upload` | Reserve an id for the `POST /ota-upload/<upload_id>` HTTP endpoint (schema 13+) |
| `send_webrtc_provider_command` | Send `ProvideOffer`, `SolicitOffer`, `ProvideAnswer` or `ProvideIceCandidates` to a camera endpoint yourself (schema 12+) |
| `camera_get_capabilities` | Report a camera endpoint's stated capabilities and current stream allocations (schema 14+) |
| `camera_start_stream` | Allocate or reuse a video/audio stream and open a WebRTC session on it (schema 14+) |
| `camera_provide_answer` | Answer the offer a camera sent for a session, as `ProvideAnswer` (schema 14+) |
| `camera_provide_ice_candidates` | Trickle ICE candidates into a session, as `ProvideIceCandidates` (schema 14+) |
| `camera_stop_stream` | End a WebRTC session, keeping the stream allocation (schema 14+) |
| `camera_snapshot` | Capture one still frame from a camera endpoint (schema 14+) |
| `camera_release_stream` | Deallocate a stream nothing references (schema 14+) |
| `read` | Read attributes and events in one Matter Read request (schema 14+) |
| `subscribe` | Open a Matter subscription owned by the connection (schema 14+) |
| `unsubscribe` | End a subscription opened with `subscribe` (schema 14+) |
| `enable_auto_subscription` | Switch the server's own subscription to one node on, with `--disable-auto-subscription` (schema 14+) |
| `disable_auto_subscription` | Switch the server's own subscription to one node off again (schema 14+) |
| `parse_onboarding_payload` | Decode a QR code or manual pairing code without contacting a device (schema 14+) |
| `batched_device_commands` | Send several commands to one node in a single InvokeRequest (schema 14+) |

### Data Differences

| Field | Python | Matter.js |
|-------|--------|-----------|
| `MatterNode.attribute_subscriptions` | Tracks per-node subscriptions | Always empty array |
| Test node IDs | `>= 900000` | `>= 0xFFFF_FFFE_0000_0000` |
| `MatterNode.endpoint_parents` | Not sent | Direct parent endpoint of each endpoint whose parent is known |

### Behavioral Differences

- **Fabric Label**: `set_default_fabric_label` with null or an empty label resets to "HomeAssistant" instead of clearing. The argument itself is required: a message that states no `label` is refused with error 8 rather than resetting
- **Attribute Subscriptions**: All attributes are subscribed automatically (unless the server runs with `--disable-auto-subscription`); the `attribute_subscriptions` field is not used
- **Test Nodes**: Use high bigint range to prevent collision with real Matter node IDs
- **Malformed `args`**: An `args` that is not a JSON object is refused with error 8. The Python Matter
  Server fails while decoding the frame instead, with an error its message handler does not catch, and
  closes the connection without answering the request
- **Unknown argument keys**: The `camera_*` commands and `send_webrtc_provider_command` refuse an
  argument key they do not know with error 8, listing the keys they accept. The Python Matter Server
  ignores unknown keys (`parse_arguments` is called with `strict=False`). The difference is confined to
  commands the Python server does not have, and it is deliberate: a request whose argument was ignored
  gets a result nobody asked for
- **A missing required argument**: The Python server converts each argument against the handler's type
  hints, so a missing or mistyped one is error 8 for every command. Here each command checks its own
  arguments: the `camera_*` commands, `send_webrtc_provider_command`, `set_thread_dataset`,
  `set_wifi_credentials`, `set_default_fabric_label`, `read`, `subscribe` and `unsubscribe` answer error 8, while a command that hands
  `node_id` straight to the Matter conversion — `get_node`, `interview_node`, `ping_node` and the
  other node-targeted ones — reports a missing or non-numeric one as error 0 (`UnknownError`)
- **Node ID classes**: every node-targeted command refuses a `node_id` whose class can never name a
  node, with error 8 naming the class. Accepted are the Operational range (a commissioned node) and
  the Temporary Local range (this server's imported test nodes); a CASE Authenticated Tag, a PAKE key
  identifier, the Unspecified Node ID and the reserved ranges are refused (Matter Core specification
  § 2.5.5, Table 4). A Group Node ID is accepted by `write_attribute` and `device_command`, which
  multicast to the group, and refused by every other command, which needs one node's answer. Group id
  `0` is refused everywhere: the specification calls it the Null or unspecified Group ID, so it names
  no group (§ 2.5.4, Table 2). The
  Python Matter Server hands all of them to the node lookup and answers `NODE_NOT_EXISTS`, which says
  the node is not commissioned rather than that the argument could never name one
- **Commands that read no arguments**: `server_info`, `get_all_credentials`, `get_thread_border_routers`,
  `discover`, `get_loglevel` and `initiate_ota_upload` still require `args` to be an object when the
  message states one, although they read nothing from it
~~~~

## 6. go-fabric side (local main a0b842b6)

### 6.1 device.New and device.Options
`func New(t *Type, opts Options, decls ...Declaration) (*Device, error)` device/device.go:113. Generated constructors per device type: `func New<Type>(opts Options, decls ...Declaration) (*<Type>, error)` in device/*_gen.go (e.g. device/aggregator_gen.go:52, device/air_purifier_gen.go:68; 79 of them).
[/home/markus/Dokumente/GitHub/go-fabric/device/options.go:23-48]
~~~~go
type Options struct {
	// StableKey identifies the device across reassemblies and restarts
	// ([endpoint.Spec.StableKey]); required, and unique per device and
	// per part.
	StableKey SourceKey
	// Name is the BridgedDeviceBasicInformation NodeLabel
	// ([endpoint.Spec.FriendlyName]); [Device.Rename] changes it.
	Name string
	// Vendor is the bridged device's manufacturer
	// ([endpoint.Spec.VendorName]).
	Vendor string
	// DeviceAddress and ChannelAddress are the host's own addresses of the
	// physical device and of this function of it, carried through
	// verbatim.
	DeviceAddress  string
	ChannelAddress string
	// Reachable probes the device's live reachability
	// ([endpoint.Spec.Availability]); nil reads as always reachable. The
	// device is reachable while the probe says so and [Device.SetReachable]
	// has not set it unreachable.
	Reachable AvailabilityProbe
	// Conditions are the device-type conditions the host states for the
	// endpoint ([endpoint.Spec.DeviceConditions]).
	Conditions []string
}

~~~~
Declaration interface device/options.go:71-79; `type Option = Declaration` :81. Exported option functions in device/options.go:
273:func WithServer(srv contract.ClusterServer) Option {
291:func HostServer(srv contract.ClusterServer) Declaration {
314:func WithChangeNotifier(n contract.ChangeNotifier) Option {
327:func WithClientClusters(clusterIDs ...uint32) Option {
[/home/markus/Dokumente/GitHub/go-fabric/device/options.go:71-82]
~~~~go
type Declaration interface {
	// ClusterID is the cluster declared; 0 for a statement about the
	// device as a whole.
	ClusterID() uint32
	// ClusterDeclaration is the declaration in the form the kit applies
	// it.
	ClusterDeclaration() spec.ClusterDeclaration
}

// Option is the kit's own [Declaration], what the With… functions return.
type Option = Declaration

~~~~
settings fields (unexported, what declarations collect) device/options.go:~98-125:

func (deviceOption) ClusterID() uint32                           { return 0 }
func (deviceOption) ClusterDeclaration() spec.ClusterDeclaration { return spec.ClusterDeclaration{} }

// settings is what the declarations of one New call collected.
type settings struct {
	// key is the device's StableKey ([defaultBuild] key).
	key string
	// self is the device's endpoint number once assembled
	// ([Device.EndpointNumber]): a default PowerSource names it in its
	// EndpointList.
	self     func() (uint16, bool)
	features map[uint32]uint32
	optional map[uint32]bool
	servers  map[uint32]contract.ClusterServer
	// serverOrder is the order the host's servers were declared in; a host
	// server for a cluster the type does not list is mounted in it.
	serverOrder []uint32
	notifier    contract.ChangeNotifier
	initial     map[uint32]map[uint32]any
	elements    map[uint32]spec.Options
	// clients are the client clusters [WithClientClusters] declared, in
	// call order.
	clients []uint32
	// measurements are the [Measurement] sources, in call order.
	measurements []contract.MeasurementSource
	// clock is the device's clock, for the servers the device builds.
	clock clock.Clock
	// typed are the clusters a generated Declare declared; attach their
	// hooks once the server is built; ports and transitions what their
	// kit servers are built with.
	typed       map[uint32]bool
	implied     map[uint32]spec.Options
	attach      map[uint32][]func(contract.ClusterServer) error
	ports       map[uint32]any
	transitions map[uint32]bool
	// declared are the clusters whose declaration carries a port or a
	// feature selection: their self-held server is asked for.

### 6.2 Generated cluster declaration, OnOff (cluster/spec/onoff/server_gen.go)
[/home/markus/Dokumente/GitHub/go-fabric/cluster/spec/onoff/server_gen.go:15-41]
~~~~go

// State is the typed state of the OnOff cluster: one field per attribute
// of [Definition], a nullable one a pointer (nil for null), a list a
// slice. [Server.Get] fills it from the stored values; an attribute the
// server does not serve stays at its zero value.
type State struct {
	OnOff              bool              // attribute 0x0000: bool, M, R V, N S
	GlobalSceneControl bool              // attribute 0x4000: bool, LT, R V
	OnTime             uint16            // attribute 0x4001: uint16, LT, RW VO
	OffWaitTime        uint16            // attribute 0x4002: uint16, LT, RW VO
	StartUpOnOff       *StartUpOnOffEnum // attribute 0x4003: StartUpOnOffEnum, LT, RW VM, X N
}

// Patch is a change to the OnOff state: each non-nil field is set, the
// others are left as they are.
// Null lists the nullable attributes the patch sets to null.
type Patch struct {
	OnOff              *bool             // attribute 0x0000
	GlobalSceneControl *bool             // attribute 0x4000
	OnTime             *uint16           // attribute 0x4001
	OffWaitTime        *uint16           // attribute 0x4002
	StartUpOnOff       *StartUpOnOffEnum // attribute 0x4003
	Null               []uint32          // attribute ids set to null
}

// FromAttributeValues implements spec.StateReader: each field the value
// read answers for its attribute, converted to the field's type; an
~~~~
[/home/markus/Dokumente/GitHub/go-fabric/cluster/spec/onoff/server_gen.go:68-102]
~~~~go
func StateFrom(values spec.Values) (state State, absent []uint32) {
	return spec.StateFrom[State](Definition, values)
}

// AttributeValues implements spec.PatchValuer: the attributes p sets,
// by id, in their stored form — a list of a structure as its
// spec.List — and nil for each id in Null.
func (p Patch) AttributeValues() map[uint32]any {
	v := map[uint32]any{}
	if p.OnOff != nil {
		v[AttrOnOff] = *p.OnOff
	}
	if p.GlobalSceneControl != nil {
		v[AttrGlobalSceneControl] = *p.GlobalSceneControl
	}
	if p.OnTime != nil {
		v[AttrOnTime] = *p.OnTime
	}
	if p.OffWaitTime != nil {
		v[AttrOffWaitTime] = *p.OffWaitTime
	}
	if p.StartUpOnOff != nil {
		v[AttrStartUpOnOff] = *p.StartUpOnOff
	}
	for _, id := range p.Null {
		v[id] = nil
	}
	return v
}

// Server is the typed OnOff server over the cluster's server — the
// generated spec.Server or a self-held one (Underlying) — with a typed
// state, typed change reports, a write veto, typed command handlers and
// typed events. It adds no behavior: each method reaches the server's
// own. It is not a contract.ClusterServer: mount the underlying server,
~~~~
[/home/markus/Dokumente/GitHub/go-fabric/cluster/spec/onoff/server_gen.go:210-262]
~~~~go
// Sink switches the device: the kit's OnOff server calls it before it
// stores a change of OnOff a command makes, and an error leaves the
// state and answers FAILURE (cluster/onoff Sink).
type Sink interface {
	SetOnOff(ctx context.Context, on bool) error
}

// Declare is the declaration of the OnOff cluster on a device:
// device.New<Type> and device.FromSpec take it, and every field is
// optional — the zero value mounts the cluster (an optional one too)
// with the kit's default server and the device type's defaults. A
// device takes one Declare per cluster.
type Declare struct {
	// Initial is the state the server starts with, over the device
	// type's defaults; an optional attribute it sets is served.
	Initial Patch
	// Features is the FeatureMap selection; zero is the device type's.
	Features Feature
	// Commands are optional commands the server accepts beyond the
	// ones the selection mandates: the kit's server carries them out
	// (through its port), a Handle field below handles one in its place.
	Commands []uint32
	// OnWrite vetoes a controller's write before it is stored
	// (Server.OnWrite); it runs inline.
	OnWrite func(ctx context.Context, next State, attrID uint32) error
	// HandleOff handles the Off command in place of the server
	// (Server.HandleOff), inline; an optional command it handles is accepted.
	HandleOff func(ctx context.Context, req OffRequest) (struct{}, error)
	// HandleOn handles the On command in place of the server
	// (Server.HandleOn), inline; an optional command it handles is accepted.
	HandleOn func(ctx context.Context, req OnRequest) (struct{}, error)
	// HandleToggle handles the Toggle command in place of the server
	// (Server.HandleToggle), inline; an optional command it handles is accepted.
	HandleToggle func(ctx context.Context, req ToggleRequest) (struct{}, error)
	// HandleOffWithEffect handles the OffWithEffect command in place of the server
	// (Server.HandleOffWithEffect), inline; an optional command it handles is accepted.
	HandleOffWithEffect func(ctx context.Context, req OffWithEffectRequest) (struct{}, error)
	// HandleOnWithRecallGlobalScene handles the OnWithRecallGlobalScene command in place of the server
	// (Server.HandleOnWithRecallGlobalScene), inline; an optional command it handles is accepted.
	HandleOnWithRecallGlobalScene func(ctx context.Context, req OnWithRecallGlobalSceneRequest) (struct{}, error)
	// HandleOnWithTimedOff handles the OnWithTimedOff command in place of the server
	// (Server.HandleOnWithTimedOff), inline; an optional command it handles is accepted.
	HandleOnWithTimedOff func(ctx context.Context, req OnWithTimedOffRequest) (struct{}, error)
	// Sink is the host port the kit's server runs on ([Sink]).
	Sink Sink
	// HostServer is the host's own server for the cluster, mounted in
	// place of the kit's — also for a cluster the device type does not
	// list. The hooks above go on it; Initial, Features and the ports are
	// the kit's server's and are refused beside it.
	HostServer contract.ClusterServer
}

// ClusterID implements device.Declaration: the cluster declared.
~~~~
Server methods (typed): Set(Patch) :124, Get() State :127, OnChanged :134, MarkChanged :143, OnChangedAsync :153, OnWrite :164, Handle<Command> :171-206.
Attribute id constants used as keys of Patch.AttributeValues(): AttrOnOff, AttrGlobalSceneControl, AttrOnTime, AttrOffWaitTime, AttrStartUpOnOff:
30:	AttrOnOff              uint32 = 0x0000 // bool, M, R V, N S
31:	AttrGlobalSceneControl uint32 = 0x4000 // bool, LT, R V
32:	AttrOnTime             uint32 = 0x4001 // uint16, LT, RW VO
33:	AttrOffWaitTime        uint32 = 0x4002 // uint16, LT, RW VO
34:	AttrStartUpOnOff       uint32 = 0x4003 // StartUpOnOffEnum, LT, RW VM, X N
Keying: Patch.AttributeValues() returns map[uint32]any keyed by the numeric attribute id (uint32), values in stored form, nil for each id in Patch.Null (cluster/spec/onoff/server_gen.go:75-102).

### 6.3 node.Config top-level fields (node/config.go; `type Config struct` at line 43), name and line
node/config.go:45 Network Network
node/config.go:48 Commissioning Commissioning
node/config.go:52 ProductDescription ProductDescription
node/config.go:55 BasicInformation BasicInformation
node/config.go:58 Store Store
node/config.go:63 Advertiser Advertiser
node/config.go:67 DeviceTypeValidation DeviceTypeValidationMode
node/config.go:73 Attestation Attestation
node/config.go:76 OTA *OtaConfig
node/config.go:82 OTAProvider *OTAProviderConfig
node/config.go:86 CommissionerControl *CommissionerControlConfig
node/config.go:88 FixedLabels []fixedlabeldef.LabelStruct
node/config.go:94 Locales []string
node/config.go:106 Groups *Groups
node/config.go:116 Topology Snapshotter
node/config.go:122 EndpointPersistence EndpointPersistence
node/config.go:126 AggregatorServers []contract.ClusterServer
node/config.go:132 SupportedFabrics uint8
node/config.go:141 DisableTimeSynchronization bool
node/config.go:148 GeneralDiagnosticsFeatures *GeneralDiagnosticsFeatures
node/config.go:156 MaxPathsPerInvoke uint16
node/config.go:163 CapabilityMinima CapabilityMinimaStruct
node/config.go:166 Limits Limits
node/config.go:170 Client Client
node/config.go:173 BootID BootID
node/config.go:176 Health Health
node/config.go:180 Observers Observers
node/config.go:184 Metrics Metrics
node/config.go:187 Diagnostics Diagnostics
node/config.go:191 Logger *slog.Logger
node/config.go:200 Clock clock.Clock

### 6.4 node.Node exported methods (grep -n '^func (n \*Node) [A-Z]' node/*.go, non-test)
node/attestation.go:48:func (n *Node) AttestationError() error { return n.attestationErr }
node/client.go:173:func (n *Node) Peers() *peer.Set {
node/client.go:184:func (n *Node) Bindings() *binding.Manager { return n.bindings }
node/commissioner_control.go:56:func (n *Node) CommissionerControl() *mattercore.CommissionerControl { return n.commissionerControl }
node/commissioning.go:452:func (n *Node) OpenBasicWindow(ctx context.Context, creds WindowCredentials) error {
node/credentials.go:77:func (n *Node) PairingCodesFor(creds WindowCredentials) (qr, manual string, err error) {
node/diagnostics.go:30:func (n *Node) DiagnosticEvents() []DiagnosticEvent {
node/diagnostics.go:40:func (n *Node) OnDiagnosticEvent(fn func(DiagnosticEvent)) (unsubscribe func()) {
node/diagnostics.go:53:func (n *Node) OnDiagnosticEventAsync(fn func(DiagnosticEvent)) (unsubscribe func()) {
node/health.go:96:func (n *Node) Health() (HealthSample, bool) {
node/lifecycle.go:123:func (n *Node) Topology() BridgeTopology {
node/mdns.go:76:func (n *Node) MDNSEnabled() bool { return mdns.IsEnabled(n.advertiser) }
node/mdns.go:80:func (n *Node) MDNSServices() []MDNSService { return n.advertiser.Active() }
node/mdns.go:86:func (n *Node) DiagnoseMDNS() []MDNSFinding {
node/node.go:626:func (n *Node) Start(ctx context.Context) error {
node/node.go:734:func (n *Node) Stop(ctx context.Context) error {
node/node.go:793:func (n *Node) Run(ctx context.Context) error {
node/node.go:805:func (n *Node) Bridge() *bridge.Bridge { return n.br }
node/node.go:810:func (n *Node) LocalAddr() string { return n.br.LocalAddr() }
node/node.go:813:func (n *Node) Root() *RootClusters { return n.root }
node/node.go:817:func (n *Node) Aggregator() *Aggregator { return n.agg }
node/node.go:824:func (n *Node) Sessions() *SessionManager { return n.sec.sessions }
node/node.go:830:func (n *Node) Subscriptions() *SubscriptionManager { return n.sec.subs }
node/node.go:841:func (n *Node) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
node/node.go:846:func (n *Node) CommissioningWindow() *CommissioningWindow { return n.br.CommissioningWindow() }
node/node.go:858:func (n *Node) SetCommissioningBlocked(ctx context.Context, err error) {
node/node.go:873:func (n *Node) FabricInfos(ctx context.Context) ([]FabricInfo, error) {
node/node.go:889:func (n *Node) UniqueID() string { return n.cfg.BasicInformation.UniqueID }
node/node.go:895:func (n *Node) BootReason() uint8 { return n.bootReason }
node/node.go:898:func (n *Node) Store() store.Port { return n.st }
node/node.go:904:func (n *Node) Persistence() EndpointPersistence { return n.endpoints }
node/node.go:911:func (n *Node) Groups() *Groups { return n.groups }
node/node.go:917:func (n *Node) PairingCodes() (qr, manual string, err error) {
node/ota_provider.go:93:func (n *Node) OTAProvider() *mattercore.OTAProvider { return n.otaProvider }
node/reannounce.go:47:func (n *Node) TriggerReannounce(ctx context.Context) {

### 6.5 Aggregator (node/aggregator.go exported funcs/methods, non-test)
153:func (a *Aggregator) Partition(name string) *Partition {
211:func (a *Aggregator) RetirePartition(ctx context.Context, name string) error {
243:func (a *Aggregator) Add(ctx context.Context, ep Endpoint) error {
258:func (a *Aggregator) Replace(ctx context.Context, key device.SourceKey, ep Endpoint) error {
268:func (a *Aggregator) Remove(ctx context.Context, key device.SourceKey) error {
278:func (a *Aggregator) MarkComplete() { a.Partition(Scope).MarkComplete() }
281:func (a *Aggregator) Endpoints() []Endpoint { return a.Partition(Scope).Endpoints() }
285:func (a *Aggregator) Batch(ctx context.Context, fn func(b *Batch) error) error {
291:func (p *Partition) Add(ctx context.Context, ep Endpoint) error {
297:func (p *Partition) Replace(ctx context.Context, key device.SourceKey, ep Endpoint) error {
303:func (p *Partition) Remove(ctx context.Context, key device.SourceKey) error {
311:func (p *Partition) MarkComplete() {
319:func (p *Partition) Endpoints() []Endpoint {
350:func (p *Partition) Batch(ctx context.Context, fn func(b *Batch) error) error {
375:func (b *Batch) Add(ep Endpoint) error {
400:func (b *Batch) Replace(key device.SourceKey, ep Endpoint) error {
438:func (b *Batch) Remove(key device.SourceKey) error {
461:func (b *Batch) MarkComplete() {
515:func (a *Aggregator) IncreaseConfigurationVersion(key device.SourceKey) error {
539:func (a *Aggregator) IncreaseConfigurationVersions(keys ...device.SourceKey) error {
Node.Aggregator(): node/node.go:817 `func (n *Node) Aggregator() *Aggregator { return n.agg }`

### 6.6 device.Device exported methods (grep -n '^func (d \*Device) [A-Z]' device/*.go, non-test)
device/base.go:138:func (d *Device) FixedLabel() *fixedlabeldef.Server {
device/base.go:149:func (d *Device) PowerSource() *powersourcedef.Server {
device/base.go:158:func (d *Device) UserLabel() *userlabeldef.Server {
device/device.go:536:func (d *Device) Type() *Type { return d.typ }
device/device.go:541:func (d *Device) Server(clusterID uint32) contract.ClusterServer {
device/device.go:552:func (d *Device) Servers() []contract.ClusterServer { return slices.Clone(d.servers) }
device/device.go:555:func (d *Device) MatterDeviceType() uint16 { return uint16(d.typ.ID) }
device/device.go:559:func (d *Device) MatterClusterServers() []contract.ClusterServer { return d.Servers() }
device/device.go:571:func (d *Device) AddPart(p *Device) error {
device/device.go:644:func (d *Device) Parts() []*Device {
device/device.go:654:func (d *Device) Spec() endpoint.Spec {
device/lifecycle.go:95:func (d *Device) EndpointNumber() (uint16, bool) {
device/lifecycle.go:112:func (d *Device) SetReachable(ctx context.Context, reachable bool) {
device/lifecycle.go:123:func (d *Device) Reachable() bool { return d.available() }
device/lifecycle.go:143:func (d *Device) Rename(ctx context.Context, name string) {
device/lifecycle.go:190:func (d *Device) Update(ctx context.Context, fn func(*HostStatement)) {
device/lifecycle.go:233:func (d *Device) OnChanged(clusterID uint32, fn func(change contract.Change)) (unsubscribe func()) {
device/lifecycle.go:245:func (d *Device) OnChangedAsync(clusterID uint32, fn func(change contract.Change)) (unsubscribe func()) {

### 6.7 PairingCodes, Health, DiagnosticEvents, Metrics
[/home/markus/Dokumente/GitHub/go-fabric/node/node.go:917-921]
~~~~go
func (n *Node) PairingCodes() (qr, manual string, err error) {
	cm := n.cfg.Commissioning
	return pairingCodes(n.cfg.ProductDescription, cm.Discriminator, cm.Passcode)
}

~~~~
[/home/markus/Dokumente/GitHub/go-fabric/node/health.go:14-20]
~~~~go
// ([bridge.HealthSample]): Healthy while the operational UDP listener is
// bound, with a stable English Note.
type HealthSample = bridge.HealthSample

// DefaultProbeInterval is the probe cadence a zero [Health].Interval
// selects ([bridge.DefaultProbeInterval], 30 s).
const DefaultProbeInterval = bridge.DefaultProbeInterval
~~~~
[/home/markus/Dokumente/GitHub/go-fabric/node/health.go:94-100]
~~~~go
// Health returns the health probe's last verdict; false before the first
// one and when [Config].Health runs no probe.
func (n *Node) Health() (HealthSample, bool) {
	n.healthMu.Lock()
	p := n.health
	n.healthMu.Unlock()
	if p == nil {
~~~~
HealthSample = bridge.HealthSample (node/health.go:16); fields:
bridge/health_probe.go:41:type HealthSample struct {
bridge/health_probe.go-42-	Healthy bool
bridge/health_probe.go-43-	// Note is the stable, English machine string. The tracker's scoring
bridge/health_probe.go-44-	// treats it as a sentinel, so it must stay stable and un-localized.
bridge/health_probe.go-45-	Note string
bridge/health_probe.go-46-}
bridge/health_probe.go-47-
bridge/health_probe.go-48-// HealthRecorder is the slim tracker contract the probe touches. The
bridge/health_probe.go-49-// daemon's health tracker satisfies it through a host-side adapter that
bridge/health_probe.go-50-// translates [HealthSample] into the tracker's own sample type.
bridge/health_probe.go-51-type HealthRecorder interface {
bridge/health_probe.go-52-	Record(name string, sample HealthSample)
bridge/health_probe.go-53-	// RecordUnhealthy states an unhealthy condition rather than sampling one,
bridge/health_probe.go-54-	// so it takes effect without the flap-damping Record applies.
bridge/health_probe.go-55-	RecordUnhealthy(name string, sample HealthSample)
[/home/markus/Dokumente/GitHub/go-fabric/node/diagnostics.go:20-60]
~~~~go
// capacity is the ring's capacity: Capacity, or the default below one.
func (d Diagnostics) capacity() int {
	if d.Capacity < 1 {
		return DefaultDiagnosticsCapacity
	}
	return d.Capacity
}

// DiagnosticEvents returns the node's diagnostic trace, newest first: a
// copy the caller may keep (the REST handler of a daemon serialises it).
func (n *Node) DiagnosticEvents() []DiagnosticEvent {
	return n.diagRing.Snapshot()
}

// OnDiagnosticEvent registers fn for every event the node records from
// now on. fn runs inline, on the goroutine that recorded the event — the
// receive path of a pairing or a session — after the trace stored it and
// outside its lock: it must not block, and may read
// [Node.DiagnosticEvents]. [Node.OnDiagnosticEventAsync] queues it
// instead. It returns the unsubscribe function.
func (n *Node) OnDiagnosticEvent(fn func(DiagnosticEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return n.diagRing.Observe(fn)
}

// OnDiagnosticEventAsync is [Node.OnDiagnosticEvent] with fn queued on
// the node's callback goroutine: the event is taken when it is recorded,
// and fn runs later, one call after the other, while the node runs
// (between [Node.Start] and [Node.Stop]; Stop returns once every queued
// call ran). An event recorded while the node does not run is not
// delivered. fn must not stop the node.
func (n *Node) OnDiagnosticEventAsync(fn func(DiagnosticEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	q := n.callbacks
	return n.diagRing.Observe(func(e DiagnosticEvent) { q.Enqueue(func() { fn(e) }) })
}
~~~~
DiagnosticEvent type: node/facade.go:57:type DiagnosticEvent = diagevent.Event
[/home/markus/Dokumente/GitHub/go-fabric/contract/metrics.go:22-30]
~~~~go
type Metrics interface {
	// Counter adds delta (at least one) to the monotonic counter name.
	Counter(name string, delta int64)
	// Gauge sets the gauge name to value, the current level.
	Gauge(name string, value int64)
}

// The named metric set. The names are spec-near — the Matter terms for
// what is counted — not the names of matter.js internals, so a dashboard
~~~~
Metric names (contract/metrics.go):
36:	MetricSessionsOpen = "sessions.open"
39:	MetricSubscriptionsActive = "subscriptions.active"
43:	MetricIMReceived = "im.received"
46:	MetricIMSent = "im.sent"
50:	MetricPASEFailures = "pase.failures"
53:	MetricPASELockouts = "pase.lockouts"
56:	MetricAggregatorReassemblies = "aggregator.reassemblies"
59:	MetricMRPRetransmits = "mrp.retransmits"
63:	MetricPanicsRecovered = "panics.recovered"
Config.Metrics: node/config.go:184 (`Metrics Metrics`), `type Metrics = contract.Metrics` node/metrics.go:12.

### 6.8 Generator and schema JSON
script/clustergen/main.go header (lines 1-30) states: generates one Go package per cluster under cluster/spec/ (ids, enums, bitmaps, structs with TLV codecs, command/event payloads, spec.Cluster definition, generated test). Reads parity/schema.json (snapshotPath main.go:39), writes outDir cluster/spec (main.go:40). Files in script/clustergen: clusters.go compile_test.go declare.go gen.go gen_errors_test.go gen_test.go hull.go main.go names.go ports_device.go ports_media.go snapshot.go testdata 
[/home/markus/Dokumente/GitHub/go-fabric/script/clustergen/main.go:1-30]
~~~~go
// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command clustergen generates cluster definitions from the resolved layer
// of parity/schema.json (the matter.js model extract): one package per
// cluster under cluster/spec/, holding the cluster's ids, typed enums,
// bitmaps and structs with their TLV codecs, the command and event
// payloads, the spec.Cluster definition the cluster/spec runtime works from,
// and a generated test that holds the definition against the snapshot and
// round-trips every codec.
//
// It is matter.js's support/codegen translated: matter.js generates its
// cluster types (packages/types/src/clusters) from the model and writes its
// behaviors by hand; so does this module. See
// docs/adr/0013-generated-cluster-definitions.md.
//
// Usage, from the module root (make generate-matter-schema runs it):
//
//	go run ./script/clustergen            # regenerate the committed clusters
//	go run ./script/clustergen -all -out DIR
//	                                      # every cluster into DIR
//
// The committed set is the clusters a server in this module is built on
// (committed in clusters.go); adding one is adding its name there.
package main

import (
	"bytes"
	"errors"
	"flag"
~~~~
File writes: script/clustergen/main.go:104 `os.WriteFile(path, content, 0o644)`. grep for 'json' in script/clustergen non-test files that marshal output: (empty means none). The only JSON in that pipeline is the input (script/clustergen/snapshot.go:13 decodes the resolved layer of parity/schema.json).
Other generators: apidiff archcheck ble-virtual ble-virtual-test.sh chiptool-setup.sh citecheck clustergen coverfloor dclsnapshot devicegen docscheck errprefix extract-from-matter-js.ts generate_matter_schema.go matterjs-pin-distance.sh mkdocs reachability slogkeys testparity 

### 6.9 parity/schema.json
Top-level keys: ['matter', 'deviceTypes', 'clusters', 'globalDatatypes', 'baseDeviceTypes']
matter: {"revision": "1.6.1", "specificationVersion": 17170688, "interactionModelRevision": 12, "dataModelRevision": 21, "sourceCommit": "862ad75f4d3c7ab528b8a3954168146082cad8a6"}
  matter dict 5
  deviceTypes list 91
  clusters list 135
  globalDatatypes list 22
  baseDeviceTypes list 1
OnOff cluster keys: ['id', 'name', 'revision', 'featureMap', 'attributes', 'commands', 'events', 'features', 'datatypes', 'classification']
OnOff header: {"id": 6, "name": "OnOff", "revision": 6, "featureMap": 0}
OnOff attributes[0] verbatim (compact):
{"id": 0, "name": "OnOff", "type": "bool", "conformance": "M", "access": "R V", "quality": "N S", "effective": {"type": "bool", "metatype": "boolean", "primitive": "bool", "conformance": {"text": "M", "ast": {"type": "M"}}, "access": {"rw": "R", "readPriv": "V"}, "quality": {"nonvolatile": true, "scene": true}}}
OnOff attribute names/ids: [(0, 'OnOff'), (16384, 'GlobalSceneControl'), (16385, 'OnTime'), (16386, 'OffWaitTime'), (16387, 'StartUpOnOff'), (65532, 'FeatureMap'), (65533, 'ClusterRevision')]
OnOff commands[0] verbatim:
{"id": 0, "name": "Off", "direction": "request", "conformance": "M", "response": "status", "effective": {"direction": "request", "response": "status", "conformance": {"text": "M", "ast": {"type": "M"}}, "access": {"rw": "R", "readPriv": "O", "writePriv": "O"}, "fields": []}}
OnOff features: [{"name": "LT", "conformance": "[!OFFONLY]", "bit": 0, "effective": {"title": "Lighting", "conformance": {"text": "[!OFFONLY]", "ast": {"type": "optionalIf", "param": {"type": "!", "param": {"type": "name", "param": "OFFONLY"}}}}}}, {"name": "DF", "conformance": "[!OFFONLY]", "bit": 1, "effective": {"title": "DeadFrontBehavior", "conformance": {"text": "[!OFFONLY]", "ast": {"type": "optionalIf", "
deviceTypes[0] keys: ['id', 'name', 'classification', 'revision', 'requirements', 'effective']
3410:          "name": "OnOff",
3457:            "name": "OnOff",
3467:              "name": "OnOff"

## 7. fabrictest exported API (grep -n '^func ' fabrictest/*.go | grep -v _test; unexported omitted)
fabrictest/access.go:25:func (p *Pair) Grant(ctx context.Context, privilege Privilege) error 
fabrictest/controller.go:106:func (s *Site) AddController(opts ControllerOptions) *Controller 
fabrictest/controller.go:241:func (c *Controller) Close() 
fabrictest/controller.go:259:func (c *Controller) Restart(ctx context.Context) error 
fabrictest/controller.go:319:func (c *Controller) Index() int { return c.index }
fabrictest/controller.go:322:func (c *Controller) Addr() *net.UDPAddr { return c.listener.LocalAddr() }
fabrictest/controller.go:325:func (c *Controller) Exchanges() *exchange.Manager 
fabrictest/controller.go:332:func (c *Controller) Names() *discovery.Names 
fabrictest/controller.go:339:func (c *Controller) CA() *ca.CA 
fabrictest/controller.go:346:func (c *Controller) Fabric() *commissioner.Fabric 
fabrictest/controller.go:353:func (c *Controller) Peers() *peer.Set 
fabrictest/controller.go:360:func (c *Controller) Resumption() *peer.MemoryResumptionStore 
fabrictest/controller.go:367:func (c *Controller) Subscriptions() *interaction.Subscriptions 
fabrictest/controller.go:374:func (c *Controller) Commissioner() *commissioner.Commissioner 
fabrictest/controller.go:382:func (c *Controller) Registry() *client.Registry 
fabrictest/controller.go:410:func (c *Controller) Commission(ctx context.Context, d *Device, opts CommissionOptions) (*Pair, error) 
fabrictest/controller.go:455:func (p *Pair) Commissioned() bool { return p.Peer != nil }
fabrictest/controller.go:460:func (p *Pair) Node() (*client.Node, error) 
fabrictest/controller.go:485:func (s *Site) UncommissionedPair(opts PairOptions) *Pair 
fabrictest/controller.go:495:func (s *Site) CommissionedPair(opts PairOptions) *Pair 
fabrictest/device.go:115:func (s *Site) AddDevice(topo Topology) *Device 
fabrictest/device.go:152:func (d *Device) Index() int { return d.index }
fabrictest/device.go:156:func (d *Device) Start(ctx context.Context) error 
fabrictest/device.go:283:func (d *Device) Peers() *peer.Set { return d.Node().Peers() }
fabrictest/device.go:287:func (d *Device) Stop(ctx context.Context) error 
fabrictest/device.go:303:func (d *Device) Restart(ctx context.Context) error 
fabrictest/device.go:311:func (d *Device) Node() *node.Node 
fabrictest/device.go:318:func (d *Device) Bridge() *bridge.Bridge { return d.Node().Bridge() }
fabrictest/device.go:323:func (d *Device) Store() store.Port { return d.Node().Store() }
fabrictest/device.go:327:func (d *Device) Addr() *net.UDPAddr 
fabrictest/device.go:334:func (d *Device) Passcode() uint32 
fabrictest/device.go:341:func (d *Device) Discriminator() uint16 
fabrictest/device.go:349:func (d *Device) Advertiser() mdns.Advertiser 
fabrictest/device.go:357:func (d *Device) Endpoints() []*endpoint.Endpoint 
fabrictest/device.go:366:func (d *Device) EndpointOf(key string) (uint16, bool) 
fabrictest/device.go:384:func (d *Device) Act(fn func(n *node.Node) error) error 
fabrictest/device.go:410:func (r siteResolver) ResolveOperational(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) 
fabrictest/endpoint.go:23:func Endpoint(key string, deviceType uint16, servers ...contract.ClusterServer) node.Endpoint 
fabrictest/endpoint.go:34:func (e *bridgedEndpoint) Spec() endpoint.Spec 
fabrictest/endpoint.go:41:func (e *bridgedEndpoint) MatterDeviceType() uint16 { return e.deviceType }
fabrictest/endpoint.go:43:func (e *bridgedEndpoint) MatterClusterServers() []contract.ClusterServer 
fabrictest/mdns.go:45:func (q *queryConn) Send(msg []byte) error 
fabrictest/mdns.go:74:func (q *queryConn) Responses() <-chan mdns.QueryResponse { return q.out }
fabrictest/mdns.go:76:func (q *queryConn) Close() error 
fabrictest/metrics.go:25:func NewMetrics() *Metrics 
fabrictest/metrics.go:30:func (m *Metrics) Counter(name string, delta int64) 
fabrictest/metrics.go:37:func (m *Metrics) Gauge(name string, value int64) 
fabrictest/metrics.go:44:func (m *Metrics) Count(name string) int64 
fabrictest/metrics.go:52:func (m *Metrics) GaugeValue(name string) (int64, bool) 
fabrictest/multicast.go:57:func (c MembershipChange) String() string 
fabrictest/multicast.go:147:func (n *Network) FailJoins(fail func(group net.IP) error) 
fabrictest/multicast.go:159:func (n *Network) WatchMemberships(fn func(MembershipChange)) (stop func()) 
fabrictest/multicast.go:174:func (n *Network) Members(group net.IP) int 
fabrictest/multicast.go:228:func (d *Device) MulticastGroups() []net.IP 
fabrictest/multicast.go:237:func (s *socket) JoinGroup(group net.IP) error 
fabrictest/multicast.go:247:func (s *socket) LeaveGroup(group net.IP) error 
fabrictest/multicast.go:265:func (s *socket) SendMulticast(group net.IP, port int, payload []byte) error 
fabrictest/net.go:94:func (n *Network) Options() NetOptions 
fabrictest/net.go:103:func (n *Network) Set(opts NetOptions) 
fabrictest/net.go:115:func (n *Network) Filter(keep func(Datagram) bool) 
fabrictest/net.go:123:func (n *Network) Sent() uint64 { return n.sent.Load() }
fabrictest/net.go:250:func (s *socket) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) 
fabrictest/net.go:259:func (s *socket) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) 
fabrictest/net.go:269:func (s *socket) LocalAddr() net.Addr { return cloneAddr(s.local) }
fabrictest/net.go:271:func (s *socket) Close() error 
fabrictest/node.go:22:func (c *Controller) Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]client.Device, error) 
fabrictest/node.go:55:func (c *Controller) CommissionNode(ctx context.Context, d *Device, o client.CommissionOptions) (*client.Node, error) 
fabrictest/pase.go:26:func (c *Controller) PASE(ctx context.Context, d *Device) (*Pair, error) 
fabrictest/raw.go:24:func Exchange(ctx context.Context, p *Pair, opcode uint8, payload []byte) (*Message, error) 
fabrictest/raw.go:53:func Send(ctx context.Context, p *Pair, opcode uint8, payload []byte) error 
fabrictest/site.go:121:func NewSite(tb testing.TB, opts Options) *Site 
fabrictest/site.go:163:func (s *Site) Clock() clock.Clock { return s.clk }
fabrictest/site.go:167:func (s *Site) Fake() *clocktest.Fake { return s.fake }
fabrictest/site.go:170:func (s *Site) Net() *Network { return s.net }
fabrictest/site.go:173:func (s *Site) Loopback() bool { return s.opts.Loopback }
fabrictest/site.go:176:func (s *Site) Logger() *slog.Logger { return s.log }
fabrictest/site.go:179:func (s *Site) Devices() []*Device { return s.devicesSnapshot() }
fabrictest/site.go:182:func (s *Site) Controllers() []*Controller 
fabrictest/site.go:214:func (s *Site) Close() 
fabrictest/site.go:240:func (s *Site) Settle(ctx context.Context) error { return s.quiet(ctx, nil, s.waitUntil(ctx)) }
fabrictest/site.go:341:func (s *Site) Run(ctx context.Context, fn func(ctx context.Context) error) error 
fabrictest/site.go:371:func (s *Site) Eventually(ctx context.Context, what string, cond func() bool) error 
fabrictest/window.go:20:func (d *Device) OpenWindow(ctx context.Context, timeout time.Duration) error 
fabrictest/wire.go:46:func Connect(ctx context.Context, p *Pair) (*Interaction, error) 
fabrictest/wire.go:64:func Read(ctx context.Context, p *Pair, path AttributePath) (AttributeData, error) 
fabrictest/wire.go:83:func ReadWith(ctx context.Context, p *Pair, req ReadRequest) (*Report, error) 
fabrictest/wire.go:105:func ReadCluster(ctx context.Context, p *Pair, endpoint uint16, cluster uint32) (spec.AttributeValues, error) 
fabrictest/wire.go:123:func Events(ctx context.Context, p *Pair, paths ...EventPath) ([]EventData, error) 
fabrictest/wire.go:142:func (e *eventStatusError) Error() string 
fabrictest/wire.go:147:func (e *eventStatusError) Unwrap() error { return &e.StatusError }
fabrictest/wire.go:152:func Write(ctx context.Context, p *Pair, path AttributePath, value spec.Encodable) error 
fabrictest/wire.go:157:func WriteTimed(ctx context.Context, p *Pair, path AttributePath, value spec.Encodable) error 
fabrictest/wire.go:176:func Invoke(ctx context.Context, p *Pair, path CommandPath, fields spec.Encodable) (Result, error) 
fabrictest/wire.go:182:func InvokeTimed(ctx context.Context, p *Pair, path CommandPath, fields spec.Encodable) (Result, error) 
fabrictest/wire.go:200:func Subscribe(ctx context.Context, p *Pair, req SubscribeRequest) (*Subscription, error) 
Exported types:
fabrictest/controller.go:33:type ControllerOptions struct {
fabrictest/controller.go:72:type Controller struct {
fabrictest/controller.go:397:type CommissionOptions struct {
fabrictest/controller.go:436:type Pair struct {
fabrictest/controller.go:470:type PairOptions struct {
fabrictest/device.go:47:type Topology struct {
fabrictest/device.go:81:type Device struct {
fabrictest/metrics.go:15:type Metrics struct {
fabrictest/multicast.go:47:type MembershipChange struct {
fabrictest/net.go:32:type NetOptions struct {
fabrictest/net.go:50:type Datagram struct {
fabrictest/net.go:62:type Network struct {
fabrictest/site.go:44:type Options struct {
fabrictest/site.go:89:type Site struct {
[/home/markus/Dokumente/GitHub/go-fabric/fabrictest/site.go:44-120]
~~~~go
type Options struct {
	// Clock is the time source of every device and controller of the site;
	// nil is a new [clocktest.Fake] at [DefaultStart]. A test that drives
	// parts of its own on the system clock against a device of the site
	// passes clock.System{}.
	Clock clock.Clock
	// Net shapes the in-memory network's delivery; [Network.Set] changes
	// it later. Ignored with Loopback.
	Net NetOptions
	// Loopback runs the site's hosts on real UDP sockets on the loopback
	// address ([::1], an ephemeral port each) instead of the in-memory
	// network, for a test that needs sockets. The site still answers its
	// controllers' DNS-SD queries from what the devices publish.
	Loopback bool
	// Settle is the quiet period of [Site.Settle]; 0 is [DefaultSettle].
	Settle time.Duration
	// Wait bounds [Site.Eventually] and [Site.Settle] on the Wall clock
	// when their context has no deadline; 0 is [DefaultWait]. A context
	// with a deadline is waited on as it is.
	Wait time.Duration
	// Wall is the clock the quiet period is measured on and the quiet
	// check paces itself by — the machine's time, whatever Clock the hosts
	// run on, since "nothing ran for 10 ms" is a statement about the
	// process; nil is [clock.System].
	Wall clock.Clock
	// StoreOnDisk keeps each device's state in a SQLite file in the test's
	// temporary directory, as a deployment does, instead of in memory
	// (store/memstore and endpoint/memstore, the default). Either way the
	// state survives [Device.Restart], which starts the new node on the
	// same store; a test sets it to exercise the SQLite encoding across a
	// restart.
	StoreOnDisk bool
	// Logger receives the logs of the hosts the site builds — the nodes,
	// the controllers and their components; nil discards those. A
	// component that logs to slog.Default() rather than to the logger it
	// is given (OperationalCredentials' matter.opcreds.cmd lines, gate 1
	// G1-18) still reaches the process's default logger.
	Logger *slog.Logger
}

// Site is a set of in-process Matter hosts — devices ([Site.AddDevice])
// and controllers ([Site.AddController]) — on one network and one clock.
// It mirrors matter.js's MockSite (packages/node/src/testing/mock-site.ts):
// every host closes with the site, and the site closes when the test
// ends. It is safe for concurrent use.
type Site struct {
	tb   testing.TB
	opts Options
	// dir holds the devices' SQLite files (Options.StoreOnDisk). It is created before Close is
	// registered, so the test's cleanup removes it only after Close has
	// stopped every node (cleanups run last-registered first).
	dir  string
	clk  clock.Clock
	wall clock.Clock // Options.Wall
	fake *clocktest.Fake
	net  *Network
	log  *slog.Logger

	// inSyscall is the number of the process's standing goroutines in a
	// system call (os/signal's loop, once a test of the binary called
	// signal.Notify), which never leave it and are no work of the site:
	// the fewest seen since the site was built. A count taken once would
	// include another parallel test's passing system calls and hide this
	// site's work once those end (the G2-16 flakes); the minimum only ever
	// errs towards waiting longer.
	inSyscall atomic.Uint64

	// mu guards closers and nextIndex.
	mu          sync.Mutex
	nextIndex   int
	devices     []*Device
	controllers []*Controller
	closers     []func()
	closeOnce   sync.Once
}

// NewSite returns an empty site, closed when tb ends.
~~~~
