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
| `--node-limit` | `8` | the most device nodes the pages may create beside the `--bridge-*` one ([Several device nodes](#several-device-nodes)); `0` creates none |
| `--bridge-node`, `--node-default` | — | removed: further device nodes are created in the pages; either flag (or its environment variable) stops the start with that message |
| `--enable-test-net-dcl` | `false` | trust the CSA test roots besides the production DCL (as `--trust development`; matterjs-server's flag) |
| `--bluetooth-adapter` | — | the HCI id of the BLE adapter `commission_with_code` may commission over (matterjs-server's flag; `0` is `hci0`, an adapter name is taken as it is): go-fabric's BlueZ central over the system bus, which needs rights to `org.bluez` (the `bluetooth` group or a polkit rule); the start fails when the adapter cannot be opened |
| `--bluetooth-proxy` | — | an ESPHome Bluetooth proxy `commission_with_code` may commission through, `host[:port][,key]` (port 6053; `key` the base64 API encryption key, none for a plaintext API); repeatable, the environment variable gives one; not with `--bluetooth-adapter`. go-fabric's `ble/esphome` central, pure Go, no adapter or D-Bus; the start fails when a proxy refuses (wrong or missing key, no active connections) or none answers ([Bluetooth through ESPHome proxies](#bluetooth-through-esphome-proxies)) |
| `--web` | — | serve go-fabric-ui's pages on this address: the device nodes' with `--bridge`, the controller's with `--controller`, the Operation page always; read-only unless `--web-admin` or `--web-admin-source` |
| `--web-admin` | `false` | the pages may write, show the pairing code and use the controller's admin routes (the page's token mode) |
| `--web-admin-source` | — | answer only requests from this IP address, as an admin (an Ingress proxy: `172.30.32.2` in a Home Assistant add-on) |
| `--health-check URL` | — | probe a `/health` URL and exit 0 when healthy, instead of running |
| `--version` | — | print the version and exit |

### The controller's vendor id on paired devices

Commissioning writes the fabric's vendor id to the device, and every other
controller of that device can read it. By default it is `0xFFF1`, a CSA test
vendor id, so other ecosystems see this server as a test controller: Apple
Home has been seen to list it as another paired controller named
"Matter Test". This is expected. The fabric label is a separate value, which
Home Assistant sets to its location name. That the name comes from the vendor
id is inferred from these values; Apple Home's naming is not documented here
and has not been verified.

A different name needs a vendor id of your own, assigned by the CSA. The other
test ids, `0xFFF2` to `0xFFF4`, are test vendors as well, and another vendor's
id must not be used. `--vendor-id` only applies when the fabric is first
created: an existing fabric keeps its id, and devices already paired keep the
id they were given, so changing it means a new fabric and pairing again.

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

### Several device nodes

The first node is configuration: the `--bridge-*` flags configure the
node under the key `default` (store `bridge.db`, `/bridge`), and a daemon
configured as before runs that one node unchanged. Every further node is
registry state: an admin creates it in the pages (`--web` with
`--web-admin` or `--web-admin-source`, the node selector's "Create node…",
go-fabric-ui contract 0.6.0) by naming it, and renames or deletes it there.
No restart, and no port, discriminator or passcode by hand: the daemon
allocates them —

- the **key** from the label (lower-case letters and digits, every other
  run one `-`, at most 24 characters; `Küche Süd` is `kueche-sued`), with a
  suffix `-2`, `-3`, … when the default node, a page name, another
  registry node or a store file left on disk holds it;
- the **port**: the next UDP port after the default node's (`--bridge-listen`,
  5540) that no registry node holds and the host can bind, on the default
  node's host; a default node on port 0 gives every registry node an
  ephemeral port too;
- the **discriminator**: random 12 bits, no other node's (a commissioner
  tells the nodes apart by it); the **passcode**: random, of the valid set
  (go-fabric's `node.NewCommissioningCredentials`).

Every node is a Matter node of its own: its port, its discriminator, its
passcode, its fabrics, its mDNS advertisement, its store `bridge-<key>.db`
and its SerialNumber `FSB-<key>`; vendor and product are the `--bridge-*`
node's. The surface policies (`--bridge-extensions`, `--bridge-spec-level`,
`--bridge-profile`) are the same for all. A client feeds a node at
`/bridge/<key>` from the moment it is created (`/bridge` is the default
node); which bridged device goes to which node is the client's decision,
and `server_info` names the node it feeds (`node`). Every registry node
starts with the daemon; one that fails to start is logged
(`fabricserver.registry.start_failed`) and listed as not enabled, so the
pages can still delete it. `--node-limit` (8) caps the registry; above it
the pages answer `node_limit`.

The key is the node's identity (`docs/by_design.md` B23): a rename changes
the label only, which the pages show at once (the running node keeps the
NodeLabel it started with). **Deleting a node is final**: its client
receives `server_shutdown`, `/bridge/<key>` answers 404, the node removes
every fabric it holds (go-fabric's `node.FactoryReset`: each controller
that paired it loses it) and stops, and its store files and its entry are
deleted.

#### Erasing a node

The fabrics page of every device node — the `--bridge-*` one and each
registry node, the add-on's one node included — offers **Erase** to an
admin, behind two confirmations (`POST /api/v1/node/maintenance/erase`,
`/api/v1/nodes/<key>/maintenance/erase`, body `{"confirm":"erase-node"}`).
It returns the node to its factory state and brings it back in place:

- every fabric is removed (go-fabric's `node.FactoryReset`: every
  controller that paired the node loses it), the node stops, and its store
  and endpoint records are erased;
- a new node starts on the erased store under the same key, port,
  discriminator and passcode, uncommissioned and with a new UniqueID; the
  pages follow it (they poll the status until it answers again);
- the bridge client receives `server_shutdown`, dials the same path back
  and declares again — the endpoint numbers are assigned anew;
- the node's expvar counters continue the old node's (one prefix per key).

The log says `fabricserver.node.erased` with `node=<key>`. Either failure
keeps the daemon up, and every status page carries a warning naming the
key until an erase succeeds: when the erase itself fails
(`fabricserver.node.erase_failed`, warning `node_erase_failed`) nothing is
restarted; when the new node does not start
(`fabricserver.node.restart_failed`, warning `node_restart_failed`) the
pages read the stopped node. **Erase** again retries both, and after a
failed restart a restart of the daemon also brings the node back (its
store is erased).

The registry is `nodes.json` under `--storage-path` ([Files](#files)).
The Home Assistant add-on runs the one `--bridge-*` node only.

In 0.5.0 further nodes came from `--bridge-node`; that flag and
`--node-default` are removed, and a daemon started with either refuses with
a message naming the pages. Create the nodes in the pages instead; the
store files of the old nodes (`bridge-<key>.db`) are left as they are, never
adopted by a new node, and can be deleted.

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
  does not know is withheld: the declaration succeeds, the device serves
  no endpoint, `declare`'s answer names it in `withheld` with the level,
  the log has one `fabricserver.bridge.withheld` warning for it, and the
  `surface` report lists its withheld elements. The node runs on with
  the devices the level knows.
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
| `bridge.db` | with `--bridge`: the bridge node's SQLite store — its fabrics, ACLs and the endpoint numbers of the declared keys; an erase in the pages empties it and keeps the file |
| `nodes.json` (mode 0600) | with `--bridge`: the node registry, `{"nodes": [{"key", "label", "listen", "discriminator", "passcode", "created_at"}]}` — the nodes created in the pages, in their order; it holds their passcodes |
| `bridge-<key>.db` | per registry node: that node's store, the same content |

Back up the whole directory with the daemon stopped (or with
`sqlite3 fabric-server.db .backup`). Losing `fabric-server.db` loses the
fabric: every node must be commissioned again.

## Ports and network

- The API: `--listen` (5580 in the image and the unit).
- Matter: UDP on `--matter-listen` (the controller), `--bridge-listen`
  (the bridge node, 5540) and each registry node's port (the next free ones after it, `nodes.json`), IPv6 link-local and site addresses; DNS-SD on UDP
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
stack); a record of a registry node carries `node=<key>`. The same
records go through go-fabric's log ring (`logring`, the newest 1000 in
memory), which the pages' Operation page shows and streams. One level gates
both: `--log-level` at start, then `set_loglevel` over `/ws` or the
Operation page, each seen by the other's `get_loglevel`/level read; no log
file is written (`file_loglevel` is `null`).

## The pages

`--web host:port` serves go-fabric-ui's pages (`fabricui.Handler`, contract
0.7.1), both families and the Operation page in one handler:

