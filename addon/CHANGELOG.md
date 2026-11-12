# Changelog

The add-on follows go-fabric-server's versions; the full list of changes is
the repository's [`changelog.md`](https://github.com/SukramJ/go-fabric-server/blob/main/changelog.md).

## 0.9.2

- Built with Go 1.27.4 (a security fix in its HTTP server).
- Library update (go-fabric 0.22.1): a Thread device behind a border
  router is no longer locked out of reconnecting when the border router
  answers for it with its own host name; a pairing without its own deadline is
  no longer cut off after one minute once the device was found, a pairing
  that is cancelled resets the device's fail-safe instead of leaving it to
  expire, and the mDNS responder joins network interfaces that come up after
  the add-on started.

## 0.9.1

- Library update (go-fabric BlueZ central 0.21.0): commissioning over a
  local Bluetooth adapter uses the larger packet size BlueZ negotiates
  (BlueZ 5.62 and later) instead of the minimum.

## 0.9.0

- Library updates (go-fabric 0.21.0, go-fabric-ui 0.9.0): the controller
  pages gain a Diagnostics tab (sessions per node, mDNS findings, a trace of
  pairing, session, discovery, reachability, ICD, OTA and camera events);
  further reconnect fixes for nodes several responders answer for.

## 0.8.1

- Library updates (go-fabric 0.20.0, go-fabric-ui 0.8.1): the controller
  reconnects to Thread nodes behind a border router without a 45 s detour;
  the node page shows the node's operational host.
- Erasing a device node no longer takes about 14 s.

## 0.8.0

- Library updates (go-fabric 0.19.1, go-fabric-ui 0.8.0): the network
  topology and the Network page show the Thread border routers on the LAN.
- A device node that is erased, or that leaves the server's fabric, leaves
  the controller's node list at once.

## 0.7.0

- Cameras: Home Assistant's Matter integration can stream from and take
  snapshots of Matter cameras through this server (the `camera_*` commands
  and WebRTC signalling of schema 14, on go-fabric's controller side).

## 0.6.2

- Erase in the web UI: a device node leaves its controllers, erases its
  store and comes back at once with its commissioning window open — for the
  default node and every registry node (go-fabric-ui 0.7.1).
- A pairing over `/ws` says in the log why it uses or skips Bluetooth LE;
  the Commission page preselects the stored default credential.

## 0.6.1

- Library updates (go-fabric 0.18.0, go-fabric-ui 0.6.1). Nothing to do for
  an existing installation: the server never ran an OTA provider, so
  go-fabric's migration of the controller's former second node leaves its
  database untouched.

## 0.6.0

- Bluetooth proxies work: a single `bluetooth_proxies` entry was never
  passed to the server since 0.4.1, and with several the last one was lost;
  the same held for `surface_extensions` and `surface_profiles`. The log now
  says how many proxies the add-on uses.
- Device nodes are created, renamed and deleted in the Fabric Controller
  panel (go-fabric-ui 0.6.0). The add-on never set `--bridge-node`, so
  nothing changes for an existing installation.

## 0.5.0

- Several device nodes per daemon (`--bridge-node`), the Operation page with
  the log stream and level, the ping measured per address, and the node
  selector of the Fabric Controller panel (go-fabric-ui 0.5.0, go-fabric
  v0.17.0).

## 0.4.9

- The add-on's entry in the Home Assistant sidebar is titled "Fabric" instead
  of the full add-on name, as the other go-* add-ons do.

## 0.4.8

- Library updates (go-fabric 0.15.6, go-fabric-ui 0.2.9). The Surface tab
  keeps working when a newer go-fabric adds a field to the node's surface
  report; a bridge host's refusal of a command answers its own status
  instead of a plain failure; colour values are the same on aarch64 as on
  amd64.

## 0.4.7

- Pairing a device with a test Certification Declaration, such as a
  go-ha2fabric bridge, works again: the attestation policy is now
  matterjs-server's, which refuses only an untrusted test certificate
  (naming the `enable_test_net_dcl` option) or an error-level finding.
- The server pings every client every 30 seconds, so proxies and remote
  access relays no longer close idle connections.
- Setting the fabric label to the value it already has, as Home Assistant
  does on every connect, no longer sends it to every node again.
- An icon and a logo, and option labels in English and German on the
  configuration page.
- The matterjs-server pin of the API contract moves to 1.5.6-alpha
  (`0bd06f22`); the API is unchanged.

## 0.4.6

- Library updates (go-fabric 0.15.4, go-fabric-ui 0.2.8). The Surface tab
  learns of every surface change the node makes, including reassemblies the
  host runs on its own, and shows the root endpoint.
- A device the configured specification level does not know is withheld
  instead of refusing the whole declaration; the `declare` answer lists it.

## 0.4.5

- Library updates (go-fabric 0.15.3, go-fabric-ui 0.2.7). The node's surface
  report has its own page (Surface tab): effective surface per endpoint,
  decisions, withheld elements and announced changes.
- Options `surface_extensions`, `surface_level` and `surface_profiles` for
  the bridge node's surface policies.

## 0.4.4

- Library updates (go-fabric 0.15.2, go-fabric-ui 0.2.6). The status page shows
  the node's surface policies: specification level, controller profiles and
  enabled extensions.

## 0.4.3

- Library updates (go-fabric 0.15.1, go-fabric-ui 0.2.5). The selection
  page hides items that cannot be mapped to an endpoint; a toggle above
  the list shows them again. A bridged device whose served clusters or
  features change across a restart is announced to paired controllers
  through its ConfigurationVersion.

## 0.4.2

- Library updates (go-fabric 0.14.0, go-fabric-ui 0.2.4); the pages follow
  Home Assistant's theme from the first frame (go-fabric-ui 0.2.2).

## 0.4.1

- Option `bluetooth_proxies`: commission BLE devices through ESPHome
  Bluetooth proxies, without a Bluetooth adapter on the host. Each proxy
  needs `bluetooth_proxy: active: true`; Home Assistant's Bluetooth
  integration shares it, so a dedicated proxy pairs most reliably.

## 0.4.0

- The web UI shows the controller's pages: the commissioned nodes,
  commissioning, the Wi-Fi and Thread credentials (the same list Home
  Assistant's integration fills) and the fabric label.

## 0.3.2

- The `/bridge` half gains `forget`, which frees a device's endpoint
  number; `remove` keeps it.

## 0.3.1

- On go-fabric v0.13.3 and go-fabric-ui v0.1.2: the node pages show the
  commissioning window's deadline and every endpoint's clusters.

## 0.3.0

- The first add-on: the controller half on `ws://localhost:<port>/ws`, the
  optional bridge half, the bridge node's pages through Ingress.
