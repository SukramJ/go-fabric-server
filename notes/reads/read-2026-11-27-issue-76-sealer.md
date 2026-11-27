# go-fabric-server #76 — sealing the stores at rest: fact note and plan

Read-only. Sources: go-fabric module cache at **v0.28.0** (`GF/` =
`$(go env GOMODCACHE)/thingfabric.dev/go-fabric@v0.28.0/`), go-fabric-server
at **origin/main f97e53b** (`S/`, read with `git show`). Nothing was run;
every behaviour below is read from godoc, code or a go-fabric test, not
executed. "Unverified" marks what was not read.

## 1. Facts

### Facade (go-fabric v0.28.0)

| # | Fact | Where |
| --- | --- | --- |
| F1 | `controller.NewAESGCMSealer(key []byte) (*store.AESGCMSealer, error)`; key must be 32 bytes; "generated once (crypto/rand) and kept outside the database" | `GF/controller/seal.go:29-36` |
| F2 | `node.NewAESGCMSealer(key)` same; `node.Sealer = store.Sealer` (alias new after v0.26.0) | `GF/node/facade.go:123-130`, `:65-68` |
| F3 | `controller.Config.Sealer`: opt-in, same sealer as the node's. Sealed: the CA, the controller's fabric, CASE resumption records, group key sets, and in the registry **the kept onboarding codes** and ICD Check-In keys. Certificates, ids, attribute values stay readable | `GF/controller/controller.go:159-168` |
| F4 | **Migration is built in**: "A database written without a sealer is sealed on the way in and its file scrubbed". One transaction seals every clear row and writes the canary; a failure leaves the database as it was; then the file is rewritten and the `-wal` emptied | `controller.go:168-170`; `GF/store/seal.go:142-151`; registry `GF/controller/client/sqlstore/sqlstore.go:112-119` |
| F5 | A scrub that cannot finish (reader on the `-wal`, full disk, process ended) does **not** fail `New`; it stays owed, is logged once (`matter.store.seal_scrub_owed`) and the next `New` retries | `controller.go:171-174`, `:686-690`; node `GF/node/node.go:582-590` |
| F6 | "copies taken before the upgrade keep the clear values" | `store/seal.go:151-152`; `GF/docs/security.md:124-126` |
| F7 | Controller, wrong key: `New` fails wrapping `ErrUnseal`, before anything is written (canary opened first). Controller, no sealer on a sealed database: `New` fails reading the sealed CA with `ErrNoSealer` | `controller.go:178-182`; `controller/seal.go:11-19`; `store/seal.go:130-133`; tested `GF/controller/facade_sealer_test.go:111-116` |
| F8 | `node.Store.Sealer`: with `Path` or `DB`; seals clear rows, marks the canary, scrubs; wrong key → `New` fails with `ErrUnseal`; excludes `Credentials` | `GF/node/config.go:803-817`, `:827-828` |
| F9 | **Node, no sealer on a sealed store: `New` succeeds** in go-fabric's own test (an uncommissioned node); only reading the secret returns `ErrNoSealer`. Godoc: "a store without one reads a sealed secret as store.ErrNoSealer and writes none" | `GF/node/facade_reseal_test.go:91-98`; `node/config.go:813-815`; `node/facade.go:117-120` |
| F10 | `controller.Reseal(ctx, db *sql.DB, old, next store.Sealer) error`: both stores in one transaction, one scrub. Wrong old key, a value that does not open, or a secret in clear → `ErrUnseal`, database left as it was. Any error after the commit wraps `ErrScrubIncomplete`: values are under `next`, the next `New` under `next` scrubs | `GF/controller/reseal.go:16-39`, `:57-63` |
| F11 | "No controller may be open on db while Reseal runs, and no other process may write the file"; "db must carry the schemas a New applied"; "There is no unseal: the way back to a clear database is a restore or a factory reset" | `controller/reseal.go:35-38` |
| F12 | `node.Reseal(ctx, db, old, next Sealer)`: same semantics for a node store; "no node may run on it"; db is "`Store.Path` opened with database/sql under the "sqlite" driver the node registers". The node store's Reseal does not touch a controller registry | `node/facade.go:132-153` |
| F13 | go-fabric's facade-only test opens the node file for Reseal with `sql.Open("sqlite", url.URL{Scheme:"file", OmitHost:true, Path:path, RawQuery:"_pragma=foreign_keys(1)"})` | `node/facade_reseal_test.go:60-61` |
| F14 | `controller.OpenDB` adds `journal_mode(WAL)`; `node.Store.Path` is opened without it (`store.OpenSQLite(path)`); both create a new file 0600 | `GF/controller/open.go:39-48`; `GF/node/store_path.go:17-23`; `GF/store/sqlite.go:41-47` |
| F15 | The AAD binds table, column and row key — not the database file. Sealing gives confidentiality only: no integrity, no rollback detection; deleting the canary row makes the database count as never sealed | `store/seal.go:32-34`, `:122-126`, `:137-138` |
| F16 | A factory reset drops the canary; the next sealed open marks it again (first-NewSealed path) | `store/seal.go:139-145` |
| F17 | A node whose sealed store does not open "has no FactoryReset to call": the host erases with `store.New(db).Erase` or removes the file | `GF/docs/security.md:174-179` |
| F18 | Reference pattern: `--seal-key file:<path>`, 64 hex digits, white space trimmed; a missing file is created `O_CREATE|O_EXCL` 0600 with a `crypto/rand` key | `GF/examples/reference-controller/cli/sealkey.go:22-87` (the bridge example: `examples/reference-bridge/sealkey.go`, same format per its `:31`) |
| F19 | The seal sources are byte-identical between v0.26.0 and v0.28.0 (`store/seal.go`, `controller/seal.go`, `controller/reseal.go`, `sqlstore/seal.go`, `controller.go:155-186`); `node/facade.go` differs only by F2's alias | `diff` in the module cache |

