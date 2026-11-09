// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
)

func ptr[T any](v T) *T { return &v }

func res(w, h uint16) csdef.VideoResolutionStruct {
	return csdef.VideoResolutionStruct{Width: w, Height: h}
}

// sameAsFixture holds v's encoding against a python fixture, modulo key
// order.
func sameAsFixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pythonDir, name))
	if err != nil {
		t.Fatal(err)
	}
	got := protocol.MustEncode(v)
	if !sameModuloExtensions(normalize(t, b), normalize(t, got)) {
		t.Fatalf("%s\nwant %s\ngot  %s", name, b, got)
	}
}

// TestWireCapabilities: go-fabric's Capabilities in matterjs-server's
// shape — the names, the optional keys absent while nil, the overlays
// false while unstated.
func TestWireCapabilities(t *testing.T) {
	off := false
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	c := &camera.Capabilities{
		Features: []string{"Audio", "Video", "Snapshot"},
		Privacy:  camera.Privacy{Hard: &off},
		Video: camera.VideoCapabilities{
			Sensor: ptr(res(1920, 1080)), MinViewport: ptr(res(640, 480)), MaxFPS: ptr(uint16(30)), HDRCapable: &off,
			RateDistortionPoints: []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: res(640, 480), MinBitRate: 100_000}},
			Codecs:               []csdef.VideoCodecEnum{csdef.VideoCodecH264},
		},
		Audio: camera.AudioCapabilities{
			Codecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus}, Channels: ptr(uint8(2)), SampleRates: []uint32{48000},
			BitDepths: []uint8{16}, TwoWayTalkSupport: ptr(csdef.TwoWayTalkSupportTypeHalfDuplex),
		},
		Snapshot: []csdef.SnapshotCapabilitiesStruct{{Resolution: res(640, 480), MaxFrameRate: 1, ImageCodec: csdef.ImageCodecJpeg}},
		Limits: camera.Limits{
			MaxEncodedPixelRate: ptr(uint32(248832000)), MaxConcurrentEncoders: ptr(uint8(4)), MaxNetworkBandwidth: ptr(uint32(20_000_000)),
			SupportedStreamUsages: usages, StreamUsagePriorities: usages,
		},
		Allocated: camera.Allocated{
			Video: []camera.AllocatedVideo{{VideoStreamStruct: csdef.VideoStreamStruct{
				VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264, MinFrameRate: 15, MaxFrameRate: 30,
				MinResolution: res(640, 480), MaxResolution: res(1280, 720), MinBitRate: 100_000, MaxBitRate: 2_000_000, ReferenceCount: 1,
			}, AllocatedByUs: true}},
			Audio: []camera.AllocatedAudio{{AudioStreamStruct: csdef.AudioStreamStruct{
				AudioStreamId: 2, StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 2,
				SampleRate: 48000, BitRate: 64000, BitDepth: 16, ReferenceCount: 1,
			}, AllocatedByUs: true}},
			Snapshot: []camera.AllocatedSnapshot{{SnapshotStreamStruct: csdef.SnapshotStreamStruct{
				SnapshotStreamId: 3, ImageCodec: csdef.ImageCodecJpeg, FrameRate: 1, MinResolution: res(640, 480), MaxResolution: res(640, 480),
			}}},
		},
		Sessions: []camera.DeviceSession{{
			ID: 3, PeerNodeID: 112233, PeerEndpoint: 1, StreamUsage: uint8(csdef.StreamUsageLiveView),
			VideoStreams: []uint16{1}, AudioStreams: []uint16{2}, EstablishedByUs: true,
		}},
	}
	sameAsFixture(t, "camera_get_capabilities.value.json", wireCapabilities(c))

	// Nil features and an empty camera: the key absent, the lists empty.
	b := string(protocol.MustEncode(wireCapabilities(&camera.Capabilities{})))
	if strings.Contains(b, "features") || strings.Contains(b, "null") || !strings.Contains(b, `"sessions":[]`) {
		t.Fatalf("empty capabilities %s", b)
	}
	if b := string(protocol.MustEncode(wireCapabilities(&camera.Capabilities{Features: []string{}}))); !strings.Contains(b, `"features":[]`) {
		t.Fatalf("a stated empty feature list %s", b)
	}
}

