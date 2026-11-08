// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// The wire names of the camera codecs: matterjs-server's SDP spellings,
// not the enum member names the schema carries (Hevc, Vvc, Av1, AacLc) —
// a wire-contract table (wireNames.ts:16-31 of matterjs-server at
// 0bd06f2; docs/websockets_api.md:1843-1849), the ids and values staying
// the defs'. A value without a name is its decimal digits both ways, and
// a name matches without regard to case (wireNames.ts:44-73). go-fabric
// holds the same table unexported (brief S8 §10(a)).
var (
	videoCodecNames = map[csdef.VideoCodecEnum]string{
		csdef.VideoCodecH264: "H264", csdef.VideoCodecHevc: "H265", csdef.VideoCodecVvc: "H266", csdef.VideoCodecAv1: "AV1",
	}
	audioCodecNames = map[csdef.AudioCodecEnum]string{csdef.AudioCodecOpus: "OPUS", csdef.AudioCodecAacLc: "AAC"}
	imageCodecNames = map[csdef.ImageCodecEnum]string{csdef.ImageCodecJpeg: "JPEG", csdef.ImageCodecHeic: "HEIC"}
)

func wireName[E ~uint8](names map[E]string, v E) string {
	if n, ok := names[v]; ok {
		return n
	}
	return strconv.Itoa(int(v))
}

func videoCodecName(v csdef.VideoCodecEnum) string { return wireName(videoCodecNames, v) }
func audioCodecName(v csdef.AudioCodecEnum) string { return wireName(audioCodecNames, v) }
func imageCodecName(v csdef.ImageCodecEnum) string { return wireName(imageCodecNames, v) }

// imageCodecByName is the image codec a name spells, a known name or the
// decimal digits of a value.
func imageCodecByName(name string) (csdef.ImageCodecEnum, bool) {
	for v, n := range imageCodecNames {
		if strings.EqualFold(n, name) {
			return v, true
		}
	}
	if name != "" && strings.Trim(name, "0123456789") == "" {
		if n, err := strconv.ParseUint(name, 10, 8); err == nil {
			return csdef.ImageCodecEnum(n), true
		}
	}
	return 0, false
}

// enumValues are the member names of the enum an AV Stream Management
// attribute (or its list entry) is typed with, from protocol/schema: the
// stream usages and the talkback modes are the enum member names
// (wireNames.ts:41-42 reads them off the enum).
func enumValues(attribute string) []schema.EnumValue {
	a := schema.ClusterByID(csdef.ClusterID).AttributeByName(attribute)
	if a == nil {
		return nil
	}
	t := &a.Type
	if t.Entry != nil {
		t = t.Entry
	}
	if t.Enum == nil {
		return nil
	}
	return t.Enum.Values
}

func enumName(values []schema.EnumValue, v uint64) string {
	for _, e := range values {
		if e.Value == v {
			return e.Name
		}
	}
	return strconv.FormatUint(v, 10)
}

func streamUsageName(v csdef.StreamUsageEnum) string {
	return enumName(enumValues("SupportedStreamUsages"), uint64(v))
}

func twoWayTalkSupportName(v csdef.TwoWayTalkSupportTypeEnum) string {
	return enumName(enumValues("TwoWayTalkSupport"), uint64(v))
}

// streamUsageByName is the usage a member name spells, without regard to
// case; only member names are accepted.
func streamUsageByName(name string) (csdef.StreamUsageEnum, bool) {
	for _, e := range enumValues("SupportedStreamUsages") {
		if strings.EqualFold(e.Name, name) {
			return csdef.StreamUsageEnum(e.Value), true //nolint:gosec // G115: an 8-bit enum's value
		}
	}
	return 0, false
}

// The results (docs/websockets_api.md:1286-1370, :1603-1630, :1743-1748;
// packages/ws-client/src/models/model.ts:608-678, :704-764).

type wireResolution struct {
	Width  uint16 `json:"width"`
	Height uint16 `json:"height"`
}

