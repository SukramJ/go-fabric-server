// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// fixtureError is a python fixture's error.
func fixtureError(t *testing.T, name string) protocol.Error {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pythonDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var e protocol.Error
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// TestCameraErrors: each typed error of go-fabric becomes its code with
// the details byte for byte as matterjs-server orders them.
func TestCameraErrors(t *testing.T) {
	privacy := &camera.PrivacyError{Modes: []camera.PrivacyMode{camera.PrivacySoftLivestream, camera.PrivacyHard}, DeviceStatus: 0xCB}
	maxEncoders := 2
	for name, err := range map[string]error{
		"camera_start_stream.error.json": &camera.IncompatibleError{
			Reason: camera.ReasonBounds, Track: camera.TrackVideo, Device: []string{"H264"}, Requested: []string{"H264"},
			Bound: &camera.IncompatibleBound{Field: camera.BoundMinResolution, Requested: "3840x2160", Limit: "1920x1080"},
		},
		"camera_snapshot.error.json": &camera.ExhaustedError{
			Allocated:   []camera.OccupyingStream{{Kind: camera.StreamVideo, StreamID: 1, ReferenceCount: 1}, {Kind: camera.StreamSnapshot, StreamID: 3}},
			MaxEncoders: &maxEncoders,
		},
		"camera_release_stream.error.json":       fmt.Errorf("release: %w", &camera.InUseError{StreamID: 1, ReferenceCount: 1}),
		"camera_get_capabilities.error.json":     &camera.NotSupportedError{Missing: []uint32{csdef.ClusterID, wpdef.ClusterID}},
		"camera_start_stream.error.privacy.json": privacy,
	} {
		want := fixtureError(t, name)
		got := cameraError(name, err)
		if got.ErrorCode != want.ErrorCode || got.Details != want.Details {
			t.Errorf("%s:\nwant %d %s\ngot  %d %s", name, want.ErrorCode, want.Details, got.ErrorCode, got.Details)
		}
	}

	st := privacy.DeviceStatus
	for _, c := range []struct {
		err  *camera.IncompatibleError
		want string
	}{
		{
			&camera.IncompatibleError{Reason: camera.ReasonFeature, Track: camera.TrackAudio, Feature: "Audio", DeviceStatus: &st},
			`{"message":"Camera does not advertise the feature this request needs","reason":"feature","track":"audio","feature":"Audio","device":[],"requested":[],"device_status":203}`,
		},
		{
			&camera.IncompatibleError{Reason: camera.ReasonNoMedia, Track: camera.TrackVideo},
			`{"message":"The request leaves no media for the session to carry","reason":"no_media","device":[],"requested":[]}`,
		},
		{
			&camera.IncompatibleError{Reason: camera.ReasonBounds, Requested: []string{"H264"}},
			`{"message":"Camera cannot serve the requested stream parameters","reason":"bounds","device":[],"requested":["H264"]}`,
		},
		{
			&camera.IncompatibleError{Reason: camera.ReasonCodec, Track: camera.TrackVideo, Device: []string{"H264"}, Requested: []string{"H265"}},
			`{"message":"No codec supported by both the camera and the caller","reason":"codec","track":"video","device":["H264"],"requested":["H265"]}`,
		},
	} {
		if got := cameraError("cmd", c.err).Details; got != c.want {
			t.Errorf("102\nwant %s\ngot  %s", c.want, got)
		}
	}

	if got := cameraError("cmd", &camera.InUseError{StreamID: 4}).Details; got != `{"message":"Stream is in use and cannot be released","stream_id":4}` {
		t.Errorf("104 without a count: %s", got)
	}
	if got := cameraError("cmd", camera.ErrStreamInUse); got.ErrorCode != protocol.CameraStreamInUse {
		t.Errorf("the bare sentinel: %v", got)
	}
	if got := cameraError("cmd", &camera.ExhaustedError{}).Details; got != `{"message":"Camera has no capacity for this stream","allocated":[]}` {
		t.Errorf("103 without limits: %s", got)
	}
	if got := cameraError("cmd", &camera.StackError{}); got.ErrorCode != protocol.SDKStackError {
		t.Errorf("stack: %v", got)
	}
	if got := cameraError("cmd", fmt.Errorf("x: %w", camera.ErrInvalidArgument)); got.ErrorCode != protocol.InvalidArguments {
		t.Errorf("argument: %v", got)
	}
	if got := cameraError("cmd", errors.New("other")); got.ErrorCode != protocol.UnknownError {
		t.Errorf("other: %v", got)
	}
	if got := cameraError("cmd", context.DeadlineExceeded); got.ErrorCode != protocol.NodeNotResolving {
		t.Errorf("unreachable: %v", got)
	}
}