- **The node pages** for every device node — the `--bridge-*` one and the
  registry's — under a node selector that lists the default node first
  (`GET /api/v1/nodes`, `/api/v1/nodes/<key>/…`): status with the node's
  read-only configuration block (`source` `configuration` for the
  `--bridge-*` node, `registry` for the others; never the passcode),
  pairing, fabrics, the commissioning window, diagnostics; an admin
  creates, renames and deletes the registry's nodes there
  ([Several device nodes](#several-device-nodes)) and erases any node in
  place ([Erasing a node](#erasing-a-node)). Without `--bridge` the
  node family is shown as disabled. The client decides what is exposed over
  `/bridge`, so the pages have no exposure selection; the selection page
  says so ("the server keeps no host item model; selection is the
  client's").
- **The Operation page**: the log level (the one `/ws` `set_loglevel`
  moves; changing it is an admin route), the log ring and its stream, the
  version, the go-fabric version and the uptime.
- **The controller pages** for the controller half (`--controller`, on by
  default): the fabric, the commissioned nodes (attributes, commands, ACL,
  bindings, ICD, fabrics, the commissioning window, a ping — one Matter
  read through go-fabric-ui's adapter; `/ws` `ping_node` probes each
  address instead),
  commissioning by code
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
- **The log says whether a pairing used BLE** (with a proxy or an adapter
  alike). `commission_with_code` logs `fabricserver.commission.ble` when
  it hands BLE to go-fabric — `network=wifi|thread|wifi+thread` and
  `wifi_id`/`thread_id` as the request named them or `default`; never an
  SSID, a password or a dataset — and `fabricserver.commission.ble_skipped`
  with `reason` when the daemon has a central and BLE was left out:
  `network_only` (the request's flag), `code_without_ble` (a QR code
  without the BLE capability) or `no_network` (no credential named and no
  `default` entry stored; the line's `hint` says how to store one). Without
  a central there is no line: `server_info.bluetooth_enabled` is false.
  `/ws` needs no selection: without `wifi_credentials_id` or
  `thread_dataset_id` it falls back to the `default` entry, the one an
  id-less `set_wifi_credentials` or `set_thread_dataset` stores. The pages
  (go-fabric-ui 0.7.1, #49) report the same decision with the same four
  reason names (`no_central`, `network_only`, `code_without_ble`,
  `no_network`): the Commission page preselects the host's default or only
  credential, hints while none is picked on a host with a central, and the
  commissioning's progress says whether BLE was used and why not (`ble` on
  `commissioning_progress`, `fabricui.pair.ble_skipped` in the log).

```sh
fabric-server --bluetooth-proxy proxy-office.local,<base64 key> --bluetooth-proxy 10.0.0.42:6053,<key>
```

## Home Assistant

[home-assistant.md](home-assistant.md) says how the Matter integration is
pointed at the server; [addon.md](addon.md) is the add-on.
