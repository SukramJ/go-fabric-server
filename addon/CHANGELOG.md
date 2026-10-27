# Changelog

The add-on follows go-fabric-server's versions; the full list of changes is
the repository's [`changelog.md`](https://github.com/SukramJ/go-fabric-server/blob/main/changelog.md).

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
