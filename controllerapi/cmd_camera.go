// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera"
	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// The camera commands of schema 14 and send_webrtc_provider_command, each
// one call of go-fabric's controller/camera (brief S8 §3). The keys each
// command takes, in matterjs-server's order: cameraCommands.ts:188-199
// (video hints), :203-208 (audio hints), :212-220 (snapshot), :222-235
// (start), :237-268 (capabilities, stop, answer, ICE candidates, release),
// :271-279 (CAMERA_ARG_KEYS); send_webrtc_provider_command's
// handler.ts:174-179.
var (
	capabilitiesKeys  = []string{"node_id", "endpoint_id"}
	stopStreamKeys    = []string{"node_id", "endpoint_id", "webrtc_session_id", "client_session_key"}
	snapshotKeys      = []string{"node_id", "endpoint_id", "max_resolution", "resolution", "codec", "watermark_enabled", "osd_enabled"}
	releaseStreamKeys = []string{"node_id", "endpoint_id", "kind", "stream_id"}
	provideAnswerKeys = []string{"node_id", "endpoint_id", "webrtc_session_id", "sdp"}
	startStreamKeys   = []string{
		"node_id", "endpoint_id", "stream_usage", "sdp", "video", "audio", "ice_servers",
		"ice_transport_policy", "metadata_enabled", "allow_eviction", "talkback", "client_session_key",
	}
	videoHintKeys        = []string{"codecs", "min_resolution", "max_resolution", "min_frame_rate", "max_frame_rate", "min_bit_rate", "max_bit_rate", "watermark_enabled", "osd_enabled"}
	audioHintKeys        = []string{"codecs", "channel_count", "sample_rate", "bit_rate"}
	iceServerKeys        = []string{"urls", "username", "credential", "caid"}
	iceCandidatesKeys    = []string{"node_id", "endpoint_id", "webrtc_session_id", "client_session_key", "ice_candidates"}
	sendProviderKeys     = []string{"node_id", "endpoint_id", "command_name", "payload"}
	providerCommandNames = "ProvideOffer, SolicitOffer, ProvideAnswer, ProvideIceCandidates"
)

// cameraCommands are the commands the camera service answers; a backend
// without one does not offer them (gapOf).
var cameraCommands = map[string]bool{
	"camera_get_capabilities": true, "camera_snapshot": true, "camera_release_stream": true,
	"camera_stop_stream": true, "camera_provide_answer": true, "camera_start_stream": true,
	"camera_provide_ice_candidates": true, "send_webrtc_provider_command": true,
}

// cameraNode is the service and the node a camera command addresses,
// looked up once every argument is checked.
func (s *Server) cameraNode(command string, id *protocol.Uint64) (*camera.Service, *client.Node, *protocol.Error) {
	n, _, perr := s.node(command, id)
	if perr != nil {
		return nil, nil, perr
	}
	return s.cameraService(), n, nil
}

// sessionRef is how camera_stop_stream and camera_provide_ice_candidates
// name a session: exactly one of the id and the connection's key
// (docs/websockets_api.md:1681, :1710).
type sessionRef struct {
	id  uint16
	key string
}

func (a *cameraArgs) sessionRef() (sessionRef, *protocol.Error) {
	if a.has("webrtc_session_id") == a.has("client_session_key") {
		return sessionRef{}, protocol.Errorf(protocol.InvalidArguments, "%s requires exactly one of webrtc_session_id and client_session_key", a.command)
	}
	if a.has("client_session_key") {
		key, perr := a.clientSessionKey()
		return sessionRef{key: key}, perr
	}
	id, ok := uintOfJSON(a.raw("webrtc_session_id"), 0xFFFF)
	if !ok {
		return sessionRef{}, protocol.Errorf(protocol.InvalidArguments, "%s webrtc_session_id must be an integer between 0 and 65535", a.command)
	}
	return sessionRef{id: uint16(id)}, nil //nolint:gosec // G115: checked above
}

// clientSessionKey reads a stated client_session_key: a string of at
// least one character (docs/websockets_api.md:1410); the ceiling is
// go-fabric's (session.go:400-401).
func (a *cameraArgs) clientSessionKey() (string, *protocol.Error) {
	k, perr := a.str("client_session_key", "client_session_key")
	if perr != nil || k == nil || *k == "" {
		return "", protocol.Errorf(protocol.InvalidArguments, "client_session_key must be a non-empty string")
	}
	return *k, nil
}

