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

The database and settings are under `/data`; a backup stops the add-on
first. The add-on's web UI (Ingress) shows the controller's pages — the
commissioned nodes, commissioning, the Wi-Fi and Thread credentials, the
fabric label — and the bridge node's pages when **bridge** is on; it
answers the Ingress proxy only.

Devices that need BLE commissioning are not supported yet; commission them
over the network or share them from another ecosystem.
