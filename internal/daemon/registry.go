// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/node"
)

// registryFile is the node registry's file under --storage-path.
const registryFile = "nodes.json"

// defaultNodeLimit is --node-limit's default: the registry nodes the
// pages may create beside the default node.
const defaultNodeLimit = 8

// registryEntry is one node of the registry, as nodes.json holds it — the
// fields go-ha2fabric's registry holds (go-fabric-ui
// docs/concept-u6-nodes.md C.3): the key, the operator's label, and what
// the daemon allocated at its creation.
type registryEntry struct {
	Key           string    `json:"key"`
	Label         string    `json:"label"`
	Listen        string    `json:"listen"`
	Discriminator uint16    `json:"discriminator"`
	Passcode      uint32    `json:"passcode"`
	CreatedAt     time.Time `json:"created_at"`
}

// registryData is nodes.json.
type registryData struct {
	Nodes []registryEntry `json:"nodes"`
}

// startFunc builds and starts one device node with its bridge half
// (startBridge in the daemon; a test's site device in multinode_test.go).
type startFunc func(ctx context.Context, s nodeSpec) (bridgeHalf, error)

// nodeRegistry is fabricui.NodeRegistry over nodes.json (go-fabric-ui
// contract 0.6.0): the device nodes beside the default node, which the
// operator creates, renames and deletes in the pages. The first node is
// the --bridge-* configuration; every further node is registry state.
// Each registry node is a Matter node of its own — its port, its
// discriminator, its passcode, its store bridge-<key>.db — and its bridge
// half is served at /bridge/<key> from its creation to its deletion.
type nodeRegistry struct {
	ctx     context.Context // the daemon's: the nodes' lifetime
	dir     string
	limit   int
	primary nodeSpec
	start   startFunc
	routes  *bridgeRoutes
	log     *slog.Logger
	// probe reports whether a UDP address can be bound, for the port
	// allocation; a variable for the tests.
	probe func(ctx context.Context, addr string) bool

	// mu serialises the writes (Create, Rename, Delete, Close); list is
	// the entries as List answers them, swapped after each write.
	mu      sync.Mutex
	entries []registryEntry
	running map[string]*bridgeHalf
	list    atomic.Pointer[[]fabricui.NodeEntry]
}

// registryOptions configure openRegistry.
type registryOptions struct {
	dir     string
	limit   int
	primary nodeSpec
	start   startFunc
	routes  *bridgeRoutes
	log     *slog.Logger
}

// openRegistry reads nodes.json under dir (none is an empty registry);
// an entry that is malformed — an invalid or duplicated key, a
// discriminator or port another node holds — stops the start, naming the
// file.
func openRegistry(ctx context.Context, o registryOptions) (*nodeRegistry, error) {
	r := &nodeRegistry{
		ctx: ctx, dir: o.dir, limit: o.limit, primary: o.primary, start: o.start, routes: o.routes, log: o.log,
		probe: udpFree, running: map[string]*bridgeHalf{},
	}
	path := filepath.Join(o.dir, registryFile)
	b, err := os.ReadFile(path) //nolint:gosec // the operator's --storage-path
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("node registry: %w", err)
	default:
		var d registryData
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, fmt.Errorf("node registry %s: %w", path, err)
		}
		for i := range d.Nodes {
			if err := r.check(&d.Nodes[i], d.Nodes[:i]); err != nil {
				return nil, fmt.Errorf("node registry %s: %w", path, err)
			}
		}
		r.entries = d.Nodes
	}
	r.publish()
	return r, nil
}

