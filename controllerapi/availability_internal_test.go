// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/client"

	"thingfabric.dev/go-fabric-server/protocol"
)

// TestAvailableFor is matterjs-server's rule (Nodes.ts:122-137 at
// 0bd06f22): Connected or Reconnecting, and a restored node only once it
// reported (#81).
func TestAvailableFor(t *testing.T) {
	states := []client.ConnectionState{client.Connected, client.Reconnecting, client.WaitingForDeviceDiscovery, client.Disconnected}
	for _, state := range states {
		for _, restored := range []bool{false, true} {
			for _, primed := range []bool{false, true} {
				want := (state == client.Connected || state == client.Reconnecting) && (!restored || primed)
				if got := availableFor(state, restored, primed); got != want {
					t.Errorf("availableFor(%v, restored=%v, primed=%v) = %v, want %v", state, restored, primed, got, want)
				}
			}
		}
	}
}

// frameSocket records the frames a connection writes.
type frameSocket struct{ frames chan string }

func (f *frameSocket) Write(_ context.Context, frame []byte) error {
	f.frames <- string(frame)
	return nil
}
func (*frameSocket) Ping(context.Context) error { return nil }
func (*frameSocket) Abort(string)               {}

// cacheRig is a server with one listening connection and the cache
// transitions of one node driven by connection states, without a device.
type cacheRig struct {
	t    *testing.T
	s    *Server
	sock *frameSocket
	cur  *nodeState
}

func newCacheRig(t *testing.T, restored bool, state client.ConnectionState) *cacheRig {
	t.Helper()
	sock := &frameSocket{frames: make(chan string, 64)}
	c := &wsConn{send: protocol.NewConn(sock, protocol.ConnOptions{}), listening: true}
	t.Cleanup(c.send.Close)
	s := &Server{nodes: map[string]*nodeState{}, left: map[string]bool{}, conns: map[*wsConn]struct{}{c: {}}}
	r := &cacheRig{t: t, s: s, sock: sock}
	st := r.entry(state)
	st.restored = restored
	st.setState(state)
	s.nodes["n"] = st
	r.cur = st
	return r
}

func (r *cacheRig) entry(state client.ConnectionState) *nodeState {
	st := &nodeState{
		nodeID: 7, local: "n", attrs: map[string]json.RawMessage{"0/40/1": json.RawMessage(`"v"`)},
		clusters: map[string]clusterState{}, endpoints: map[uint16]bool{0: true}, parents: map[uint16]uint16{},
	}
	if r.cur != nil {
		st.primed, st.restored = r.cur.primed, r.cur.restored
	}
	st.setState(state)
	return st
}

// to moves the node to state and returns the frames that went out.
func (r *cacheRig) to(state client.ConnectionState) []string {
	r.t.Helper()
	st := r.entry(state)
	r.s.mu.Lock()
	r.s.applyLocked("n", r.cur, st)
	r.s.mu.Unlock()
	r.cur = st
	return r.drain()
}

func (r *cacheRig) drain() []string {
	var out []string
	for {
		select {
		case f := <-r.sock.frames:
			out = append(out, f)
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

func (r *cacheRig) listed(onlyAvailable bool) int {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return len(r.s.nodeListLocked(onlyAvailable))
}

func wantNodeUpdated(t *testing.T, frames []string, available bool) {
	t.Helper()
	want := `"available":false`
	if available {
		want = `"available":true`
	}
	if len(frames) != 1 || !strings.Contains(frames[0], `"node_updated"`) || !strings.Contains(frames[0], want) {
		t.Fatalf("want one node_updated with %s, got %q", want, frames)
	}
}

// A restored node that is Reconnecting at Start waits for its first
// report: unavailable, left out of only_available, and one node_updated
// with available true on Connected (ControllerCommandHandler.ts:949-951,
// :637-667).
func TestRestoredNodeWaitsForFirstReport(t *testing.T) {
	r := newCacheRig(t, true, client.Reconnecting)
	if r.cur.available || r.listed(true) != 0 || r.listed(false) != 1 {
		t.Fatalf("restored Reconnecting: available=%v listed(only)=%d", r.cur.available, r.listed(true))
	}
	wantNodeUpdated(t, r.to(client.Connected), true)
	if r.listed(true) != 1 {
		t.Fatal("a primed restored node is in only_available")
	}
}

// A commissioned node is available while Reconnecting; its first
// Connected primes it with one node_updated (ControllerCommandHandler.ts:654-655).
func TestCommissionedNodeAvailableWhileReconnecting(t *testing.T) {
	r := newCacheRig(t, false, client.Reconnecting)
	if !r.cur.available || r.cur.primed {
		t.Fatalf("commissioned Reconnecting: available=%v primed=%v", r.cur.available, r.cur.primed)
	}
	wantNodeUpdated(t, r.to(client.Connected), true)
	if !r.cur.primed {
		t.Fatal("not primed after Connected")
	}
}

// Connected -> Reconnecting -> Connected of a primed node changes nothing
// on the wire (ControllerCommandHandler.ts:1039-1058); Reconnecting ->
// WaitingForDeviceDiscovery makes it unavailable.
func TestPrimedNodeReconnecting(t *testing.T) {
	r := newCacheRig(t, true, client.Connected)
	if f := r.to(client.Reconnecting); len(f) != 0 || !r.cur.available {
		t.Fatalf("Connected -> Reconnecting: available=%v frames %q", r.cur.available, f)
	}
	if f := r.to(client.Connected); len(f) != 0 {
		t.Fatalf("Reconnecting -> Connected: frames %q", f)
	}
	r.to(client.Reconnecting)
	wantNodeUpdated(t, r.to(client.WaitingForDeviceDiscovery), false)
	if r.listed(true) != 0 {
		t.Fatal("an unavailable node is in only_available")
	}
}
