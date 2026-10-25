# Deliberate differences

Each entry is a difference from matterjs-server's wire behaviour (the
controller half, `/ws`) or a design rule of the bridge half (`/bridge`)
that a reader could take for a defect. Citations: the read map
`notes/reads/read-v8-matterjs-server.md` ("map *line*") and the
matterjs-server clone at `a7922e85` ("clone *file:line*"). A gap that is
not deliberate is a question in a bundle report, not an entry here.

## Controller half

**B1 — `ping_node` is a Matter read.** Each address of the node is
answered with the outcome of a Matter read of the node, not of ICMP echoes
(clone `packages/ws-controller/src/controller/ControllerCommandHandler.ts:2261-2300`;
map 1795-1806): the daemon runs non-root in a distroless image, without the
raw sockets an echo needs.

**B2 — a missing `node_id` is `InvalidArguments` for every command.** A
missing or non-numeric `node_id` answers error 8 everywhere; matterjs-server
answers `UnknownError` for some node-targeted commands (map 4196-4201).
python-matter-server's argument check is the contract Home Assistant was
written against, and one rule for every command is the smaller surface.

**B3 — a `node_id` that can never name a node is `NodeNotExists`.** A group,
CAT or reserved node id answers error 5, not error 8 (map 4202-4211): the
node id classes are go-fabric's to name, and the server has no table of
them.

**B4 — path ids are checked by width.** Endpoints up to 0xFFFE, clusters
and elements up to 0xFFFFFFFE are accepted, not checked against matter.js's
id classes (map 2382-2383): the classes are Matter knowledge the server
does not keep; the node answers an id it does not have.

**B5 — name-based structs carry the property name only.** Command responses
and event data key a struct member by its matter.js property name;
matterjs-server also writes chip's acronym-preserving alias where it differs
(map 2504-2516, `wire-naming.ts`): the alias table is not in go-fabric's
snapshot, and one name per member is what a client matches.

**B6 — `write_attribute` waits for the device's report.** The node data
changes with the device's next report, not when the write is answered
(clone `docs/websockets_api.md:604-606`): the device decides what it
stored, and the report is its word.

**B7 — `get_all_credentials`' Thread entries carry only `id`.** No network
name and extended PAN id (map 1504): reading them needs a parser of the
operational dataset that go-fabric's facade does not export (report S1,
Q13).

**B8 — `set_default_fabric_label` is stored, not pushed.** The label is
kept in `server.json` and reaches the fabric on the controller's next first
run (map 1523): go-fabric's facade has no route that relabels a running
fabric on its nodes (report S1, Q8).

**B9 — `get_matter_fabrics` carries no `vendor_name`.** The entries name
the vendor id only (clone `packages/ws-client/src/models/model.ts:1844-1850`,
`vendor_name` optional): a name needs the vendor table, which this server
reads from the DCL on `get_vendor_names` only, not on every fabric listing.

**B10 — `get_vendor_names` answers from the DCL only.** matterjs-server
merges its own static vendor table under the ledger's
(clone `packages/ws-controller/src/server/WebSocketControllerHandler.ts:1695-1728`,
`packages/ws-controller/src/data/VendorIDs.ts`): a vendor table is Matter
data the server does not keep of its own, so without the ledger the command
fails with `UnknownError` instead of answering a partial list.

## Bridge half

**B11 — one bridge client at a time.** A second `/bridge` connection gets
an error frame naming the holder (error 200) and the close status 4409
(brief S2 §1.1): the daemon serves one node, and two authors of its topology
would undo each other's declarations; a reconnecting host is served once the
old connection is gone.

**B12 — writes and commands reach the host only on opt-in.** Without
`handle_writes`/`handle_commands` the kit applies a write and answers a
command from its state, and the host sees `changed` (brief S2 §1.2,
correcting concept v8 C.3's command-only model): a host that only mirrors
state would otherwise have to answer every controller request in time, and
a host that is slow or away would make every command fail.

**B13 — a declaration is the whole topology.** `declare` replaces the
topology: a key the document no longer names is removed and the topology
is marked complete (go-fabric `node/aggregator.go`, `Batch`,
`MarkComplete`): a host that restarts declares what it has, and the node
forgets the endpoint numbers of what it no longer has without a second
command; an unchanged device is left as it is, so its subscriptions and
endpoint stay.
