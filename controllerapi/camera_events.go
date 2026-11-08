// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// The camera's events (brief S8 §5): who receives them, and what waits
// for a camera_start_stream's response.
//
// A connection opts in by the commands it issues, before they are
// dispatched (WebSocketControllerHandler.ts:820-827 of matterjs-server
// at 0bd06f2, handler.ts below): any command of webrtcOptIn to the
// webrtc_callback of the sessions nobody owns (handler.ts:150-157), any
// camera_* command to camera_session_ended and camera_stream_evicted
// (handler.ts:159-171; send_webrtc_provider_command is schema 12 and does
// not count, docs/websockets_api.md:2294). The connection's id is the
// owner of the sessions it starts.
//
// A session's own signals are its Session.Signals channel, buffered by
// go-fabric from the camera's first signal and closed at the end
// (session.go:151-176): the handler hands the session to a drain once the
// response is sent (call.after), so the response that names the session
// reaches its connection before the first webrtc_callback of it, and no
// other connection sees them (docs/websockets_api.md:1408, :2255-2259).
// What is left to hold is the table of each unanswered start: the
// camera_session_ended of its session, and the candidates queued by key.
var (
	webrtcOptIn = map[string]bool{
		"send_webrtc_provider_command": true, "camera_start_stream": true,
		"camera_provide_answer": true, "camera_provide_ice_candidates": true,
	}
	cameraOptIn = map[string]bool{
		"camera_get_capabilities": true, "camera_start_stream": true, "camera_stop_stream": true,
		"camera_snapshot": true, "camera_release_stream": true, "camera_provide_answer": true,
		"camera_provide_ice_candidates": true,
	}
)

// maxQueuedCandidates bounds the candidates that wait for one start's
// response (startingSessionSignalling.ts:14-15).
const maxQueuedCandidates = 64

// releaseTimeout bounds the EndSessions of a closing connection's
// sessions.
const releaseTimeout = 10 * time.Second

// connCamera is one connection's part of the camera: its opt-ins, the
// sessions its starts were answered with, its starts not yet done.
type connCamera struct {
	webrtc, camera atomic.Bool

	mu       sync.Mutex
	starts   []*pendingStart
	sessions map[sessionAt]*ownedSession
}

// sessionAt names a session: the camera's node and endpoint, its id.
type sessionAt struct {
	node     uint64
	endpoint uint16
	id       uint16
}

// pendingStart is a camera_start_stream from dispatch until the
// candidates queued for it are sent.
type pendingStart struct {
	node     uint64
	endpoint uint16
	key      string
	// id is the session's id once StartStream returned (returned), before
	// the response is sent.
	id       uint16
	returned bool
	// answered: the response is sent; the start holds nothing more, and
	// stays only while its queue is sent.
	answered bool
	held     []camera.SessionEnd
	queue    []*queuedCandidates
	queued   int
}

// queuedCandidates is one camera_provide_ice_candidates waiting for its
// start; done carries its answer.
type queuedCandidates struct {
	list []wpdef.ICECandidateStruct
	done chan *protocol.Error
}

// ownedSession is a session handed out on this connection, until its end
// is delivered: drained once its Signals closed, end the end that
// arrived before.
type ownedSession struct {
	sess    *camera.Session
	drained bool
	end     *camera.SessionEnd
}

// CameraObserver is the observer the camera service reports to: pass it
// as camera.Config.Observer to camera.New, before Start binds the service
// (as Hooks goes into the controller's configuration before the
// controller exists). Its callbacks queue frames and never block
// (service.go:69-70); none takes the server's lock.
func (s *Server) CameraObserver() camera.Observer {
	return camera.Observer{
		SessionEnded:  s.cameraSessionEnded,
		StreamEvicted: s.cameraStreamEvicted,
		Signal:        s.cameraSignal,
	}
}

// cameraOptIn opts conn in for the events of command; run before the
// command is dispatched.
func (s *Server) cameraOptIn(conn *wsConn, command string) {
	w, c := webrtcOptIn[command], cameraOptIn[command]
	if !w && !c {
		return
	}
	if w {
		conn.cam.webrtc.Store(true)
	}
	if c {
		conn.cam.camera.Store(true)
	}
	s.camMu.Lock()
	if !conn.closed.Load() {
		s.camConns[conn.id] = conn
	}
	s.camMu.Unlock()
}

// cameraConns are the opted-in connections that want.
func (s *Server) cameraConns(want func(*wsConn) bool) []*wsConn {
	s.camMu.Lock()
	defer s.camMu.Unlock()
	out := make([]*wsConn, 0, len(s.camConns))
	for _, c := range s.camConns {
		if want(c) {
			out = append(out, c)
		}
	}
	return out
}