// payloadOf is the arguments without the target: the provider command's
// fields as the client named them, which go-fabric matches with case and
// word separators ignored.
func (a *cameraArgs) payloadOf() json.RawMessage {
	fields := map[string]json.RawMessage{}
	for k, v := range a.fields {
		if k != "node_id" && k != "endpoint_id" {
			fields[k] = v
		}
	}
	b, _ := json.Marshal(fields)
	return b
}

// cmdCameraGetCapabilities is svc.Capabilities (docs/websockets_api.md:1263-1276,
// :1286-1370).
func cmdCameraGetCapabilities(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, capabilitiesKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	caps, err := svc.Capabilities(ctx, n, ep)
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	return wireCapabilities(caps), nil
}

// cmdCameraSnapshot is svc.Snapshot (docs/websockets_api.md:1722-1797).
func cmdCameraSnapshot(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, snapshotKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	req := camera.SnapshotRequest{Endpoint: ep}
	if req.MaxResolution, perr = a.resolution("max_resolution", "max_resolution"); perr != nil {
		return nil, perr
	}
	target, perr := a.object("resolution", "resolution", "resolution must be an object with a width, a height, or both", "width", "height")
	if perr != nil {
		return nil, perr
	}
	if target != nil {
		w, _, perr := target.number("width", "resolution.width", 0xFFFF)
		if perr != nil {
			return nil, perr
		}
		h, _, perr := target.number("height", "resolution.height", 0xFFFF)
		if perr != nil {
			return nil, perr
		}
		req.Resolution = &camera.ResolutionTarget{Width: uint16(w), Height: uint16(h)} //nolint:gosec // G115: checked above
	}
	if raw := a.raw("codec"); raw != nil {
		name, _ := a.str("codec", "codec")
		var codec csdef.ImageCodecEnum
		ok := false
		if name != nil {
			codec, ok = camera.ImageCodecByName(*name)
		}
		if !ok {
			return nil, protocol.Errorf(protocol.InvalidArguments, `camera_snapshot codec must be an image codec name such as "JPEG" or "HEIC", not %s`, raw)
		}
		req.Codec = &codec
	}
	if req.Overlays.Watermark, perr = a.boolean("watermark_enabled", "watermark_enabled"); perr != nil {
		return nil, perr
	}
	if req.Overlays.OSD, perr = a.boolean("osd_enabled", "osd_enabled"); perr != nil {
		return nil, perr
	}
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	req.Node = n
	snap, err := svc.Snapshot(ctx, req)
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	return wireSnapshot(snap), nil
}

// cmdCameraReleaseStream is svc.ReleaseStream (docs/websockets_api.md:1807-1831).
func cmdCameraReleaseStream(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, releaseStreamKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	kind, _ := a.str("kind", "kind")
	if kind == nil || *kind != string(camera.StreamVideo) && *kind != string(camera.StreamAudio) && *kind != string(camera.StreamSnapshot) {
		return nil, protocol.Errorf(protocol.InvalidArguments, `camera_release_stream requires kind to be "video", "audio", or "snapshot"`)
	}
	stream, ok := uintOfJSON(a.raw("stream_id"), 0xFFFF)
	if !ok {
		return nil, protocol.Errorf(protocol.InvalidArguments, "camera_release_stream stream_id must be an integer between 0 and 65535")
	}
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	if err := svc.ReleaseStream(ctx, n, ep, camera.StreamKind(*kind), uint16(stream)); err != nil { //nolint:gosec // G115: checked above
		return nil, cameraError(c.req.Command, err)
	}
	return nil, nil
}

// cmdCameraStopStream is svc.StopSession by id or svc.StopByKey by the
// connection's key, the connection the requester
// (docs/websockets_api.md:1690-1718).
func cmdCameraStopStream(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, stopStreamKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	ref, perr := a.sessionRef()
	if perr != nil {
		return nil, perr
	}
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	var (
		ended bool
		err   error
	)
	if ref.key != "" {
		ended, err = svc.StopByKey(ctx, n, ep, c.conn.id, ref.key)
	} else {
		ended, err = svc.StopSession(ctx, n, ep, ref.id, c.conn.id)
	}
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	return cameraStopResult{Ended: ended}, nil
}