func resolutionOf(r csdef.VideoResolutionStruct) wireResolution {
	return wireResolution{Width: r.Width, Height: r.Height}
}

func resolutionPtr(r *csdef.VideoResolutionStruct) *wireResolution {
	if r == nil {
		return nil
	}
	w := resolutionOf(*r)
	return &w
}

type cameraCapabilities struct {
	Features  *[]string       `json:"features,omitempty"`
	Privacy   cameraPrivacy   `json:"privacy"`
	Video     cameraVideoCaps `json:"video"`
	Audio     cameraAudioCaps `json:"audio"`
	Snapshot  cameraSnapCaps  `json:"snapshot"`
	Limits    cameraLimits    `json:"limits"`
	Allocated cameraAllocated `json:"allocated"`
	Sessions  []cameraSession `json:"sessions"`
}

type cameraPrivacy struct {
	SoftRecording  *bool `json:"soft_recording_mode_enabled,omitempty"`
	SoftLivestream *bool `json:"soft_livestream_mode_enabled,omitempty"`
	Hard           *bool `json:"hard_mode_on,omitempty"`
}

type cameraVideoCaps struct {
	Sensor               *wireResolution       `json:"sensor,omitempty"`
	MinViewport          *wireResolution       `json:"min_viewport,omitempty"`
	MaxFPS               *uint16               `json:"max_fps,omitempty"`
	MaxHDRFPS            *uint16               `json:"max_hdr_fps,omitempty"`
	HDRCapable           *bool                 `json:"hdr_capable,omitempty"`
	RateDistortionPoints []cameraRateDistPoint `json:"rate_distortion_points"`
	Codecs               []string              `json:"codecs"`
}

type cameraRateDistPoint struct {
	Codec      string         `json:"codec"`
	Resolution wireResolution `json:"resolution"`
	MinBitRate uint32         `json:"min_bit_rate"`
}

type cameraAudioCaps struct {
	Codecs            []string `json:"codecs"`
	Channels          *uint8   `json:"channels,omitempty"`
	SampleRates       []uint32 `json:"sample_rates"`
	BitDepths         []int    `json:"bit_depths"` // not []uint8, which encodes as base64
	TwoWayTalkSupport *string  `json:"two_way_talk_support,omitempty"`
}

type cameraSnapCaps struct {
	Capabilities []cameraSnapCapability `json:"capabilities"`
}

type cameraSnapCapability struct {
	Resolution              wireResolution `json:"resolution"`
	MaxFrameRate            uint16         `json:"max_frame_rate"`
	ImageCodec              string         `json:"image_codec"`
	RequiresEncodedPixels   bool           `json:"requires_encoded_pixels"`
	RequiresHardwareEncoder bool           `json:"requires_hardware_encoder"`
}

type cameraLimits struct {
	MaxEncodedPixelRate   *uint32  `json:"max_encoded_pixel_rate,omitempty"`
	MaxConcurrentEncoders *uint8   `json:"max_concurrent_encoders,omitempty"`
	MaxNetworkBandwidth   *uint32  `json:"max_network_bandwidth,omitempty"`
	SupportedStreamUsages []string `json:"supported_stream_usages"`
	StreamUsagePriorities []string `json:"stream_usage_priorities"`
}

type cameraAllocated struct {
	Video    []cameraAllocatedVideo    `json:"video"`
	Audio    []cameraAllocatedAudio    `json:"audio"`
	Snapshot []cameraAllocatedSnapshot `json:"snapshot"`
}

type cameraAllocatedVideo struct {
	VideoStreamID     uint16         `json:"video_stream_id"`
	StreamUsage       string         `json:"stream_usage"`
	VideoCodec        string         `json:"video_codec"`
	MinResolution     wireResolution `json:"min_resolution"`
	MaxResolution     wireResolution `json:"max_resolution"`
	MinFrameRate      uint16         `json:"min_frame_rate"`
	MaxFrameRate      uint16         `json:"max_frame_rate"`
	MinBitRate        uint32         `json:"min_bit_rate"`
	MaxBitRate        uint32         `json:"max_bit_rate"`
	ReferenceCount    uint8          `json:"reference_count"`
	AllocatedByServer bool           `json:"allocated_by_server"`
	WatermarkEnabled  bool           `json:"watermark_enabled"`
	OSDEnabled        bool           `json:"osd_enabled"`
}