// cameraReleased drops a closing connection from the camera's events and
// ends the sessions it owns (handler.ts:810-812; service.go:316-323),
// within ctx and at most releaseTimeout: no event reaches it.
func (s *Server) cameraReleased(ctx context.Context, conn *wsConn) {
	s.camMu.Lock()
	_, opted := s.camConns[conn.id]
	delete(s.camConns, conn.id)
	s.camMu.Unlock()
	if svc := s.cameraService(); svc != nil && opted {
		ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
		defer cancel()
		svc.Release(ctx, conn.id)
	}
}

// cameraSignal is a signal of a session nobody owns (the raw route): to
// every connection opted in for webrtc_callback, without a key
// (docs/websockets_api.md:2261-2269; handler.ts:776-793).
func (s *Server) cameraSignal(sig camera.Signal) {
	frame := protocol.MustEncode(protocol.Event{Event: "webrtc_callback", Data: wireSignal(sig, "")})
	for _, c := range s.cameraConns(func(c *wsConn) bool { return c.cam.webrtc.Load() }) {
		c.send.SendReliable(frame)
	}
}

// cameraStreamEvicted goes to every connection opted in for the camera
// events, the requester included (docs/websockets_api.md:2326;
// handler.ts:743-760).
func (s *Server) cameraStreamEvicted(e camera.StreamEvicted) {
	frame := protocol.MustEncode(protocol.Event{Event: "camera_stream_evicted", Data: wireStreamEvicted(e)})
	for _, c := range s.cameraConns(func(c *wsConn) bool { return c.cam.camera.Load() }) {
		c.send.SendReliable(frame)
	}
}

// cameraSessionEnded announces an end (docs/websockets_api.md:2296-2305;
// handler.ts:717-728): never a release (the owner went away, or the
// server shuts down), never to the requester (it has its response); an
// owned session to its owner only, an ownerless one to every opted-in
// connection.
func (s *Server) cameraSessionEnded(e camera.SessionEnd) {
	if e.Cause == camera.EndReleased {
		return
	}
	if e.Owner == "" {
		frame := protocol.MustEncode(protocol.Event{Event: "camera_session_ended", Data: wireSessionEnded(e)})
		for _, c := range s.cameraConns(func(c *wsConn) bool { return c.cam.camera.Load() && c.id != e.RequestedBy }) {
			c.send.SendReliable(frame)
		}
		return
	}
	s.camMu.Lock()
	c := s.camConns[e.Owner]
	s.camMu.Unlock()
	if c != nil {
		c.sessionEnded(e)
	}
}

// sessionEnded routes the end of a session c owns: after the session's
// last signal, held while its start is unanswered (handler.ts:741), and
// dropped for an id this connection was never handed (a failed start
// can end).
func (c *wsConn) sessionEnded(e camera.SessionEnd) {
	k := sessionAt{e.NodeID, e.Endpoint, e.SessionID}
	c.cam.mu.Lock()
	if o := c.cam.sessions[k]; o != nil {
		if !o.drained {
			o.end = &e
			c.cam.mu.Unlock()
			return
		}
		delete(c.cam.sessions, k)
		c.cam.mu.Unlock()
		c.sendSessionEnded(e)
		return
	}
	// An end held for an unanswered start: the start whose StartStream
	// returned the id first (brief S8 §5), else the newest one still
	// running with the end's key — a session that ends before its start
	// returns fails that start, so such an end is dropped at the response.
	var held *pendingStart
	for i := len(c.cam.starts) - 1; i >= 0 && held == nil; i-- {
		if st := c.cam.starts[i]; !st.answered && st.returned && st.node == e.NodeID && st.endpoint == e.Endpoint && st.id == e.SessionID {
			held = st
		}
	}
	for i := len(c.cam.starts) - 1; i >= 0 && held == nil; i-- {
		if st := c.cam.starts[i]; !st.answered && !st.returned && st.node == e.NodeID && st.endpoint == e.Endpoint && st.key == e.Key {
			held = st
		}
	}
	if held != nil {
		held.held = append(held.held, e)
	}
	c.cam.mu.Unlock()
}

// startReturned records the id StartStream returned for st, before its
// response is sent.
func (c *wsConn) startReturned(st *pendingStart, id uint16) {
	c.cam.mu.Lock()
	st.id, st.returned = id, true
	c.cam.mu.Unlock()
}

func (c *wsConn) sendSessionEnded(e camera.SessionEnd) {
	if e.RequestedBy == c.id {
		return
	}
	c.send.SendReliable(protocol.MustEncode(protocol.Event{Event: "camera_session_ended", Data: wireSessionEnded(e)}))
}

// beginStart records a camera_start_stream before it is dispatched to
// the camera.
func (c *wsConn) beginStart(node uint64, endpoint uint16, key string) *pendingStart {
	st := &pendingStart{node: node, endpoint: endpoint, key: key}
	c.cam.mu.Lock()
	c.cam.starts = append(c.cam.starts, st)
	c.cam.mu.Unlock()
	return st
}

