// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"fmt"
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/camera/cameratest"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// cameraEndpoint is the camera's endpoint number on the site device.
func (e *env) cameraEndpoint() uint16 {
	e.t.Helper()
	ep, ok := e.camera.Device().EndpointNumber()
	if !ok {
		e.t.Fatal("the camera has no endpoint")
	}
	return ep
}

// TestCameraOnTheSite: the env's camera is commissioned with the light,
// its AV Stream Management cluster in the node data.
func TestCameraOnTheSite(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{camera: true})
	n := e.commission()
	prefix := fmt.Sprintf("%d/%d/", e.cameraEndpoint(), csdef.ClusterID)
	for k := range n.Attributes {
		if strings.HasPrefix(k, prefix) {
			return
		}
	}
	t.Fatalf("no attribute under %s", prefix)
}

// TestCameraCommands: the commands that need no session — capabilities,
// a snapshot, a release, a stop of an id the camera does not hold, an
// answer for one — and their refusals.
func TestCameraCommands(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{camera: true})
	n := e.commission()
	ep := e.cameraEndpoint()
	at := func(more map[string]any) map[string]any {
		args := map[string]any{"node_id": n.NodeID, "endpoint_id": ep}
		for k, v := range more {
			args[k] = v
		}
		return args
	}
	ctx := e.ctx

	var caps struct {
		Features []string `json:"features"`
		Video    struct {
			Codecs []string `json:"codecs"`
			Sensor struct {
				Width int `json:"width"`
			} `json:"sensor"`
		} `json:"video"`
		Snapshot struct {
			Capabilities []struct {
				ImageCodec string `json:"image_codec"`
			} `json:"capabilities"`
		} `json:"snapshot"`
		Limits struct {
			SupportedStreamUsages []string `json:"supported_stream_usages"`
		} `json:"limits"`
		Allocated struct {
			Video []struct {
				VideoStreamID     int    `json:"video_stream_id"`
				StreamUsage       string `json:"stream_usage"`
				AllocatedByServer bool   `json:"allocated_by_server"`
			} `json:"video"`
		} `json:"allocated"`
		Sessions []any `json:"sessions"`
	}
	if err := e.client.Call(ctx, "camera_get_capabilities", at(nil), &caps); err != nil {
		t.Fatal(err)
	}
	if strings.Join(caps.Features, ",") != "Audio,Video,Snapshot" || strings.Join(caps.Video.Codecs, ",") != "H264" || caps.Video.Sensor.Width != 1920 ||
		len(caps.Snapshot.Capabilities) != 1 || caps.Snapshot.Capabilities[0].ImageCodec != "JPEG" ||
		strings.Join(caps.Limits.SupportedStreamUsages, ",") != "LiveView,Recording" || len(caps.Allocated.Video) != 1 ||
		caps.Allocated.Video[0].StreamUsage != "LiveView" || caps.Allocated.Video[0].AllocatedByServer || caps.Sessions == nil {
		t.Fatalf("capabilities %+v", caps)
	}

	var snap struct {
		Data       string `json:"data"`
		Codec      string `json:"codec"`
		StreamID   int    `json:"stream_id"`
		Provenance string `json:"provenance"`
	}
	if err := e.client.Call(ctx, "camera_snapshot", at(map[string]any{"codec": "jpeg", "resolution": map[string]any{"width": 640}}), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Data != "/9g=" || snap.Codec != "JPEG" || snap.Provenance != "allocated" {
		t.Fatalf("snapshot %+v", snap)
	}
	if _, err := e.client.CallRaw(ctx, "camera_release_stream", at(map[string]any{"kind": "snapshot", "stream_id": snap.StreamID})); err != nil {
		t.Fatalf("release the snapshot stream: %v", err)
	}
	// A video stream the camera still references: it answers
	// INVALID_IN_STATE (0xCB, Matter Core §8.10.1), which is error 104.
	e.port.Refuse(cameratest.OpVideoStreamDeallocate, contract.StatusError{Status: 0xCB, Message: "stream 1 is in use"})
	_, err := e.client.CallRaw(ctx, "camera_release_stream", at(map[string]any{"kind": "video", "stream_id": cameratest.AllocatedVideoStream}))
	wantCode(t, err, protocol.CameraStreamInUse, `"stream_id":1`)
	e.port.Refuse(cameratest.OpVideoStreamDeallocate, nil)

	var stop struct {
		Ended bool `json:"ended"`
	}
	if err := e.client.Call(ctx, "camera_stop_stream", at(map[string]any{"webrtc_session_id": 99}), &stop); err != nil || stop.Ended {
		t.Fatalf("stop an unknown id: %+v %v", stop, err)
	}
	if err := e.client.Call(ctx, "camera_stop_stream", at(map[string]any{"client_session_key": "none"}), &stop); err != nil || stop.Ended {
		t.Fatalf("stop an unknown key: %+v %v", stop, err)
	}
	// An answer for a session the camera does not hold is the camera's own
	// NOT_FOUND (docs/websockets_api.md:1634-1658).
	_, err = e.client.CallRaw(ctx, "camera_provide_answer", at(map[string]any{"webrtc_session_id": 4, "sdp": "v=0 host"}))
	if pe := wantCode(t, err, protocol.UnknownError, ""); pe.Status == nil {
		t.Fatalf("no device status: %+v", pe)
	}
	_, err = e.client.CallRaw(ctx, "camera_provide_answer", at(map[string]any{"webrtc_session_id": 4}))
	wantCode(t, err, protocol.InvalidArguments, "")

	// Refusals.
	_, err = e.client.CallRaw(ctx, "camera_get_capabilities", map[string]any{"node_id": n.NodeID, "endpoint_id": e.lightEndpoint()})
	wantCode(t, err, protocol.CameraNotSupported, "missing_clusters")
	_, err = e.client.CallRaw(ctx, "camera_get_capabilities", at(map[string]any{"bogus": 1}))
	wantCode(t, err, protocol.InvalidArguments, "unknown camera_get_capabilities argument key: bogus. Accepted: node_id, endpoint_id")
	_, err = e.client.CallRaw(ctx, "camera_stop_stream", at(map[string]any{"webrtc_session_id": 1, "client_session_key": "k"}))
	wantCode(t, err, protocol.InvalidArguments, "exactly one of webrtc_session_id and client_session_key")
	_, err = e.client.CallRaw(ctx, "camera_stop_stream", at(nil))
	wantCode(t, err, protocol.InvalidArguments, "exactly one of webrtc_session_id and client_session_key")
	_, err = e.client.CallRaw(ctx, "camera_stop_stream", at(map[string]any{"webrtc_session_id": 65536}))
	wantCode(t, err, protocol.InvalidArguments, "between 0 and 65535")
	_, err = e.client.CallRaw(ctx, "camera_release_stream", at(map[string]any{"kind": "depth", "stream_id": 1}))
	wantCode(t, err, protocol.InvalidArguments, `kind to be "video", "audio", or "snapshot"`)
	_, err = e.client.CallRaw(ctx, "camera_snapshot", at(map[string]any{"codec": "PNG"}))
	wantCode(t, err, protocol.InvalidArguments, `not "PNG"`)
	_, err = e.client.CallRaw(ctx, "camera_snapshot", at(map[string]any{"resolution": map[string]any{}}))
	wantCode(t, err, protocol.InvalidArguments, "resolution")
	_, err = e.client.CallRaw(ctx, "camera_snapshot", map[string]any{"node_id": n.NodeID, "endpoint_id": 65535})
	wantCode(t, err, protocol.InvalidArguments, "endpoint_id")
	_, err = e.client.CallRaw(ctx, "camera_get_capabilities", map[string]any{"node_id": 4242, "endpoint_id": 1})
	wantCode(t, err, protocol.NodeNotExists, "4242")
}
