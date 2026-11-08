// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"context"
	"sync"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// newTestCamera is the site's camera, built as go-fabric's acceptance
// camera is (controller/camera/service_fabrictest_test.go:410-445): H.264
// up to 1920x1080 at 30 fps, one allocated live-view stream, a second
// stream the port refuses to release (VideoStreamDeallocate), a JPEG snapshot capability, and the provider and the
// encoders on port — def types only (brief S8 §2, §8).
func newTestCamera(t *testing.T, port *cameraPort) *device.Camera {
	t.Helper()
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	off := false
	streams := []csdef.VideoStreamStruct{{
		VideoStreamId: 1, StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 15, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: 100_000, MaxBitRate: 2_000_000, KeyFrameInterval: 4000, WatermarkEnabled: &off, OsdEnabled: &off,
	}, {
		VideoStreamId: 2, StreamUsage: csdef.StreamUsageRecording, VideoCodec: csdef.VideoCodecH264,
		MinFrameRate: 15, MaxFrameRate: 15,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480},
		MinBitRate: 100_000, MaxBitRate: 500_000, KeyFrameInterval: 4000, WatermarkEnabled: &off, OsdEnabled: &off,
	}}
	points := []csdef.RateDistortionTradeOffPointsStruct{{Codec: csdef.VideoCodecH264, Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MinBitRate: 100_000}}
	sensor := csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
	minView := csdef.VideoResolutionStruct{Width: 640, Height: 480}
	snapshots := []csdef.SnapshotCapabilitiesStruct{{Resolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxFrameRate: 1, ImageCodec: csdef.ImageCodecJpeg}}
	encoders := uint8(4)
	pixelRate := uint32(1920 * 1080 * 30 * 4)
	bandwidth := uint32(20_000_000)
	cam, err := device.NewCamera(device.Options{StableKey: device.StringKey("camera"), Name: "Camera"},
		csdef.Declare{
			Features: csdef.FeatureVideo | csdef.FeatureAudio | csdef.FeatureSnapshot,
			Initial: csdef.Patch{
				SupportedStreamUsages: &usages, StreamUsagePriorities: &usages, AllocatedVideoStreams: &streams,
				RateDistortionTradeOffPoints: &points, VideoSensorParams: &sensor, MinViewportResolution: &minView, SnapshotCapabilities: &snapshots,
				MaxConcurrentEncoders: &encoders, MaxEncodedPixelRate: &pixelRate, MaxNetworkBandwidth: &bandwidth,
			},
			VideoStreamPort: port, AudioStreamPort: port, SnapshotStreamPort: port, SnapshotCapturePort: port,
		},
		wpdef.Declare{ProviderPort: port},
	)
	if err != nil {
		t.Fatal(err)
	}
	return cam
}

// cameraPort is the camera's WebRTC stack and encoders: every offer
// becomes a session, every allocate a new stream id; it records what the
// controller sent.
type cameraPort struct {
	mu      sync.Mutex
	offers  []wpdef.OfferArgs
	ends    map[uint16]uint8
	cands   map[uint16][]string
	answers map[uint16]string
	nextID  uint16
	// gate, when set, holds ProvideOffer until it is closed; entered is
	// closed once an offer waits on it.
	gate, entered chan struct{}
	// exhausted refuses that many video allocations with
	// RESOURCE_EXHAUSTED (0x89, Matter Core §8.10.1), as a full camera does.
	exhausted int
}

func newCameraPort() *cameraPort {
	return &cameraPort{nextID: 10, ends: map[uint16]uint8{}, cands: map[uint16][]string{}, answers: map[uint16]string{}}
}

func (p *cameraPort) session(args wpdef.OfferArgs) wpdef.WebRTCSessionStruct {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offers = append(p.offers, args)
	return wpdef.WebRTCSessionStruct{
		Id: args.SessionID, PeerNodeId: args.PeerNodeID, PeerEndpointId: args.OriginatingEndpointID,
		StreamUsage: args.StreamUsage, VideoStreams: args.VideoStreams, AudioStreams: args.AudioStreams, FabricIndex: args.FabricIndex,
	}
}

func (p *cameraPort) SolicitOffer(_ context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, bool, error) {
	return p.session(args), false, nil
}

func (p *cameraPort) ProvideOffer(ctx context.Context, args wpdef.OfferArgs) (wpdef.WebRTCSessionStruct, error) {
	p.mu.Lock()
	gate, entered := p.gate, p.entered
	p.gate, p.entered = nil, nil
	p.mu.Unlock()
	if gate != nil {
		close(entered)
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	return p.session(args), nil
}

func (p *cameraPort) ProvideAnswer(_ context.Context, id uint16, sdp string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answers[id] = sdp
	return nil
}

func (p *cameraPort) ProvideIceCandidates(_ context.Context, id uint16, c []wpdef.ICECandidateStruct) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, x := range c {
		p.cands[id] = append(p.cands[id], x.Candidate)
	}
	return nil
}

func (p *cameraPort) EndSession(_ context.Context, id uint16, reason uint8) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ends[id] = reason
	return nil
}

func (p *cameraPort) VideoStreamAllocate(_ context.Context, args csdef.VideoStreamStruct) (uint16, error) {
	p.mu.Lock()
	if p.exhausted > 0 {
		p.exhausted--
		p.mu.Unlock()
		return 0, contract.StatusError{Status: 0x89, Message: "no encoder free"}
	}
	p.mu.Unlock()
	if args.VideoStreamId != 0 {
		return args.VideoStreamId, nil
	}
	return p.newID(), nil
}

func (p *cameraPort) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}

func (p *cameraPort) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }

// VideoStreamDeallocate refuses stream 2 as a camera that still
// references it does, with INVALID_IN_STATE (0xCB, Matter Core §8.10.1).
func (p *cameraPort) VideoStreamDeallocate(_ context.Context, id uint16) error {
	if id == 2 {
		return contract.StatusError{Status: 0xCB, Message: "stream 2 is in use"}
	}
	return nil
}

func (p *cameraPort) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	return p.newID(), nil
}

func (p *cameraPort) AudioStreamDeallocate(context.Context, uint16) error { return nil }

func (p *cameraPort) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	return p.newID(), nil
}

func (p *cameraPort) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error { return nil }

func (p *cameraPort) SnapshotStreamDeallocate(context.Context, uint16) error { return nil }

func (p *cameraPort) CaptureSnapshot(_ context.Context, _ *uint16, r csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{Data: []byte{0xFF, 0xD8}, ImageCodec: csdef.ImageCodecJpeg, Resolution: r}, nil
}

func (p *cameraPort) newID() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	return p.nextID
}
