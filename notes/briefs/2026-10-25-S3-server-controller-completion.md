# Brief S3 — go-fabric-server v0.3.0: the controller commands CJ unblocked, ICD and topology commands, the python-client compatibility job, the add-on form, the node pages

Date 2026-10-25. Repository `/home/markus/Dokumente/GitHub/go-fabric-server`
(main = v0.2.1). Read `CLAUDE.md`, `CONCEPT.md`, `docs/reports/S1.md`
(the command table with its `gap Qn → InvalidCommand` rows and "planned
for S2|S3|never"), `docs/reports/S2.md` §Questions, `docs/by_design.md`,
`internal/arch` (the `internal/wire` exception Q1), `controllerapi/`
(the command table, `cmd_*.go`), the compat fixtures, `deploy/`. go-fabric
**`v0.12.1`** is the pin (its `CHANGELOG.md` sections 0.12.0 and 0.12.1
list every new route: `client.EncodeJSON/DecodeJSON`, `controller.ParsePairingCode`,
`client.Node.OpenCommissioningWindow`, `commissioner.ErrAttestationRejected`,
`client.Node.InvokeBatch`, `Registry.SetFabricLabel`, `client.Node.SetAutoSubscribe`,
`controller.ProductionTrust`, `controller.OpenDB`, `Controller.Discover`,
`commissioner.ParseThreadDataset`, `PairOptions.BLE`, `client.Properties.SpecificationVersion`,
`contract.StatusError`, `controller/dcl/snapshot.Vendors`, `fabrictest.Site.AddNode`,
`controller/client.ICDClient`, `node.Config.ICD`). go-fabric-ui **`v0.1.1`**
(`/home/markus/Dokumente/GitHub/go-fabric-ui`: `README.md`,
`docs/embedding.md`). matterjs-server protocol facts via the existing
fixtures and, where a new command needs its shape, the python client at
`github.com/matter-js/matterjs-server` `python_client/matter_server/common/models.py`
(raw file; cite the commit you read) — the `APICommand` list there is the
contract. The Home Assistant integration (core `2026.10.3`,
`homeassistant/components/matter/{__init__,adapter,api}.py`, raw files)
calls after `connect`: `start_listening`, `get_nodes`, `get_node`,
`commission_with_code`, `commission_on_network`, `set_wifi_credentials`,
`set_thread_operational_dataset`, `open_commissioning_window`,
`remove_node`, `remove_matter_fabric`, `interview_node`, `ping_node`,
`node_diagnostics`, `get_network_topology`, `set_default_fabric_label`,
`subscribe_events`. Those are the ones a user of the integration hits.

**Model:** Opus. Branch `s3/controller-completion`, conventional commits,
one PR, no auto-merge, no label, no attribution lines, English; the
owner merges and tags **`v0.3.0`** (`internal/version`, `changelog.md`
section — the release pipeline refuses a mismatch). go-fabric's `CLAUDE.md`
token rules. **Hard rules:** (1) go-fabric through its facade only
(`controller`, `controller/client`, `controller/commissioner`, `node`,
`device`, `contract`, `eligibility`, `fabrictest`); the `internal/arch`
guard holds it and **the `internal/wire` exception goes** (§2). (2) A
command the facade cannot serve yet stays `InvalidCommand` with
`details` naming the go-fabric route it waits for, and the report's
Questions name the symbol. (3) Wire compatibility with matterjs-server
is the contract: every command added gets a compat fixture recorded in
the python client's shape. (4) Secrets (passcodes, Wi-Fi credentials,
Thread datasets) are never logged; tests assert it.

## 1 Commands on the CJ routes

Replace each `InvalidCommand` stub whose gap CJ closed, with the facade
route named in parentheses and a fixture per command:
`parse_onboarding_payload` (`controller.ParsePairingCode`),
`batched_device_commands` (`InvokeBatch`, `MaxPathsPerInvoke`),
`enable_auto_subscription`/`disable_auto_subscription` (`SetAutoSubscribe`),
`open_commissioning_window` (`client.Node.OpenCommissioningWindow`; the
response carries the setup payload fields matterjs-server returns),
`discover`/`discover_commissionable_nodes` (`Controller.Discover`
instead of the S1 `Discoverer` seam, if the seam is still there),
`commission_on_network` (Discover + Pair by discriminator/instance),
`set_thread_dataset` (`commissioner.ParseThreadDataset` validates before
storing), `set_default_fabric_label` (`Registry.SetFabricLabel` for the
connected nodes, the default kept for the next), `get_vendor_names`
(`dcl/snapshot.Vendors` if not already), error mapping through
`contract.StatusError` where the S2 report said FAILURE was wrong,
`get_node`/`get_nodes` `attributes` on the JSON route (`client.EncodeJSON`/
`DecodeJSON`, §2), `commission_with_code` with `PairOptions.BLE` when
the code says BLE and the server was started with an adapter (document
the flag; no BLE in CI). Attestation rejection (`ErrAttestationRejected`)
becomes the error code matterjs-server uses for it (fixture).

