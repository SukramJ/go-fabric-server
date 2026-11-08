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
  worked around here; the shape of that report is *Work that needs go-fabric*
  below.
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

Go 1.27.3, `CGO_ENABLED=0`. Dependencies: go-fabric (and its ble/bluez and ble/esphome), coder/websocket,
go-fabric-ui — every further one is a decision recorded in
`THIRD-PARTY-NOTICES.md`.

## The architecture standard

Adopted on 2026-11-05 from go-fabric's `CLAUDE.md` (*The architecture
standard*, set by the owner on 2026-10-20, in the owner's words:
"hervorragende Architektur, zukunftsweisend, innovativ, herausragend"), which
holds its whole family to it, this server included.

- **Outstanding, not adequate.** Every layer is the best design we can state,
  coherent with the others, and judged as API design: one shape, no
  convenience seam. The smallest change is not by itself the best one.
- **Forward-looking.** A decision is weighed against where Matter, the
  ecosystems and the consumers are going, not only against today's need.
  Going ahead of matterjs-server is allowed and recorded in
  `docs/protocol.md`; copying a limitation is not.
- **Better than others, better than before, the right cut.** Every concept
  answers three questions in its own section: better than the comparison
  targets (matterjs-server, matter.js, chip's apps)? better than the state
  before? is the split between go-fabric, go-fabric-ui, this server and the
  host right? A concept without that section is not finished.
- **The check is stated, not assumed.** When a decision is put to the owner,
  the recommendation says whether it is the best architecture and why. When
  the owner's choice is not the best architecture in the assistant's
  judgement, the assistant says so with its reasons before implementing; the
  owner decides and the reasons are on record. A scope boundary that is a
  decision is kept unless the owner reopens it.

## Work that needs go-fabric

Adopted on 2026-11-05 from go-fabric's `CLAUDE.md` (*Work that starts in a
host*). This server is a host of go-fabric.

- **Matter rules live in go-fabric.** A rule decided while working here (how
  a server must behave, how a divergence is recorded) is written into
  go-fabric's `CLAUDE.md` or `notes/parity/`, not only into this repository's
  notes; this repository's documents point there.
- **Implementation go-fabric needs is handed over in a go-fabric issue**,
  complete enough to continue there without this session's context: why (this
  server's need, in go-fabric's terms), the verified sources with `file:line`
  at the pins (matter.js at `parity/schema.json` → `matter.sourceCommit`,
  connectedhomeip at the harness commit), the scope and the port or facade
  shape, the parity tests to port, the `by_design.md` row it adds, acceptance
  criteria, what stays here, and the open questions.
- **This server's follow-up** is a separate issue in this repository that
  references the go-fabric one. This repository does not implement in
  go-fabric from its own session.

## Which model does which work

