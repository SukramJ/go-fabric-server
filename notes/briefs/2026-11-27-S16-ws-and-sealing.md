# Brief S16 — /ws availability, load-failed nodes, sealing (2026-11-27)

Issues #81, #75, #74, #76 and the bump's follow-up `commitErrors`. One
agent. Copy this brief to `notes/briefs/2026-11-27-S16-ws-and-sealing.md`
and commit it together with the two untracked Fable notes
(`notes/reads/read-2026-11-27-issues-81-75-ws.md`,
`notes/reads/read-2026-11-27-issue-76-sealer.md`) as the first commit.

## Branch

`git fetch origin && git switch -c s16-ws-sealing origin/main` (main
carries go-fabric v0.28.0 / go-fabric-ui v0.16.0 from #83). Author
`me@sukramj.dev`. Commits only — **no push, no pull request, no tag, no
issue comment or close.**

## Read

- `CLAUDE.md` of this repository (rules, guard set, report shape). It wins
  over this brief where they differ; say where.
- The two Fable notes above — they are the specification. Every sentence
  about matterjs-server, matter.js or go-fabric that you write into
  `docs/`, a comment or the report comes from a note row and cites the
  line the note cites. Nothing beyond the notes; a fact you need and the
  note does not carry goes under Questions as unverified.
- Only the files the sections name, in slices.

## Owner decisions (2026-11-27)

- #81: align with matterjs-server.
- #75: keep load-failed nodes hidden; `remove_node` on such a node calls
  `Registry.Forget` — a deliberate difference (going ahead), recorded in
  `docs/protocol.md`.
- #74: not over `/ws`. Record in `docs/protocol.md` only.
- #76: daemon only; `--seal-key-file` opt-in plus a rotation flag. The
  add-on and the clear `server.json` / `nodes.json` are follow-up issues
  (the session opens them, not you). No add-on change in this wave.
- #76 note §3, taken with Fable's recommendation (owner may overrule at
  review): Q1 `--seal-key-file <path>`; Q2 create a missing key file, and
  if the first open then fails with `ErrUnseal`, remove the just-created
  file and say so; Q3 refuse a key file readable by group/others; Q5
  rotation as start-time flag `--seal-key-previous-file <path>`; Q6 only
  fail, never delete; Q7 do not fail closed on an owed scrub; Q9 opt-in,
  a Warn line at start when codes are kept without a key; Q10 no bump.

## Sections (one commit or more each)

1. **#81 availability** — note "#81 / Change for the implementer" items
   1–5 and its test list. Pure rule function, table-tested; `restored` on
   `nodeState`; `primed` = was Connected once; harness `waitAvailable`
   (`controllerapi/harness_test.go:211-231`) waits for something that
   means primed. `docs/protocol.md:188-189`, `:220-222` and the two
   recorded differences (same-pass changes; topology `available` as an
   open question). Item 6 (`update_node` gate): check and report; do not
   change behaviour.
2. **#75 load-failed nodes** — note "#75 / Change for the implementer":
   `cmdRemoveNode` only, `Registry.Forget`, no event; `docs/protocol.md`
   sentence (hidden, as matterjs-server) and the going-ahead entry; the
   test list. If producing a load-failed node in a test needs anything
   below the facade, stop and report (the import guard holds).
3. **#74** — one `docs/protocol.md` entry: the onboarding-code inventory
   is not on `/ws` (token weaker than the pages' admin gate; matterjs-server
   has no such command), served only by the pages with `Options.Admin`.
   Cite the issue's facts only.
4. **#76 sealing** — note §2 steps 1–7 with the decisions above. New
   `internal/daemon/sealkey.go`; wiring at `run.go` (S2, S3 of the note);
   start-up error mapping; rotation per file as step 5; docs step 6
   **without** the add-on parts; tests step 7, including the F9
   measurement on a commissioned node and the step-5 unverified case —
   the test pins what is measured; if the facade lets a commissioned
   sealed node start without a sealer, write that as a facade gap under
   Questions in the *Work that needs go-fabric* shape, do not work around.
5. **`commitErrors`** (`bridgeapi/commands.go:168`,
   `bridgeapi/commiterrors_internal_test.go:20`): move to the v0.28.0
   error shape (`*node.BatchError`, read its godoc at v0.28.0 in the module
   cache), behaviour on the wire unchanged unless the godoc forces it —
   then report.
6. **Changelog** under `[Unreleased]`; `CLAUDE.md` state line if the repo
   keeps one; report `docs/reports/S16.md` in the fixed shape. No release.

## Guards

The full local set of `CLAUDE.md` (the pipeline's jobs), in the background
with a summary log; on macOS the 386 build is `GOOS=linux GOARCH=386`, the
386 tests run on CI only. `make cover-check` must hold (ratchet up where
coverage rose, as the repo does).

## Report back

Fixed shape: per section the commits; guards with HEAD; differences from
the brief; decisions taken; left out; questions (the topology question,
the `update_node` gate, F9 and step-5 measurements, any facade gap). Short.