// check validates e against the default node and the entries before it.
func (r *nodeRegistry) check(e *registryEntry, before []registryEntry) error {
	switch {
	case !nodeKey.MatchString(e.Key) || e.Key == fabricui.DefaultNodeKey || slices.Contains(pageNames, e.Key):
		return fmt.Errorf("node %q: the key must match %s and be neither %q nor a page name", e.Key, nodeKey, fabricui.DefaultNodeKey)
	case strings.TrimSpace(e.Label) == "":
		return fmt.Errorf("node %q: the label is empty", e.Key)
	case uint(e.Discriminator) == r.primary.Discriminator:
		return fmt.Errorf("node %q: discriminator %d is the default node's", e.Key, e.Discriminator)
	case samePort(e.Listen, r.primary.Listen):
		return fmt.Errorf("node %q: listen %q takes the default node's port", e.Key, e.Listen)
	}
	for _, o := range before {
		switch {
		case o.Key == e.Key:
			return fmt.Errorf("node %q is listed twice", e.Key)
		case o.Discriminator == e.Discriminator:
			return fmt.Errorf("node %q: discriminator %d is node %q's as well", e.Key, e.Discriminator, o.Key)
		case samePort(o.Listen, e.Listen):
			return fmt.Errorf("node %q: listen %q takes node %q's port", e.Key, e.Listen, o.Key)
		}
	}
	return nil
}

// samePort reports whether two listen addresses name one fixed UDP port
// ("" is node.Network's :5540; port 0 is an ephemeral one and never
// collides).
func samePort(a, b string) bool {
	_, pa, errA := splitListen(a)
	_, pb, errB := splitListen(b)
	return errA == nil && errB == nil && pa != 0 && pa == pb
}

// startAll starts every node of the registry, in its order. A node that
// fails to start is logged and listed as not enabled — the pages still
// reach it, to delete it — and the daemon runs on: a registry node is
// state, and a start error of one must not lock the operator out of the
// pages that fix it.
func (r *nodeRegistry) startAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.entries {
		e := &r.entries[i]
		h, err := r.start(r.ctx, registrySpec(r.primary, e))
		if err != nil {
			r.log.Error("fabricserver.registry.start_failed", slog.String("node", e.Key), slog.Any("error", err))
			continue
		}
		r.running[e.Key] = &h
		r.routes.add(BridgeHalf{Key: e.Key, Server: h.srv})
	}
	r.publish()
}

// shutdownHalves sends server_shutdown to every registry node's client.
func (r *nodeRegistry) shutdownHalves(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.running {
		h.srv.Shutdown(ctx)
	}
}

// stopAll stops every running registry node, the last first.
func (r *nodeRegistry) stopAll(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.entries) - 1; i >= 0; i-- {
		if h := r.running[r.entries[i].Key]; h != nil {
			_ = h.node.Stop(ctx)
		}
	}
	clear(r.running)
}

// List is the registry's nodes, in their order of creation, from memory.
func (r *nodeRegistry) List() []fabricui.NodeEntry {
	return slices.Clone(*r.list.Load())
}

// publish rebuilds List's answer; r.mu is held, or r is not shared yet.
func (r *nodeRegistry) publish() {
	out := make([]fabricui.NodeEntry, 0, len(r.entries))
	for i := range r.entries {
		e := &r.entries[i]
		ne := fabricui.NodeEntry{Key: e.Key, Label: e.Label, Kind: "bridge", Config: r.config(e)}
		if h := r.running[e.Key]; h != nil {
			ne.Source = h.node
		}
		out = append(out, ne)
	}
	r.list.Store(&out)
}

// config is a registry node's configuration block on its status page.
func (r *nodeRegistry) config(e *registryEntry) *fabricui.NodeConfigView {
	return &fabricui.NodeConfigView{
		Source: fabricui.NodeConfigSourceRegistry, Label: e.Label, Listen: e.Listen,
		Discriminator: e.Discriminator,
		VendorID:      uint16(r.primary.VendorID), VendorName: bridgeVendorName, //nolint:gosec // checked by bridgeConfig before the registry opens
		ProductID: uint16(r.primary.ProductID), ProductName: bridgeProductName, //nolint:gosec // as above
		Store: registryStore(e.Key),
	}
}

