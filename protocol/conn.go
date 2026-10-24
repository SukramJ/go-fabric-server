// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Backpressure defaults — matterjs-server's WebSocketConnection
// (packages/ws-controller/src/server/WebSocketConnection.ts:55-59):
// DEFAULT_HIGH_WATER 1 MiB, DEFAULT_HIGH_WATER_CEILING 50 MiB,
// DEFAULT_CAP_BASE 1000, DEFAULT_CAP_PER_NODE 20, DEFAULT_WATCHDOG 5 min.
const (
	DefaultHighWater        = 1 << 20
	DefaultHighWaterCeiling = 50 << 20
	DefaultCapBase          = 1000
	DefaultCapPerNode       = 20
	DefaultWatchdog         = 5 * time.Minute
)

// Socket is the outgoing half of one WebSocket. Write blocks until the
// frame is written or ctx ends; Abort tears the connection down without a
// closing handshake (a half-open peer may never complete one).
type Socket interface {
	Write(ctx context.Context, frame []byte) error
	Abort(reason string)
}

// Metrics is the counter/gauge sink of the send path; go-fabric's
// contract.Metrics satisfies it. The names are the Metric* constants.
type Metrics interface {
	Counter(name string, delta int64)
	Gauge(name string, value int64)
}

// The metric names of the send path.
const (
	// MetricConnections is the gauge of open connections.
	MetricConnections = "ws.connections"
	// MetricQueuedBytes is the gauge of bytes handed to the sockets and not
	// yet written, over every connection.
	MetricQueuedBytes = "ws.queued_bytes"
	// MetricDropped counts frames dropped under backpressure.
	MetricDropped = "ws.dropped"
	// MetricClosedOverflow counts connections closed by the send path: more
	// undroppable frames waiting than the cap, or a stalled send.
	MetricClosedOverflow = "ws.closed_overflow"
)

// Meter aggregates the gauges of every [Conn] of a server; nil Metrics
// discards.
type Meter struct {
	metrics Metrics
	mu      sync.Mutex
	conns   int64
	queued  int64
}

// NewMeter returns a meter reporting to m (nil discards).
func NewMeter(m Metrics) *Meter { return &Meter{metrics: m} }

func (m *Meter) add(conns, queued int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.conns += conns
	m.queued += queued
	c, q := m.conns, m.queued
	m.mu.Unlock()
	if m.metrics != nil {
		if conns != 0 {
			m.metrics.Gauge(MetricConnections, c)
		}
		if queued != 0 {
			m.metrics.Gauge(MetricQueuedBytes, q)
		}
	}
}

func (m *Meter) count(name string, delta int64) {
	if m != nil && m.metrics != nil {
		m.metrics.Counter(name, delta)
	}
}

// ConnOptions configures a [Conn]; zero values are the defaults.
type ConnOptions struct {
	// ID tags the connection's log lines.
	ID string
	// HighWater is the in-flight byte count above which the connection
	// queues; it ratchets up to twice the largest frame sent, at most
	// HighWaterCeiling.
	HighWater, HighWaterCeiling int
	// CapBase and CapPerNode bound the outbox: max(CapBase, nodes ×
	// CapPerNode) entries.
	CapBase, CapPerNode int
	// Nodes returns the current node count for the cap.
	Nodes func() int
	// Watchdog closes the connection when an in-flight write makes no
	// progress for this long.
	Watchdog time.Duration
	// Meter receives the gauges and counters.
	Meter *Meter
	// Logger receives the mode changes; nil discards.
	Logger *slog.Logger
}

type policy uint8

const (
	droppable policy = iota
	pinned
	coalescable
)

type outEntry struct {
	key    string
	build  func() []byte
	policy policy
}

// Conn is the per-connection send path — matterjs-server's
// WebSocketConnection (WebSocketConnection.ts:60-310) translated. In direct
// mode a frame goes to the socket at once, in emit order. When the bytes
// in flight (handed to the socket, not yet written: the Go form of
// ws.bufferedAmount) pass the high-water mark, the connection queues:
// ordered and coalescable frames wait in an outbox that drains while the
// bytes in flight stay below the mark, so a window of frames is in flight,
// not one; coalescable frames collapse latest-wins per key, keeping the
// first position, and are built only when sent; past the cap the oldest
// droppable frames are dropped, and more pinned (undroppable) frames than
// the cap close the connection, as does an in-flight write without
// progress for the watchdog interval. Reliable frames (responses,
// server_info, server_shutdown) always go to the socket at once. When the
// outbox and the socket drain, the connection returns to direct mode.
type Conn struct {
	sock Socket
	opts ConnOptions
	log  *slog.Logger

	mu        sync.Mutex
	queued    bool // mode
	highWater int
	// inflight is the socket's buffer: frames handed over, not yet written.
	inflight      [][]byte
	inflightBytes int
	outbox        []*outEntry
	byKey         map[string]*outEntry
	pinned        int
	seq           uint64
	disposed      bool
	capWarned     bool
	watchdog      *time.Timer
	wake          chan struct{}
	done          chan struct{}
	cancel        context.CancelFunc
	ctx           context.Context
}