### Server (origin/main f97e53b)

| # | Fact | Where |
| --- | --- | --- |
| S1 | go-fabric **v0.26.0** — the facade of F1–F18 is already there (F19); no bump needed for #76 | `S/go.mod:7` |
| S2 | Controller database: `controller.OpenDB(<storage>/fabric-server.db)`, then `controller.Config{DB, …, KeepOnboardingCodes}` — the one place for `Sealer` | `S/internal/daemon/run.go:309-325`, `New` at `:354` |
| S3 | Every device node's store: `node.Store{Path: <storage>/<s.Store>}` in `bridgeConfig` — one place for all nodes. Files: `bridge.db` (the `--bridge-*` node), `bridge-<key>.db` (registry nodes, created at run time from the pages) | `run.go:556`; `S/internal/daemon/nodes.go:68`, `:73` |
| S4 | Every flag has `FABRIC_SERVER_<FLAG>` as environment twin automatically | `run.go:148-151`, `:204-212` |
| S5 | Storage directory created 0700; `server.json` (fabric label, **stored Wi-Fi credentials and Thread datasets**) and `nodes.json` (**passcodes**) are JSON files 0600 — not in any database, so no sealer reaches them | `run.go:260`; `S/docs/operations.md:225-233` |
| S6 | A do-and-exit mode exists as precedent: `--health-check` | `run.go:189` |
| S7 | Docs today: kept codes "stored unsealed until the database is sealed (a follow-up)"; backup = whole directory, daemon stopped, or `sqlite3 … .backup` | `docs/operations.md:235-237`, `:256-260` |
| S8 | Add-on: storage is `/data`, `backup: cold`; options map to environment variables in `script/run.sh` (`keep_onboarding_codes` → `FABRIC_SERVER_CONTROLLER_KEEP_ONBOARDING_CODES`) | `S/addon/config.yaml:35-37`, `:49-65`; `S/script/run.sh:17`, `:20` |
| S9 | Import guard: `node` and `controller` are allowed, `store` is not in the list read | `S/internal/arch/arch_test.go:31-75` (the list continues past :75 — tail unverified) |
| S10 | Removing a registry node deletes its store files; an erase is `node.FactoryReset`, then a new node on the erased store | `S/internal/daemon/registry.go:350-361`; `nodes.go:235`, `:250`, `:272` |

## 2. Plan

1. **Key source.** One flag `--seal-key-file <path>` (env
   `FABRIC_SERVER_SEAL_KEY_FILE` by S4), unset = today's behaviour. File
   format as go-fabric's reference (F18): 64 hex digits, trimmed. One key for
   `fabric-server.db` and every `bridge*.db` (F3 "the same sealer"; F15 names
   the limit). New file `internal/daemon/sealkey.go`: read, create
   (`O_EXCL`, 0600, `crypto/rand`), build the sealer once with
   `controller.NewAESGCMSealer` (F1) — its result satisfies `node.Store.Sealer`
   too (F2). No `store` import needed (S9).
2. **Wiring.** `cfg.Sealer` at `run.go:318-325` (S2); `Store.Sealer` at
   `run.go:556` (S3) — that covers registry nodes created at run time and
   the node restarted after an erase (F16).
3. **Existing unsealed databases.** Nothing to build: the first start with
   the key seals and scrubs in place (F4, F8). It is one-way (F11). The work
   is documentation: back up before, old backups stay clear (F6), a
   `seal_scrub_owed` warning means "restart once nothing else reads the
   file" (F5).
