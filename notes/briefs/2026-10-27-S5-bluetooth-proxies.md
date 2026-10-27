# Brief S5 — go-fabric-server commissions over ESPHome Bluetooth proxies (`v0.4.1`)

Date 2026-10-27. Owner decision 2026-10-27 (brief BP1 in go-fabric,
released as `v0.13.5`): the daemon and the add-on commission Matter devices
over BLE through the owner's ESPHome Bluetooth proxies, so a host needs
neither a Bluetooth adapter nor `host_dbus`. This replaces the earlier
add-on plan of a USB adapter (`host_dbus`), which stays possible for the
plain daemon through `--bluetooth-adapter`. **Model:** Opus. Repository
`/home/markus/Dokumente/GitHub/go-fabric-server` (main = v0.4.0), branch
`s5/bluetooth-proxies`. One pull request, one short report
`docs/reports/S5.md`. Conventional commits, English, no attribution lines,
no auto-merge, no label; go-fabric's token rules. Copy this brief to
`notes/briefs/` in the first commit.

## 0 Precondition

go-fabric `v0.13.5` carries the nested module
`github.com/SukramJ/go-fabric/ble/esphome` (its own `go.mod`, tag
`ble/esphome/v0.13.5` may or may not exist — check `git -C
/home/markus/Dokumente/GitHub/go-fabric tag -l 'ble/esphome/*'`; if the
nested module has no tag, pin it by pseudo-version at the `v0.13.5` commit
and say so in the report; poll once a minute up to 30 minutes for the
`v0.13.5` tag before pinning). Read `ble/esphome/central.go` (`NewCentral`,
`Proxy`, `Options`, `Close`), `ble/esphome/proxy.go` (`ParseProxy`,
`ErrProxyBusy` and the other errors), `docs/guides/ble-proxies.md` and
`notes/briefs/reports/BP1.md` in go-fabric at that tag.

## 1 The daemon

- `--bluetooth-proxy host[:port][,key]`, repeatable (a `flag.Value` that
  appends; `ParseProxy` validates), next to `--bluetooth-adapter`; both
  given is a start-up error ("one BLE backend"). The central from
  `esphome.NewCentral(proxies, esphome.Options{Logger, Clock})` goes into
  `controllerapi.Options.BLE` exactly where the BlueZ central goes
  (`internal/daemon/run.go` `bleCentral` / `newCentral` / `controllerOptions`),
  and is closed on shutdown before the controller.
- `server_info.bluetooth_enabled` and the UI's `capabilities.ble` are true
  with proxies; a new `bluetooth_proxies` list (names only, no keys) in
  `server_info` is NOT added (the protocol is HA's); the daemon logs the
  proxies at start (`ble.esphome.proxy_taken` comes from the module).
- Errors a pairing hits through a proxy (`ErrProxyBusy`, no
  ACTIVE_CONNECTIONS, bad key) surface in `commission_with_code`'s error
  text and the pages' commissioning failure kind as they are.
- Tests: the flag parsing (one, several, bad spec, both backends), the
  central wired into the options (as `TestBluetoothAdapterWired` does for
  BlueZ, with `newCentral`-style stubbing so no socket is dialled), and an
  in-process pairing over go-fabric's `ble/esphome/esphometest` fake proxy
  on `fabrictest` if the daemon's test composition allows it (say if not).

## 2 The add-on

- `addon/config.yaml`: option `bluetooth_proxies` (list of strings, `[]`
  default; schema `list(str)?`), passed as repeated `--bluetooth-proxy`
  by `addon/run.sh` (or wherever the options become flags); no `host_dbus`,
  no `--bluetooth-adapter` in the add-on. `addon/DOCS.md`,
  `docs/home-assistant.md`, `docs/operations.md` (a section "Bluetooth
  through ESPHome proxies": `active: true`, the API key, one API subscriber
  per proxy — Home Assistant's Bluetooth integration usually holds it, so
  dedicate a proxy or accept that it flaps while a pairing runs; the
  owner's live test is the acceptance), README.
- Version 0.4.1: `internal/version`, `changelog.md`
  (`## [0.4.1] - 2026-10-27`), `addon/config.yaml`, `addon/CHANGELOG.md`;
  `make version-check`; the compat job stays green.

## 3 Guards and hand-back

`make ci`, race, cover-check, fuzz-smoke, vuln, licenses, secrets,
version-check, the release dry run (all targets), docker, addon-image,
compat-python locally. `df -h /` first. Hand back once: PR number, HEAD,
guards with results, differences from the brief, Questions verbatim.
Out of scope: the Thread border-router commands (go-fabric wave 26a).
