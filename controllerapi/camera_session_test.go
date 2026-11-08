// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// The offer of the tests: one H.264 video section the host only receives
// on (go-fabric's acceptance offer).
const testOffer = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H264/90000\r\na=recvonly\r\n"

// rawFrame is one frame a raw connection read, in order.
type rawFrame struct {
	MessageID string          `json:"message_id"`
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Result    json.RawMessage `json:"result"`
	ErrorCode *int            `json:"error_code"`
	Details   string          `json:"details"`
}

// rawConn is a connection that sees every frame in the order the server
// sent it: what a response-before-event rule is measured on.
type rawConn struct {
	t      *testing.T
	ws     *websocket.Conn
	frames chan rawFrame
	seq    atomic.Uint64
	// kept are the frames read and not matched yet, in order.
	kept []rawFrame
}

func (e *env) raw() *rawConn {
	e.t.Helper()
	ws, resp, err := websocket.Dial(e.ctx, e.url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		e.t.Fatal(err)
	}
	ws.SetReadLimit(16 << 20)
	r := &rawConn{t: e.t, ws: ws, frames: make(chan rawFrame, 256)}
	if _, _, err := ws.Read(e.ctx); err != nil { // server_info
		e.t.Fatal(err)
	}
	go func() {
		for {
			_, b, err := ws.Read(context.Background())
			if err != nil {
				close(r.frames)
				return
			}
			var f rawFrame
			if json.Unmarshal(b, &f) == nil {
				r.frames <- f
			}
		}
	}()
	e.t.Cleanup(func() { _ = ws.CloseNow() })
	return r
}

// send sends a command and returns its message id.
func (r *rawConn) send(command string, args map[string]any) string {
	r.t.Helper()
	id := strconv.FormatUint(r.seq.Add(1), 10)
	b := protocol.MustEncode(map[string]any{"message_id": id, "command": command, "args": args})
	if err := r.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		r.t.Fatal(err)
	}
	return id
}

// next is the first frame match accepts, the frames read before it
// returned too; those stay for a later next.
func (r *rawConn) next(match func(rawFrame) bool) (found rawFrame, before []rawFrame) {
	r.t.Helper()
	for i, f := range r.kept {
		if match(f) {
			before = append([]rawFrame(nil), r.kept[:i]...)
			r.kept = append(r.kept[:i], r.kept[i+1:]...)
			return f, before
		}
	}
	skipped := append([]rawFrame(nil), r.kept...)
	timeout := time.After(30 * time.Second)
	for {
		select {
		case f, ok := <-r.frames:
			if !ok {
				r.t.Fatal("the connection closed")
			}
			if match(f) {
				return f, skipped
			}
			skipped = append(skipped, f)
			r.kept = append(r.kept, f)
		case <-timeout:
			r.t.Fatalf("no such frame; read %+v", skipped)
		}
	}
}

func response(id string) func(rawFrame) bool {
	return func(f rawFrame) bool { return f.MessageID == id }
}

func eventOf(name string) func(rawFrame) bool {
	return func(f rawFrame) bool { return f.Event == name }
}

// quiet holds that no frame match accepts was read or arrives within a
// short while.
func (r *rawConn) quiet(match func(rawFrame) bool) {
	r.t.Helper()
	for _, f := range r.kept {
		if match(f) {
			r.t.Fatalf("unexpected frame %+v", f)
		}
	}
	timeout := time.After(300 * time.Millisecond)
	for {
		select {
		case f, ok := <-r.frames:
			if ok && match(f) {
				r.t.Fatalf("unexpected frame %+v", f)
			}
			if ok {
				r.kept = append(r.kept, f)
			}
		case <-timeout:
			return
		}
	}
}

// call sends a command and waits for its answer: the result, or the
// failure.
func (r *rawConn) call(command string, args map[string]any) rawFrame {
	r.t.Helper()
	f, _ := r.next(response(r.send(command, args)))
	return f
}

// cameraSignaller is the camera side of the device's WebRTC provider
// (go-fabric cluster/camera Provider): the signals a camera sends to the
// controller's requestor.
type cameraSignaller interface {
	Offer(ctx context.Context, sessionID uint16, sdp string) error
	Answer(ctx context.Context, sessionID uint16, sdp string) error
	IceCandidates(ctx context.Context, sessionID uint16, candidates []wpdef.ICECandidateStruct) error
	End(ctx context.Context, sessionID uint16, reason uint8) error
}

func (e *env) signaller() cameraSignaller {
	e.t.Helper()
	p, ok := e.camera.WebRtcTransportProvider().Underlying().(cameraSignaller)
	if !ok {
		e.t.Fatal("the camera serves no signalling provider")
	}
	return p
}

