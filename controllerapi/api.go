// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package controllerapi is the controller half of go-fabric-server: the
// WebSocket API at /ws, wire-compatible with matterjs-server's (schema 14,
// the python-matter-server contract Home Assistant's Matter integration
// speaks), over go-fabric's controller facade.
//
// The shapes are the read map's (notes/reads/read-v8-matterjs-server.md,
// matterjs-server a7922e85): the dispatch table (§1.7), the arguments
// (§1.8), the results (§1.9), the node data (§2), the events (§1.5-1.6)
// and the connection model (§3.3-3.7) — server_info on connect, events
// only after start_listening has answered, the backpressure send path of
// protocol.Conn.
//
// Lifecycle: [New] → wire [Server.Hooks] into the controller's node
// configuration → build the controller → [Server.Start] → serve
// [Server.ServeHTTP] at /ws → [Server.Shutdown]. The hooks come before the
// controller because the controller starts its nodes while it is built.
package controllerapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// Options configures a [Server].
type Options struct {
	// Version is go-fabric-server's version, for server_info's sdk_version.
	Version string
	// GoFabricVersion is the go-fabric module version, for sdk_version.
	GoFabricVersion string
	// StoragePath is the directory of the server's own settings
	// (server.json: credentials, the fabric label); "" keeps them in
	// memory.
	StoragePath string
	// DisableAutoSubscription is matterjs-server's
	// --disable-auto-subscription: new nodes are paired without the
	// server's own subscription, and subscribe/unsubscribe are available.
	DisableAutoSubscription bool
	// DefaultFabricLabel pins the fabric label; set_default_fabric_label
	// then succeeds without effect (--default-fabric-label).
	DefaultFabricLabel string
	// PairTimeout bounds a commissioning; 0 is 5 minutes.
	PairTimeout time.Duration
	// Logger receives the server's logs; nil discards them.
	Logger *slog.Logger
	// LogLevel is the level get_loglevel reports and set_loglevel changes;
	// nil is a fixed info.
	LogLevel *slog.LevelVar
	// Metrics receives the send path's metrics.
	Metrics protocol.Metrics
	// Conn overrides the backpressure defaults of each connection (tests).
	Conn protocol.ConnOptions
	// MaxInFlight bounds the requests a connection runs at once; 0 is 64.
	MaxInFlight int
}

// Server is the controller half. Its methods are safe for concurrent use.
type Server struct {
	opts  Options
	log   *slog.Logger
	meter *protocol.Meter
	cfg   *settingsStore

	backend atomic.Pointer[backendRef]

	// mu guards the node cache, the connections and their listening
	// flags; every event is emitted under it, so a start_listening that
	// holds it answers with a dump no event overtakes.
	mu      sync.Mutex
	nodes   map[string]*nodeState // by the registry's local id
	conns   map[*wsConn]struct{}
	history []any // the last node events, for diagnostics
	closing bool

	dirtyMu  sync.Mutex
	dirty    map[string]bool
	wake     chan struct{}
	stop     chan struct{}
	loopDone chan struct{}
	started  atomic.Bool

	connSeq atomic.Uint64
	labelMu sync.Mutex
	// labelOwner is the connection whose set_default_fabric_label owns
	// the label (docs/websockets_api.md:1521-1527).
	labelOwner *wsConn
}

type backendRef struct{ b Backend }

// New returns a server; it loads the settings under opts.StoragePath.
func New(opts Options) (*Server, error) {
	if opts.PairTimeout <= 0 {
		opts.PairTimeout = 5 * time.Minute
	}
	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = 64
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	cfg, err := openSettings(opts.StoragePath)
	if err != nil {
		return nil, err
	}
	return &Server{
		opts: opts, log: log, meter: protocol.NewMeter(opts.Metrics), cfg: cfg,
		nodes: map[string]*nodeState{}, conns: map[*wsConn]struct{}{},
		dirty: map[string]bool{}, wake: make(chan struct{}, 1), stop: make(chan struct{}), loopDone: make(chan struct{}),
	}, nil
}