## 2 The JSON route replaces `internal/wire`

`internal/wire` built TLV values from `cluster/spec` and `tlv` directly
(arch exception Q1). Move every read, write, invoke and subscription
value through `client.EncodeJSON`/`DecodeJSON` (and `client.Value`), keep
the tag-based attribute keys the S2 report documents, delete `internal/wire`
and its exception, regenerate nothing by hand. The 88 compat fixtures
must pass byte for byte; a difference is a finding to report, not a
fixture to edit.

## 3 New commands the facade now allows

- `get_network_topology`: from the registry's nodes (fabric, node id,
  endpoints, parents, IP addresses via `GetNodeIPAddresses`'s route,
  Thread/Wi-Fi from the node's network diagnostics attributes when
  cached) in matterjs-server's shape.
- `node_diagnostics` / `diagnostics` (whichever name the python client
  uses; check both are answered as matterjs-server does).
- ICD: `get_icd_state`, `register_icd`, `resync_icd`, `unregister_icd`
  on `controller/client.ICDClient` (go-fabric 0.12.0); fixtures from the
  python client's models.
- `import_test_node` (concept v8 D row S3): import a node dump into the
  registry without a network peer, for matterjs-server's test tooling —
  read how the python client shapes the dump; if go-fabric's registry has
  no import route, implement it in the server's own store and answer
  the node from there (reads from cache, writes refused with the status
  matterjs-server gives), and say so.
- Still `InvalidCommand` with a reason (not this wave): `check_node_update`,
  `update_node`, `initiate_ota_upload` (go-fabric OTA push is milestone 5
  phase 25), `get_thread_border_routers`, `get_thread_diagnostics`
  (ADR 0027, milestone 6), `send_webrtc_provider_command`.

## 4 The python-client compatibility job

A CI job `compat-python` that builds the server, starts it against a
`fabrictest`-backed in-process node? No: the server is a process; start
it with a temporary store on a free port, then run a pytest file under
`test/compat/` with `matter-python-client==1.4.0` (pin; `pip install`
in the job, a `requirements.txt` with hashes) that connects, checks
`server_info` (schema 14 / min 11), `start_listening`, `get_nodes`,
`get_vendor_names`, `set_default_fabric_label`, `discover` (empty),
`parse_onboarding_payload`, `open_commissioning_window` on a missing
node (the error shape), and the event subscription handshake. Commissioning
a real node in that job only if a go-fabric reference bridge can be run
in the same job cheaply (it can: `examples/reference-bridge` from the
go-fabric module cache; try, and say whether it held). The job is
required for merge from v0.3.0 on; list its name in the report. Document
in `docs/` how the Home Assistant Matter integration is pointed at this
server: "No" to the official add-on, URL `ws://<host>:5580/ws`; the
slug check in core (`config_flow.py:259-260` at 2026.10.3) means it is
never auto-selected.

## 5 The add-on form and the node pages

- `addon/` in go-ha2fabric's form (`/home/markus/Dokumente/GitHub/go-ha2fabric/addon`,
  `script/run.sh`, `repository.yaml`, `.github/workflows/addon-image.yml`):
  `host_network: true`, option `port` default 5580 (the official add-on's
  port: DOCS say only one of the two can run), `log_level`, the storage
  under `/data`, no `discovery` key (useless with the slug check; say so
  in DOCS), Ingress on an internal port for §5's pages. Version in
  `addon/config.yaml` = `internal/version` (`make version-check`).
- The node pages: for the bridge role, mount `fabricui.Handler` (go-fabric-ui
  v0.1.1) on the Ingress listener with `Node` = the bridge's node, no
  `Exposure` (the host decides exposure over `/bridge`), `Admin` = the
  Ingress source check; outside the add-on behind a `--web` flag, read-only
  unless `--web-admin`. The controller pages come with U2; nothing to
  invent here.

## 6 Docs, tests, report

`README.md` status and command table, `docs/` (the HA integration page,
the add-on page, the compat job), `changelog.md` `## [0.3.0]`,
`internal/version` 0.3.0, `docs/by_design.md` additions (where the
server departs from matterjs-server, cited). Guards: the family standard
(`make ci`, race, cover-check, fuzz-smoke, licenses, vuln, secrets, the
release dry run, docker + addon-image builds, `make version-check`), the
compat job locally (python3 with a venv under the scratchpad, never in
the repository). Background runs with a summary log; `df -h /` before,
`go clean -cache -testcache` after. Report `docs/reports/S3.md` (PR
body): per § with commit hashes; guards with HEAD; differences;
decisions; left out; questions (go-fabric facade gaps with the symbol,
matterjs-server shape questions); by_design candidates with citations.
Copy this brief to `notes/briefs/` in the first commit. If a permission
is denied, say which and stop that step; never ask the launcher to
perform it. Hand back with the PR number, HEAD, and the Questions and
by_design sections verbatim.