// Create allocates a node — its key from the label, the next free port
// after the default node's, a random discriminator no other node holds,
// a random valid passcode —, starts it with its bridge half, persists it
// and serves /bridge/<key>. Above --node-limit it wraps
// fabricui.ErrNodeLimit.
func (r *nodeRegistry) Create(ctx context.Context, spec fabricui.NodeSpec) (fabricui.NodeEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) >= r.limit {
		return fabricui.NodeEntry{}, fmt.Errorf("%w: %d nodes beside the default node (--node-limit)", fabricui.ErrNodeLimit, r.limit)
	}
	e := registryEntry{Key: r.allocKey(spec.Label), Label: strings.TrimSpace(spec.Label), CreatedAt: time.Now().UTC().Truncate(time.Second)}
	var err error
	if e.Listen, err = r.allocListen(ctx); err != nil {
		return fabricui.NodeEntry{}, err
	}
	if e.Discriminator, e.Passcode, err = r.allocCredentials(); err != nil {
		return fabricui.NodeEntry{}, err
	}
	h, err := r.start(r.ctx, registrySpec(r.primary, &e))
	if err != nil {
		_ = r.removeStore(e.Key)
		return fabricui.NodeEntry{}, err
	}
	entries := append(slices.Clone(r.entries), e)
	if err := r.save(entries); err != nil {
		h.srv.Shutdown(ctx)
		_ = h.node.Stop(context.WithoutCancel(ctx))
		_ = r.removeStore(e.Key)
		return fabricui.NodeEntry{}, err
	}
	r.entries = entries
	r.running[e.Key] = &h
	r.routes.add(BridgeHalf{Key: e.Key, Server: h.srv})
	r.publish()
	r.log.Info("fabricserver.registry.created", slog.String("node", e.Key), slog.String("listen", e.Listen))
	return (*r.list.Load())[len(r.entries)-1], nil
}

