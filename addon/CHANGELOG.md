# Changelog

The add-on follows go-fabric-server's versions; the full list of changes is
the repository's [`changelog.md`](https://github.com/SukramJ/go-fabric-server/blob/main/changelog.md).

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
