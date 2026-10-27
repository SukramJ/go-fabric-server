# go-fabric-server

A Matter controller server for Home Assistant's Matter integration. Only one
of this add-on and the official Matter Server add-on can run: both use port
5580 and the host's Matter networking.

## Connecting the Matter integration

Home Assistant does not select this add-on on its own — its Matter setup
acts on the official add-on only — so this add-on sends no discovery:

1. **Settings → Devices & services → Add integration → Matter**.
2. When asked whether to use the official Matter Server add-on, answer
   **No**.
3. Enter `ws://localhost:5580/ws` (or the port you set).

## Options

- **port** (5580): the API port, on loopback only.
- **log_level** (info): critical, error, warning, notice, info or debug.
- **enable_test_net_dcl** (false): also trust the CSA test roots, for
  devices with test certificates.
- **bridge** (false): also run the bridge half for hosts that declare
  devices over `/bridge`, with its own Matter node.
- **bridge_port** (5540): that node's Matter port.
- **bluetooth_proxies** (empty): ESPHome Bluetooth proxies to commission
  BLE devices through, one entry each: `host[:port][,key]`, for example
  `proxy-office.local,<base64 API encryption key>` (port 6053 by default).

The database and settings are under `/data`; a backup stops the add-on
first. The add-on's web UI (Ingress) shows the controller's pages — the
commissioned nodes, commissioning, the Wi-Fi and Thread credentials, the
fabric label — and the bridge node's pages when **bridge** is on; it
answers the Ingress proxy only.

## Bluetooth through ESPHome proxies

Devices that need BLE commissioning are paired through ESPHome Bluetooth
proxies listed in **bluetooth_proxies**; the host needs no Bluetooth
adapter. Each proxy needs `bluetooth_proxy: active: true` and, with an
encrypted API, its key after the comma. Store a Wi-Fi or Thread credential
first: the device is put on that network over BLE. A proxy serves its
Bluetooth to one API client at a time, usually Home Assistant's Bluetooth
integration: while a pairing runs the add-on takes the proxy over and Home
Assistant takes it back. Dedicate a proxy to commissioning, or accept that
it flaps during a pairing; a pairing that loses the proxy fails with "the
Bluetooth proxy is in use by another API client". A wrong key or a proxy
without `active: true` stops the add-on at start, naming the proxy (see the
log).

Without proxies, commission devices over the network or share them from
another ecosystem.
