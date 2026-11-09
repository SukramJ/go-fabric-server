// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
)

// nodeSpec is one device node of the bridge half: the node of the
// --bridge-* flags under the key "default", or a node of the registry
// (nodes.json, registry.go). The key is the node's identity
// (docs/by_design.md B23): it names the node in the pages' selector, in
// /bridge/<key> and in its store file.
type nodeSpec struct {
	Key, Listen, Label                           string
	Passcode, Discriminator, VendorID, ProductID uint
	// Store is the node's store file under --storage-path, Serial its
	// SerialNumber (an input of the UniqueID the facade derives, so it
	// differs per node).
	Store, Serial string
}

// nodeKey is a registry node's key: go-fabric-ui's NodeEntry.Key pattern
// (^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$), narrowed to 28 bytes so the
// SerialNumber "FSB-<key>" stays within BasicInformation's "max 32"
// (go-fabric parity/schema.json, BasicInformation SerialNumber, id 15).
var nodeKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,27}$`)

// pageNames are the node family's page names, which go-fabric-ui's
// NodeEntry.Key must not be (its doc at v0.6.0).
var pageNames = []string{"status", "pair", "fabrics", "selection", "endpoints", "endpoint", "surface", "diagnostics", "topology"}

// primaryNode is the node of the --bridge-* flags, the default node:
// key "default", store bridge.db, serial FABRIC-SERVER-BRIDGE.
func primaryNode(c Config) nodeSpec {
	return nodeSpec{
		Key: fabricui.DefaultNodeKey, Listen: c.BridgeListen, Label: c.BridgeLabel,
		Passcode: c.BridgePasscode, Discriminator: c.BridgeDiscriminator,
		VendorID: c.BridgeVendorID, ProductID: c.BridgeProductID,
		Store: "bridge.db", Serial: "FABRIC-SERVER-BRIDGE",
	}
}

// registrySpec is the spec of a registry node: its own key, label,
// listen address, discriminator and passcode; the vendor and product of
// the --bridge-* node; the store bridge-<key>.db and the serial FSB-<key>.
func registrySpec(primary nodeSpec, e *registryEntry) nodeSpec {
	return nodeSpec{
		Key: e.Key, Listen: e.Listen, Label: e.Label,
		Passcode: uint(e.Passcode), Discriminator: uint(e.Discriminator),
		VendorID: primary.VendorID, ProductID: primary.ProductID,
		Store: registryStore(e.Key), Serial: "FSB-" + e.Key,
	}
}

// registryStore is a registry node's store file under --storage-path.
func registryStore(key string) string { return "bridge-" + key + ".db" }

// splitListen is a listen address's host and UDP port ("" is
// node.Network's :5540).
func splitListen(addr string) (host string, port int, err error) {
	if addr == "" {
		return "", 5540, nil
	}
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err = strconv.Atoi(p)
	if err != nil || port < 0 || port > 65535 {
		return "", 0, fmt.Errorf("port %q", p)
	}
	return host, port, nil
}

// removedFlag is a flag this release no longer has: setting it, on the
// command line or through its environment variable, stops the start with
// the flag's migration instead of flag's "flag provided but not defined".
type removedFlag string

func (removedFlag) String() string     { return "" }
func (f removedFlag) Set(string) error { return fmt.Errorf("removed: %s", string(f)) }

// liveNode is one run of a device node: the node and the bridge half
// bound to it. Both are single-use — a stopped node.Node does not start
// again (go-fabric node/factory_reset.go:30-33 at v0.18.0), nor does a
// bridgeapi.Server (Server.Start) — so an erase replaces the pair.
type liveNode struct {
	node *node.Node
	srv  *bridgeapi.Server
}

// bridgeHalf is one device node under its key, across its erases: its
// spec, the run that serves it now, and the pages' source, a
// fabricui.NodeSwitch over the current run's node (go-fabric-ui 0.7.1),
// so the pages keep one source while an erase remakes the node. The
// nodeLifecycle drives it; nothing else starts, erases or stops its node.
type bridgeHalf struct {
	spec nodeSpec
	sw   *fabricui.NodeSwitch
	// mu serialises the half's lifecycle: an erase, the end of a
	// registry node, the daemon's stop.
	mu  sync.Mutex
	cur atomic.Pointer[liveNode]
	// failed is the last erase's failure — the erase or the restart
	// after it —, nil while a node runs.
	failed atomic.Pointer[eraseFailure]
	// ended (under mu): the half is stopped or deleted for good, and an
	// erase no longer brings it back.
	ended bool
}

// eraseFailure is an erase that did not bring the node back: its warning
// code (eraseFailedCode, restartFailedCode), the error, and when the host
// first saw that code (fabricui.Warning.Since).
type eraseFailure struct {
	code  string
	err   error
	since time.Time
}

// rename sets the half's label: the spec an erase restarts the node with,
// and the running node's NodeLabel (Node.SetNodeLabel). An ended half
// changes its spec only.
func (h *bridgeHalf) rename(ctx context.Context, label string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.spec.Label = label
	if run := h.live(); run != nil && !h.ended {
		return run.node.SetNodeLabel(ctx, label)
	}
	return nil
}

// fail records an erase's failure; a failure of the same kind again keeps
// its first sighting. h.mu is held.
func (h *bridgeHalf) fail(code string, err error, now time.Time) {
	if prev := h.failed.Load(); prev != nil && prev.code == code {
		now = prev.since
	}
	h.failed.Store(&eraseFailure{code: code, err: err, since: now})
}

// live is the run that serves the half now.
func (h *bridgeHalf) live() *liveNode { return h.cur.Load() }

// stop stops the half's current node at the daemon's shutdown — the one
// an erase brought back, if any. Run shuts the halves' servers down with
// their routes.
func (h *bridgeHalf) stop(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ended = true
	_ = h.live().node.Stop(ctx)
}

// eraseTimeout bounds an erase, as the registry's Delete: a page that goes
// away does not leave a node half erased.
const eraseTimeout = 30 * time.Second

// The pages' warning codes of a node an erase did not bring back
// (fabricui.Warning.Code): the erase failed (node.FactoryReset; the node
// is not restarted), or the new node did not start.
const (
	eraseFailedCode   = "node_erase_failed"
	restartFailedCode = "node_restart_failed"
)

// nodeLifecycle starts, erases and ends the device nodes — the default
// node and the registry's — in one place: each node through startFunc on
// the daemon's context, its bridge half at its route, its source the
// half's NodeSwitch.
type nodeLifecycle struct {
	ctx    context.Context // the daemon's: the nodes' lifetime
	start  startFunc
	routes *bridgeRoutes
	log    *slog.Logger

	mu     sync.Mutex
	halves []*bridgeHalf // started and not ended, the default first
}

func newNodeLifecycle(ctx context.Context, start startFunc, routes *bridgeRoutes, log *slog.Logger) *nodeLifecycle {
	return &nodeLifecycle{ctx: ctx, start: start, routes: routes, log: log}
}

// startHalf starts the node of s with its bridge half and serves the half
// at /bridge/<key> (and /bridge for the first).
func (l *nodeLifecycle) startHalf(s nodeSpec) (*bridgeHalf, error) {
	run, err := l.start(l.ctx, s)
	if err != nil {
		return nil, err
	}
	h := &bridgeHalf{spec: s, sw: fabricui.NewNodeSwitch(run.node)}
	h.cur.Store(&run)
	l.routes.add(BridgeHalf{Key: s.Key, Server: run.srv})
	l.mu.Lock()
	l.halves = append(l.halves, h)
	l.mu.Unlock()
	return h, nil
}

// end ends h for good: its route is gone, its client receives
// server_shutdown, and its node stops — with reset, after removing every
// fabric (node.FactoryReset: each controller's Leave event) and erasing
// its store.
func (l *nodeLifecycle) end(ctx context.Context, h *bridgeHalf, reset bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ended = true
	l.mu.Lock()
	l.halves = slices.DeleteFunc(l.halves, func(o *bridgeHalf) bool { return o == h })
	l.mu.Unlock()
	l.routes.remove(h.spec.Key)
	run := h.live()
	run.srv.Shutdown(ctx)
	if reset {
		err := run.node.FactoryReset(ctx)
		if err == nil {
			return
		}
		l.log.Warn("fabricserver.registry.reset_failed", slog.String("node", h.spec.Key), slog.Any("error", err))
	}
	_ = run.node.Stop(ctx)
}

// eraser is the pages' Erase of h (fabricui.Options.Erase,
// NodeEntry.Erase).
func (l *nodeLifecycle) eraser(h *bridgeHalf) func(context.Context) error {
	return func(ctx context.Context) error { return l.erase(ctx, h) }
}

// erase is node.FactoryReset of h's node, then a new node on the erased
// store, served again (go-fabric node/factory_reset.go:30-33 at v0.18.0:
// the host builds a new node on the same store, as after a restart;
// fabrictest/device.go:396-416 the pattern): the bridge client receives
// server_shutdown and dials the same route back, the pages follow the
// switch. Either failure keeps the daemon up and is h's warning on every
// status page: a failed erase restarts nothing (node_erase_failed); a new
// node that does not start leaves the switch on the stopped node
// (node_restart_failed). An erase again — FactoryReset of a stopped node
// erases its store again — retries both.
func (l *nodeLifecycle) erase(ctx context.Context, h *bridgeHalf) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), eraseTimeout)
	defer cancel()
	h.mu.Lock()
	defer h.mu.Unlock()
	key := h.spec.Key
	if h.ended {
		return fmt.Errorf("%s is ended", nodeName(key))
	}
	log := l.log.With(slog.String("node", key))
	old := h.live()
	old.srv.Shutdown(ctx)
	if err := old.node.FactoryReset(ctx); err != nil {
		h.fail(eraseFailedCode, err, time.Now())
		log.Error("fabricserver.node.erase_failed", slog.Any("error", err))
		return fmt.Errorf("%s: erase: %w", nodeName(key), err)
	}
	// The node lives as long as the daemon, not as the request that
	// erased it.
	run, err := l.start(l.ctx, h.spec) //nolint:contextcheck // the daemon's context, above
	if err != nil {
		h.fail(restartFailedCode, err, time.Now())
		log.Error("fabricserver.node.restart_failed", slog.Any("error", err))
		return fmt.Errorf("%s: erased, not restarted: %w", nodeName(key), err)
	}
	l.routes.replace(key, run.srv)
	h.cur.Store(&run)
	h.sw.Set(run.node)
	h.failed.Store(nil)
	log.Info("fabricserver.node.erased")
	return nil
}

// warnings is the pages' Options.Warnings: one node_erase_failed or
// node_restart_failed per node an erase did not bring back, naming its key (the sibling handlers
// answer the host's one hook, so the message names the node).
func (l *nodeLifecycle) warnings(context.Context) []fabricui.Warning {
	l.mu.Lock()
	hs := slices.Clone(l.halves)
	l.mu.Unlock()
	var out []fabricui.Warning
	for _, h := range hs {
		if f := h.failed.Load(); f != nil {
			out = append(out, fabricui.Warning{Code: f.code, Message: h.spec.Key + ": " + f.err.Error(), Since: f.since})
		}
	}
	return out
}

// BridgeHalf is one bridge half under its node's key, for MuxNodes.
type BridgeHalf struct {
	Key    string
	Server *bridgeapi.Server
}

// bridgeRoutes are the bridge halves /bridge and /bridge/<key> serve, the
// default node's first: a registry node's half is added when the node is
// created and removed when it is deleted, while the daemon runs.
type bridgeRoutes struct {
	mu     sync.RWMutex
	halves []BridgeHalf
}

func newBridgeRoutes(halves ...BridgeHalf) *bridgeRoutes {
	return &bridgeRoutes{halves: append([]BridgeHalf(nil), halves...)}
}

// all is a copy of the halves, the default first.
func (b *bridgeRoutes) all() []BridgeHalf {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]BridgeHalf(nil), b.halves...)
}

// get is the half of key, or the default node's for "" (/bridge).
func (b *bridgeRoutes) get(key string) *bridgeapi.Server {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i, h := range b.halves {
		if h.Key == key || (key == "" && i == 0) {
			return h.Server
		}
	}
	return nil
}

func (b *bridgeRoutes) add(h BridgeHalf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.halves = append(b.halves, h)
}

// replace serves srv as key's half in its place: the default stays first.
func (b *bridgeRoutes) replace(key string, srv *bridgeapi.Server) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.halves {
		if b.halves[i].Key == key {
			b.halves[i].Server = srv
			return
		}
	}
}

func (b *bridgeRoutes) remove(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, h := range b.halves {
		if h.Key == key {
			b.halves = append(b.halves[:i:i], b.halves[i+1:]...)
			return
		}
	}
}