// NewConn starts the send path over sock. Close (or a fatal condition)
// ends it; [Conn.Done] is closed then.
func NewConn(sock Socket, opts ConnOptions) *Conn {
	if opts.HighWaterCeiling <= 0 {
		opts.HighWaterCeiling = DefaultHighWaterCeiling
	}
	if opts.HighWater <= 0 {
		opts.HighWater = DefaultHighWater
	}
	if opts.CapBase <= 0 {
		opts.CapBase = DefaultCapBase
	}
	if opts.CapPerNode <= 0 {
		opts.CapPerNode = DefaultCapPerNode
	}
	if opts.Watchdog <= 0 {
		opts.Watchdog = DefaultWatchdog
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		sock: sock, opts: opts, log: log.With(slog.String("conn", opts.ID)),
		highWater: min(opts.HighWater, opts.HighWaterCeiling),
		byKey:     map[string]*outEntry{},
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
		ctx:       ctx, cancel: cancel,
	}
	opts.Meter.add(1, 0)
	go c.writer()
	return c
}

// Done is closed once the send path ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Queued reports whether the connection is in backpressure mode.
func (c *Conn) Queued() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queued
}

// OutboxLen is the number of frames waiting in the outbox.
func (c *Conn) OutboxLen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.outbox)
}

// SendReliable sends a frame that is never queued or dropped: a response,
// server_info, server_shutdown. Clients match responses by message id, so
// jumping the outbox is harmless (WebSocketConnection.ts sendReliable).
func (c *Conn) SendReliable(frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return
	}
	c.directLocked(frame)
}

// SendOrdered sends an event frame in order. Under backpressure a
// droppable frame (node_event, subscription_report) may be dropped
// oldest-first; an undroppable one (node_added, node_removed,
// endpoint_added, endpoint_removed, subscription_ended) never is.
func (c *Conn) SendOrdered(frame []byte, isDroppable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return
	}
	if !c.queued {
		c.directLocked(frame)
		return
	}
	p := pinned
	if isDroppable {
		p = droppable
	}
	c.seq++
	c.enqueueLocked(&outEntry{key: "seq:" + strconv.FormatUint(c.seq, 10), build: func() []byte { return frame }, policy: p})
}

// SendCoalescable sends the frame build returns; under backpressure
// repeated sends with the same key collapse to the latest, at the first
// one's position, and build runs once, when the frame is sent. build may
// return nil to send nothing. It runs with the connection's lock held: it
// must not call back into the connection.
func (c *Conn) SendCoalescable(key string, build func() []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return
	}
	if !c.queued {
		if f := build(); f != nil {
			c.directLocked(f)
		}
		return
	}
	if e, ok := c.byKey[key]; ok {
		e.build = build
		return
	}
	c.enqueueLocked(&outEntry{key: key, build: build, policy: coalescable})
}

// Flush waits until every frame sent so far is written, or ctx ends.
func (c *Conn) Flush(ctx context.Context) error {
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for {
		c.mu.Lock()
		idle := c.disposed || (c.inflightBytes == 0 && len(c.outbox) == 0)
		c.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		case <-t.C:
		}
	}
}

// Close ends the send path; frames not yet written are discarded.
func (c *Conn) Close() {
	c.mu.Lock()
	c.disposeLocked()
	c.mu.Unlock()
}

// directLocked hands a frame to the socket and enters queued mode when the
// bytes in flight pass the mark (WebSocketConnection.ts #directSend).
func (c *Conn) directLocked(frame []byte) {
	c.raiseHighWaterLocked(frame)
	c.handLocked(frame)
	if !c.queued && c.inflightBytes > c.highWater {
		c.queued = true
		c.log.Warn("fabricserver.ws.backpressure", slog.Int("in_flight_bytes", c.inflightBytes), slog.Int("high_water", c.highWater))
	}
}

func (c *Conn) raiseHighWaterLocked(frame []byte) {
	if need := min(c.opts.HighWaterCeiling, 2*len(frame)); need > c.highWater {
		c.highWater = need
	}
}

func (c *Conn) handLocked(frame []byte) {
	if c.inflightBytes == 0 {
		c.armWatchdogLocked()
	}
	c.inflight = append(c.inflight, frame)
	c.inflightBytes += len(frame)
	c.opts.Meter.add(0, int64(len(frame)))
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Conn) enqueueLocked(e *outEntry) {
	c.outbox = append(c.outbox, e)
	if e.policy == coalescable {
		c.byKey[e.key] = e
	}
	if e.policy == pinned {
		c.pinned++
	}
	c.enforceCapLocked()
	c.drainLocked()
}