// cmdCameraProvideAnswer sends ProvideAnswer on the raw route: the id
// need not be one this server opened (docs/websockets_api.md:1634-1658).
func cmdCameraProvideAnswer(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, provideAnswerKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	_, perr = s.providerSend(ctx, c, id, ep, "ProvideAnswer", a.payloadOf())
	return nil, perr
}

// providerSend sends one WebRtcTransportProvider command on the raw route
// (Provider.Send): go-fabric checks the payload against the cluster's
// model; its notices are logged.
func (s *Server) providerSend(ctx context.Context, c *call, id *protocol.Uint64, ep uint16, command string, payload json.RawMessage) (any, *protocol.Error) {
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	p, err := svc.Provider(n, ep)
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	result, notices, err := p.Send(ctx, command, payload)
	for _, notice := range notices {
		s.log.Info("fabricserver.camera.notice", slog.String("conn", c.conn.id), slog.String("command", c.req.Command), slog.String("notice", notice))
	}
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	return result, nil
}

// cmdCameraStartStream is svc.StartStream with the connection as the
// owner (docs/websockets_api.md:1376-1452). The session is handed to the
// connection's drain after the response (camera_events.go).
func cmdCameraStartStream(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, startStreamKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	req, perr := a.startRequest()
	if perr != nil {
		return nil, perr
	}
	svc, n, perr := s.cameraNode(c.req.Command, id)
	if perr != nil {
		return nil, perr
	}
	req.Node, req.Endpoint, req.Owner = n, ep, c.conn.id
	st := c.conn.beginStart(uint64(*id), ep, req.Key)
	c.ready()
	var sess *camera.Session
	// Set in a defer, so a panicking StartStream still leaves the start.
	defer func() { c.after = func() { c.conn.startAnswered(ctx, st, sess) } }()
	sess, err := svc.StartStream(ctx, req)
	if err != nil {
		return nil, cameraError(c.req.Command, err)
	}
	c.conn.startReturned(st, sess.ID)
	return wireStartResult(sess), nil
}

// startRequest reads camera_start_stream's arguments in matterjs-server's
// order; every range is go-fabric's (brief S8 §3b).
func (a *cameraArgs) startRequest() (camera.StartRequest, *protocol.Error) {
	var req camera.StartRequest
	usage, _ := a.str("stream_usage", "stream_usage")
	u, ok := csdef.StreamUsageEnum(0), false
	if usage != nil {
		u, ok = camera.StreamUsageByName(*usage)
	}
	if !ok || u == csdef.StreamUsageInternal {
		shown := "undefined"
		if usage != nil {
			shown = *usage
		} else if raw := a.raw("stream_usage"); raw != nil {
			shown = string(bytes.TrimSpace(raw))
		}
		return req, protocol.Errorf(protocol.InvalidArguments, "Unknown or device-only stream_usage %q", shown)
	}
	req.Usage = u
	var perr *protocol.Error
	if req.Offer, perr = a.str("sdp", "sdp"); perr != nil {
		return req, perr
	}
	if a.has("ice_servers") {
		if req.ICEServers, perr = a.iceServers(); perr != nil {
			return req, perr
		}
	}
	if a.has("ice_transport_policy") {
		p, _ := a.str("ice_transport_policy", "ice_transport_policy")
		if p == nil || *p == "" {
			return req, protocol.Errorf(protocol.InvalidArguments, "ice_transport_policy must be a non-empty string")
		}
		req.ICETransportPolicy = *p
	}
	metadata, perr := a.boolean("metadata_enabled", "metadata_enabled")
	if perr != nil {
		return req, perr
	}
	req.MetadataEnabled = metadata != nil && *metadata
	if req.AllowEviction, perr = a.boolean("allow_eviction", "allow_eviction"); perr != nil {
		return req, perr
	}
	talkback, perr := a.boolean("talkback", "talkback")
	if perr != nil {
		return req, perr
	}
	req.Talkback = talkback != nil && *talkback
	if a.has("client_session_key") {
		if req.Key, perr = a.clientSessionKey(); perr != nil {
			return req, perr
		}
	}
	// docs/websockets_api.md:1534.
	if req.Talkback && req.Offer == nil {
		return req, protocol.Errorf(protocol.InvalidArguments, "talkback: true requires sdp; without it the answer decides talkback")
	}
	if string(bytes.TrimSpace(a.raw("video"))) == "false" {
		req.DeclineVideo = true
	} else if req.Video, perr = a.videoBounds(); perr != nil {
		return req, perr
	}
	if string(bytes.TrimSpace(a.raw("audio"))) == "false" {
		req.DeclineAudio = true
	} else if req.Audio, perr = a.audioBounds(); perr != nil {
		return req, perr
	}
	return req, nil
}

