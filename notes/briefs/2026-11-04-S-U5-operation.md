# Brief S-U5 — several device nodes per daemon, logring, the operation and ping ports (go-fabric-ui 0.5.0)

Date 2026-11-04. Issue #34 (M9 U5, owner decisions F.1–F.4). Worktree
`../go-fabric-server-su5`, branch `s/u5-operation`, on main e8bcd86. One
pull request. **Model:** Opus. Pins: go-fabric `c2dc83da` (main, `logring`
merged from #397); go-fabric-ui `2f4a922` (branch `u5/operation`, PR
go-fabric-ui#42, provisional: re-pinned to its squash-merge commit before
this pull request merges).

## 1 Bumps and migrations

- `go get` both pins, `go mod tidy`. go-fabric migrations the build demands:
  `Registry.ForDevice(ctx, dev, "")` (`cmd_commission.go`),
  `SubscribeRequest.Updated` returns `error` (`cmd_subscribe.go` `onReport`).
  `*node.Node` satisfies the extended `fabricui.NodeSource` as is.

## 2 Several device nodes (F.1)

- Decision: the primary node of the `--bridge-*` flags keeps key `default`
  (`fabricui.DefaultNodeKey`), store `bridge.db`, serial
  `FABRIC-SERVER-BRIDGE` — a daemon configured as today runs unchanged.
- `--bridge-node key,listen=ADDR[,discriminator=N][,passcode=N][,vendor-id=N][,product-id=N][,label=TEXT]`,
  repeatable (`;` separates entries in `FABRIC_SERVER_BRIDGE_NODE`); a
  field left out takes the `--bridge-*` flag's value; `listen` required;
  listen and discriminator unique across nodes; store `bridge-<key>.db`,
  serial `FSB-<key>`; key `^[A-Za-z0-9][A-Za-z0-9._-]{0,27}$`, not
  `default`, not a node-family page name. Needs `--bridge`.
- `--node-default KEY` picks the default node (first in the selector,
  `/bridge`, `/node/*`); default `default`.
- One `startBridge` per entry (own `bridgeapi.Server`, own `node.Node`, own
  mDNS advertiser); `/bridge` = the default node, `/bridge/{key}` = each
  node; `server_info` gains `node` (the key); `client.Options.Node`.
- Key = the config name (open question answered): known before start,
  survives a factory reset, names the store file. `docs/by_design.md` B23;
  B11 per node.
- Add-on: not exposed (more than one line: options schema, run.sh,
  translations).

## 3 logring (one level source)

- `slog.New(logring.New(textHandler{Level: lvl}, logring.Options{Level: lvl}))`;
  `/ws` `set_loglevel`/`get_loglevel` on `lvl` (unchanged), the pages'
  `Operations` on the ring = the same `LevelVar`. `file_loglevel` stays `null`.

## 4 Ports for the UI

- `operations` adapter (`internal/daemon/operation.go`) over
  `*logring.Handler`: level, records → `fabricui.LogRecord`, observe, info
  (`version.Version`, `version.GoFabric()`, start time).
- Ping: go-fabric-ui's gofabric adapter; nothing passed; `/ws` `ping_node`
  unchanged.
- `ExposureReason` = "the server keeps no host item model; selection is the client's".

## 5 Docs

`changelog.md`, `docs/protocol.md` (`/bridge/{key}`, `server_info.node`,
`file_loglevel`), `docs/operations.md` (flags, pages, files),
`docs/by_design.md`.

## Acceptance

`bridgeapi/multinode_test.go`: two fabrictest devices, two bridge halves
under `daemon.MuxNodes`, each fed through its own `/bridge/{key}`,
commissioned independently by the controller half's
`commission_with_code`. Unit tests for the flag parsing, the operations
adapter (level both ways with `get_loglevel`), the web handler's node list.

## Guards

`make ci`, `make race`, `make cover-check`, `make generate-check`,
`make compat-python`, `make fuzz-smoke`; summary
`$SCRATCH/su5/summary.log`.
