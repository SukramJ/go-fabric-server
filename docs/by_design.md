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

**B7 — retired in S3.** `get_all_credentials`' Thread entries carry
`networkName` and `extPanId` again (go-fabric's `controller/thread`
dataset codec).

**B8 — retired in S3.** `set_default_fabric_label` is pushed to the
connected nodes (`client.Registry.SetFabricLabel`).

**B9 — `get_matter_fabrics` carries no `vendor_name`.** The entries name
the vendor id only (clone `packages/ws-client/src/models/model.ts:1844-1850`,
`vendor_name` optional): a name needs the vendor table, which this server
reads from the DCL on `get_vendor_names` only, not on every fabric listing.

**B10 — retired in S3.** `get_vendor_names` answers the vendors of
go-fabric's embedded ledger snapshot with the live ledger's over them, as
matterjs-server merges its static table under the DCL's.

**B14 — `enable_auto_subscription` persists.** The choice is kept in the
node's settings across a restart (go-fabric `controller/client/node.go`,
`SetAutoSubscribe`); matterjs-server keeps it in memory only (clone
`ControllerCommandHandler.ts:2488-2562`, "not kept across a restart"): the
facade has one switch, and a server that forgets what a client switched
off would resubscribe a node the client chose to read itself.

**B15 — `set_thread_dataset` reads the dataset.** A dataset must be hex
and MeshCoP TLVs (go-fabric `controller/thread.DecodeDatasetHex`, the port
of matter.js `OperationalDataset.ts`); matterjs-server checks the hex only
(clone `WebSocketControllerHandler.ts:2173-2200`): a dataset no device can
join fails at commissioning time otherwise, far from its cause.

**B16 — `commission_on_network`'s `ip_addr` selects, it does not dial.**
The server searches DNS-SD for the device of the filter that announces the
address; matterjs-server commissions at the address first and falls back
to discovery (clone `ControllerCommandHandler.ts:2052-2095`): go-fabric's
registry commissions a discovered device (`Registry.ForDevice`), and the
facade has no commissioning by bare address.

**B17 — `discover` answers what a device announced.** Absent TXT keys are
absent; matterjs-server fills them with `""`, `0` or `-1` (clone
`ControllerCommandHandler.ts:2135-2185`): a default of its own would read
as an announced value, and every key of the python client's
`CommissionableNodeData` has a default (models.py:256-277).

**B18 — a 4-byte float is its shortest decimal.** A TLV float32 renders
as the shortest decimal that reads back as that float (`0.1`,
go-fabric `client.DecodeJSON`); matterjs-server prints the double
expansion of the JavaScript number matter.js decodes it into
(`0.10000000149011612`): the shorter form is the device's value, and a
client parses both to the same float32.

**B19 — `get_network_topology` adds `ip_addresses`.** A `matter` node
carries the node's known addresses (brief S3 §3); matterjs-server's node
has none (models.py:318-347): the topology page is where an operator looks
for an address, and an old client ignores the key.

**B20 — BLE commissioning needs a reason to use BLE.** With an adapter,
`commission_with_code` takes BLE only when the code does not rule it out
(a QR code without the BLE capability does) and a stored Wi-Fi or Thread
credential can put the device on its network; matterjs-server takes BLE
whenever it is enabled and `network_only` is false (clone
`ControllerCommandHandler.ts:1965-2115`): go-fabric's BLE route requires a
network to provision (`controller.PairOptions.BLE`), and a device already
on the network is found over DNS-SD.

**B21 — `open_commissioning_window`'s `timeout` is at most 65535.** Above
it the command is `InvalidArguments`; matterjs-server sets no maximum and
leaves the error to matter.js (clone `WebSocketControllerHandler.ts:2316-2330`):
the command's field is 16 bits (go-fabric `client.Node.OpenCommissioningWindow`).

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
topology: a key the document no longer names is removed and its endpoint
number forgotten (go-fabric `node/reserve.go`, `Batch.Forget`): a host
that restarts declares what it has, and the node
forgets the endpoint numbers of what it no longer has without a second
command; an unchanged device is left as it is, so its subscriptions and
endpoint stay.
