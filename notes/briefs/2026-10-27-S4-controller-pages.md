# Brief S4 — go-fabric-server binds the controller family of go-fabric-ui (`v0.4.0`)

Date 2026-10-27. Concept: go-fabric-ui `docs/concept-u2-controller-family.md`
§D (S4), go-fabric concept v9 ("go-fabric-server bindet beide Familien").
Owner decisions 2026-10-26/27: the add-on Bluetooth option (`host_dbus`,
adapter → `--bluetooth-adapter`) waits for the owner's manual BLE
commissioning test on HA OS and is **not** in this wave. **Model:** Opus.
Repository `/home/markus/Dokumente/GitHub/go-fabric-server` (main = v0.3.2),
branch `s4/controller-pages`. One pull request, one short report
`docs/reports/S4.md`. Conventional commits, English, no attribution lines,
no auto-merge, no label; go-fabric's token rules. Copy this brief to
`notes/briefs/` in the first commit.

## 0 Precondition

go-fabric-ui `v0.2.0` (pull request #12: `fabricui.ControllerSource`,
`fabricui/gofabric.ControllerOf(ctrl, opts)`, `Options.Controller`, the
controller pages, contract 0.2.0) is on auto-merge and a waiter tags it.
`git -C /home/markus/Dokumente/GitHub/go-fabric-ui fetch --tags origin &&
git -C /home/markus/Dokumente/GitHub/go-fabric-ui tag -l v0.2.0`; poll once
a minute up to 60 minutes. Read go-fabric-ui's `docs/embedding.md` (the
controller option and the credential store), `fabricui/controller.go`
(the port), `fabricui/gofabric/gofabric.go` (the adapter's options) and
`docs/reports/U2.md` at that tag.

## 1 The binding

- Pin go-fabric-ui `v0.2.0` (and go-fabric stays `v0.13.3` unless the ui
  module requires more).
- `internal/daemon/web.go`: `Options.Controller = gofabric.ControllerOf(ctrl, gofabric.Options{...})`
  when the controller half runs (`Config.Controller`, `run.go` `ctrl`);
  the daemon's existing settings store (Wi-Fi/Thread credentials of
  `cmd_server.go`: `set_wifi_credentials`, `set_thread_dataset`,
  `get_all_credentials`) implements `fabricui.CredentialStore` through a
  small adapter so the UI and the WS protocol see one list; the BLE
  capability from `--bluetooth-adapter`.
- `Events`: the node family's events stay as they are; the controller
  stream comes from the adapter's `Observe` (go-fabric `client.Hooks`) —
  check that `controllerapi`'s `HooksFor` and the adapter's hooks compose
  (both want the per-node hooks; wrap, do not replace; say how).
- The web handler serves both families; Ingress (`addon/`) shows both
  tabs; `--web-admin` semantics unchanged (admin routes of the controller
  family follow the same gate).
- `docs/operations.md` (the controller pages, the credential list shared
  with the protocol), `docs/home-assistant.md` (Ingress shows the
  controller pages), `docs/protocol.md` untouched except a sentence that
  the UI's `forget`/credentials are the same operations.
- Tests: an in-process test that mounts the daemon's web handler with a
  controller on `fabrictest` (go-fabric-ui's `gofabric` e2e is the
  pattern) and reads `/api/v1/controller/status` and `/controller/nodes`;
  a test that a credential set over WS appears in the UI's list (names
  only) and one set over the UI appears in `get_all_credentials`.
- Version 0.4.0: `internal/version`, `changelog.md` `## [0.4.0] - <date>`,
  `addon/config.yaml`, `addon/CHANGELOG.md`; `make version-check`; the
  compat job stays green.

## 2 Guards

`make ci`, race, cover-check, fuzz-smoke, vuln, licenses, secrets,
version-check, the release dry run, docker and addon-image, compat-python
locally; `df -h /` first; `go clean -cache -testcache` only when disk is
tight. Another process may switch the main checkout's branch: continue in
a sibling worktree (`../go-fabric-server-wt-s4`) if so and remove it after.

## 3 Hand-back

Once: PR number, HEAD, guards with results, differences from the brief,
Questions verbatim. Out of scope: the add-on Bluetooth option (after the
owner's BLE test), the Thread border-router commands (go-fabric milestone 6
wave 26a), removing `Item.Fields`' comma form (go-fabric-ui v0.2.1).
