# go-fabric-server

[![ci](https://github.com/SukramJ/go-fabric-server/actions/workflows/ci.yml/badge.svg)](https://github.com/SukramJ/go-fabric-server/actions/workflows/ci.yml)
[![codeql](https://github.com/SukramJ/go-fabric-server/actions/workflows/codeql.yml/badge.svg)](https://github.com/SukramJ/go-fabric-server/actions/workflows/codeql.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

A stand-alone daemon on [go-fabric](https://github.com/SukramJ/go-fabric) with
a WebSocket/JSON API in both Matter roles: a **controller server**
wire-compatible with the API Home Assistant's Matter integration speaks (the
python-matter-server / matterjs-server contract, schema 14), and a
**bridge door** (`/bridge`) for hosts that are not written in Go.

> **Status: S3 (v0.3.1).** The controller half (`/ws`) answers every
> command of matterjs-server's dispatch Home Assistant's Matter integration
> uses, the ICD commands, the network topology and test nodes; the OTA
> commands, the two Thread border router commands and camera/WebRTC answer
> `InvalidCommand` naming what they wait for. The bridge half (`/bridge`,
> `--bridge`) lets a host declare devices by name;
> [`examples/bridge-python/`](examples/bridge-python/) is a host in 71 lines
> of Python, and `--web` serves the bridge node's pages. A Home Assistant
> add-on is in [`addon/`](addon/). See [`docs/protocol.md`](docs/protocol.md).

## Commands of `/ws`

| Group | Commands | |
| --- | --- | --- |
| Server | `server_info`, `start_listening`, `diagnostics`, `get_loglevel`, `set_loglevel` | implemented |
| Nodes | `get_nodes`, `get_node`, `remove_node`, `interview_node`, `ping_node`, `get_node_ip_addresses`, `import_test_node` | implemented |
| Commissioning | `commission_with_code`, `commission_on_network`, `parse_onboarding_payload`, `discover`, `discover_commissionable_nodes`, `open_commissioning_window` | implemented (BLE with `--bluetooth-adapter`, Linux/BlueZ) |
| Interaction | `read_attribute`, `read`, `write_attribute`, `device_command`, `batched_device_commands`, `subscribe`, `unsubscribe`, `enable_auto_subscription`, `disable_auto_subscription` | implemented |
| Credentials, fabric | `set_/remove_wifi_credentials`, `set_/remove_thread_dataset`, `get_all_credentials`, `set_default_fabric_label`, `get_fabric_label`, `get_matter_fabrics`, `remove_matter_fabric`, `set_acl_entry`, `set_node_binding`, `get_vendor_names` | implemented |
| ICD, topology | `get_icd_state`, `register_icd`, `resync_icd`, `unregister_icd`, `get_network_topology` | implemented |
| OTA | `check_node_update`, `update_node`, `initiate_ota_upload` | waits for go-fabric (milestone 5 phase 25) |
| Thread | `get_thread_border_routers`, `get_thread_diagnostics` | waits for go-fabric (milestone 6) |
| Camera | `camera_*`, `send_webrtc_provider_command` | never |

## Quickstart

```sh
go install github.com/SukramJ/go-fabric-server/cmd/fabric-server@latest
fabric-server --listen 127.0.0.1:5580 --storage-path ./data
```

Point Home Assistant's Matter integration at `ws://localhost:5580/ws`
(answer "No" to the official add-on, [`docs/home-assistant.md`](docs/home-assistant.md)). A
container: `docker run -d --network host -v fabric-server:/data
ghcr.io/sukramj/go-fabric-server`. Flags, files, ports and the token:
[`docs/operations.md`](docs/operations.md).

## Why "Matter" is not in the name

"Matter" and the Matter logo are trademarks of the Connectivity Standards
Alliance; their use in a product, project or repository name is reserved to
Alliance members and certified products. This project is neither, so the mark
is not part of its name; the word appears only descriptively, never to
suggest certification or endorsement. Nothing built with this project is
certified by the Alliance. See go-fabric's README, section "Not certified".

## Documentation

- [`CONCEPT.md`](CONCEPT.md) — the design (go-fabric's concept v8, parts B–E)
- [`docs/protocol.md`](docs/protocol.md) — the envelope, the compatibility statement, the commands, the extensions
- [`docs/operations.md`](docs/operations.md) — flags, files, ports, token, container, systemd
- [`docs/home-assistant.md`](docs/home-assistant.md) — pointing Home Assistant's Matter integration at the server
- [`docs/addon.md`](docs/addon.md) — the Home Assistant add-on
- [`docs/compat.md`](docs/compat.md) — the python-client compatibility job
- [`docs/by_design.md`](docs/by_design.md) — the deliberate differences, each with its reason
- [`docs/reports/S1.md`](docs/reports/S1.md), [`docs/reports/S2.md`](docs/reports/S2.md), [`docs/reports/S3.md`](docs/reports/S3.md) — what each bundle delivered and what it left to go-fabric

## Development

```sh
make build        # go build ./... and bin/fabric-server
make test         # go test -count=1 ./...
make race         # go test -race (CGO_ENABLED=1 for this run only)
make lint         # gofumpt + golangci-lint + go mod tidy -diff
make cover-check  # the per-package coverage floors
make fuzz-smoke   # the fuzz targets of the decoders, iteration-budgeted
make generate     # regenerate protocol/schema and bridgeapi/clusters_gen.go from go-fabric
make release      # the release matrix into dist/ (dry run; runs version-check)
make compat-python # the python client against the daemon (docs/compat.md)
make addon-image  # the add-on image for amd64
make ci           # build vet test lint
```

The tests run the server in process against go-fabric's `fabrictest` site:
a real controller stack commissions a simulated light over an in-memory
network, and the Go client of this repository drives the API.

## Acknowledgements

Built on [go-fabric](https://github.com/SukramJ/go-fabric), a semantic port
of [matter.js](https://github.com/matter-js/matter.js) (Apache-2.0). The
controller API follows [matterjs-server](https://github.com/matter-js/matterjs-server)
(Apache-2.0), whose documented examples are this repository's compatibility
fixtures.

## License

Apache-2.0, see [LICENSE](LICENSE) and [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).
Contributions are accepted under the [contributor licence agreement](CLA.md);
the use of AI tools is governed by [`AI_POLICY.md`](AI_POLICY.md).
