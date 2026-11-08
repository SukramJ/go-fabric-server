// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"log/slog"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/controller/camera"
	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// The camera commands of schema 14 and send_webrtc_provider_command, each
// one call of go-fabric's controller/camera (brief S8 §3). The keys each
// command takes, in matterjs-server's order (cameraCommands.ts:139-168).
var (
	capabilitiesKeys  = []string{"node_id", "endpoint_id"}
	stopStreamKeys    = []string{"node_id", "endpoint_id", "webrtc_session_id", "client_session_key"}
	snapshotKeys      = []string{"node_id", "endpoint_id", "max_resolution", "resolution", "codec", "watermark_enabled", "osd_enabled"}
	releaseStreamKeys = []string{"node_id", "endpoint_id", "kind", "stream_id"}
	provideAnswerKeys = []string{"node_id", "endpoint_id", "webrtc_session_id", "sdp"}
)

// cameraCommands are the commands the camera service answers; a backend
// without one does not offer them (gapOf).
var cameraCommands = map[string]bool{
	"camera_get_capabilities": true, "camera_snapshot": true, "camera_release_stream": true,
	"camera_stop_stream": true, "camera_provide_answer": true,
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
			codec, ok = imageCodecByName(*name)
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
