# Read S7: BLE through the ESPHome proxy in a pairing started from the web UI or `/ws`

Inventory, no judgement, no code changes. Every row is read from the file named; `file:line` are at the pins below.

## Pins

| Repo | Pin read | Evidence |
| --- | --- | --- |
| go-fabric | v0.18.0 plus one commit on `main` (`f1e9677e`, flake pass FP2, #451) | `git describe` = `v0.18.0-1-gf1e9677e`; `git diff --stat v0.18.0 HEAD` touches `controller/client/*`, `controller/commissioner/*_test.go` only; `controller/controller.go`, `controller/pair_race.go`, `client/discovery/session.go`, `ble/esphome/*` are unchanged |
| go-fabric-ui | branch `u7/completeness` at `0ecef14` (chore(release): 0.7.0). `main` is `1ae48af` = v0.6.1; the U7 merge (`7200da3`, #52) is NOT on the local `main` | `git branch --show-current` = `u7/completeness`; `git log -3 main` ends at `1ae48af` (v0.6.1); `git log main..HEAD` lists `7200da3` |
| go-fabric-server | `main` at `b947c93` = v0.6.1 | `git describe` = `v0.6.1` |
| go-fabric-ui as the server builds with it | v0.6.1, not U7 | go-fabric-server `go.mod:7` |
| go-fabric as the server builds with it | v0.18.0; `ble/esphome` v0.13.10 | go-fabric-server `go.mod:6`, `go.mod:9` |
| go-fabric-ui v0.6.1 vs `u7/completeness`, the BLE decision | same logic, shifted lines (`Capabilities` 157 -> 162, `networkOf` 224 -> 229, BLE condition 269-271 -> 274-277; Commission.svelte `network` 26 -> 30) | `git show v0.6.1:fabricui/gofabric/gofabric.go`, `git show v0.6.1:ui/src/lib/pages/controller/Commission.svelte` |

## Part 1: go-fabric

| Fact | Source |
| --- | --- |
| `PairOptions.BLE` is the BLE central (`ble.Central`); field doc: "A device on BLE has no network yet, so one of WiFi and Thread is required (ErrConfiguration otherwise)" | `controller/controller.go:587-593` |
| `PairOptions.Address`, `WiFi`, `Thread`, `Progress` fields; `WiFi`/`Thread` doc: "nil provisions none" | `controller/controller.go:604` (`Address`), `:609-610` (`WiFi`, `Thread`), `:618` (`Progress`) |
| `PairOptions` has no `NetworkOnly` field (grep `NetworkOnly` in `controller/*.go` finds nothing) | `controller/controller.go` (grep result: empty) |
| `Pair` parses the code, then refuses `BLE` together with `Address` as `ErrConfiguration` | `controller/controller.go:658-665` |
| `Pair` refuses `opts.BLE != nil && opts.WiFi == nil && opts.Thread == nil` as `ErrConfiguration`: "pairing over BLE needs PairOptions.WiFi or Thread" | `controller/controller.go:674-676` |
| `Pair` bounds a ctx without deadline by `DefaultPairTimeout`, then calls `pairDiscovered` when no `Address` | `controller/controller.go:679-685` (timeout at 681, `pairDiscovered` at 685) |
| `pairDiscovered` adds the IP transport when `c.names != nil` | `controller/pair_race.go:42-47` |
| `pairDiscovered` builds the BLE scanner only `if opts.BLE != nil`: `ble.NewScanner(opts.BLE, ...)`, then appends `ble.Transport(bsc)` observed as `PairingStepDiscoveryBLE` | `controller/pair_race.go:49-53` |
| Nothing else in `pair_race.go` constructs a BLE scanner or reads a central (the `bsc` variable is set at one place) | `controller/pair_race.go:49-52` (grep `bsc = ` finds only line 50) |
| Capabilities passed to the race are set only for a QR code (`!pc.Manual`): `caps = CapabilitiesOf(pc.Capabilities)`; a manual code leaves `caps` nil | `controller/pair_race.go:54-58` (`r.multi` at 59) |
| `SelectTransports(available, nil)` returns every transport; with caps it keeps BLE only `if caps.BLE` (QR bit 1) | `client/discovery/session.go:78-93`, bit decode `session.go:73-76` |
| Comment: "BLE where a QR code's capabilities name it or the code names none (a manual code)" | `controller/pair_race.go:34-36` |
| `Session.run` computes `selected := SelectTransports(...)` and calls `reportTransports(transports, selected)` first | `client/discovery/session.go:304-307` |
| `matter.controller.discovery.initiating`: Info, emitted by `reportTransports` on every discovery; adds `note="BLE not requested"` when a BLE transport is available but not selected | `client/discovery/session.go:391-404` (note at 401-403) |
| `matter.controller.discovery.no_scanner`: Warn when no transport is selected | `client/discovery/session.go:405-408` |
| `matter.controller.discovery.ble_not_enabled`: Info when `Capabilities != nil && Capabilities.BLE && !bleInstalled` (a QR code naming BLE, no BLE transport among the available ones) | `client/discovery/session.go:409-411` |
| `matter.controller.discovery.interrupted`: Debug when the session had an `abortReason` and the completion error is not that reason; carries `instead_of=<err>` | `client/discovery/session.go:332-336` |
| `matter.controller.discovery.interruption_ignored`: Info | `client/discovery/session.go:286-287` |
| `ble.Scanner` asks the client to start scanning in `reconcileScan` when `activeDiscoveries > 0 && !closed` | `controller/ble/scanner.go:320-332` |
| `esphome.Central.StartScanning`: sets `scanning`, takes `proxyList()`; with no proxy yet (only a discovered set) it triggers a rescan and returns nil without subscribing | `ble/esphome/central.go:271-281` |
| `StartScanning` with proxies: resets busy marks on a fresh scan, counts busy ones, calls `p.subscribe()` on each, returns `ErrProxyBusy` when every proxy is busy | `ble/esphome/central.go:283-296` |
| `proxy.subscribe()` returns early `if a == nil || p.subscribed` (no API connection, or already subscribed) | `ble/esphome/proxy.go:396-402` |
| `ble.esphome.proxy_taken`: Info, emitted in `subscribe()` right before `SubscribeBluetoothLEAdvertisementsRequest` is sent; note text names Home Assistant's Bluetooth integration | `ble/esphome/proxy.go:410-412` |
| `ble.esphome.proxy_connected`: Info, emitted in `attach()` when the API connection is up (fields proxy, device, encrypted, feature_flags); then `if p.c.isScanning() { p.subscribe() }` | `ble/esphome/proxy.go:310-314` |
| Package doc: "The central logs ble.esphome.proxy_taken when it subscribes"; taking the proxy replaces the current API subscriber | `ble/esphome/doc.go:36-44` |
| `ble.esphome` is a separate module, tagged `ble/esphome/v0.13.10` | `ble/esphome/go.mod`; `git tag --list 'ble/esphome/*'` |
| Pairing progress: BLE start is reported as step "2.1 Discovery.BLE started" when the BLE transport exists | `controller/pair_race_test.go:59` (test expectation), `controller/pair_race.go:51` |

## Part 2: go-fabric-ui (`u7/completeness` at `0ecef14`)

| Fact | Source |
| --- | --- |
| `source.Pair` parses the code with `controller.ParsePairingCode`; a failure is `ErrInvalidArgument` | `fabricui/gofabric/gofabric.go:264-268` |
| `source.Pair` calls `s.networkOf(o)` | `fabricui/gofabric/gofabric.go:269` |
| `networkOf` returns `nil, nil, nil` when `o.WiFiID == "" && o.ThreadID == ""` (no lookup, no default) | `fabricui/gofabric/gofabric.go:229-231` |
| `networkOf` with an id: `ErrInvalidArgument` when the host has no credential store; `ErrUnknownCredential` for an id not stored; lookups `Credentials.WiFi(id)` / `Credentials.Thread(id)` | `fabricui/gofabric/gofabric.go:232-250` |
| `opts := s.opts.Pair` (template, includes the host's `BLE`), then `opts.BLE, opts.WiFi, opts.Thread = nil, wifi, th` | `fabricui/gofabric/gofabric.go:273-274` |
| BLE is set back only when `s.opts.Pair.BLE != nil && !o.NetworkOnly && (pc.Manual \|\| pc.Capabilities&capabilityBLE != 0) && (wifi != nil \|\| th != nil)` | `fabricui/gofabric/gofabric.go:275-277` |
| `capabilityBLE = 1 << 1` | `fabricui/gofabric/gofabric.go:255-257` |
| Doc comment: "as go-fabric-server's commission_with_code decides (controllerapi/cmd_commission.go:98-141 at v0.3.2)" | `fabricui/gofabric/gofabric.go:259-263` |
| `Pair` emits no log line: no `s.log` call in `Pair`; the only `s.log` use in the file is `fabricui.gofabric.label_push_failed` | `fabricui/gofabric/gofabric.go:264-290` (no log), `:542` (only `s.log.Warn`) |
| The branch where BLE is dropped has no `else`, no log, no error | `fabricui/gofabric/gofabric.go:275-277` |
| `Capabilities().BLE` is `s.opts.Pair.BLE != nil` (host's central present), independent of credentials | `fabricui/gofabric/gofabric.go:162-164` |
| `GET /controller/status` reports `capabilities.ble` from `s.src.Capabilities().BLE` | `fabricui/handlers_controller.go:307-311` |
| The in-memory controller reports `ControllerCapabilities{}` (no BLE) | `fabricui/memory_controller.go:209` |
| `fabricui.PairOptions`: `WiFiID, ThreadID string`; `NetworkOnly bool` ("never commissions over Bluetooth LE"); `TermsAndConditions *TermsAcknowledgement`; `Progress func(PairingStep)` | `fabricui/controller.go:565-582` |
| `pairOptionsOf` copies `wifi_id`, `thread_id`, `network_only`, `terms_and_conditions` from the request body; no default | `fabricui/handlers_controller.go:1233-1247` |
| `CommissionNode` sets `opts.Progress` to publish `commissioning_progress` with the step, publishes a "started" event, calls `src.Pair` for a code | `fabricui/handlers_controller.go:1279-1288` |
| U7 `Pair` adds `opts.Progress = progressFor(opts.Progress, o.Progress)` and `opts.Edit = editFor(opts.Edit, o.TermsAndConditions)` | `fabricui/gofabric/gofabric.go:278-279` |
| `progressFor` chains the host's `PairOptions.Progress`, then the port's with `PairingStep{Number, Sub, Name, Phase}` (no error text) | `fabricui/gofabric/gofabric.go:288-306` |
| `editFor` sets `commissioner.Options.TermsAndConditions` from the request's acknowledgement | `fabricui/gofabric/gofabric.go:310-327` |
| `CommissionDiscovered` (device picked in the page's search table) calls `networkOf` and passes `WiFiNetwork`/`ThreadNetwork` to `client.CommissionOptions`; no BLE option there | `fabricui/gofabric/gofabric.go:442-482` |
| Commission.svelte: `let network = $state("")` (default is empty) and `let networkOnly = $state(false)` | `ui/src/lib/pages/controller/Commission.svelte:30-31` |
| The network select options: first `{ value: "", label: t("controller.commission.network_none") }`, then one per stored credential `kind:id` | `ui/src/lib/pages/controller/Commission.svelte:49-52` |
| `withNetwork`: `wifi_id` / `thread_id` go into the request only when `network` splits to kind `wifi` / `thread`; `network_only: true` only when the checkbox is on; terms added when set | `ui/src/lib/pages/controller/Commission.svelte:54-63` |
| `onMount` loads credentials when `capabilities.credentials`; no code preselects a credential | `ui/src/lib/pages/controller/Commission.svelte:44-47` (grep: no preselect logic for `network`) |
| The select and the `network_only` checkbox in the form | `ui/src/lib/pages/controller/Commission.svelte:155-158` |
| The Commission page does not read `capabilities.ble`; the only UI use of the BLE capability is the label `controller.settings.cap.ble` | grep `capabilities.ble` in `ui/src` finds none; `ui/src/lib/i18n/en.json:219` |
| Terms inputs (version, acknowledgements), validated by `terms()`/`termsInvalid`, sent as `terms_and_conditions` | `ui/src/lib/pages/controller/Commission.svelte:34-35,60-61,67-76,161-176` |
| Progress steps rendered from `controller.progress.steps` under the progress card | `ui/src/lib/pages/controller/Commission.svelte:139-140,181` |
| Search-table pairing (`pair(d)`) sends `{discovered, passcode}` through the same `commission()` and `withNetwork` | `ui/src/lib/pages/controller/Commission.svelte:78-84` (`commission`; `withNetwork` called at 83) |

## Part 3: go-fabric-server (`main`, v0.6.1)

| Fact | Source |
| --- | --- |
| `Options.BLE ble.Central`: "the Bluetooth LE central commission_with_code finds a device on BLE through (--ble-adapter); nil commissions over DNS-SD only" | `controllerapi/api.go:81-83` |
| `commission_with_code` args: `code`, `network_only`, `wifi_credentials_id`, `thread_dataset_id`, `discovery_timeout_s` | `controllerapi/cmd_commission.go:107-112` |
| It calls `s.bleNetwork(cmd, wifi_credentials_id, thread_dataset_id)` | `controllerapi/cmd_commission.go:128` |
| `bleNetwork`: "the ids given, else the default entries; an id given that is not stored is InvalidArguments" | `controllerapi/cmd_commission.go:76-85` |
| `bleNetwork` calls `s.cfg.credentials(wifiID, threadID)`; a non-empty Wi-Fi SSID builds `WiFiNetwork`; a non-empty hex Thread dataset builds `ThreadNetwork` | `controllerapi/cmd_commission.go:80-93` |
| `settingsStore.credentials(wifiID, threadID *string)` doc: "(nil or empty: the default entries, absent ones nil)" | `controllerapi/settings.go:145-148` |
| Default fallback: `find(id, ...)` looks up `credentialID(id)`; `credentialID` returns `defaultCredentialID` for nil or "" | `controllerapi/settings.go:149-168`, `controllerapi/cmd_server.go:195-200` |
| `defaultCredentialID = "default"` (reserved entry) | `controllerapi/settings.go:21-23` |
| An absent default entry is not an error: `w`/`t` stay nil; an explicit id not stored returns "Unknown wifi_credentials_id: <id>" / "Unknown thread_dataset_id: <id>" | `controllerapi/settings.go:169-180` |
| `settingsStore.defaults()` returns the default entries (separate helper) | `controllerapi/settings.go:122-143` |
| Pair options: `opts := s.pairOptions()` (attestation policy, auto-subscribe; no BLE in it) | `controllerapi/cmd_commission.go:66-74`, `:132` |
| BLE decision: `s.opts.BLE != nil && !a.NetworkOnly && (pc.Manual \|\| pc.Capabilities&capabilityBLE != 0) && (wifi != nil \|\| thread != nil)` then `opts.BLE, opts.WiFi, opts.Thread = s.opts.BLE, wifi, thread` | `controllerapi/cmd_commission.go:133-135` |
| Doc comment cites matterjs-server `ControllerCommandHandler.ts:1971-2121` ("takes BLE whenever it is enabled and network_only is false") | `controllerapi/cmd_commission.go:98-105` |
| No log line in `cmdCommissionWithCode` about BLE being used or skipped | `controllerapi/cmd_commission.go:106-143` (grep: no `Log`/`slog` call) |
| `capabilityBLE = 1 << 1` | `controllerapi/cmd_commission.go:254-257` |
| `Server.PairOptions()` (for outside `/ws`): `pairOptions()` plus `o.BLE = s.opts.BLE` | `controllerapi/host.go:162-168` |
| Host credential accessors: `WiFiCredential(id)` / `ThreadDataset(id)` ("" the default one), via `cfg.credentials` | `controllerapi/host.go:126-141` (`WiFiCredential` 128, `ThreadDataset` 137) |
| `/ws` `server_info` field `BluetoothEnabled: s.opts.BLE != nil` | `controllerapi/events.go:104` |
| Daemon flags: `--bluetooth-adapter`, `--bluetooth-proxy` (repeatable, `host[:port][,key]`) | `internal/daemon/run.go:147-148`; Config fields `:85`, `:90` |
| `bleCentral(c, log)`: proxies present -> adapter set is an error, else logs `fabricserver.ble_proxies` and calls `newProxyCentral(c.BluetoothProxies, esphome.Options{Logger: log})`; no proxies -> `adapterCentral` | `internal/daemon/run.go:568-581` (log at 573, central at 574) |
| `newProxyCentral` = `esphome.NewCentral(proxies, opts)` | `internal/daemon/run.go:524-532` |
| `bleCentral` is called at start-up, before the controller; the central is closed by `closeCentral` | `internal/daemon/run.go:230-242` |
| `controllerOptions(c, log, lvl, central)` sets `controllerapi.Options.BLE: central` | `internal/daemon/run.go:606-611`; call `:250` |
| Pages source built only when `--web` is set: `pageHooks` created `if c.Web != ""`; `ctrlPages = controllerSource(...)` | `internal/daemon/run.go:264-267`, `:296-298` |
| `controllerSource` hands the pages `gofabric.ControllerOptions{..., Credentials: credentialStore{api}, Pair: api.PairOptions(), ...}` (so the pages' `Pair.BLE` is the same central) | `internal/daemon/controllerui.go:30-36` |
| `credentialStore.WiFi(id)` / `Thread(id)` call `api.WiFiCredential` / `api.ThreadDataset` | `internal/daemon/controllerui.go:87-91` |
| `capabilities.ble` on `GET /api/v1/controller/status` = go-fabric-ui `source.Capabilities().BLE` = `Pair.BLE != nil` = the central from `bleCentral` (server builds go-fabric-ui v0.6.1) | `internal/daemon/controllerui.go:33`, `controllerapi/host.go:165-167`, go-fabric-ui `fabricui/gofabric/gofabric.go:162-164`, `go.mod:7` |
| Add-on option `bluetooth_proxies: []` (default) and schema `bluetooth_proxies: - str?` | `addon/config.yaml:55`, `:65-69` |
| `run.sh` reads `bashio::config 'bluetooth_proxies'`, per entry `args+=(--bluetooth-proxy "${proxy}")`, logs `Bluetooth proxy <host>` and the count; none -> logs "No Bluetooth proxies configured; BLE commissioning is off." | `script/run.sh:59-76` |
| `run.sh` execs `/usr/bin/fabric-server "${args[@]}"` | `script/run.sh:80` |
| Dockerfile copies `script/run.sh` and runs it as `CMD` | `addon/Dockerfile:36-44` |
| Operator page documents the option and says the daemon takes the proxy over | `addon/DOCS.md:26`, `:48-59`; `docs/operations.md:43`, `:325-332` |

## Part 4: the join (what the code does today)

Inputs: A = central present; B = code allows BLE (manual code, or QR with bit 1); C = network named by the request (UI: `wifi_id`/`thread_id`; `/ws`: `wifi_credentials_id`/`thread_dataset_id`); D = `network_only`.

| Case | UI Commission page -> `source.Pair` | `/ws` `commission_with_code` | Then, in go-fabric |
| --- | --- | --- | --- |
| A yes, B yes, C named, D off | BLE set: `opts.BLE = s.opts.Pair.BLE` (`gofabric.go:275-277`) | BLE set from the named ids (`cmd_commission.go:128,133-135`) | BLE scanner built (`pair_race.go:49-52`); StartScanning -> `proxy_taken` (`central.go:293`, `proxy.go:410`) |
| A yes, B yes, C not named, D off | `networkOf` returns nil (`gofabric.go:229-231`); `wifi == nil && th == nil`, BLE stays nil (`:274-277`); no log | `bleNetwork` -> `credentials(nil,nil)` -> default entries (`cmd_commission.go:80`, `settings.go:149-168`); BLE set if a default entry exists with SSID/dataset (`cmd_commission.go:81-93,133`); no default -> BLE stays unset, no log | UI path: `opts.BLE == nil` so no BLE scanner (`pair_race.go:49`), only DNS-SD (`pair_race.go:43-48`); `/ws` path: as row 1 when a default exists |
| A yes, B yes, C named, D on | BLE stays nil (`!o.NetworkOnly` false, `gofabric.go:275`) | BLE stays unset (`!a.NetworkOnly` false, `cmd_commission.go:133`) | DNS-SD only |
| A yes, B no (QR without bit 1), any C, D off | BLE stays nil (`pc.Manual \|\| caps&bit` false, `gofabric.go:275`) | BLE unset (`cmd_commission.go:133`) | DNS-SD only; if opts.BLE were set, `SelectTransports` drops BLE for this QR (`session.go:89`) |
| A no (no central), any B/C/D | `Capabilities().BLE` false (`gofabric.go:163`); BLE nil | `s.opts.BLE == nil`, BLE unset (`cmd_commission.go:133`); `server_info.bluetooth_enabled` false (`events.go:104`) | DNS-SD only; for a QR with bit 1 go-fabric logs `ble_not_enabled` (`session.go:409-411`) |
| BLE passed but no network | not reachable from the page: `opts.BLE` is set only with `wifi != nil \|\| th != nil` (`gofabric.go:275`) | not reachable: same condition (`cmd_commission.go:133`) | would be `ErrConfiguration` (`controller.go:674-676`) |
| Explicit id not stored | `ErrUnknownCredential` before pairing (`gofabric.go:239-250`) | `InvalidArguments` "Unknown wifi_credentials_id/thread_dataset_id" (`settings.go:169-179`, `cmd_commission.go:81-83`) | not reached |
| Credentials store absent | `ErrInvalidArgument` when an id is named (`gofabric.go:233-235`) | n/a (store is `s.cfg`) | not reached |
| Which log lines the path itself writes about the BLE decision | none (`gofabric.go:264-290`) | none (`cmd_commission.go:106-143`) | `discovery.initiating` always (`session.go:404`); `proxy_taken` only if the central's `subscribe()` runs (`proxy.go:396-412`) |
| Default for the page's network select | `""` = "No network credential" (`Commission.svelte:30,50`) | request field absent -> default entries (`settings.go:149-168`) | n/a |

## Part 5: tests that exist for these paths

go-fabric (v0.18.0 + 1)

| File | Case |
| --- | --- |
| `controller/pair_race_test.go:29` | `TestPairRaceOverBLEAndIP` (manual code, `PairOptions.BLE` with `bletest.NewCentral` and `WiFi`; expects "2.1 Discovery.IP started" and "2.1 Discovery.BLE started" at `:59`) |
| `controller/ble_test.go:44` | `TestPairOverBLE` |
| `controller/pair_progress_test.go:51,89,118,153` | `TestPairProgress`, `TestPairProgressAttestationFailure`, `TestPairProgressSubscribeFailure`, `TestPairProgressByAddress` |
| `client/discovery/session_parity_matterjs_test.go:695` | `TestDiscoveryTransportsParity` with subtests "discovers on every installed transport where the caller states nothing" (`:708`), "discovers on the IP network alone unless the payload names BLE" (`:716`), "discovers on BLE where the payload names it" (`:725`), "names the device to a scanner without the options that steer discovery itself" (`:734`), "resolves with the winner while a losing attempt's rejection stays handled" (`:751`) |
| `ble/esphome/central_test.go:110,142,210,244,355` | `TestScanReportsMatterAdvertisements`, `TestConnectAndGATT`, `TestCancelledWriteEndsTheConnection`, `TestConnectErrors`, `TestProxyTakenThenBusy` |
| none found | a test asserting the log lines `ble.esphome.proxy_taken`, `proxy_connected`, `discovery.initiating`, `ble_not_enabled` or "BLE not requested" (grep over `*_test.go` for these strings finds nothing) |
| none found | a test of `controller.Pair` returning `ErrConfiguration` for BLE without WiFi/Thread (grep of `controller/*_test.go` names above: none by that name; not searched by body) |

go-fabric-ui (`u7/completeness`)

| File | Case |
| --- | --- |
| `fabricui/gofabric/gofabric_test.go:129,159` | `TestEditFor`, `TestProgressFor` |
| `fabricui/gofabric/e2e_test.go:211,576,787,836` | `TestEndToEnd_Controller`, `TestEndToEnd_DetailsAndEvents`, `TestEndToEnd_TermsAndConditions`, `TestEndToEnd_UpdateEventsFromTheManager`; the e2e `Pair` template is `controller.PairOptions{OnAttestationFailure: attestation.Accept()}` (`e2e_test.go:101`), i.e. no BLE central |
| `fabricui/handlers_controller_errors_test.go:210` | a request table row with `{"code":..., "wifi_id":"home","thread_id":"mesh","network_only":true,...}` (body only) |
| `ui/src/lib/pages/controller/Commission.test.ts` | "commissions by code and links the new node; the code field is cleared" (`:19`), "shows the server's problem when no device answers" (`:31`), "discovers devices and pairs one with its passcode" (`:39`), "refuses a passcode out of range before any request" (`:53`), "follows the stream's progress" (`:63`), "explains that the camera needs a secure context instead of offering the scan" (`:70`), "fills the code from a scanned QR payload" (`:78`), "sends the terms acknowledgement when both values are set, and refuses one out of range (0.7.0)" (`:93`), "lists the steps the stream reports under the progress (0.7.0)" (`:108`) |
| none found | a test of `source.Pair`'s BLE decision, `networkOf`, `Capabilities().BLE`, or of the page's `thread_id`/`wifi_id`/`network_only` in the request (grep of `fabricui/` `*_test.go` for `NetworkOnly`, `network_only`, `BLE`, and of `Commission.test.ts` for `network`/`thread`/`wifi`: only the row above) |

go-fabric-server (`main`, v0.6.1)

| File | Case |
| --- | --- |
| `internal/daemon/proxy_test.go:48,76,113,166` | `TestBluetoothProxyFlag`, `TestBluetoothProxyWired` (asserts `opts.BLE == fake` at `:91`), `TestRunRefusesBothBackends`, `TestCommissionThroughProxy` (sets a Wi-Fi credential with `set_wifi_credentials` and no id, then `CommissionWithCode(code, false)`; PASE over BTP through the proxy) |
| `internal/daemon/ble_test.go:35` | `TestBluetoothAdapterWired` |
| `internal/daemon/controllerui_test.go:203,301,308,323` | `TestControllerPages` (asserts `st.Capabilities.BLE` false with no central at `:215`; `/ws` `CommissionWithCode(qr, true)` at `:225`), `TestControllerPagesPinnedLabel`, `TestControllerPagesReadOnly`, `TestControllerPagesCredentials`; `newPagesEnvWith` takes a `central func(*fabrictest.Device) ble.Central` (`:61`) |
| `controllerapi/host_test.go:18,113` | `TestHostCredentials`; `PairOptions()` check `po.BLE != nil` is false without a central (`:113`) |
| `controllerapi/commands_test.go:402` | `commission_with_code` with `network_only: true`, expects `NodeCommissionFailed` / `ReasonNotDiscovered` |
| `controllerapi/s3_test.go:256` | `commission_with_code` with `wifi_credentials_id: "nope"` expects `InvalidArguments` "Unknown wifi_credentials_id: nope" |
| `controllerapi/errors_internal_test.go:19` | `TestCommissionErrorProxy` |
| none found | a test of the pages path (`/api/v1/controller/commission`) pairing over a central; a test of `capabilities.ble` true; a test of `commission_with_code` BLE decision without an id (default fallback) other than `TestCommissionThroughProxy` |

## Issue citations re-read (old -> new)

| Citation in the issues | Today |
| --- | --- |
| UI `Commission.svelte:26` (select starts at none) | `:30` (`network`), option `:50` |
| UI `Commission.svelte:42-44` | `:49-52` |
| UI `Commission.svelte:47-52` | `:54-63` (`wifi_id`/`thread_id` at `:57-58`) |
| UI `gofabric.go:268-272` | `:273-277` |
| UI `gofabric.go:224-227` | `:229-231` |
| UI `gofabric.go:157-158` | `:162-164` |
| go-fabric `controller/pair_race.go:49-52` | unchanged `:49-52` |
| go-fabric `controller/controller.go:602-604` | `:674-676` |
| go-fabric `ble/esphome/central.go:271-293` | unchanged `:271-293` |
| go-fabric `ble/esphome/proxy.go:410` | unchanged `:410` |
| server `controllerapi/settings.go:145-179` | `:145-181` |
| server `controllerapi/cmd_commission.go:98-105` | unchanged |
| server `controllerapi/cmd_commission.go:133` | unchanged `:133` |
| server `controllerapi/host.go:126` | unchanged (comment at `:126-127`, `WiFiCredential` at `:128`) |