// Rename sets the label of the node key: the pages and the registry show
// it at once; the running node keeps the NodeLabel it started with.
func (r *nodeRegistry) Rename(_ context.Context, key, label string) error {
	if key == fabricui.DefaultNodeKey {
		return fmt.Errorf("%w: the default node's label is --bridge-label", fabricui.ErrDefaultNode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.index(key)
	if i < 0 {
		return fmt.Errorf("node registry: no node %q", key)
	}
	entries := slices.Clone(r.entries)
	entries[i].Label = strings.TrimSpace(label)
	if err := r.save(entries); err != nil {
		return err
	}
	r.entries = entries
	r.publish()
	return nil
}

// Delete ends the node key for good: /bridge/<key> is gone, its client
// receives server_shutdown, the node removes every fabric it holds (each
// controller's Leave event: they lose the node) and stops
// (node.FactoryReset), its store files are deleted, and so is its entry.
func (r *nodeRegistry) Delete(ctx context.Context, key string) error {
	if key == fabricui.DefaultNodeKey {
		return fmt.Errorf("%w: the default node is --bridge's", fabricui.ErrDefaultNode)
	}
	// A page that goes away does not leave a node half deleted.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.index(key)
	if i < 0 {
		return fmt.Errorf("node registry: no node %q", key)
	}
	if h := r.running[key]; h != nil {
		r.routes.remove(key)
		h.srv.Shutdown(ctx)
		if err := h.node.FactoryReset(ctx); err != nil {
			r.log.Warn("fabricserver.registry.reset_failed", slog.String("node", key), slog.Any("error", err))
			_ = h.node.Stop(ctx)
		}
		delete(r.running, key)
	}
	entries := slices.Delete(slices.Clone(r.entries), i, i+1)
	if err := r.save(entries); err != nil {
		r.publish()
		return err
	}
	r.entries = entries
	if err := r.removeStore(key); err != nil {
		r.publish()
		return err
	}
	r.publish()
	r.log.Info("fabricserver.registry.deleted", slog.String("node", key))
	return nil
}

func (r *nodeRegistry) index(key string) int {
	return slices.IndexFunc(r.entries, func(e registryEntry) bool { return e.Key == key })
}

// save writes nodes.json atomically, readable by the owner only: it holds
// the nodes' passcodes.
func (r *nodeRegistry) save(entries []registryEntry) error {
	b, err := json.MarshalIndent(registryData{Nodes: entries}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir, registryFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil { //nolint:gosec // the operator's --storage-path
		return fmt.Errorf("node registry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil { //nolint:gosec // as above
		return fmt.Errorf("node registry: %w", err)
	}
	return nil
}

// removeStore deletes the store files of the node key: the SQLite
// database and its journal files.
func (r *nodeRegistry) removeStore(key string) error {
	var errs []error
	base := filepath.Join(r.dir, registryStore(key))
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(base + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// keyFold transliterates the letters a German or Nordic label carries
// before the slug drops what is not ASCII.
var keyFold = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "å", "aa", "æ", "ae", "ø", "oe")

// slugMax leaves room for a "-NN" suffix within nodeKey's 28 bytes.
const slugMax = 24

// allocKey is a key from label: lower-case ASCII letters and digits,
// every other run one "-", at most slugMax bytes ("node" when nothing
// remains); a key the registry, the default node or a page name holds,
// or whose store file is on disk (a store this registry does not own:
// it is never adopted), takes the first free suffix "-2", "-3", ….
func (r *nodeRegistry) allocKey(label string) string {
	var b strings.Builder
	dash := false
	for _, c := range keyFold.Replace(strings.ToLower(label)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if len(slug) > slugMax {
		slug = strings.TrimRight(slug[:slugMax], "-")
	}
	if slug == "" {
		slug = "node"
	}
	taken := func(k string) bool {
		if k == fabricui.DefaultNodeKey || slices.Contains(pageNames, k) || r.index(k) >= 0 {
			return true
		}
		_, err := os.Stat(filepath.Join(r.dir, registryStore(k)))
		return !errors.Is(err, os.ErrNotExist)
	}
	key := slug
	for n := 2; taken(key); n++ {
		key = slug + "-" + strconv.Itoa(n)
	}
	return key
}

// allocListen is the next free UDP port after the default node's, on its
// host: one no registry node holds and the host can bind. A default node
// on an ephemeral port (0) gives every registry node one too.
func (r *nodeRegistry) allocListen(ctx context.Context) (string, error) {
	host, base, err := splitListen(r.primary.Listen)
	if err != nil {
		return "", fmt.Errorf("--bridge-listen %q: %w", r.primary.Listen, err)
	}
	if base == 0 {
		return net.JoinHostPort(host, "0"), nil
	}
	held := map[int]bool{base: true}
	for _, e := range r.entries {
		if _, p, err := splitListen(e.Listen); err == nil {
			held[p] = true
		}
	}
	for p := base + 1; p <= 65535; p++ {
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		if !held[p] && r.probe(ctx, addr) {
			return addr, nil
		}
	}
	return "", fmt.Errorf("node registry: no free UDP port after %d", base)
}

// udpFree reports whether addr can be bound for UDP now.
func udpFree(ctx context.Context, addr string) bool {
	pc, err := (&net.ListenConfig{}).ListenPacket(ctx, "udp", addr)
	if err != nil {
		return false
	}
	_ = pc.Close()
	return true
}

// allocCredentials draws a node's discriminator and passcode with
// go-fabric's node.NewCommissioningCredentials (a random 12-bit
// discriminator, a random passcode node.Config.Validate accepts), drawing
// again while the discriminator is another node's: a commissioner tells
// the nodes apart by it.
func (r *nodeRegistry) allocCredentials() (uint16, uint32, error) {
	for range 64 {
		c, err := node.NewCommissioningCredentials()
		if err != nil {
			return 0, 0, err
		}
		if uint(c.Discriminator) == r.primary.Discriminator || slices.ContainsFunc(r.entries, func(e registryEntry) bool { return e.Discriminator == c.Discriminator }) {
			continue
		}
		return c.Discriminator, c.Passcode, nil
	}
	return 0, 0, errors.New("node registry: no free discriminator drawn")
}