func TestWireStartResultAndSnapshot(t *testing.T) {
	s := &camera.Session{
		ID: 3, Mode: camera.ModeProvideOffer, Talkback: true, Evicted: []uint16{7},
		Streams: []camera.Lease{{
			Kind: camera.StreamVideo, ID: 12, Provenance: camera.ProvenanceAllocated,
			Video: &camera.VideoEnvelope{
				Codec: csdef.VideoCodecH264, MinResolution: res(1280, 720), MaxResolution: res(1280, 720),
				MinFrameRate: 30, MaxFrameRate: 30, MinBitRate: 4_000_000, MaxBitRate: 4_000_000,
			},
			BudgetNarrowed: &camera.BudgetNarrowing{MaxResolution: ptr(res(1920, 1080))},
		}, {
			Kind: camera.StreamAudio, ID: 2, Provenance: camera.ProvenanceReused,
			Audio: &camera.AudioEnvelope{Codec: csdef.AudioCodecOpus, ChannelCount: 2, SampleRate: 48000, BitRate: 64000, BitDepth: 16},
		}},
	}
	sameAsFixture(t, "camera_start_stream.value.json", wireStartResult(s))

	// No lease: null tracks, no evicted or narrowed keys.
	b := string(protocol.MustEncode(wireStartResult(&camera.Session{ID: 1, Mode: camera.ModeSolicitOffer})))
	if b != `{"webrtc_session_id":1,"mode":"solicit_offer","video":null,"audio":null,"talkback":false}` {
		t.Fatalf("bare start %s", b)
	}

	sameAsFixture(t, "camera_snapshot.value.json", wireSnapshot(&camera.Snapshot{
		Data: []byte{0xFF, 0xD8}, Codec: csdef.ImageCodecJpeg, Resolution: res(640, 480), StreamID: 3, Provenance: camera.ProvenanceAllocated,
	}))
}

