// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// BridgeSchemaVersion is the schema number of the bridge half, apart from
// the controller half's (concept v8 C.1).
const BridgeSchemaVersion = 1

// StatusBusy is the WebSocket close status of a second bridge client: the
// daemon's node has one author (brief S2 §1.1).
const StatusBusy websocket.StatusCode = 4409

// Options configures a [Server].
type Options struct {
	// Version is go-fabric-server's version, GoFabricVersion go-fabric's,
	// for server_info's sdk_version.
	Version, GoFabricVersion string
	// ResultTimeout is how long a write or command event waits for its
	// result; 0 is 5 seconds.
	ResultTimeout time.Duration
	// SubscriptionPoll is how often the node's subscription count is
	// read for the subscriptions event; 0 is 5 seconds.
	SubscriptionPoll time.Duration
	// Logger receives the server's logs; nil discards them.
	Logger *slog.Logger
	// Metrics receives the send path's metrics.
	Metrics protocol.Metrics
	// Conn overrides the backpressure defaults of the connection (tests).
	Conn protocol.ConnOptions
	// MaxInFlight bounds the requests the client runs at once; 0 is 64.
	MaxInFlight int
}

// Server is the bridge half. Its methods are safe for concurrent use.
type Server struct {
	opts  Options
	log   *slog.Logger
	meter *protocol.Meter
	node  atomic.Pointer[node.Node]

	// declMu serialises declare and remove: the topology has one
	// author, and a batch reassembles while it holds the lock.
	declMu sync.Mutex

	// mu guards the devices, the connection and its listening flag and
	// the pending results; every event is emitted under it.
	mu      sync.Mutex
	devices map[string]*bridged
	order   []string // the device keys in declaration order
	conn    *bconn
	pending map[uint64]*pending
	closing bool
	subs    int

	seq     atomic.Uint64
	connSeq atomic.Uint64
	started atomic.Bool
	stop    chan struct{}
	done    chan struct{}
}

// New returns a server.
func New(opts Options) *Server {
	if opts.ResultTimeout <= 0 {
		opts.ResultTimeout = 5 * time.Second
	}
	if opts.SubscriptionPoll <= 0 {
		opts.SubscriptionPoll = 5 * time.Second
	}
	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = 64
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Server{
		opts: opts, log: log, meter: protocol.NewMeter(opts.Metrics),
		devices: map[string]*bridged{}, pending: map[uint64]*pending{},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// Observers are the node observers the server needs for its
// fabric_added, fabric_removed and commissioning_window events: wire
// them as node.Config.Observers before node.New.
func (s *Server) Observers() node.Observers {
	return node.Observers{
		OnFabricAdded:      func(f node.FabricInfo) error { s.emit("fabric_added", fabricData(f)); return nil },
		OnFabricRemoved:    func(f node.FabricInfo) error { s.emit("fabric_removed", fabricData(f)); return nil },
		OnWindowTransition: func(w node.WindowSnapshot) error { s.emit("commissioning_window", windowData(w)); return nil },
	}
}

// Start binds the server to its node, which must have an Aggregator (no
// node.Config.Topology of the host's own); the node may be started before
// or after.
func (s *Server) Start(_ context.Context, n *node.Node) error {
	if n == nil || n.Aggregator() == nil {
		return errors.New("bridgeapi: Start needs a node with an Aggregator")
	}
	if !s.started.CompareAndSwap(false, true) {
		return errors.New("bridgeapi: already started")
	}
	s.node.Store(n)
	go s.poll()
	return nil
}

// poll reads the node's subscription count and sends the subscriptions
// event when it moved: the node has no hook for a subscription's start or
// end the host may take (report S2, Questions Q17).
func (s *Server) poll() {
	defer close(s.done)
	t := time.NewTicker(s.opts.SubscriptionPoll)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		n := s.node.Load()
		if n == nil || n.Subscriptions() == nil {
			continue
		}
		count := n.Subscriptions().Active()
		s.mu.Lock()
		if count != s.subs {
			s.subs = count
			s.emitLocked("subscriptions", map[string]any{"count": count})
		}
		s.mu.Unlock()
	}
}

// Health is the bridge half's state for GET /health: healthy once
// started and until shut down; devices is the declared device count.
func (s *Server) Health() (healthy bool, devices int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started.Load() && !s.closing, len(s.devices)
}

// Shutdown sends server_shutdown to the client, waits until it is written
// (or ctx ends) and closes it; pending results are answered. The node is
// the caller's to stop.
func (s *Server) Shutdown(ctx context.Context) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	c := s.conn
	if c != nil {
		c.send.SendReliable(protocol.MustEncode(protocol.Event{Event: "server_shutdown", Data: struct{}{}}))
	}
	s.failPendingLocked()
	s.mu.Unlock()
	if c != nil {
		_ = c.send.Flush(ctx)
		c.close(websocket.StatusGoingAway, "server shutting down")
	}
	if s.started.Load() {
		close(s.stop)
		<-s.done
	}
}

// ServeHTTP serves the bridge client's WebSocket. A second client while
// one is connected receives an error frame naming the holder and the
// close status [StatusBusy].
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing || !s.started.Load() {
		http.Error(w, "the bridge half is not running", http.StatusServiceUnavailable)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	id := "bridge" + strconv.FormatUint(s.connSeq.Add(1), 10)
	opts := s.opts.Conn
	opts.ID, opts.Meter, opts.Logger = id, s.meter, s.log
	opts.Nodes = func() int { _, n := s.Health(); return n }
	c := &bconn{s: s, id: id, remote: r.RemoteAddr, since: time.Now(), ws: ws, sem: make(chan struct{}, s.opts.MaxInFlight)}
	c.send = protocol.NewConn(protocol.WebSocket(ws), opts)
	s.mu.Lock()
	holder := s.conn
	if holder == nil {
		s.conn = c
	}
	s.mu.Unlock()
	if holder != nil {
		s.refuse(r.Context(), c, holder)
		return
	}
	s.log.Info("fabricserver.bridge.connected", slog.String("conn", id), slog.String("remote", r.RemoteAddr))
	defer c.close(websocket.StatusNormalClosure, "")
	c.run(r.Context())
}

// refuse answers a second client: one error frame naming the holder,
// then the close.
func (s *Server) refuse(ctx context.Context, c, holder *bconn) {
	details := fmt.Sprintf("the bridge half has a client: %s from %s since %s; one bridge client at a time",
		holder.id, holder.remote, holder.since.UTC().Format(time.RFC3339))
	s.log.Info("fabricserver.bridge.refused", slog.String("conn", c.id), slog.String("remote", c.remote), slog.String("holder", holder.id))
	c.send.SendReliable(protocol.MustEncode(&protocol.Error{ErrorCode: protocol.BridgeClientConnected, Details: details}))
	flush, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = c.send.Flush(flush)
	c.send.Close()
	reason := "bridge client connected: " + holder.id
	_ = c.ws.Close(StatusBusy, reason)
}