// requestorHolds waits until the controller's requestor holds the
// session: the camera's signals reach the controller from then on.
func (e *env) requestorHolds(id uint16) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range e.ctrl.CameraRequestor().Sessions() {
			if s.Id == id {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatalf("the requestor does not hold session %d", id)
}

type startResult struct {
	WebRTCSessionID uint16 `json:"webrtc_session_id"`
	Mode            string `json:"mode"`
	Video           *struct {
		StreamID uint16 `json:"stream_id"`
	} `json:"video"`
}

type callbackData struct {
	EventType        string          `json:"event_type"`
	WebRTCSessionID  uint16          `json:"webrtc_session_id"`
	ClientSessionKey string          `json:"client_session_key"`
	Data             json.RawMessage `json:"data"`
}

type endedData struct {
	WebRTCSessionID  uint16 `json:"webrtc_session_id"`
	Cause            string `json:"cause"`
	ClientSessionKey string `json:"client_session_key"`
}

// cameraSite is a commissioned camera and the arguments addressing it.
func cameraSite(t *testing.T) (e *env, at func(map[string]any) map[string]any) {
	t.Helper()
	e = newEnv(t, envOptions{camera: true})
	n := e.commission()
	ep := e.cameraEndpoint()
	return e, func(more map[string]any) map[string]any {
		args := map[string]any{"node_id": n.NodeID, "endpoint_id": ep}
		for k, v := range more {
			args[k] = v
		}
		return args
	}
}

// TestCameraStartHoldsUntilResponse: the candidates a client sends by key
// while its start is unanswered wait, and reach the camera after the
// response in order; the response precedes the session's first event,
// which carries the key; a stop by key ends it without an event to the
// requester.
func TestCameraStartHoldsUntilResponse(t *testing.T) {
	t.Parallel()
	e, at := cameraSite(t)
	a := e.raw()

	gate, entered := make(chan struct{}), make(chan struct{})
	e.port.mu.Lock()
	e.port.gate, e.port.entered = gate, entered
	e.port.mu.Unlock()
	start := a.send("camera_start_stream", at(map[string]any{"stream_usage": "LiveView", "sdp": testOffer, "client_session_key": "living-room"}))
	<-entered
	cand := func(c string) map[string]any {
		return at(map[string]any{"client_session_key": "living-room", "ice_candidates": []map[string]any{{"candidate": c, "sdpMid": "0", "sdpMLineIndex": 0}}})
	}
	// Each request runs on its own goroutine, so the two are spaced to
	// queue in their order; both queue before the camera answers.
	c1 := a.send("camera_provide_ice_candidates", cand("candidate:1"))
	time.Sleep(50 * time.Millisecond)
	c2 := a.send("camera_provide_ice_candidates", cand("candidate:2"))
	time.Sleep(50 * time.Millisecond)
	close(gate)

	f, before := a.next(response(start))
	if f.ErrorCode != nil {
		t.Fatalf("start: %s", f.Details)
	}
	for _, b := range before {
		if b.MessageID == c1 || b.MessageID == c2 || b.Event != "" {
			t.Fatalf("%+v before the start's response", b)
		}
	}
	var res startResult
	if err := json.Unmarshal(f.Result, &res); err != nil || res.Mode != "provide_offer" || res.Video == nil {
		t.Fatalf("start %s %v", f.Result, err)
	}
	for _, id := range []string{c1, c2} {
		if f, _ := a.next(response(id)); f.ErrorCode != nil {
			t.Fatalf("candidates %s: %s", id, f.Details)
		}
	}
	e.port.mu.Lock()
	got := e.port.cands[res.WebRTCSessionID]
	e.port.mu.Unlock()
	if len(got) != 2 || got[0] != "candidate:1" || got[1] != "candidate:2" {
		t.Fatalf("the camera got %v", got)
	}

	// The camera answers; the event carries the key.
	e.requestorHolds(res.WebRTCSessionID)
	if err := e.signaller().Answer(e.ctx, res.WebRTCSessionID, "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=sendrecv\r\n"); err != nil {
		t.Fatal(err)
	}
	ev, _ := a.next(eventOf("webrtc_callback"))
	var cb callbackData
	if err := json.Unmarshal(ev.Data, &cb); err != nil || cb.EventType != "answer" || cb.WebRTCSessionID != res.WebRTCSessionID || cb.ClientSessionKey != "living-room" {
		t.Fatalf("callback %s", ev.Data)
	}

	// More candidates by key go straight to the open session.
	if f := a.call("camera_provide_ice_candidates", cand("candidate:3")); f.ErrorCode != nil {
		t.Fatalf("candidates after the start: %s", f.Details)
	}
	// A stop by key: ended, no camera_session_ended to the requester.
	f = a.call("camera_stop_stream", at(map[string]any{"client_session_key": "living-room"}))
	if string(f.Result) != `{"ended":true}` {
		t.Fatalf("stop %s %s", f.Result, f.Details)
	}
	a.quiet(eventOf("camera_session_ended"))
	// The key is free again; one that names nothing is error 8.
	if f := a.call("camera_provide_ice_candidates", cand("candidate:4")); f.ErrorCode == nil || *f.ErrorCode != int(protocol.InvalidArguments) {
		t.Fatalf("candidates for an ended key: %+v", f)
	}
}

// TestCameraOwnership: another connection stops the first's session — the
// owner hears other_connection, the requester nothing; a connection that
// issued no camera command hears nothing; the camera's end reaches the
// owner as webrtc_callback end and camera_session_ended peer; a closing
// connection's sessions end on the camera.
func TestCameraOwnership(t *testing.T) {
	t.Parallel()
	e, at := cameraSite(t)
	owner, other, bystander := e.raw(), e.raw(), e.raw()
	bystander.call("get_nodes", nil)

	startOn := func(c *rawConn, key string) startResult {
		t.Helper()
		f := c.call("camera_start_stream", at(map[string]any{"stream_usage": "LiveView", "sdp": testOffer, "client_session_key": key}))
		var res startResult
		if f.ErrorCode != nil || json.Unmarshal(f.Result, &res) != nil {
			t.Fatalf("start: %s %s", f.Result, f.Details)
		}
		return res
	}
	first := startOn(owner, "k")
	other.call("camera_get_capabilities", at(nil))
	if f := other.call("camera_stop_stream", at(map[string]any{"webrtc_session_id": first.WebRTCSessionID})); string(f.Result) != `{"ended":true}` {
		t.Fatalf("stop by id %s %s", f.Result, f.Details)
	}
	ev, _ := owner.next(eventOf("camera_session_ended"))
	var ended endedData
	if err := json.Unmarshal(ev.Data, &ended); err != nil || ended.Cause != "other_connection" || ended.WebRTCSessionID != first.WebRTCSessionID || ended.ClientSessionKey != "k" {
		t.Fatalf("ended %s", ev.Data)
	}
	other.quiet(eventOf("camera_session_ended"))

	// The same through device_command EndSession on another connection.
	second := startOn(owner, "k2")
	if f := other.call("device_command", at(map[string]any{
		"cluster_id": wpdef.ClusterID, "command_name": "EndSession",
		"payload": map[string]any{"webRtcSessionId": second.WebRTCSessionID, "reason": 2},
	})); f.ErrorCode != nil {
		t.Fatalf("device_command EndSession: %s", f.Details)
	}
	ev, _ = owner.next(eventOf("camera_session_ended"))
	if err := json.Unmarshal(ev.Data, &ended); err != nil || ended.Cause != "other_connection" || ended.WebRTCSessionID != second.WebRTCSessionID {
		t.Fatalf("ended by EndSession %s", ev.Data)
	}

	// The camera ends a session: end, then the peer's camera_session_ended.
	third := startOn(owner, "k3")
	e.requestorHolds(third.WebRTCSessionID)
	if err := e.signaller().End(e.ctx, third.WebRTCSessionID, 3); err != nil {
		t.Fatal(err)
	}
	ev, _ = owner.next(eventOf("webrtc_callback"))
	var cb callbackData
	if err := json.Unmarshal(ev.Data, &cb); err != nil || cb.EventType != "end" || cb.ClientSessionKey != "k3" {
		t.Fatalf("end %s", ev.Data)
	}
	ev, _ = owner.next(eventOf("camera_session_ended"))
	if err := json.Unmarshal(ev.Data, &ended); err != nil || ended.Cause != "peer" || ended.WebRTCSessionID != third.WebRTCSessionID {
		t.Fatalf("ended by the peer %s", ev.Data)
	}
	other.quiet(eventOf("webrtc_callback"))
	bystander.quiet(func(f rawFrame) bool { return f.Event == "webrtc_callback" || f.Event == "camera_session_ended" })

	// A closing connection's session ends on the camera.
	fourth := startOn(owner, "k4")
	_ = owner.ws.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(30 * time.Second)
	for {
		e.port.mu.Lock()
		_, ended := e.port.ends[fourth.WebRTCSessionID]
		e.port.mu.Unlock()
		if ended {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the closed connection's session was not ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCameraRawRoute: a session opened with send_webrtc_provider_command
// has no owner — the camera's answer reaches every connection opted in for
// WebRTC, not one that issued only a camera_* command; the solicited
// offer of a camera_start_stream without sdp is answered with
// camera_provide_answer.
func TestCameraRawRoute(t *testing.T) {
	t.Parallel()
	e, at := cameraSite(t)
	a, b, camOnly := e.raw(), e.raw(), e.raw()
	b.call("camera_provide_answer", at(map[string]any{"webrtc_session_id": 999, "sdp": "x"})) // opts b in; the camera refuses
	camOnly.call("camera_get_capabilities", at(nil))

	f := a.call("send_webrtc_provider_command", at(map[string]any{
		"command_name": "ProvideOffer",
		"payload":      map[string]any{"webRtcSessionId": nil, "sdp": testOffer, "streamUsage": 3, "videoStreams": []int{1}},
	}))
	var resp struct {
		WebRTCSessionID *uint16 `json:"webRtcSessionId"`
	}
	if f.ErrorCode != nil || json.Unmarshal(f.Result, &resp) != nil || resp.WebRTCSessionID == nil {
		t.Fatalf("ProvideOffer %s %s", f.Result, f.Details)
	}
	id := *resp.WebRTCSessionID
	e.requestorHolds(id)
	if err := e.signaller().Answer(e.ctx, id, "v=0\r\na=sendonly\r\n"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*rawConn{a, b} {
		ev, _ := c.next(eventOf("webrtc_callback"))
		var cb callbackData
		if err := json.Unmarshal(ev.Data, &cb); err != nil || cb.EventType != "answer" || cb.WebRTCSessionID != id || cb.ClientSessionKey != "" {
			t.Fatalf("broadcast %s", ev.Data)
		}
	}
	camOnly.quiet(eventOf("webrtc_callback"))

	// SolicitOffer: the camera's offer arrives, the answer goes back.
	f = a.call("camera_start_stream", at(map[string]any{"stream_usage": "LiveView"}))
	var res startResult
	if f.ErrorCode != nil || json.Unmarshal(f.Result, &res) != nil || res.Mode != "solicit_offer" {
		t.Fatalf("solicit %s %s", f.Result, f.Details)
	}
	e.requestorHolds(res.WebRTCSessionID)
	if err := e.signaller().Offer(e.ctx, res.WebRTCSessionID, "v=0 camera offer"); err != nil {
		t.Fatal(err)
	}
	ev, _ := a.next(eventOf("webrtc_callback"))
	var cb callbackData
	if err := json.Unmarshal(ev.Data, &cb); err != nil || cb.EventType != "offer" {
		t.Fatalf("offer %s", ev.Data)
	}
	if f := a.call("camera_provide_answer", at(map[string]any{"webrtc_session_id": res.WebRTCSessionID, "sdp": "v=0 host answer"})); f.ErrorCode != nil {
		t.Fatalf("answer: %s", f.Details)
	}
	e.port.mu.Lock()
	answer := e.port.answers[res.WebRTCSessionID]
	e.port.mu.Unlock()
	if answer != "v=0 host answer" {
		t.Fatalf("the camera got %q", answer)
	}
	// By id, candidates go on the raw route.
	if f := a.call("camera_provide_ice_candidates", at(map[string]any{"webrtc_session_id": res.WebRTCSessionID, "ice_candidates": []map[string]any{{"candidate": "c", "sdpMid": nil, "sdpMLineIndex": nil}}})); f.ErrorCode != nil {
		t.Fatalf("candidates by id: %s", f.Details)
	}
}

// TestCameraStartRefusals: the start's argument checks and go-fabric's
// typed refusals.
func TestCameraStartRefusals(t *testing.T) {
	t.Parallel()
	e, at := cameraSite(t)
	a := e.raw()
	code := func(f rawFrame) int {
		if f.ErrorCode == nil {
			t.Fatalf("no error: %s", f.Result)
		}
		return *f.ErrorCode
	}
	for _, args := range []map[string]any{
		{"stream_usage": "Internal"},
		{"stream_usage": "Bogus"},
		{"stream_usage": "LiveView", "talkback": true},
		{"stream_usage": "LiveView", "video": true},
		{"stream_usage": "LiveView", "video": map[string]any{"frames": 1}},
		{"stream_usage": "LiveView", "video": map[string]any{"min_frame_rate": 70000}},
		{"stream_usage": "LiveView", "ice_servers": []any{map[string]any{"urls": 1}}},
		{"stream_usage": "LiveView", "ice_transport_policy": ""},
		{"stream_usage": "LiveView", "client_session_key": ""},
		{"stream_usage": "LiveView", "metadata_enabled": "yes"},
	} {
		if c := code(a.call("camera_start_stream", at(args))); c != int(protocol.InvalidArguments) {
			t.Errorf("%v: %d", args, c)
		}
	}
	// A codec the H.264-only camera lacks is error 102 codec.
	f := a.call("camera_start_stream", at(map[string]any{"stream_usage": "LiveView", "sdp": testOffer, "video": map[string]any{"codecs": []string{"H265"}}}))
	if code(f) != int(protocol.CameraStreamIncompatible) || !json.Valid([]byte(f.Details)) {
		t.Fatalf("102: %s", f.Details)
	}
	var d struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal([]byte(f.Details), &d) != nil || d.Reason != "codec" {
		t.Fatalf("102 reason: %s", f.Details)
	}
	// A key in use on the connection is error 8.
	if f := a.call("camera_start_stream", at(map[string]any{"stream_usage": "LiveView", "sdp": testOffer, "client_session_key": "dup"})); f.ErrorCode != nil {
		t.Fatalf("start: %s", f.Details)
	}
	if c := code(a.call("camera_start_stream", at(map[string]any{"stream_usage": "LiveView", "sdp": testOffer, "client_session_key": "dup"}))); c != int(protocol.InvalidArguments) {
		t.Fatalf("a duplicate key: %d", c)
	}
	// The light has no camera clusters.
	if c := code(a.call("camera_start_stream", map[string]any{"node_id": at(nil)["node_id"], "endpoint_id": e.lightEndpoint(), "stream_usage": "LiveView"})); c != int(protocol.CameraNotSupported) {
		t.Fatalf("the light: %d", c)
	}
	// The ICE command's own checks.
	for _, args := range []map[string]any{
		{"ice_candidates": []any{}},
		{"client_session_key": "dup", "webrtc_session_id": 1, "ice_candidates": []any{map[string]any{"candidate": "c", "sdpMid": nil, "sdpMLineIndex": nil}}},
		{"client_session_key": "dup", "ice_candidates": []any{map[string]any{"candidate": "c"}}},
		{"client_session_key": "dup", "ice_candidates": []any{map[string]any{"candidate": "c", "sdpMid": nil, "sdpMLineIndex": nil, "x": 1}}},
		{"client_session_key": "none", "ice_candidates": []any{map[string]any{"candidate": "c", "sdp_mid": "0", "sdpMLineIndex": 0}}},
	} {
		if c := code(a.call("camera_provide_ice_candidates", at(args))); c != int(protocol.InvalidArguments) {
			t.Errorf("%v: %d", args, c)
		}
	}
	for _, args := range []map[string]any{{}, {"command_name": 3}, {"command_name": "EndSession", "payload": map[string]any{}}} {
		if c := code(a.call("send_webrtc_provider_command", at(args))); c != int(protocol.InvalidArguments) {
			t.Errorf("send %v: %d", args, c)
		}
	}
}

// TestCameraStreamEvicted: a start that has to make room evicts an idle
// stream; every connection opted in for the camera events hears of it,
// the requester included, and the response lists it.
func TestCameraStreamEvicted(t *testing.T) {
	t.Parallel()
	e, at := cameraSite(t)
	a, b := e.raw(), e.raw()
	b.call("camera_get_capabilities", at(nil))
	e.port.mu.Lock()
	e.port.exhausted = 2
	e.port.mu.Unlock()
	f := a.call("camera_start_stream", at(map[string]any{
		"stream_usage": "LiveView", "sdp": testOffer,
		"video": map[string]any{"min_resolution": map[string]any{"width": 1280, "height": 720}, "min_frame_rate": 30},
	}))
	if f.ErrorCode != nil {
		t.Fatalf("start: %s", f.Details)
	}
	var res struct {
		Video struct {
			EvictedStreamIDs []uint16 `json:"evicted_stream_ids"`
		} `json:"video"`
	}
	if json.Unmarshal(f.Result, &res) != nil || len(res.Video.EvictedStreamIDs) == 0 {
		t.Fatalf("no eviction: %s", f.Result)
	}
	for _, c := range []*rawConn{a, b} {
		ev, _ := c.next(eventOf("camera_stream_evicted"))
		var d struct {
			Kind     string `json:"kind"`
			StreamID uint16 `json:"stream_id"`
		}
		if json.Unmarshal(ev.Data, &d) != nil || d.Kind != "video" || d.StreamID != res.Video.EvictedStreamIDs[0] {
			t.Fatalf("evicted %s", ev.Data)
		}
	}
}
