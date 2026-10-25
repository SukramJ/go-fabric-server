# Operations

## Running

```sh
fabric-server --listen 127.0.0.1:5580 --storage-path /var/lib/fabric-server
```

Home Assistant's Matter integration then connects to
`ws://localhost:5580/ws`.

## Flags

Every flag has an environment variable `FABRIC_SERVER_<FLAG>` (upper case,
`-` as `_`), used when the flag is not given.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--listen` | `unix:$XDG_RUNTIME_DIR/fabric-server.sock` | the API listener: `host:port` or `unix:/path` |
| `--token` | — | the bearer token every HTTP request must carry; required on a TCP listener beyond loopback |
| `--storage-path` | `$STATE_DIRECTORY`, else `$XDG_STATE_HOME/fabric-server`, else `./data` | the controller's database and the server's settings |
| `--matter-listen` | `[::]:0` | the controller's UDP address for Matter |
| `--vendor-id` | `0xFFF1` | our fabric's vendor id, on the first run |
| `--trust` | `production` | attestation roots: `production` (the DCL snapshot go-fabric embeds) or `development` (plus the CSA test roots) |
| `--log-level` | `info` | `critical`, `error`, `warning`, `notice`, `info`, `debug`; `set_loglevel` changes it until the next start |
| `--default-fabric-label` | — | pins the fabric label; `set_default_fabric_label` then has no effect |
| `--disable-auto-subscription` | `false` | new nodes are paired without the server's subscription; `subscribe`/`unsubscribe` become available |
| `--controller` | `true` | run the controller half (`/ws`) |
| `--bridge` | `false` | run the bridge half (`/bridge`) and its bridge node |
| `--bridge-listen` | `:5540` | the bridge node's UDP address for Matter |
| `--bridge-passcode` | `20202021` | the bridge node's setup passcode — the public test value: change it before pairing for real |
| `--bridge-discriminator` | `3840` | the bridge node's discriminator |
| `--bridge-vendor-id`, `--bridge-product-id` | `0xFFF1`, `0x8001` | the bridge node's vendor and product (the CSA test vendor) |
| `--bridge-label` | `fabric-server bridge` | the bridge node's NodeLabel |
| `--bridge-result-timeout` | `5s` | how long a `write` or `command` event waits for the bridge client's `result` |
| `--enable-test-net-dcl` | `false` | trust the CSA test roots besides the production DCL (as `--trust development`; matterjs-server's flag) |
| `--bluetooth-adapter` | — | the HCI id of the BLE adapter `commission_with_code` may commission over (matterjs-server's flag); refused at start until go-fabric releases its BlueZ central on v0.12 |
| `--web` | — | serve the bridge node's pages (go-fabric-ui) on this address; read-only unless `--web-admin` or `--web-admin-source` |
| `--web-admin` | `false` | the pages may write and show the pairing code (the page's token mode) |
| `--web-admin-source` | — | answer only requests from this IP address, as an admin (an Ingress proxy: `172.30.32.2` in a Home Assistant add-on) |
| `--health-check URL` | — | probe a `/health` URL and exit 0 when healthy, instead of running |
| `--version` | — | print the version and exit |

## The bridge node

With `--bridge` the daemon runs a Matter bridge node besides (or, with
`--controller=false`, instead of) the controller: an Aggregator whose
bridged devices the `/bridge` client declares (`docs/protocol.md`, the
bridge half). At start it logs its pairing codes
(`fabricserver.bridge.pairing`); the client reads them with
`pairing_codes`. The node carries the CSA test vendor and go-fabric's test
attestation chain, so a controller may warn that it is uncertified; set
`--bridge-passcode` and `--bridge-discriminator` before pairing for real.
The declared devices are not stored by the server: the client declares
them after every connect, and each keeps its endpoint number under its key.

## Files

Under `--storage-path` (created with mode 0700):

| File | Content |
| --- | --- |
| `fabric-server.db` (+ `-wal`, `-shm`) | the controller's SQLite database: our fabric and its CA, the paired nodes and their attribute images (go-fabric's stores) |
| `server.json` (mode 0600) | the server's settings: the fabric label, the stored Wi-Fi credentials and Thread datasets |
| `bridge.db` | with `--bridge`: the bridge node's SQLite store — its fabrics, ACLs and the endpoint numbers of the declared keys |

Back up the whole directory with the daemon stopped (or with
`sqlite3 fabric-server.db .backup`). Losing `fabric-server.db` loses the
fabric: every node must be commissioned again.

## Ports and network

- The API: `--listen` (5580 in the image and the unit).
- Matter: UDP on `--matter-listen` (the controller) and `--bridge-listen`
  (the bridge node, 5540), IPv6 link-local and site addresses; DNS-SD on UDP
  5353 (multicast). The host needs IPv6 and multicast on the interface
  the devices are on; in a container, use host networking.

## Authentication

- A Unix socket or a loopback TCP listener needs no token: whoever can reach
  it is on the host.
- A TCP listener beyond loopback requires `--token`; the daemon refuses to
  start without one. Clients send `Authorization: Bearer <token>` or the
  query parameter `?token=<token>`. Every route (`/ws`, `/health`,
  `/metrics`, `/schema`, `/bridge`) is behind it.
- Home Assistant's Matter integration sends no token: run it on the same
  host against a loopback listener.
- The WebSocket handshake refuses a cross-origin browser request (its
  `Origin` must match the host), so a web page cannot use a loopback
  listener behind the user's back.

## HTTP routes

| Route | Answer |
| --- | --- |
| `/ws` | the controller half (WebSocket) |
| `/bridge` | the bridge half (WebSocket), with `--bridge`; one client at a time |
| `GET /health` | `{"version", "node_count", "healthy"}`, with `--bridge` also `"device_count"`; 503 while a half is not healthy |
| `GET /metrics` | expvar: `fabricserver.ws.connections`, `fabricserver.ws.queued_bytes`, `fabricserver.ws.dropped`, `fabricserver.ws.closed_overflow`, the Go runtime's `memstats` |
| `GET /schema` | the JSON Schema of the node data values; header `X-Go-Fabric-Version` |
| `GET /schema/declaration` | the JSON Schema of the bridge half's declaration and patches |

## Container

```sh
docker run -d --name fabric-server --network host \
  -v fabric-server:/data ghcr.io/sukramj/go-fabric-server:0.1.0