type cameraAllocatedAudio struct {
	AudioStreamID     uint16 `json:"audio_stream_id"`
	StreamUsage       string `json:"stream_usage"`
	AudioCodec        string `json:"audio_codec"`
	ChannelCount      uint8  `json:"channel_count"`
	SampleRate        uint32 `json:"sample_rate"`
	BitRate           uint32 `json:"bit_rate"`
	BitDepth          uint8  `json:"bit_depth"`
	ReferenceCount    uint8  `json:"reference_count"`
	AllocatedByServer bool   `json:"allocated_by_server"`
}

type cameraAllocatedSnapshot struct {
	SnapshotStreamID  uint16         `json:"snapshot_stream_id"`
	ImageCodec        string         `json:"image_codec"`
	MinResolution     wireResolution `json:"min_resolution"`
	MaxResolution     wireResolution `json:"max_resolution"`
	ReferenceCount    uint8          `json:"reference_count"`
	AllocatedByServer bool           `json:"allocated_by_server"`
	FrameRate         uint16         `json:"frame_rate"`
	EncodedPixels     bool           `json:"encoded_pixels"`
	HardwareEncoder   bool           `json:"hardware_encoder"`
	WatermarkEnabled  bool           `json:"watermark_enabled"`
	OSDEnabled        bool           `json:"osd_enabled"`
}

type cameraSession struct {
	WebRTCSessionID         uint16   `json:"webrtc_session_id"`
	PeerNodeID              uint64   `json:"peer_node_id"`
	PeerEndpointID          uint16   `json:"peer_endpoint_id"`
	StreamUsage             string   `json:"stream_usage"`
	VideoStreamIDs          []uint16 `json:"video_stream_ids"`
	AudioStreamIDs          []uint16 `json:"audio_stream_ids"`
	EstablishedByThisServer bool     `json:"established_by_this_server"`
}