// startAnswered runs once the start's response is sent: the session (nil
// for a failed start) is handed to its drain, the end held for it
// follows its signals, the other held ends are dropped (no id of them was
// handed out), then the queued candidates go to the camera, in order, in
// one Session.ProvideICECandidates (startingSessionSignalling.ts:106-112,
// :178-227; go-fabric splits them per message, session.go:186-258).
func (c *wsConn) startAnswered(ctx context.Context, st *pendingStart, sess *camera.Session) {
	c.cam.mu.Lock()
	st.answered = true
	var (
		k sessionAt
		o *ownedSession
	)
	if sess != nil {
		k = sessionAt{st.node, st.endpoint, sess.ID}
		o = &ownedSession{sess: sess}
		for _, e := range st.held {
			if e.SessionID == sess.ID {
				end := e
				o.end = &end
			}
		}
		if c.cam.sessions == nil {
			c.cam.sessions = map[sessionAt]*ownedSession{}
		}
		c.cam.sessions[k] = o
	}
	st.held = nil
	c.cam.mu.Unlock()
	if o != nil {
		go c.drain(k, o)
	}
	for {
		c.cam.mu.Lock()
		batch := st.queue
		st.queue, st.queued = nil, 0
		if len(batch) == 0 {
			c.removeStartLocked(st)
			c.cam.mu.Unlock()
			return
		}
		open := o != nil && c.cam.sessions[k] == o && !o.drained
		c.cam.mu.Unlock()
		var perr *protocol.Error
		if open {
			var all []wpdef.ICECandidateStruct
			for _, q := range batch {
				all = append(all, q.list...)
			}
			if err := sess.ProvideICECandidates(ctx, all); err != nil {
				perr = cameraError("camera_provide_ice_candidates", err)
			}
		} else {
			perr = unknownSessionKey(st.key, st.node, st.endpoint)
		}
		for _, q := range batch {
			q.done <- perr
		}
	}
}

func (c *wsConn) removeStartLocked(st *pendingStart) {
	for i, s := range c.cam.starts {
		if s == st {
			c.cam.starts = append(c.cam.starts[:i], c.cam.starts[i+1:]...)
			return
		}
	}
}

// drain sends the session's signals as webrtc_callback, reliably
// (handler.ts:763-770: control-plane, never coalesced or dropped), then
// the session's end if it arrived meanwhile.
func (c *wsConn) drain(k sessionAt, o *ownedSession) {
	for sig := range o.sess.Signals() {
		c.send.SendReliable(protocol.MustEncode(protocol.Event{Event: "webrtc_callback", Data: wireSignal(sig, o.sess.Key)}))
	}
	c.cam.mu.Lock()
	o.drained = true
	end := o.end
	if end != nil && c.cam.sessions[k] == o {
		delete(c.cam.sessions, k)
	}
	c.cam.mu.Unlock()
	if end != nil {
		c.sendSessionEnded(*end)
	}
}

// candidatesByKey sends candidates to the session the connection named
// key on the node and endpoint: queued while its start is not done, else
// to the open session; a key naming neither is error 8
// (docs/websockets_api.md:1682-1684).
//
// ready is called once the candidates are queued or the session chosen,
// so the connection's next frame queues behind them.
func (c *wsConn) candidatesByKey(ctx context.Context, node uint64, endpoint uint16, key string, list []wpdef.ICECandidateStruct, ready func()) *protocol.Error {
	defer ready()
	c.cam.mu.Lock()
	for i := len(c.cam.starts) - 1; i >= 0; i-- {
		st := c.cam.starts[i]
		if st.node != node || st.endpoint != endpoint || st.key != key {
			continue
		}
		// startingSessionSignalling.ts:165-170.
		if st.queued+len(list) > maxQueuedCandidates {
			c.cam.mu.Unlock()
			return protocol.Errorf(protocol.InvalidArguments, "client_session_key %q: at most %d ICE candidates wait for a session that is still starting", key, maxQueuedCandidates)
		}
		q := &queuedCandidates{list: list, done: make(chan *protocol.Error, 1)}
		st.queue = append(st.queue, q)
		st.queued += len(list)
		c.cam.mu.Unlock()
		ready()
		select {
		case perr := <-q.done:
			return perr
		case <-ctx.Done():
			return nodeError("camera_provide_ice_candidates", ctx.Err())
		}
	}
	var sess *camera.Session
	for at, o := range c.cam.sessions {
		if at.node == node && at.endpoint == endpoint && o.sess.Key == key && !o.drained {
			sess = o.sess
			break
		}
	}
	c.cam.mu.Unlock()
	ready()
	if sess == nil {
		return unknownSessionKey(key, node, endpoint)
	}
	if err := sess.ProvideICECandidates(ctx, list); err != nil {
		return cameraError("camera_provide_ice_candidates", err)
	}
	return nil
}

func unknownSessionKey(key string, node uint64, endpoint uint16) *protocol.Error {
	return protocol.Errorf(protocol.InvalidArguments, "client_session_key %q names no session of this connection that is starting or open on node %d endpoint %d", key, node, endpoint)
}