// videoBounds reads the video hints, each fitting its go-fabric field.
func (a *cameraArgs) videoBounds() (*camera.VideoBounds, *protocol.Error) {
	h, perr := a.object("video", "video hint", "video hints must be an object", videoHintKeys...)
	if perr != nil || h == nil {
		return nil, perr
	}
	b := &camera.VideoBounds{}
	if b.Codecs, perr = h.strings("codecs", "video.codecs"); perr != nil {
		return nil, perr
	}
	if b.MinResolution, perr = h.resolution("min_resolution", "video.min_resolution"); perr != nil {
		return nil, perr
	}
	if b.MaxResolution, perr = h.resolution("max_resolution", "video.max_resolution"); perr != nil {
		return nil, perr
	}
	if b.MinFrameRate, perr = stated[uint16](h, "min_frame_rate", "video.min_frame_rate"); perr != nil {
		return nil, perr
	}
	if b.MaxFrameRate, perr = stated[uint16](h, "max_frame_rate", "video.max_frame_rate"); perr != nil {
		return nil, perr
	}
	if b.MinBitRate, perr = stated[uint32](h, "min_bit_rate", "video.min_bit_rate"); perr != nil {
		return nil, perr
	}
	if b.MaxBitRate, perr = stated[uint32](h, "max_bit_rate", "video.max_bit_rate"); perr != nil {
		return nil, perr
	}
	if b.Overlays.Watermark, perr = h.boolean("watermark_enabled", "video.watermark_enabled"); perr != nil {
		return nil, perr
	}
	if b.Overlays.OSD, perr = h.boolean("osd_enabled", "video.osd_enabled"); perr != nil {
		return nil, perr
	}
	return b, nil
}

// audioBounds reads the audio hints.
func (a *cameraArgs) audioBounds() (*camera.AudioBounds, *protocol.Error) {
	h, perr := a.object("audio", "audio hint", "audio hints must be an object", audioHintKeys...)
	if perr != nil || h == nil {
		return nil, perr
	}
	b := &camera.AudioBounds{}
	if b.Codecs, perr = h.strings("codecs", "audio.codecs"); perr != nil {
		return nil, perr
	}
	if b.ChannelCount, perr = stated[uint8](h, "channel_count", "audio.channel_count"); perr != nil {
		return nil, perr
	}
	if b.SampleRate, perr = stated[uint32](h, "sample_rate", "audio.sample_rate"); perr != nil {
		return nil, perr
	}
	if b.BitRate, perr = stated[uint32](h, "bit_rate", "audio.bit_rate"); perr != nil {
		return nil, perr
	}
	return b, nil
}

// stated reads a hint number fitting T, nil when the hint does not state
// it: go-fabric holds a stated value to its field's range, so a stated 0
// below a floor of 1 is refused there (controller/camera VideoBounds,
// AudioBounds).
func stated[T uint8 | uint16 | uint32](h *cameraArgs, key, field string) (*T, *protocol.Error) {
	v, ok, perr := h.number(key, field, uint64(^T(0)))
	if perr != nil || !ok {
		return nil, perr
	}
	t := T(v)
	return &t, nil
}