// enforceCapLocked mirrors WebSocketConnection.ts #enforceCap: only
// droppable entries are dropped; coalescable ones are bounded by their
// keys; more pinned ones than the cap close the connection.
func (c *Conn) enforceCapLocked() {
	if c.disposed {
		return
	}
	nodes := 0
	if c.opts.Nodes != nil {
		nodes = c.opts.Nodes()
	}
	limit := max(c.opts.CapBase, nodes*c.opts.CapPerNode)
	if len(c.outbox) <= limit {
		return
	}
	if c.pinned > limit {
		c.terminateLocked("more than " + strconv.Itoa(limit) + " node and endpoint changes waiting")
		return
	}
	dropped := 0
	kept := c.outbox[:0]
	for _, e := range c.outbox {
		if len(c.outbox)-dropped > limit && e.policy == droppable {
			dropped++
			continue
		}
		kept = append(kept, e)
	}
	clear(c.outbox[len(kept):])
	c.outbox = kept
	if dropped > 0 {
		c.opts.Meter.count(MetricDropped, int64(dropped))
		if !c.capWarned {
			c.capWarned = true
			c.log.Warn("fabricserver.ws.dropping", slog.Int("cap", limit))
		}
	}
}

// drainLocked moves outbox entries to the socket while the window has
// room; one frame always goes when nothing is in flight, so a frame larger
// than the mark cannot wedge the drain (WebSocketConnection.ts #drain).
func (c *Conn) drainLocked() {
	if !c.queued || c.disposed {
		return
	}
	for len(c.outbox) > 0 && c.inflightBytes < c.highWater {
		e := c.outbox[0]
		c.outbox[0] = nil
		c.outbox = c.outbox[1:]
		switch e.policy {
		case pinned:
			c.pinned--
		case coalescable:
			delete(c.byKey, e.key)
		case droppable:
		}
		frame := e.build()
		if frame == nil {
			continue
		}
		c.raiseHighWaterLocked(frame)
		c.handLocked(frame)
	}
	if len(c.outbox) == 0 && c.inflightBytes == 0 {
		c.queued = false
		c.capWarned = false
		c.log.Info("fabricserver.ws.drained")
	}
}

func (c *Conn) armWatchdogLocked() {
	if c.watchdog == nil {
		c.watchdog = time.AfterFunc(c.opts.Watchdog, c.onWatchdog)
		return
	}
	c.watchdog.Reset(c.opts.Watchdog)
}

func (c *Conn) onWatchdog() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed || c.inflightBytes == 0 {
		return
	}
	c.terminateLocked("outbound send stalled beyond watchdog")
}

func (c *Conn) terminateLocked(reason string) {
	c.log.Warn("fabricserver.ws.terminate", slog.String("reason", reason))
	c.opts.Meter.count(MetricClosedOverflow, 1)
	c.disposeLocked()
	go c.sock.Abort(reason)
}

func (c *Conn) disposeLocked() {
	if c.disposed {
		return
	}
	c.disposed = true
	if c.watchdog != nil {
		c.watchdog.Stop()
	}
	c.outbox, c.byKey, c.pinned = nil, map[string]*outEntry{}, 0
	c.cancel()
}

// writer writes the socket's buffer, one frame after the other; each
// written frame frees window space and drains the outbox
// (WebSocketConnection.ts #onFlushed).
func (c *Conn) writer() {
	defer func() {
		c.mu.Lock()
		c.opts.Meter.add(-1, -int64(c.inflightBytes))
		c.inflight, c.inflightBytes = nil, 0
		c.mu.Unlock()
		close(c.done)
	}()
	for {
		c.mu.Lock()
		if c.disposed && len(c.inflight) == 0 {
			c.mu.Unlock()
			return
		}
		if len(c.inflight) == 0 {
			c.mu.Unlock()
			select {
			case <-c.wake:
			case <-c.ctx.Done():
			}
			continue
		}
		frame := c.inflight[0]
		c.inflight[0] = nil
		c.inflight = c.inflight[1:]
		disposed := c.disposed
		c.mu.Unlock()
		var err error
		if !disposed {
			err = c.sock.Write(c.ctx, frame)
		}
		c.mu.Lock()
		c.inflightBytes -= len(frame)
		c.opts.Meter.add(0, -int64(len(frame)))
		if err != nil || disposed {
			if !c.disposed {
				c.disposeLocked()
				go c.sock.Abort("write failed")
			}
			c.mu.Unlock()
			continue
		}
		if c.inflightBytes > 0 {
			c.armWatchdogLocked()
		} else if c.watchdog != nil {
			c.watchdog.Stop()
		}
		c.drainLocked()
		c.mu.Unlock()
	}
}
