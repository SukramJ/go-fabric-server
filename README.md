# go-fabric-server

[![ci](https://github.com/SukramJ/go-fabric-server/actions/workflows/ci.yml/badge.svg)](https://github.com/SukramJ/go-fabric-server/actions/workflows/ci.yml)
[![codeql](https://github.com/SukramJ/go-fabric-server/actions/workflows/codeql.yml/badge.svg)](https://github.com/SukramJ/go-fabric-server/actions/workflows/codeql.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

A stand-alone daemon on [go-fabric](https://github.com/SukramJ/go-fabric) with
a WebSocket/JSON API in both Matter roles: a **controller server**
wire-compatible with the API Home Assistant's Matter integration speaks (the
python-matter-server / matterjs-server contract, schema 14), and — from
bundle S2 — a **bridge door** for hosts that are not written in Go.

> **Status: S1.** The controller half (`/ws`) answers the core command set;
> commands whose route go-fabric's controller facade does not offer yet answer
> `InvalidCommand` naming what they need. See [`docs/protocol.md`](docs/protocol.md).

## Quickstart

```sh
go install github.com/SukramJ/go-fabric-server/cmd/fabric-server@latest
fabric-server --listen 127.0.0.1:5580 --storage-path ./data
```

Point Home Assistant's Matter integration at `ws://localhost:5580/ws`. A
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
- [`docs/reports/S1.md`](docs/reports/S1.md) — what bundle S1 delivered and what it left to go-fabric

## Development

```sh
make build        # go build ./... and bin/fabric-server
make test         # go test -count=1 ./...
make race         # go test -race (CGO_ENABLED=1 for this run only)
make lint         # gofumpt + golangci-lint + go mod tidy -diff
make cover-check  # the per-package coverage floors
make fuzz-smoke   # the fuzz targets of the decoders, iteration-budgeted
make generate     # regenerate protocol/schema from go-fabric's snapshot
make release      # the release matrix into dist/ (dry run)
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
