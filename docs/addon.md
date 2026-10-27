# The Home Assistant add-on

`addon/` is a Home Assistant add-on of go-fabric-server, in the form of
go-ha2fabric's: the repository is an add-on repository (`repository.yaml`),
`addon/config.yaml` the manifest, `script/run.sh` the entrypoint that maps
the options onto the daemon's environment, `.github/workflows/addon-image.yml`
the per-architecture image (`ghcr.io/sukramj/go-fabric-server-addon-{arch}`,
HA base image + bashio + the static binary).

## Installing

1. **Settings → Add-ons → Add-on Store → ⋮ → Repositories**, add
   `https://github.com/SukramJ/go-fabric-server`.
2. Install **go-fabric-server**, stop the official **Matter Server**
   add-on if it runs (both use port 5580), start this one.
3. Add the Matter integration with **No** to the official add-on and the
   URL `ws://localhost:5580/ws` ([home-assistant.md](home-assistant.md)).

## Options

| Option | Default | Meaning |
| --- | --- | --- |
| `port` | `5580` | the API port on loopback (`ws://localhost:<port>/ws`); 5580 is the official add-on's, so only one of the two runs |
| `log_level` | `info` | `critical`, `error`, `warning`, `notice`, `info`, `debug` |
| `enable_test_net_dcl` | `false` | trust the CSA test roots (devices with test certificates) |
| `bridge` | `false` | also run the bridge half (`/bridge`) and its bridge node |
| `bridge_port` | `5540` | the bridge node's Matter port |
| `bluetooth_proxies` | `[]` | ESPHome Bluetooth proxies to commission BLE devices through, each `host[:port][,key]` (the base64 API encryption key); passed as repeated `--bluetooth-proxy` ([operations.md](operations.md#bluetooth-through-esphome-proxies)). The add-on has no `host_dbus` and no `--bluetooth-adapter` |

The controller's database and the server's settings live under `/data`
(`fabric-server.db`, `server.json`, `bridge.db`); a cold backup stops the
add-on first.

## Network and Ingress

The add-on runs with `host_network: true`: Matter needs the host's IPv6 link
and mDNS. The API listens on `127.0.0.1` only. The pages
(go-fabric-ui: the controller's, and the bridge node's with **bridge** on)
are on the Ingress port 8099; with host networking that
listener is on the LAN too, so the daemon answers only the Ingress proxy
(`172.30.32.2`, `--web-admin-source`), which authenticates the user and is
the pages' administrator.

## No discovery

The manifest has no `discovery` key: Home Assistant's Matter config flow
acts on the official add-on's slug only (`config_flow.py:259-260` at
`2026.10.3`), so a discovery message of this add-on would be aborted.

## Versions

`addon/config.yaml`'s `version` is `internal/version.Version` and the image
tag; `make version-check` (also run by `make release`) fails when they or
the `changelog.md` section disagree.
