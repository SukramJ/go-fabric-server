// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// slowSocket writes only while its gate is open; it records what it wrote.
type slowSocket struct {
	mu      sync.Mutex
	gate    chan struct{}
	written []string
	aborted chan string
	open    bool
}

func newSlowSocket() *slowSocket {
	return &slowSocket{gate: make(chan struct{}), aborted: make(chan string, 1)}
}

func (s *slowSocket) Write(ctx context.Context, frame []byte) error {
	select {
	case <-s.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.written = append(s.written, string(frame))
	s.mu.Unlock()
	return nil
}

func (s *slowSocket) Abort(reason string) {
	select {
	case s.aborted <- reason:
	default:
	}
}

func (s *slowSocket) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open {
		s.open = true
		close(s.gate)
	}
}

func (s *slowSocket) frames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.written...)
}

type countMetrics struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]int64
}

func newCountMetrics() *countMetrics {
	return &countMetrics{counters: map[string]int64{}, gauges: map[string]int64{}}
}

func (m *countMetrics) Counter(name string, delta int64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}

func (m *countMetrics) Gauge(name string, value int64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

func (m *countMetrics) counter(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

func (m *countMetrics) gauge(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[name]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConnDirectModeKeepsOrder(t *testing.T) {
	sock := newSlowSocket()
	sock.release()
	c := protocol.NewConn(sock, protocol.ConnOptions{})
	defer c.Close()
	c.SendReliable([]byte("a"))
	c.SendOrdered([]byte("b"), true)
	c.SendCoalescable("k", func() []byte { return []byte("c") })
	waitFor(t, "three frames", func() bool { return len(sock.frames()) == 3 })
	if got := sock.frames(); got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("order %v", got)
	}
	if c.Queued() {
		t.Fatal("an idle connection is in direct mode")
	}
}

// congest puts the connection in queued mode: a frame above the mark that
// the socket does not write.
func congest(t *testing.T, c *protocol.Conn) {
	t.Helper()
	c.SendReliable([]byte("0123456789-big-frame"))
	if !c.Queued() {
		t.Fatal("a frame past the high-water mark queues")
	}
}

func TestConnCoalescesAndDrops(t *testing.T) {
	sock := newSlowSocket()
	m := newCountMetrics()
	meter := protocol.NewMeter(m)
	c := protocol.NewConn(sock, protocol.ConnOptions{HighWater: 4, HighWaterCeiling: 8, CapBase: 3, Meter: meter})
	defer c.Close()
	congest(t, c)
	built := 0
	for _, v := range []string{"n1", "n2", "n3"} {
		c.SendCoalescable("node:1", func() []byte { built++; return []byte(v) })
	}
	if c.OutboxLen() != 1 {
		t.Fatalf("outbox %d: one entry per key", c.OutboxLen())
	}
	for _, v := range []string{"e1", "e2", "e3", "e4"} {
		c.SendOrdered([]byte(v), true)
	}
	// Five entries, cap three: the two oldest droppable events go.
	if c.OutboxLen() != 3 {
		t.Fatalf("outbox %d after the cap", c.OutboxLen())
	}
	if m.counter(protocol.MetricDropped) != 2 {
		t.Fatalf("dropped %d", m.counter(protocol.MetricDropped))
	}
	if m.gauge(protocol.MetricQueuedBytes) == 0 || m.gauge(protocol.MetricConnections) != 1 {
		t.Fatalf("gauges %v", m.gauges)
	}
	sock.release()
	waitFor(t, "the drain", func() bool { return !c.Queued() })
	got := sock.frames()
	want := []string{"0123456789-big-frame", "n3", "e3", "e4"}
	if len(got) != len(want) {
		t.Fatalf("frames %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frames %v, want %v", got, want)
		}
	}
	if built != 1 {
		t.Fatalf("built %d times: a superseded frame is never built", built)
	}
}

func TestConnClosesOnPinnedOverflow(t *testing.T) {
	sock := newSlowSocket()
	m := newCountMetrics()
	c := protocol.NewConn(sock, protocol.ConnOptions{HighWater: 4, HighWaterCeiling: 8, CapBase: 2, Meter: protocol.NewMeter(m)})
	congest(t, c)
	for _, v := range []string{"p1", "p2", "p3"} {
		c.SendOrdered([]byte(v), false)
	}
	select {
	case <-sock.aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("more undroppable frames than the cap close the connection")
	}
	<-c.Done()
	if m.counter(protocol.MetricClosedOverflow) != 1 {
		t.Fatalf("closed_overflow %d", m.counter(protocol.MetricClosedOverflow))
	}
	waitFor(t, "the gauges back to zero", func() bool {
		return m.gauge(protocol.MetricConnections) == 0 && m.gauge(protocol.MetricQueuedBytes) == 0
	})
}

func TestConnWatchdog(t *testing.T) {
	sock := newSlowSocket()
	c := protocol.NewConn(sock, protocol.ConnOptions{Watchdog: 30 * time.Millisecond})
	c.SendReliable([]byte("stuck"))
	select {
	case r := <-sock.aborted:
		if r == "" {
			t.Fatal("a reason")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a write without progress past the watchdog closes the connection")
	}
	<-c.Done()
}

func TestConnFlushAndClose(t *testing.T) {
	sock := newSlowSocket()
	c := protocol.NewConn(sock, protocol.ConnOptions{})
	c.SendReliable([]byte("bye"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := c.Flush(ctx); err == nil {
		t.Fatal("Flush waits for the write")
	}
	cancel()
	sock.release()
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	<-c.Done()
	c.SendReliable([]byte("after"))
	c.SendOrdered([]byte("after"), true)
	c.SendCoalescable("k", func() []byte { return []byte("after") })
	if got := sock.frames(); len(got) != 1 {
		t.Fatalf("frames after Close: %v", got)
	}
}
