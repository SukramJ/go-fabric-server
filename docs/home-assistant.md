# Home Assistant's Matter integration

go-fabric-server's `/ws` speaks the WebSocket API of matterjs-server at
schema 14, the contract the Matter integration of Home Assistant (core
`2026.10.3`, `homeassistant/components/matter/`) speaks through
`matter-python-client==1.4.0`. The integration is pointed at the server by
its URL; it never selects the server on its own.

## Pointing the integration at the server

1. Run the server on the Home Assistant host with its API on loopback:
   the add-on ([addon.md](addon.md)) does that, or
   `fabric-server --listen 127.0.0.1:5580`. Home Assistant sends no token,
   so a listener beyond loopback (which needs one) is not reachable for it.
2. In Home Assistant: **Settings → Devices & services → Add integration →
   Matter**.
3. On a Home Assistant OS or Supervised installation the flow asks whether
   to use the official Matter Server add-on: answer **No** (submit with
   the checkbox cleared).
4. Enter the URL `ws://localhost:5580/ws` — the flow's default
   (`config_flow.py:38`, `DEFAULT_URL`) — or `ws://<host>:<port>/ws` when
   the server runs elsewhere and is reachable without a token.

Only one of the official add-on and this server can run on a host: both
listen on port 5580, and the controller needs the host's Matter port and
mDNS.

## Why it is never selected automatically

The integration acts on a Supervisor discovery only from the official
add-on: `async_step_hassio` aborts with `not_matter_addon` unless the
discovery's slug is `core_matter_server` (`config_flow.py:259-260`,
`ADDON_SLUG` in `const.py:8`, at `2026.10.3`), and `async_step_on_supervisor`
installs and starts that add-on when the answer is yes. This server's
add-on therefore sends no discovery, and the URL step above is the way in.

## What the integration calls

After `connect` the integration calls `start_listening`, `get_nodes`,
`get_node`, `commission_with_code`, `commission_on_network`,
`set_wifi_credentials`, `set_thread_operational_dataset`
(`set_thread_dataset`), `open_commissioning_window`, `remove_node`,
`remove_matter_fabric`, `interview_node`, `ping_node`, `node_diagnostics`,
`get_network_topology`, `set_default_fabric_label` and `subscribe_events`.
`node_diagnostics` is computed in the client from `get_node_ip_addresses`,
`get_matter_fabrics`, `get_vendor_names` and the node data. Every one of
these is answered ([protocol.md](protocol.md#commands)); the CI job
`compat-python` runs them with the pinned client against the daemon and a
commissioned device ([compat.md](compat.md)).

Devices on BLE need `--bluetooth-adapter` (go-fabric's BlueZ central, a
daemon with system-bus rights to `org.bluez`) and a stored Wi-Fi or Thread
credential; without it devices are commissioned on the network (Wi-Fi or
Ethernet devices already joined, or a code shared from another ecosystem
through `open_commissioning_window`). The add-on does not pass the flag
yet.
