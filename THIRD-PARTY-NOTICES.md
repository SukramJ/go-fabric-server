# Third-Party Notices

go-fabric-server is licensed under the Apache License 2.0 (see
[`LICENSE`](LICENSE)). It depends on three Go modules directly (go-fabric, coder/websocket, go-fabric-ui); every
further dependency is a decision recorded here (MIT, Apache-2.0, BSD and
the equivalent ISC only). No third-party source is copied into this tree,
except the documented JSON examples of matterjs-server that serve as test
fixtures (below).

## Direct dependencies

### go-fabric — Apache-2.0

The Matter side: the controller facade, the device kit, the snapshot of the
matter.js model the protocol schema is generated from.

- Source: <https://github.com/SukramJ/go-fabric>
- Copyright (C) 2026 SukramJ. Apache License 2.0; its own notices
  (matter.js, filippo.io/nistec, …) travel in its `THIRD-PARTY-NOTICES.md`
  and `licenses/`.
- Decision: concept v8 B.1 — the one place of Matter knowledge.

### github.com/coder/websocket — ISC

The WebSocket transport: pure Go, context-aware, concurrent writes.

- Source: <https://github.com/coder/websocket>
- Copyright (c) 2025 Coder. ISC License, see
  [`licenses/LICENSE-coder-websocket.txt`](licenses/LICENSE-coder-websocket.txt).
- Decision: brief S1 §1 names it; the ISC licence is the permissive
  equivalent of MIT (report S1, Differences).

### github.com/SukramJ/go-fabric-ui — Apache-2.0

The node pages (`--web`, the add-on's Ingress): `fabricui.Handler` with
its built bundle embedded in the module (no Node.js toolchain here).

- Source: <https://github.com/SukramJ/go-fabric-ui>
- Copyright (C) 2026 SukramJ. Apache License 2.0; the bundle's own notices
  travel in its `THIRD-PARTY-NOTICES.md`.
- Decision: brief S3 §5 (concept v9, one UI for the family's daemons).

## Indirect dependencies

Through go-fabric: filippo.io/nistec (BSD-3-Clause), github.com/grandcat/zeroconf
(MIT), github.com/miekg/dns (BSD-3-Clause), golang.org/x/net and
golang.org/x/sys (BSD-3-Clause), github.com/cenkalti/backoff (MIT), and
modernc.org/sqlite (BSD-3-Clause; a direct dependency until S3, when the
daemon moved to `controller.OpenDB`, see
[`licenses/LICENSE-modernc-sqlite.txt`](licenses/LICENSE-modernc-sqlite.txt)) and
modernc.org's libc, mathutil and memory with their dependencies
(BSD-3-Clause, MIT). `make licenses` (go-licenses) checks the whole graph
in CI; modernc.org/mathutil's licence is BSD-3-Clause by its text, which
go-licenses' classifier does not recognise and only logs.

## Test fixtures

`protocol/testdata/compat/*.json` are the JSON examples of matterjs-server's
`docs/websockets_api.md` (<https://github.com/matter-js/matterjs-server>,
commit `a7922e85`), Copyright 2025-2026 Open Home Foundation, Apache License
2.0. They are the wire contract this server is compatible with; the README
there names their origin.