// Hooks are the client node hooks the server needs for the node with the
// registry's local id: wire them as client.RegistryConfig.HooksFor. An
// empty id is hooks that cannot tell the nodes apart — what
// controller.Config.Node.Hooks takes today (report S1, Questions Q2): a
// change then refreshes every node, and node events, which only the
// hook's node could name, are not forwarded.
func (s *Server) Hooks(id string) client.Hooks {
	mark := func() { s.markDirty(id) }
	h := client.Hooks{
		EndpointInstalled:      func(*client.Endpoint) { mark() },
		ClusterInstalled:       func(*client.Endpoint, *client.Cluster) { mark() },
		ClusterReplaced:        func(*client.Endpoint, *client.Cluster) { mark() },
		ClusterDeleted:         func(*client.Endpoint, *client.Cluster) { mark() },
		Changed:                mark,
		ConnectionStateChanged: func(client.ConnectionState) { mark() },
		Seeded:                 mark,
		Decommissioned:         mark,
		Commissioned:           mark,
	}
	if id != "" {
		h.Event = func(endpoint uint16, cluster, event uint32, e client.EventData, _ any) {
			s.nodeEvent(id, endpoint, cluster, event, e)
		}
	}
	return h
}

// Start binds the server to its controller: the node cache is built from
// the registry's nodes (no events for them), and changes are followed from
// then on.
func (s *Server) Start(_ context.Context, b Backend) error {
	if b == nil {
		return errors.New("controllerapi: Start needs a backend")
	}
	if !s.started.CompareAndSwap(false, true) {
		return errors.New("controllerapi: already started")
	}
	s.backend.Store(&backendRef{b})
	s.mu.Lock()
	for _, n := range b.Registry().Commissioned() {
		if st := s.snapshot(n, nil); st != nil {
			s.nodes[n.ID()] = st
		}
	}
	s.mu.Unlock()
	go s.loop()
	return nil
}

func (s *Server) b() Backend {
	if r := s.backend.Load(); r != nil {
		return r.b
	}
	return nil
}

// NodeCount is the number of commissioned nodes the server knows.
func (s *Server) NodeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.nodes)
}

// Health is the controller half's state for GET /health: healthy once
// started and until shut down.
func (s *Server) Health() (healthy bool, nodes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started.Load() && !s.closing, len(s.nodes)
}

// Shutdown sends server_shutdown to every connection, waits until it is
// written (or ctx ends) and closes them; then the server stops following
// the controller. The controller itself is the caller's to close.
func (s *Server) Shutdown(ctx context.Context) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	frame := protocol.MustEncode(protocol.Event{Event: "server_shutdown", Data: struct{}{}})
	conns := make([]*wsConn, 0, len(s.conns))
	for c := range s.conns {
		c.send.SendReliable(frame)
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.send.Flush(ctx)
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
	if s.started.Load() {
		close(s.stop)
		<-s.loopDone
	}
}

// ServeHTTP serves one WebSocket connection of the controller half.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing || !s.started.Load() {
		http.Error(w, "server is not running", http.StatusServiceUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	c := s.newConn(ws)
	defer c.close(websocket.StatusNormalClosure, "")
	c.run(r.Context())
}

func (s *Server) newConn(ws *websocket.Conn) *wsConn {
	id := "ws" + strconv.FormatUint(s.connSeq.Add(1), 10)
	opts := s.opts.Conn
	opts.ID, opts.Meter, opts.Logger, opts.Nodes = id, s.meter, s.log, s.NodeCount
	c := &wsConn{
		s: s, id: id, ws: ws, send: protocol.NewConn(protocol.WebSocket(ws), opts),
		subs: map[subKey]*ownedSub{}, sem: make(chan struct{}, s.opts.MaxInFlight),
	}
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	return c
}