4. **Start-up errors.** Map by `errors.Is` to an operator message, exit
   non-zero, never delete anything:
   - key file unreadable / not 64 hex digits → before any database is opened;
   - `controller.ErrUnseal` / `node.ErrUnseal` → "sealed under another key" (F7, F8);
   - `controller.ErrNoSealer` → "the database is sealed, set --seal-key-file" (F7).
   **Gap:** for a device node the missing key is not a start-up error by the
   facade's own test (F9). Whether `node.New` fails for a *commissioned*
   sealed store without a sealer is **unverified**. The server test in step 7
   measures it; if `New` succeeds, that is a facade gap to report to
   go-fabric (a "is this store sealed" probe), not something to guess around.
   With `--controller` (the default) the controller's `ErrNoSealer` stops the
   daemon first; a bridge-only daemon is the exposed case.
5. **Rotation.** `--seal-key-previous-file <path>`: at start, before each
   store is opened, `controller.Reseal(db, previous, current)` on
   `fabric-server.db` (the handle of S2, before `controller.New`, F11) and
   `node.Reseal` on each node file, opened as F13 does (not with
   `controller.OpenDB`, which would switch the node file to WAL, F14).
   Result per file: nil → rotated; `ErrUnseal` → file untouched (F10), go on
   to the normal open under the current key, which decides (already rotated,
   or a wrong key → step 4); `ErrScrubIncomplete` → rotated, warn, go on
   (F10). That makes a crash between files harmless on the next start — the
   reason to prefer it over a one-shot mode, since N files cannot be rotated
   in one transaction. Skip a file that does not exist (F11 needs the
   schemas). **Unverified:** what Reseal returns on a database that has the
   schema but was never sealed and holds no secret (godoc names only "a
   secret in clear" → `ErrUnseal`); the test in step 7 pins it.
6. **Docs.** `docs/operations.md`: flags table (`:12` ff.), Files table
   (`:223-233`: the key file, and that `server.json`/`nodes.json` are not
   sealed, S5), the backup section replacing `:256-260` (key outside the
   backup of the database, old backups, one-way, lost key = files removed and
   every device commissioned again, F17), rotation steps, the three log
   lines. Add-on: `addon/config.yaml` option + schema, `script/run.sh`,
   `addon/DOCS.md`, both translations, `addon/CHANGELOG.md`.
   `deploy/systemd/fabric-server.service` comment, `changelog.md`,
   `CLAUDE.md` state, the wave report. Every go-fabric sentence cites F-rows.
7. **Tests** (`internal/daemon`, beside `daemon_test.go`, `nodes_test.go`,
   `multinode_test.go`): key file read/create/mode/bad content; fresh start
   sealed and restart; unsealed database with a kept code → start with key →
   start without key fails `ErrNoSealer`; wrong key → `ErrUnseal` and the
   file unchanged; rotation including a second start with the same two flags
   and a run interrupted after the first file; registry node created, erased
   and restarted under the sealer; the F9 measurement on a commissioned node;
   the step-5 unverified case. Guard: `internal/arch` stays green without a
   new go-fabric package.

## 3. The owner's decisions

| # | Question | Recommendation |
| --- | --- | --- |
| Q1 | Flag shape: `--seal-key-file <path>` or go-fabric's `--seal-key file:<path>`? | `--seal-key-file`: the issue says "key-file flag", and no second scheme is planned. No variable that carries the key itself |
| Q2 | Create a missing key file, as the reference does (F18)? | Yes, create when missing. Whether a store is already sealed cannot be asked through the facade beforehand, so: if the open then fails with `ErrUnseal`, remove the file just created and say the database was sealed under a key that is gone |
| Q3 | Refuse a key file readable by group or others? | Refuse (a start-up error), as the token rules are strict elsewhere; a warning is the softer alternative |
| Q4 | Add-on: where does the key live? `/data` is the only place read here, and it is in the same backup as the database (S8) — the backup then protects nothing. Other add-on folders and `backup_exclude` are **unverified** (Home Assistant's docs not read) | Decide after a read of the Supervisor docs. Leaning: option `seal_databases: bool` (off), key generated under `/data` and excluded from the add-on backup, with DOCS.md saying plainly that a restore without the separately saved key loses the fabric — or leave the add-on out of this wave |
| Q5 | Rotation as start-time flag (step 5) or a do-and-exit mode like `--health-check` (S6)? | Start-time flag: restartable after a crash, usable in a container and the add-on |
| Q6 | Lost or wrong key: only fail, or offer a reset? | Only fail. Recovery is documented and manual (remove the files); the daemon never deletes a fabric on its own |
| Q7 | Fail closed while a scrub is owed (F5)? | No: go-fabric opens and warns; the server documents the warning |
| Q8 | `server.json` (Wi-Fi credentials, Thread datasets) and `nodes.json` (passcodes) stay clear (S5). In scope? | Not in #76: document it, open a follow-up issue |
| Q9 | Should sealing become the default, or required with `--controller-keep-onboarding-codes`? | Opt-in in this wave (one-way migration, F11); with kept codes and no key, a Warn line at start |
| Q10 | Bump to go-fabric v0.28.0 in the same wave? | Not needed (S1, F19); keep the bump separate |
