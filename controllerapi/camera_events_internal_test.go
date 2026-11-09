// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
)

func candidates(n int) []wpdef.ICECandidateStruct {
	out := make([]wpdef.ICECandidateStruct, n)
	for i := range out {
		out[i].Candidate = "c"
	}
	return out
}

func noReady() {}

// waitQueued waits until st holds n queued candidates.
func waitQueued(t *testing.T, c *wsConn, st *pendingStart, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c.cam.mu.Lock()
		q := st.queued
		c.cam.mu.Unlock()
		if q == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("not %d queued", n)
}

// TestQueuedCandidatesLimit: at most 64 candidates wait for one start
// (startingSessionSignalling.ts:14-15, :165-170).
func TestQueuedCandidatesLimit(t *testing.T) {
	c := &wsConn{}
	if perr := c.candidatesByKey(context.Background(), 1, 1, "k", candidates(1), noReady); perr == nil || perr.ErrorCode != protocol.InvalidArguments {
		t.Fatalf("a key without a start or a session: %v", perr)
	}
	st := c.beginStart(1, 1, "k")
	if perr := c.candidatesByKey(context.Background(), 1, 1, "k", candidates(65), noReady); perr == nil || !strings.Contains(perr.Details, "at most 64") {
		t.Fatalf("65 at once: %v", perr)
	}
	done := make(chan *protocol.Error, 1)
	go func() { done <- c.candidatesByKey(context.Background(), 1, 1, "k", candidates(40), noReady) }()
	waitQueued(t, c, st, 40)
	if perr := c.candidatesByKey(context.Background(), 1, 1, "k", candidates(25), noReady); perr == nil || !strings.Contains(perr.Details, "at most 64") {
		t.Fatalf("40 + 25: %v", perr)
	}
	// The start fails: the queued command is error 8, the start is gone.
	c.startAnswered(context.Background(), st, nil)
	if perr := <-done; perr == nil || perr.ErrorCode != protocol.InvalidArguments || !strings.Contains(perr.Details, "names no session") {
		t.Fatalf("candidates of a failed start: %v", perr)
	}
	c.cam.mu.Lock()
	left := len(c.cam.starts)
	c.cam.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d starts left", left)
	}
}

// TestQueuedCandidatesCancelled: a command waiting for its start answers
// when its request's context ends.
func TestQueuedCandidatesCancelled(t *testing.T) {
	c := &wsConn{}
	c.beginStart(1, 1, "k")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *protocol.Error, 1)
	readied := make(chan struct{})
	go func() {
		var once sync.Once // ready is idempotent, as call.ready is
		done <- c.candidatesByKey(ctx, 1, 1, "k", candidates(1), func() { once.Do(func() { close(readied) }) })
	}()
	<-readied
	cancel()
	if perr := <-done; perr == nil {
		t.Fatal("a cancelled wait answered nil")
	}
}

// TestEndHeldByID: an end that arrives while starts are unanswered is
// held on the start whose StartStream returned its id, not on the newest
// start of the node and endpoint (brief S8 §5).
func TestEndHeldByID(t *testing.T) {
	c := &wsConn{}
	a := c.beginStart(1, 1, "")
	b := c.beginStart(1, 1, "")
	c.startReturned(a, 3)
	c.sessionEnded(camera.SessionEnd{NodeID: 1, Endpoint: 1, SessionID: 3, Cause: camera.EndPeer})
	if len(a.held) != 1 || len(b.held) != 0 {
		t.Fatalf("held on a %d, on b %d", len(a.held), len(b.held))
	}
	// An end no returned start names waits on the newest running one,
	// whose start it fails; it is dropped at the response.
	c.sessionEnded(camera.SessionEnd{NodeID: 1, Endpoint: 1, SessionID: 9, Cause: camera.EndPeer})
	if len(b.held) != 1 {
		t.Fatalf("held on b %d", len(b.held))
	}
	c.startAnswered(context.Background(), b, nil)
	c.cam.mu.Lock()
	defer c.cam.mu.Unlock()
	if len(c.cam.sessions) != 0 || len(c.cam.starts) != 1 {
		t.Fatalf("sessions %d starts %d", len(c.cam.sessions), len(c.cam.starts))
	}
}

// TestSignalRefusedIsNoEvent: the requestor's refusal of a command for a
// session it does not hold (camera.SignalRefused, go-fabric v0.19.1)
// reaches no connection — matterjs-server forwards the requestor's offer,
// answer, iceCandidates and end alone (WebRtcCallbackBridge.ts:51-122 at
// 0bd06f2). The opted-in connection has no send path: a frame would panic.
func TestSignalRefusedIsNoEvent(t *testing.T) {
	c := &wsConn{id: "c"}
	c.cam.webrtc.Store(true)
	s := &Server{camConns: map[string]*wsConn{"c": c}}
	s.cameraSignal(camera.Signal{Kind: camera.SignalRefused})
}