// iceServers reads ice_servers: entries {urls (one or a list), username?,
// credential?, caid?}, keys matched exactly; the limits are go-fabric's.
func (a *cameraArgs) iceServers() ([]wpdef.ICEServerStruct, *protocol.Error) {
	var entries []json.RawMessage
	if t := bytes.TrimSpace(a.raw("ice_servers")); len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &entries) != nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "ice_servers must be an array")
	}
	out := make([]wpdef.ICEServerStruct, 0, len(entries))
	for i, raw := range entries {
		field := fmt.Sprintf("ice_servers[%d]", i)
		holder := &cameraArgs{command: a.command, fields: map[string]json.RawMessage{"e": raw}}
		e, perr := holder.object("e", field, field+" must be an object", iceServerKeys...)
		if perr != nil {
			return nil, perr
		}
		if e == nil {
			return nil, protocol.Errorf(protocol.InvalidArguments, "%s must be an object", field)
		}
		var s wpdef.ICEServerStruct
		if u, err := e.str("urls", field+".urls"); err == nil && u != nil {
			s.Urls = []string{*u}
		} else if s.Urls, perr = e.strings("urls", field+".urls"); perr != nil {
			return nil, protocol.Errorf(protocol.InvalidArguments, "%s.urls must be a string or an array of strings", field)
		}
		if s.Username, perr = e.str("username", field+".username"); perr != nil {
			return nil, perr
		}
		if s.Credential, perr = e.str("credential", field+".credential"); perr != nil {
			return nil, perr
		}
		caid, stated, perr := e.number("caid", field+".caid", 0xFFFF)
		if perr != nil {
			return nil, perr
		}
		if stated {
			v := uint16(caid) //nolint:gosec // G115: checked above
			s.Caid = &v
		}
		out = append(out, s)
	}
	return out, nil
}

// iceCandidates reads ice_candidates through go-fabric's converter, the
// one Provider.Send runs (camera.ParseICECandidates; matterjs-server
// cameraCommands.ts:342-366 at 0bd06f2): at least one {candidate, sdpMid,
// sdpMLineIndex}, keys matched with case and word separators ignored, a
// refusal in its wording (docs/websockets_api.md:1660-1688).
func (a *cameraArgs) iceCandidates() ([]wpdef.ICECandidateStruct, *protocol.Error) {
	list, err := camera.ParseICECandidates(a.raw("ice_candidates"))
	if err != nil {
		return nil, cameraError(a.command, err)
	}
	return list, nil
}

// cmdCameraProvideICECandidates sends ProvideIceCandidates: by id on the
// provider's raw route, by key to the connection's session, queued while
// its start is unanswered (docs/websockets_api.md:1660-1688).
func cmdCameraProvideICECandidates(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, iceCandidatesKeys...)
	if perr != nil {
		return nil, perr
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	ref, perr := a.sessionRef()
	if perr != nil {
		return nil, perr
	}
	list, perr := a.iceCandidates()
	if perr != nil {
		return nil, perr
	}
	if ref.key == "" {
		c.ready()
		_, perr = s.providerSend(ctx, c, id, ep, "ProvideIceCandidates", a.payloadOf())
		return nil, perr
	}
	if _, _, perr := s.cameraNode(c.req.Command, id); perr != nil {
		return nil, perr
	}
	return nil, c.conn.candidatesByKey(ctx, uint64(*id), ep, ref.key, list, c.ready)
}

// cmdSendWebRTCProviderCommand is the provider's raw route
// (docs/websockets_api.md:1868-1905; WebSocketControllerHandler.ts:1902-1942):
// go-fabric matches the payload's keys, checks it against the model,
// fills the originating endpoint and registers an offer's session with
// the requestor (provider_route.go:46-60).
func cmdSendWebRTCProviderCommand(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	a, perr := decodeCameraArgs(c.req.Command, c.req.Args, sendProviderKeys...)
	if perr != nil {
		return nil, perr
	}
	if !a.has("command_name") {
		return nil, protocol.Errorf(protocol.InvalidArguments, "send_webrtc_provider_command requires command_name, one of %s", providerCommandNames)
	}
	name, _ := a.str("command_name", "command_name")
	if name == nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "Unsupported WebRTC provider command %s; expected one of %s", a.raw("command_name"), providerCommandNames)
	}
	id, ep, perr := a.target()
	if perr != nil {
		return nil, perr
	}
	result, perr := s.providerSend(ctx, c, id, ep, *name, a.raw("payload"))
	if perr != nil {
		return nil, perr
	}
	return providerResponseJSON(result), nil
}

// providerResponseJSON renders the generated ProvideOfferResponse or
// SolicitOfferResponse as device_command renders a response, through
// go-fabric's client.ValueOf: by the schema's property names, an absent
// optional field left out, a null one null. nil (ProvideAnswer,
// ProvideIceCandidates) is null.
func providerResponseJSON(v any) any {
	r, ok := v.(interface {
		client.Encodable
		ResponseCommand() (clusterID, commandID uint32)
	})
	if !ok {
		return nil
	}
	value, err := client.ValueOf(r)
	if err != nil {
		return nil
	}
	cluster, command := r.ResponseCommand()
	return responseJSON(cluster, command, value)
}