// flag is an optional flag on the wire, false when unstated (an overlay
// the camera states nothing for).
func flag(b *bool) bool { return b != nil && *b }

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func mapped[T, U any](in []T, f func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

// wireCapabilities is camera_get_capabilities' result.
func wireCapabilities(c *camera.Capabilities) cameraCapabilities {
	out := cameraCapabilities{
		Privacy: cameraPrivacy{SoftRecording: c.Privacy.SoftRecording, SoftLivestream: c.Privacy.SoftLivestream, Hard: c.Privacy.Hard},
		Video: cameraVideoCaps{
			Sensor: resolutionPtr(c.Video.Sensor), MinViewport: resolutionPtr(c.Video.MinViewport),
			MaxFPS: c.Video.MaxFPS, MaxHDRFPS: c.Video.MaxHDRFPS, HDRCapable: c.Video.HDRCapable,
			RateDistortionPoints: mapped(c.Video.RateDistortionPoints, func(p csdef.RateDistortionTradeOffPointsStruct) cameraRateDistPoint {
				return cameraRateDistPoint{Codec: videoCodecName(p.Codec), Resolution: resolutionOf(p.Resolution), MinBitRate: p.MinBitRate}
			}),
			Codecs: mapped(c.Video.Codecs, videoCodecName),
		},
		Audio: cameraAudioCaps{
			Codecs: mapped(c.Audio.Codecs, audioCodecName), Channels: c.Audio.Channels,
			SampleRates: nonNil(c.Audio.SampleRates), BitDepths: mapped(c.Audio.BitDepths, func(d uint8) int { return int(d) }),
		},
		Snapshot: cameraSnapCaps{Capabilities: mapped(c.Snapshot, func(s csdef.SnapshotCapabilitiesStruct) cameraSnapCapability {
			return cameraSnapCapability{
				Resolution: resolutionOf(s.Resolution), MaxFrameRate: s.MaxFrameRate, ImageCodec: imageCodecName(s.ImageCodec),
				RequiresEncodedPixels: s.RequiresEncodedPixels, RequiresHardwareEncoder: flag(s.RequiresHardwareEncoder),
			}
		})},
		Limits: cameraLimits{
			MaxEncodedPixelRate: c.Limits.MaxEncodedPixelRate, MaxConcurrentEncoders: c.Limits.MaxConcurrentEncoders,
			MaxNetworkBandwidth:   c.Limits.MaxNetworkBandwidth,
			SupportedStreamUsages: mapped(c.Limits.SupportedStreamUsages, streamUsageName),
			StreamUsagePriorities: mapped(c.Limits.StreamUsagePriorities, streamUsageName),
		},
		Allocated: cameraAllocated{
			Video: mapped(c.Allocated.Video, func(v camera.AllocatedVideo) cameraAllocatedVideo {
				return cameraAllocatedVideo{
					VideoStreamID: v.VideoStreamId, StreamUsage: streamUsageName(v.StreamUsage), VideoCodec: videoCodecName(v.VideoCodec),
					MinResolution: resolutionOf(v.MinResolution), MaxResolution: resolutionOf(v.MaxResolution),
					MinFrameRate: v.MinFrameRate, MaxFrameRate: v.MaxFrameRate, MinBitRate: v.MinBitRate, MaxBitRate: v.MaxBitRate,
					ReferenceCount: v.ReferenceCount, AllocatedByServer: v.AllocatedByUs,
					WatermarkEnabled: flag(v.WatermarkEnabled), OSDEnabled: flag(v.OsdEnabled),
				}
			}),
			Audio: mapped(c.Allocated.Audio, func(a camera.AllocatedAudio) cameraAllocatedAudio {
				return cameraAllocatedAudio{
					AudioStreamID: a.AudioStreamId, StreamUsage: streamUsageName(a.StreamUsage), AudioCodec: audioCodecName(a.AudioCodec),
					ChannelCount: a.ChannelCount, SampleRate: a.SampleRate, BitRate: a.BitRate, BitDepth: a.BitDepth,
					ReferenceCount: a.ReferenceCount, AllocatedByServer: a.AllocatedByUs,
				}
			}),
			Snapshot: mapped(c.Allocated.Snapshot, func(s camera.AllocatedSnapshot) cameraAllocatedSnapshot {
				return cameraAllocatedSnapshot{
					SnapshotStreamID: s.SnapshotStreamId, ImageCodec: imageCodecName(s.ImageCodec),
					MinResolution: resolutionOf(s.MinResolution), MaxResolution: resolutionOf(s.MaxResolution),
					ReferenceCount: s.ReferenceCount, AllocatedByServer: s.AllocatedByUs, FrameRate: s.FrameRate,
					EncodedPixels: s.EncodedPixels, HardwareEncoder: s.HardwareEncoder,
					WatermarkEnabled: flag(s.WatermarkEnabled), OSDEnabled: flag(s.OsdEnabled),
				}
			}),
		},
		Sessions: mapped(c.Sessions, func(s camera.DeviceSession) cameraSession {
			return cameraSession{
				WebRTCSessionID: s.ID, PeerNodeID: s.PeerNodeID, PeerEndpointID: s.PeerEndpoint,
				StreamUsage:    streamUsageName(csdef.StreamUsageEnum(s.StreamUsage)),
				VideoStreamIDs: nonNil(s.VideoStreams), AudioStreamIDs: nonNil(s.AudioStreams), EstablishedByThisServer: s.EstablishedByUs,
			}
		}),
	}
	if c.Features != nil {
		f := c.Features
		out.Features = &f
	}
	if t := c.Audio.TwoWayTalkSupport; t != nil {
		n := twoWayTalkSupportName(*t)
		out.Audio.TwoWayTalkSupport = &n
	}
	return out
}

type cameraStartResult struct {
	WebRTCSessionID uint16             `json:"webrtc_session_id"`
	Mode            string             `json:"mode"`
	Video           *cameraVideoResult `json:"video"`
	Audio           *cameraAudioResult `json:"audio"`
	Talkback        bool               `json:"talkback"`
}

type minMax[T any] struct {
	Min T `json:"min"`
	Max T `json:"max"`
}

type cameraVideoResult struct {
	StreamID         uint16                 `json:"stream_id"`
	Codec            string                 `json:"codec"`
	Resolution       minMax[wireResolution] `json:"resolution"`
	FrameRate        minMax[uint16]         `json:"frame_rate"`
	BitRate          minMax[uint32]         `json:"bit_rate"`
	Provenance       string                 `json:"provenance"`
	WatermarkEnabled bool                   `json:"watermark_enabled"`
	OSDEnabled       bool                   `json:"osd_enabled"`
	Degraded         bool                   `json:"degraded"`
	EvictedStreamIDs []uint16               `json:"evicted_stream_ids,omitempty"`
	Narrowed         *cameraBudgetNarrowing `json:"narrowed_by_encoder_budget,omitempty"`
}

type cameraBudgetNarrowing struct {
	MaxFrameRate  *uint16         `json:"max_frame_rate,omitempty"`
	MaxResolution *wireResolution `json:"max_resolution,omitempty"`
}

type cameraAudioResult struct {
	StreamID     uint16 `json:"stream_id"`
	Codec        string `json:"codec"`
	ChannelCount uint8  `json:"channel_count"`
	SampleRate   uint32 `json:"sample_rate"`
	BitRate      uint32 `json:"bit_rate"`
	BitDepth     uint8  `json:"bit_depth"`
	Provenance   string `json:"provenance"`
}

// wireStartResult is camera_start_stream's result: the session's id, its
// mode, the stream of each track it carries (null for none), talkback.
func wireStartResult(s *camera.Session) cameraStartResult {
	out := cameraStartResult{WebRTCSessionID: s.ID, Mode: string(s.Mode), Talkback: s.Talkback}
	for _, l := range s.Streams {
		switch {
		case l.Video != nil:
			v := l.Video
			r := &cameraVideoResult{
				StreamID: l.ID, Codec: videoCodecName(v.Codec),
				Resolution: minMax[wireResolution]{Min: resolutionOf(v.MinResolution), Max: resolutionOf(v.MaxResolution)},
				FrameRate:  minMax[uint16]{Min: v.MinFrameRate, Max: v.MaxFrameRate},
				BitRate:    minMax[uint32]{Min: v.MinBitRate, Max: v.MaxBitRate},
				Provenance: string(l.Provenance), WatermarkEnabled: flag(v.Overlays.Watermark), OSDEnabled: flag(v.Overlays.OSD),
				Degraded: l.Degraded,
			}
			if len(s.Evicted) > 0 {
				r.EvictedStreamIDs = s.Evicted
			}
			if n := l.BudgetNarrowed; n != nil {
				r.Narrowed = &cameraBudgetNarrowing{MaxResolution: resolutionPtr(n.MaxResolution)}
				if n.MaxFrameRate != 0 {
					f := n.MaxFrameRate
					r.Narrowed.MaxFrameRate = &f
				}
			}
			out.Video = r
		case l.Audio != nil:
			a := l.Audio
			out.Audio = &cameraAudioResult{
				StreamID: l.ID, Codec: audioCodecName(a.Codec), ChannelCount: a.ChannelCount, SampleRate: a.SampleRate,
				BitRate: a.BitRate, BitDepth: a.BitDepth, Provenance: string(l.Provenance),
			}
		}
	}
	return out
}

type cameraSnapshotResult struct {
	Data       []byte         `json:"data"`
	Codec      string         `json:"codec"`
	Resolution wireResolution `json:"resolution"`
	Degraded   bool           `json:"degraded"`
	StreamID   uint16         `json:"stream_id"`
	Provenance string         `json:"provenance"`
}

// wireSnapshot is camera_snapshot's result, the image in standard base64.
func wireSnapshot(s *camera.Snapshot) cameraSnapshotResult {
	return cameraSnapshotResult{
		Data: nonNil(s.Data), Codec: imageCodecName(s.Codec), Resolution: resolutionOf(s.Resolution),
		Degraded: s.Degraded, StreamID: s.StreamID, Provenance: string(s.Provenance),
	}
}

type cameraStopResult struct {
	Ended bool `json:"ended"`
}

// The events (docs/websockets_api.md:2255-2269, :2290-2305, :2326; WebRtcCallbackBridge.ts:16-25,
// :55-66, :81-88, :94-107, :114-121; model.ts:836-846, :857-862).

type webrtcCallbackData struct {
	EventType        string           `json:"event_type"`
	WebRTCSessionID  uint16           `json:"webrtc_session_id"`
	NodeID           uint64           `json:"node_id"`
	EndpointID       uint16           `json:"endpoint_id"`
	FabricIndex      uint8            `json:"fabric_index"`
	ClientSessionKey string           `json:"client_session_key,omitempty"`
	Data             webrtcSignalData `json:"data"`
}

// webrtcSignalData is the data of each kind: offer {sdp, ice_servers?,
// ice_transport_policy?}, answer {sdp}, ice_candidates {ice_candidates},
// end {reason}.
type webrtcSignalData struct {
	SDP                *string               `json:"sdp,omitempty"`
	ICEServers         []webrtcICEServer     `json:"ice_servers,omitempty"`
	ICETransportPolicy *string               `json:"ice_transport_policy,omitempty"`
	ICECandidates      *[]webrtcICECandidate `json:"ice_candidates,omitempty"`
	Reason             *uint8                `json:"reason,omitempty"`
}

// webrtcICEServer is an ICE server of an offer: urls always a list.
type webrtcICEServer struct {
	URLs       []string `json:"urls"`
	Username   *string  `json:"username,omitempty"`
	Credential *string  `json:"credential,omitempty"`
	CAID       *uint16  `json:"caid,omitempty"`
}

// webrtcICECandidate is one candidate in the W3C spelling, nulls kept.
type webrtcICECandidate struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdpMid"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex"`
}

// wireSignal is the webrtc_callback of one camera signal; key is the
// session's client_session_key ("" for none).
func wireSignal(sig camera.Signal, key string) webrtcCallbackData {
	out := webrtcCallbackData{
		WebRTCSessionID: sig.Session.Id, NodeID: sig.Session.PeerNodeId, EndpointID: sig.Session.PeerEndpointId,
		FabricIndex: sig.Session.FabricIndex, ClientSessionKey: key,
	}
	switch sig.Kind {
	case camera.SignalOffer:
		out.EventType = "offer"
		sdp := sig.SDP
		out.Data.SDP = &sdp
		if s := sig.Offer.IceServers; s != nil {
			out.Data.ICEServers = make([]webrtcICEServer, 0, len(*s))
			for _, v := range *s {
				out.Data.ICEServers = append(out.Data.ICEServers, webrtcICEServer{URLs: nonNil(v.Urls), Username: v.Username, Credential: v.Credential, CAID: v.Caid})
			}
		}
		out.Data.ICETransportPolicy = sig.Offer.IceTransportPolicy
	case camera.SignalAnswer:
		out.EventType = "answer"
		sdp := sig.SDP
		out.Data.SDP = &sdp
	case camera.SignalICECandidates:
		out.EventType = "ice_candidates"
		list := make([]webrtcICECandidate, 0, len(sig.Candidates))
		for _, c := range sig.Candidates {
			w := webrtcICECandidate{Candidate: c.Candidate}
			if !c.SdpMid.Null {
				v := c.SdpMid.Value
				w.SDPMid = &v
			}
			if !c.SdpmLineIndex.Null {
				v := c.SdpmLineIndex.Value
				w.SDPMLineIndex = &v
			}
			list = append(list, w)
		}
		out.Data.ICECandidates = &list
	case camera.SignalEnd:
		out.EventType = "end"
		r := sig.Reason
		out.Data.Reason = &r
	}
	return out
}

type cameraSessionEnded struct {
	NodeID           uint64 `json:"node_id"`
	EndpointID       uint16 `json:"endpoint_id"`
	WebRTCSessionID  uint16 `json:"webrtc_session_id"`
	Cause            string `json:"cause"`
	Reason           *uint8 `json:"reason,omitempty"`
	ClientSessionKey string `json:"client_session_key,omitempty"`
}

func wireSessionEnded(e camera.SessionEnd) cameraSessionEnded {
	return cameraSessionEnded{
		NodeID: e.NodeID, EndpointID: e.Endpoint, WebRTCSessionID: e.SessionID, Cause: string(e.Cause),
		Reason: e.Reason, ClientSessionKey: e.Key,
	}
}

type cameraStreamEvicted struct {
	NodeID     uint64 `json:"node_id"`
	EndpointID uint16 `json:"endpoint_id"`
	Kind       string `json:"kind"`
	StreamID   uint16 `json:"stream_id"`
}

func wireStreamEvicted(e camera.StreamEvicted) cameraStreamEvicted {
	return cameraStreamEvicted{NodeID: e.NodeID, EndpointID: e.Endpoint, Kind: string(e.Kind), StreamID: e.StreamID}
}

// cameraArgs are the arguments of a camera command, decoded strictly
// (docs/websockets_api.md:1835-1841; cameraCommands.ts:139-168,
// wireArgumentChecks.ts:16-22): an object, every key one the command
// takes; protocol.DecodeArgs stays tolerant for the other commands.
type cameraArgs struct {
	command string
	fields  map[string]json.RawMessage
}

// decodeCameraArgs decodes raw (an absent or null args is the empty set)
// and refuses a key the command does not take, naming it and the
// accepted ones.
func decodeCameraArgs(command string, raw json.RawMessage, accepted ...string) (*cameraArgs, *protocol.Error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		t = []byte("{}")
	}
	if t[0] != '{' {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s requires an object of arguments", command)
	}
	keys, fields, err := objectFields(t)
	if err != nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s: %v", command, err)
	}
	if perr := rejectUnknown(keys, accepted, command+" argument"); perr != nil {
		return nil, perr
	}
	return &cameraArgs{command: command, fields: fields}, nil
}

// objectFields reads a JSON object's keys in their order and their
// values; a repeated key keeps its last value, as JSON.parse does.
func objectFields(b []byte) (keys []string, fields map[string]json.RawMessage, err error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil {
		return nil, nil, err
	}
	fields = map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		k, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		if _, seen := fields[k]; !seen {
			keys = append(keys, k)
		}
		fields[k] = v
	}
	return keys, fields, nil
}

// rejectUnknown is matterjs-server's rejectUnknownKeys
// (wireArgumentChecks.ts:16-22): every key a caller states is a bound.
func rejectUnknown(keys, accepted []string, subject string) *protocol.Error {
	var unknown []string
	for _, k := range keys {
		known := false
		for _, a := range accepted {
			if k == a {
				known = true
				break
			}
		}
		if !known {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return protocol.Errorf(protocol.InvalidArguments, "unknown %s key: %s. Accepted: %s", subject, strings.Join(unknown, ", "), strings.Join(accepted, ", "))
	}
	return nil
}

// has reports whether the key is stated.
func (a *cameraArgs) has(key string) bool {
	_, ok := a.fields[key]
	return ok
}

// raw is the key's value, nil when unstated.
func (a *cameraArgs) raw(key string) json.RawMessage { return a.fields[key] }

// target is the node_id and the endpoint_id every camera command takes;
// the node is looked up by the handler once every argument is checked.
func (a *cameraArgs) target() (*protocol.Uint64, uint16, *protocol.Error) {
	var id *protocol.Uint64
	if raw := a.raw("node_id"); raw != nil {
		var v protocol.Uint64
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, 0, protocol.Errorf(protocol.InvalidArguments, "%s requires a numeric node_id: %v", a.command, err)
		}
		id = &v
	}
	ep, ok := uintOfJSON(a.raw("endpoint_id"), 0xFFFE)
	if !ok {
		return nil, 0, protocol.Errorf(protocol.InvalidArguments, "%s requires endpoint_id to be an integer between 0 and 0xFFFE", a.command)
	}
	return id, uint16(ep), nil //nolint:gosec // G115: at most 0xFFFE
}

// uintOfJSON reads a JSON integer between 0 and maxValue: a number that
// is integral (1.0 is 1, as in JavaScript), not negative, not above the
// ceiling.
func uintOfJSON(raw json.RawMessage, maxValue uint64) (uint64, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '-' && (t[0] < '0' || t[0] > '9') {
		return 0, false
	}
	var f float64
	if json.Unmarshal(t, &f) != nil || f < 0 || f > float64(maxValue) || f != math.Trunc(f) {
		return 0, false
	}
	return uint64(f), true
}

// number reads an optional integer field that must fit the width of the
// go-fabric field it fills; the field's own range is go-fabric's to check.
func (a *cameraArgs) number(key, field string, maxValue uint64) (v uint64, stated bool, perr *protocol.Error) {
	raw := a.raw(key)
	if raw == nil {
		return 0, false, nil
	}
	v, ok := uintOfJSON(raw, maxValue)
	if !ok {
		return 0, false, protocol.Errorf(protocol.InvalidArguments, "%s must be a non-negative integer no greater than %d", field, maxValue)
	}
	return v, true, nil
}

// str reads an optional string field.
func (a *cameraArgs) str(key, field string) (*string, *protocol.Error) {
	raw := a.raw(key)
	if raw == nil {
		return nil, nil
	}
	var s string
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '"' || json.Unmarshal(t, &s) != nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s must be a string", field)
	}
	return &s, nil
}

// boolean reads an optional boolean field.
func (a *cameraArgs) boolean(key, field string) (*bool, *protocol.Error) {
	raw := a.raw(key)
	if raw == nil {
		return nil, nil
	}
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		v := true
		return &v, nil
	case "false":
		v := false
		return &v, nil
	}
	return nil, protocol.Errorf(protocol.InvalidArguments, "%s must be a boolean", field)
}

// strings reads an optional array of strings.
func (a *cameraArgs) strings(key, field string) ([]string, *protocol.Error) {
	raw := a.raw(key)
	if raw == nil {
		return nil, nil
	}
	var out []string
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '[' || json.Unmarshal(t, &out) != nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s must be an array of strings", field)
	}
	return nonNil(out), nil
}

// object reads an optional object field strictly: its keys must be
// among accepted.
func (a *cameraArgs) object(key, subject, notObject string, accepted ...string) (*cameraArgs, *protocol.Error) {
	raw := a.raw(key)
	if raw == nil {
		return nil, nil
	}
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s", notObject)
	}
	keys, fields, err := objectFields(t)
	if err != nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s: %v", subject, err)
	}
	if perr := rejectUnknown(keys, accepted, subject); perr != nil {
		return nil, perr
	}
	return &cameraArgs{command: a.command, fields: fields}, nil
}

// resolution reads an optional {width, height} with both stated, each
// fitting the struct's uint16; its range is go-fabric's.
func (a *cameraArgs) resolution(key, field string) (*csdef.VideoResolutionStruct, *protocol.Error) {
	expected := field + " must be an object with an integer width and height"
	o, perr := a.object(key, field, expected, "width", "height")
	if perr != nil || o == nil {
		return nil, perr
	}
	if !o.has("width") || !o.has("height") {
		return nil, protocol.Errorf(protocol.InvalidArguments, "%s", expected)
	}
	w, _, perr := o.number("width", field+".width", 0xFFFF)
	if perr != nil {
		return nil, perr
	}
	h, _, perr := o.number("height", field+".height", 0xFFFF)
	if perr != nil {
		return nil, perr
	}
	return &csdef.VideoResolutionStruct{Width: uint16(w), Height: uint16(h)}, nil //nolint:gosec // G115: checked above
}