```

The image (`deploy/Dockerfile`) is distroless and runs as `nonroot`; it
listens on `127.0.0.1:5580` (`FABRIC_SERVER_LISTEN`), keeps its state in
`/data` and checks itself with `--health-check`. Architectures: linux/amd64,
linux/arm64, linux/arm/v7.

## systemd

`deploy/systemd/fabric-server.service` runs the binary from
`/usr/local/bin` as a dynamic user with its state in
`/var/lib/fabric-server`, no capabilities and a read-only system. Settings go
into `systemctl edit fabric-server` as `Environment=FABRIC_SERVER_…` lines.

## Logs

slog text lines on stderr (journald, `docker logs`); the message keys follow
go-fabric's convention (`fabricserver.*` for the server, `matter.*` for the
stack).

## The node pages

`--web host:port` serves go-fabric-ui's node pages (`fabricui.Handler`,
v0.1.1) for the bridge node of `--bridge`: status, pairing, fabrics, the
commissioning window, diagnostics. Without `--bridge` the node family is
shown as disabled. The host decides what is exposed over `/bridge`, so the
pages have no exposure selection. Three admin modes:

- default — read-only: no writes, no pairing code;
- `--web-admin` — the page's token mode: a random per-process token in
  `GET /api/v1/node/status` the page sends with every write;
- `--web-admin-source IP` — only requests from that address are answered,
  each as an admin: the Ingress proxy of a Home Assistant add-on
  authenticates the user ([addon.md](addon.md)).

The controller pages come with go-fabric-ui's next family (U2).

## Home Assistant

[home-assistant.md](home-assistant.md) says how the Matter integration is
pointed at the server; [addon.md](addon.md) is the add-on.
