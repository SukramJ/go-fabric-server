# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

go-fabric-server: a daemon on [go-fabric](https://github.com/SukramJ/go-fabric)
with a WebSocket/JSON API — the controller half at `/ws`, wire-compatible with
matterjs-server (schema 14), and from S2 the bridge half at `/bridge`.
`CONCEPT.md` is the design (go-fabric's concept v8); `docs/protocol.md` the
wire; `docs/reports/` what each bundle delivered.

## Commands

```sh
make ci           # build vet test lint — what the pipeline's main jobs run
make race         # go test -race (CGO_ENABLED=1 for this run only)
make cover-check  # per-package floors in script/coverfloor/floors.txt (a ratchet)
make fuzz-smoke   # the fuzz targets, iteration-budgeted
make generate     # protocol/schema from go-fabric's parity snapshot
make vuln licenses secrets   # the security job
```

## Rules

- **go-fabric through its facades only.** `internal/arch` fails the build on
  an import below them; its allowlist names each exception with the report
  question it waits on. A facade gap is reported (go-fabric symbol, why), not
  worked around here.
- **No Matter knowledge of our own.** Ids, names, types come from
  `protocol/schema`, generated from go-fabric's `parity` snapshot by
  `internal/gen`; never hand-edit `tables_gen.go` or `schema.json`, and
  never write a table of ids.
- **matterjs-server is the wire contract** of `/ws` (the read map in
  `notes/reads/`); cite the matterjs-server file and line a shape comes from.
  A deliberate difference goes into `docs/protocol.md`.
- **Naming.** "Matter" is a trademark of the CSA and not part of this
  project's name, packages, binaries or images.
- **Official documents in English**; conventional commits; one pull request
  per bundle; no attribution lines.
- A user-visible change gets a `changelog.md` entry; a release bumps
  `internal/version.Version` and adds its changelog section in one commit.

Go 1.27.3, `CGO_ENABLED=0`. Dependencies: go-fabric (and its ble/bluez), coder/websocket,
go-fabric-ui — every further one is a decision recorded in
`THIRD-PARTY-NOTICES.md`.
