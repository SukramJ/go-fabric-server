<!-- Written 2026-11-13 by the owner's session. Model: Fable (brief), Opus (implementation). -->
# Brief S13 — device-side BLE and Wi-Fi for the daemon's first node; proven-address retention

Repository go-fabric-server (a Go daemon running Matter device nodes and a
controller on `github.com/SukramJ/go-fabric`, at v0.22.2). Wave **S13** (first named S12; renamed, since S12 is the 2026-11-10 bump brief):
device-side BLE commissioning for the daemon's first node, optional Wi-Fi
provisioning, and the controller's proven-address retention as an option.
Read the server's `CLAUDE.md` first, then only what this brief names. Work
on `origin/main` (go-fabric v0.22.2, go-fabric-ui v0.9.2, version 0.9.4
untagged).

## What go-fabric offers (the API)
- `docs/guides/first-boot-without-network.md`, `docs/guides/operations.md`
  §"BLE commissioning" and §"Wi-Fi provisioning".
- `examples/ble-wifi-light/main.go`: `bluez.NewPeripheral(adapter)` →
  `node.Config.Network.BLE` (close it if it is an `io.Closer`),
  `wpasupplicant.New(ctx, wpasupplicant.Options{Interface: …})` →
  `node.Config.Network.WiFi = &node.WiFi{Driver: d}` (with `Close`), and its
  `--no-wifi` variant.
- A node is on the radio only while it has no fabric; with `Network.BLE`
  set, the node's QR code names BLE discovery (`node/credentials.go:85-106`).
  Security statement: `docs/security.md` (`Bluetooth`) and ADR 0031 — anyone
  in radio range can attempt PASE on a node without a fabric; the Wi-Fi
  credential arrives inside PASE, goes to wpa_supplicant and is never logged.
- `client/peer.Timing.ProvenAddressRetention` (`client/peer/timing.go:49-60`,
  default 10 min, negative retains nothing). If go-fabric's controller facade
  has no way to set peer timing, stop that item and report what is missing.

## Where the server wires things
`internal/daemon/run.go`: `Config`, `ParseFlags` (every flag also as env
`FABRIC_SERVER_<FLAG>`), `--bluetooth-adapter` (the controller's central),
`--bluetooth-proxy`, `--controller-diagnostics-capacity`; `bridgeConfig`
builds every node's `node.Config`; the default node is `primaryNode(c)`
(`internal/daemon/nodes.go`, key "default"), registry nodes come from
`registrySpec`; `bleCentral` / `adapterCentral` normalise the controller's
adapter. Arch guard `internal/arch/arch_test.go` (`facade`). Add-on:
`addon/config.yaml`, `script/run.sh`.

## Decisions (owner, 2026-11-13)
1. **Standalone daemon only.** New flags; the HA add-on gets no option, no
   `host_dbus`, `run.sh` unchanged. Docs say so and why.
2. **Two separate flags.** `--bridge-bluetooth-adapter <adapter>` (the first
   node's BLE peripheral; a number or an adapter name; empty = off) and
   `--bridge-wifi-interface <ifname>` (the first node's Wi-Fi driver through
   wpa_supplicant; empty = off). Each works alone. Help texts state the host
   requirement.
3. **The configured first node only** (`primaryNode`, only with `--bridge`;
   either flag without `--bridge` is a start-up error naming the flag).
   Registry nodes are built without BLE and Wi-Fi; no registry field, no
   protocol or UI-contract change. `bridgeConfig` must not hand the radio to
   a registry node — a test.
4. **Same adapter for both roles is refused at start** (`0` vs `hci0`), the
   error naming both flags. Documented as "until proven"; no hidden override.
5. Lifecycle: peripheral and driver opened before the first node is built,
   closed when the daemon stops; an erase hands the SAME peripheral and
   driver to the replacement node (or fresh ones per run if a peripheral
   cannot be reused — say which). A failure to open either at start is a
   start-up error naming the flag.
6. `--controller-proven-address-retention <duration>`: 0 = go-fabric's
   default (10 min), negative = retain nothing; add-on: no option.
7. Arch guard: `wifi/wpasupplicant` in `facade`; `go.mod` gains
   `github.com/SukramJ/go-fabric/wifi/wpasupplicant v0.22.1`;
   THIRD-PARTY-NOTICES if the dependency set changes.
8. `server_info` / protocol: no new field. One Info line at start per
   enabled port, never a credential.

## Tests (no hardware)
Seams for the peripheral and the Wi-Fi driver; flag parsing and env; each
start-up error; the default node gets `Network.BLE`/`Network.WiFi`, a
registry node does not; the QR code names BLE with the flag and not
otherwise; after an erase the replacement node has the radio again; the
retention flag reaches the controller's timing. A commissioning over the
in-memory radio end to end if the rig makes it cheap; otherwise say why not.

## Documents
`docs/operations.md` (flag table + "A node without a network: Bluetooth and
Wi-Fi on the daemon's host"), `docs/addon.md`, `docs/home-assistant.md`,
`docs/by_design.md` (first node only; standalone only; same adapter
refused), README, `changelog.md`, CLAUDE.md, CONCEPT.md; the report with
"better than others / better than before / the right cut".

## Rules
Worktree `../go-fabric-server-s12`, branch `s12/node-ble-wifi`. Guards under
the shared lock with a summary log. No `time.Sleep` in production code;
never log a credential. Named paths only; no attribution lines; push, no
pull request, no tags. A go-fabric gap is reported as an issue draft.