func TestWireSignals(t *testing.T) {
	session := wrdef.WebRTCSessionStruct{Id: 3, PeerNodeId: 1, PeerEndpointId: 1, FabricIndex: 1}
	policy := "all"
	servers := []wrdef.ICEServerStruct{{Urls: []string{"stun:stun.example.org:3478"}}}
	offer := wireSignal(camera.Signal{Kind: camera.SignalOffer, Session: session, SDP: "v=0\r\n...", Offer: wrdef.OfferRequest{IceServers: &servers, IceTransportPolicy: &policy}}, "")
	b, err := os.ReadFile(filepath.Join(compatDir, "webrtc_callback.event.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := protocol.MustEncode(protocol.Event{Event: "webrtc_callback", Data: offer}); !sameModuloExtensions(normalize(t, b), normalize(t, got)) {
		t.Fatalf("offer %s", got)
	}

	var c wrdef.ICECandidateStruct
	c.Candidate = "candidate:1 1 UDP 2130706431 192.0.2.1 50000 typ host"
	c.SdpMid.Value = "0"
	c.SdpmLineIndex.Null = true
	cands := wireSignal(camera.Signal{Kind: camera.SignalICECandidates, Session: session, Candidates: []wrdef.ICECandidateStruct{c}}, "living-room-1")
	sameAsFixture(t, "webrtc_callback.event.ice_candidates.json", protocol.Event{Event: "webrtc_callback", Data: cands})

	for _, c := range []struct {
		sig  camera.Signal
		want string
	}{
		{camera.Signal{Kind: camera.SignalAnswer, Session: session, SDP: "a"}, `"event_type":"answer","webrtc_session_id":3,"node_id":1,"endpoint_id":1,"fabric_index":1,"data":{"sdp":"a"}`},
		{camera.Signal{Kind: camera.SignalEnd, Session: session, Reason: 2}, `"event_type":"end","webrtc_session_id":3,"node_id":1,"endpoint_id":1,"fabric_index":1,"data":{"reason":2}`},
	} {
		if got := string(protocol.MustEncode(wireSignal(c.sig, ""))); got != "{"+c.want+"}" {
			t.Errorf("%s", got)
		}
	}

	ended := string(protocol.MustEncode(wireSessionEnded(camera.SessionEnd{NodeID: 1, Endpoint: 1, SessionID: 3, Cause: camera.EndPeer, Reason: ptr(uint8(2)), Key: "k"})))
	if ended != `{"node_id":1,"endpoint_id":1,"webrtc_session_id":3,"cause":"peer","reason":2,"client_session_key":"k"}` {
		t.Errorf("ended %s", ended)
	}
	evicted := string(protocol.MustEncode(wireStreamEvicted(camera.StreamEvicted{NodeID: 1, Endpoint: 1, Kind: camera.StreamVideo, StreamID: 7})))
	if evicted != `{"node_id":1,"endpoint_id":1,"kind":"video","stream_id":7}` {
		t.Errorf("evicted %s", evicted)
	}
}

// TestCameraArgs: the strict decoder's refusals.
func TestCameraArgs(t *testing.T) {
	for raw, want := range map[string]string{
		`"x"`: "camera_snapshot requires an object of arguments",
		`[1]`: "camera_snapshot requires an object of arguments",
		`{"node_id":1,"endpoint_id":1,"x":1,"y":2}`: "unknown camera_snapshot argument key: x, y. Accepted: node_id, endpoint_id, codec",
	} {
		_, perr := decodeCameraArgs("camera_snapshot", json.RawMessage(raw), "node_id", "endpoint_id", "codec")
		if perr == nil || perr.ErrorCode != protocol.InvalidArguments || perr.Details != want {
			t.Errorf("%s: %v", raw, perr)
		}
	}
	for _, raw := range []string{``, `null`, `{}`} {
		a, perr := decodeCameraArgs("camera_snapshot", json.RawMessage(raw), "node_id")
		if perr != nil || a.has("node_id") {
			t.Errorf("%q: %v", raw, perr)
		}
	}
	a, _ := decodeCameraArgs("c", json.RawMessage(`{"node_id":1,"endpoint_id":65535}`), "node_id", "endpoint_id")
	if _, _, perr := a.target(); perr == nil {
		t.Error("endpoint 65535 accepted")
	}
	a, _ = decodeCameraArgs("c", json.RawMessage(`{"node_id":-1,"endpoint_id":1}`), "node_id", "endpoint_id")
	if _, _, perr := a.target(); perr == nil {
		t.Error("node -1 accepted")
	}
	a, _ = decodeCameraArgs("c", json.RawMessage(`{"node_id":1,"endpoint_id":1.0}`), "node_id", "endpoint_id")
	if id, ep, perr := a.target(); perr != nil || *id != 1 || ep != 1 {
		t.Errorf("1.0: %v", perr)
	}
	for _, v := range []string{`1.5`, `"1"`, `-1`, `70000`, `null`, `true`} {
		if _, ok := uintOfJSON(json.RawMessage(v), 0xFFFF); ok {
			t.Errorf("%s accepted", v)
		}
	}
	a, _ = decodeCameraArgs("c", json.RawMessage(`{"r":{"width":1},"s":{"width":1,"height":2,"depth":3},"b":1,"l":["a",1]}`), "r", "s", "b", "l")
	if _, stated, perr := a.number("b", "b", 10); !stated || perr != nil {
		t.Errorf("b: %v %v", stated, perr)
	}
	if _, stated, perr := a.number("none", "none", 10); stated || perr != nil {
		t.Errorf("none: %v %v", stated, perr)
	}
	a2, _ := decodeCameraArgs("c", json.RawMessage(`{"r":{"width":640,"height":480}}`), "r")
	if r, perr := a2.resolution("r", "r"); perr != nil || r.Width != 640 || r.Height != 480 {
		t.Errorf("resolution %v %v", r, perr)
	}
	if _, perr := a.resolution("r", "r"); perr == nil {
		t.Error("a resolution without height")
	}
	if _, perr := a.resolution("s", "s"); perr == nil || !strings.Contains(perr.Details, "unknown s key: depth") {
		t.Errorf("depth: %v", perr)
	}
	if _, perr := a.boolean("b", "b"); perr == nil {
		t.Error("1 as a boolean")
	}
	if _, perr := a.strings("l", "l"); perr == nil {
		t.Error("a mixed list")
	}
	if _, perr := a.str("b", "b"); perr == nil {
		t.Error("1 as a string")
	}
}
