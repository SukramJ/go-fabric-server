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
| `--bridge-extensions` | — | the features go-fabric implements ahead of matter.js that the bridge node serves, by register ID, `id[,id…]`: `BD-Thread-NetComm-ThreadVariant`, `BD-chip-Thermostat-Events`, `BD-chip-Thermostat-Schedules`, `BD-chip-TimeSynchronization-Features` at go-fabric v0.15.2 ([Surface policies](#surface-policies)); an unknown ID stops the start with the known ones |
| `--bridge-spec-level` | the pin's (`1.6.1`) | the Matter specification level the bridge node composes at: `1.4`, `1.4.1`, `1.4.2`, `1.5.1`, `1.6.0`, `1.6.1` at go-fabric v0.15.2; another value stops the start with the list |
| `--bridge-profile` | — | the controller profiles the bridge node composes for, `name[,name…]`: `alexa`, `apple-home`, `google-home`, `home-assistant`, `smartthings` at go-fabric v0.15.2; an unknown name, or two whose levels differ without `--bridge-spec-level`, stops the start |
| `--enable-test-net-dcl` | `false` | trust the CSA test roots besides the production DCL (as `--trust development`; matterjs-server's flag) |
| `--bluetooth-adapter` | — | the HCI id of the BLE adapter `commission_with_code` may commission over (matterjs-server's flag; `0` is `hci0`, an adapter name is taken as it is): go-fabric's BlueZ central over the system bus, which needs rights to `org.bluez` (the `bluetooth` group or a polkit rule); the start fails when the adapter cannot be opened |
| `--bluetooth-proxy` | — | an ESPHome Bluetooth proxy `commission_with_code` may commission through, `host[:port][,key]` (port 6053; `key` the base64 API encryption key, none for a plaintext API); repeatable, the environment variable gives one; not with `--bluetooth-adapter`. go-fabric's `ble/esphome` central, pure Go, no adapter or D-Bus; the start fails when a proxy refuses (wrong or missing key, no active connections) or none answers ([Bluetooth through ESPHome proxies](#bluetooth-through-esphome-proxies)) |
| `--web` | — | serve go-fabric-ui's pages on this address: the bridge node's with `--bridge`, the controller's with `--controller`; read-only unless `--web-admin` or `--web-admin-source` |
| `--web-admin` | `false` | the pages may write, show the pairing code and use the controller's admin routes (the page's token mode) |
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

### Surface policies

go-fabric serves each endpoint's surface — device types, clusters,
features — as a value composed under node-level policies (its
[ADR 0030](https://github.com/SukramJ/go-fabric/blob/v0.15.2/docs/adr/0030-the-served-surface-is-a-value-under-policies.md)),
the bridge node's `node.Config.Surface`. Three flags set them:

- `--bridge-extensions` enables features go-fabric implements ahead of
  matter.js, by their row in go-fabric's
  [Ahead-of-matter.js register](https://github.com/SukramJ/go-fabric/blob/v0.15.2/notes/parity/by_design.md).
  A declaration that selects such a feature while its row is not enabled
  — Thermostat's `MatterScheduleConfiguration` (row
  `BD-chip-Thermostat-Schedules`) or its events feature
  (`BD-chip-Thermostat-Events`) — is refused whole with
  `InvalidArguments`; the details name the device, the register IDs and
  the flag value that enables them besides the enabled ones:

  ```text
  declare: devices[0] (t): Thermostat: surface: composition refused (surface: ahead-of-matter.js extension not enabled: BD-chip-Thermostat-Schedules): …; the node does not enable BD-chip-Thermostat-Schedules: enable them with --bridge-extensions BD-chip-Thermostat-Schedules (go-fabric notes/parity/by_design.md, Ahead-of-matter.js register)
  ```

- `--bridge-spec-level` composes at an earlier specification level: the
  optional surface the level does not know is removed, what the pin
  mandates is kept, and a device whose type or mandated cluster the level
  does not know is refused with `InvalidArguments` naming the level and
  the endpoint.
- `--bridge-profile` selects controller profiles: each removes what a
  Reproduced row of go-fabric's profile register names (a mandatory
  element is kept) and records a warning for every other row
  (`surface.Policies.Profiles`); a profile's level applies when
  `--bridge-spec-level` is not given.

The policies are the operator's: they hold for the whole node, and a
`/bridge` client cannot ask for an extension per declaration (go-fabric's
Aggregator refuses a device composed under other policies,
`node.ErrPolicyMismatch`). The `/bridge` command `surface` answers the
node's surface report — the policies, the effective surface, every
decision, the withheld elements — and the node pages show the policies
on the status page. Changing a policy changes what the node serves: a
paired controller is told through the endpoint's ConfigurationVersion at
the next start.

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

## The pages

`--web host:port` serves go-fabric-ui's pages (`fabricui.Handler`, v0.2.1),
both families in one handler:

- **The node pages** for the bridge node of `--bridge`: status, pairing,
  fabrics, the commissioning window, diagnostics. Without `--bridge` the
  node family is shown as disabled. The host decides what is exposed over
  `/bridge`, so the pages have no exposure selection.
- **The controller pages** for the controller half (`--controller`, on by
  default): the fabric, the commissioned nodes (attributes, commands, ACL,
  bindings, ICD, fabrics, the commissioning window), commissioning by code
  or from a discovery, the fabric label, the Wi-Fi and Thread credentials
  and software updates. With `--controller=false` the family is shown as
  disabled. The source is go-fabric-ui's adapter
  (`fabricui/gofabric.ControllerOf`) over the daemon's controller.

Three admin modes, the same for both families — a controller route marked
admin (commissioning, discovery, a write, decommissioning, the credentials,
the label) follows the rule a node page's write follows:

- default — read-only: no writes, no pairing code, no admin route;
- `--web-admin` — the page's token mode: a random per-process token in
  `GET /api/v1/node/status` and `GET /api/v1/controller/status` the page
  sends with every write;
- `--web-admin-source IP` — only requests from that address are answered,
  each as an admin: the Ingress proxy of a Home Assistant add-on
  authenticates the user ([addon.md](addon.md)).

The controller pages and `/ws` share the controller half's state:

- **One credential list.** The pages' credentials are `server.json`'s, the
  entries `set_wifi_credentials` and `set_thread_dataset` store and
  `get_all_credentials` lists: an entry set on either side is listed on
  the other, by name only (SSID, the dataset's network name); a secret is
  never read back. A dataset set on the pages must be MeshCoP TLVs, as
  over `/ws`; removing an entry the store does not hold is 404 on the pages
  (the commands succeed without effect). Every change broadcasts
  `server_info`, as the commands do.
- **One label.** The pages' fabric label is `set_default_fabric_label`'s:
  stored for the next start, set on the fabric and pushed to the nodes in
  the background; with `--default-fabric-label` the pages' change is
  refused (409).
- **One pairing template.** A commissioning on the pages uses the half's
  attestation policy, its subscription mode
  (`--disable-auto-subscription`) and the BLE central of
  `--bluetooth-proxy` or `--bluetooth-adapter` (the status'
  `capabilities.ble`).
- **One event stream per side.** go-fabric-ui's node hooks are chained
  after the half's (`gofabric.Hooks.Wrap` over `controllerapi`'s hooks),
  so `/ws` keeps its events and the pages get theirs. On shutdown the
  daemon closes the pages' hooks (`gofabric.Hooks.Close`) before it closes
  the controller.

## Bluetooth through ESPHome proxies

`--bluetooth-proxy` commissions devices that are still on BLE through
ESPHome Bluetooth proxies (ESP32 boards that scan and hold GATT connections
for the daemon, reached over the ESPHome native API on TCP), so the host
needs neither an adapter nor system-bus rights. go-fabric's guide
[`docs/guides/ble-proxies.md`](https://github.com/SukramJ/go-fabric/blob/main/docs/guides/ble-proxies.md)
is the full description.

- **The proxy needs `bluetooth_proxy: active: true`.** Without it the proxy
  only forwards advertisements and the start fails ("the proxy has no
  active connections").
- **The API key.** `api: encryption: key:` is the `key` after the comma;
  a wrong key, a key at a plaintext proxy or none at an encrypted one fail
  the start, naming the proxy.
- **One API subscriber per proxy.** A proxy serves its Bluetooth to the
  last API client that subscribed — usually Home Assistant's Bluetooth
  integration. While the daemon scans or pairs it takes the proxy over
  (`ble.esphome.proxy_taken` in the log), and Home Assistant takes it back.
  Dedicate a proxy to commissioning, or accept that Home Assistant's use of
  it flaps while a device is paired; a proxy taken over twice during a
  pairing fails it with "the Bluetooth proxy is in use by another API
  client", which `commission_with_code` reports as it is.
- **Several proxies** are scanned together; the connect goes through the
  one that saw the device with the strongest signal. One that is only
  unreachable at start is retried in the background.
- `server_info.bluetooth_enabled` and the pages' `capabilities.ble` are
  true; commissioning over BLE still needs a stored Wi-Fi or Thread
  credential ([by_design.md](by_design.md) B20, B22).

```sh
fabric-server --bluetooth-proxy proxy-office.local,<base64 key> --bluetooth-proxy 10.0.0.42:6053,<key>
```

## Home Assistant

[home-assistant.md](home-assistant.md) says how the Matter integration is
pointed at the server; [addon.md](addon.md) is the add-on.