Set on 2026-10-31, adopted from go-fabric's `CLAUDE.md`. The rule follows
from the wire-contract discipline above: a statement about matterjs-server
or go-fabric behaviour that lands in the repo (`docs/protocol.md`,
`docs/by_design.md`, a report's decisions) is cited later as fact, so the
work that produces such statements gets the strongest model, and the work a
guard checks may use a cheaper one. Every brief in `notes/briefs/` names its
model in the header (`**Model:** …`); an agent runs on the model the brief
names.

| Work | Model |
| --- | --- |
| Analysis, planning, briefs; anything written into `docs/protocol.md`, `docs/by_design.md`, `CONCEPT.md`, a report's *Decisions* or *Questions*, or a comment citing a matterjs-server or go-fabric line; a facade-gap report to go-fabric; rebases and family bumps where go-fabric or go-fabric-ui may have moved under the change; the commissioning and ACL commands (`controllerapi/cmd_commission.go`, `cmd_acl.go`) and the daemon's HTTP surface (`internal/daemon/http.go`, `web.go`) | **Fable** (the default for this repo) |
| Implementation of a finished brief with guards behind it: a command whose wire shape the read map already fixes, the add-on and its docs, a `changelog.md` entry for a finished diff, the bundle report in the fixed shape | **Opus** (also what fast mode runs on) |
| **Review of a wave's pull request, split (owner decision 2026-11-08, go-fabric #456).** The code review — correctness, concurrency, error paths, tests, the fixed report shape — is **Opus**. On top of it **Fable** reads, bounded: every new citation line against the pinned source (matterjs-server at its pin, go-fabric at its tag), every line that enters `docs/protocol.md`, `docs/by_design.md` or `CONCEPT.md`, the commissioning, ACL and HTTP paths of the first row, and the brief's decisions against matterjs-server — the behavioural gaps a code review does not see. The load-bearing claims of the Opus review are verified before they reach the implementer. | **Opus**, then **Fable** |
| Work a test or generator checks immediately: `make generate`, `gofumpt`, regenerating a ratchet, adding a test to an existing pattern, running `make compat-python`; search and first reading of matterjs-server, go-fabric or this tree whose result a stronger model or the owner reads again | **Sonnet**, including as the model of a delegated search subagent |

Sonnet never writes a fact about matterjs-server or go-fabric into the repo,
and never touches the commissioning, ACL or HTTP paths named in the first
row. When in doubt about which row a task is in, it is in the first.

## Token use

Adopted from go-fabric (owner, 2026-10-14: "Wichtig ist mir eine effiziente
Nutzung der Token"). A bundle costs on the order of half a million to a
million tokens per implementing agent before the owner's review; the rules
below are what keeps that from doubling.

- **Read slices, not files.** `sed -n a,bp`, `grep -n` with a tight
  pattern and `head`; a whole file only when the task is the file. Never
  re-read what is already in context; cite the earlier read.
- **Delegate first reads.** A source tree's inventory (matterjs-server,
  go-fabric, this repo) is a Sonnet read written to `notes/reads/read-*.md`
  with `file:line` for every fact (`read-v8-matterjs-server.md` is the
  pattern); the owner spot-checks the load-bearing lines and verifies every
  matterjs-server or go-fabric fact before it enters a brief,
  `docs/protocol.md` or `docs/by_design.md` — Sonnet's result is a map,
  never a citation.
- **Logs by their summary.** Guards (`make ci`, `make race`,
  `make cover-check`, `make compat-python`) run in the background with a
  summary log (HEAD before and after, one `exit` line per guard); read the
  summary, and open a guard's log only on failure, with `grep` for `FAIL`,
  `DATA RACE`, `_test.go:` and the error strings — never `cat`. The same
  for CI: the job log is fetched once to a file and grepped.
- **One turn, every independent call.** Reads, checks and background
  starts that do not depend on each other go out together; a wait is a
  `Monitor` or an `until` loop in the background, not polling by hand.
- **Short artefacts.** A brief names decisions and sections, not prose; a
  report follows the fixed shape of `docs/reports/` (per §, Guards,
  Differences from the brief, Decisions, Left out, Questions, by_design
  candidates); a `by_design.md` entry is one paragraph with the citations
  (map line, clone `file:line`) and one sentence of reasoning; status
  messages to the owner are short, outcome first, in German.
- **Skip what cannot have changed.** No `make compat-python` when `/ws`
  did not move; no `make generate` for a comment change; no second guard
  run for a notes-only commit — say what was skipped and why.
- **Edit by script.** Several replacements in several files are one
  scripted call with asserted anchors, not a chain of interactive edits; a
  generated file (`tables_gen.go`, `schema.json`, `clusters_gen.go`) is
  regenerated, never patched.
- **Agents read what the brief names.** An implementing agent gets the
  file list and the pinned sources in the brief, reads those, and reports
  in the fixed shape; it does not survey the tree. The brief is copied to
  `notes/briefs/` in the bundle's first commit. One agent per brief; two
  agents only on disjoint paths.
- **Push, pull request and tag are the session's own calls.** An
  implementing agent works locally — branch, commits, guards — and reports;
  the owner's session reads the diff, pushes, opens the pull request with
  auto-merge and tags after CI. A delegated push, pull request or tag is
  refused by the auto-mode classifier ("Create Public Surface",
  2026-11-07), and the review above sits between the agent's report and the
  push anyway.
- **Agents read what the brief names.** An implementing agent gets the
  file list and the pinned sources (go-fabric tag, matterjs-server clone
  commit) in the brief, reads those, and reports in the fixed shape; it
  does not survey the tree. One agent per brief; two agents only on
  disjoint paths, and a release commit (`internal/version.Version` plus
  the changelog section) is one agent's at a time.
