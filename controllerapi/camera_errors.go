// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/SukramJ/go-fabric/controller/camera"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// cameraError maps an error of the camera service onto matterjs-server's
// codes (brief S8 §4): the typed errors 102 to 106 with their details a
// JSON string in matterjs-server's key order (types.ts:266-341;
// CameraErrorsTest.ts:156), a StackError 7, an invalid argument 8 with
// go-fabric's message, and anything else as an interaction with the node
// fails (nodeError).
func cameraError(command string, err error) *protocol.Error {
	var (
		inc *camera.IncompatibleError
		exh *camera.ExhaustedError
		use *camera.InUseError
		ns  *camera.NotSupportedError
		pr  *camera.PrivacyError
		se  *camera.StackError
	)
	switch {
	case errors.As(err, &inc):
		return detailed(protocol.CameraStreamIncompatible, incompatibleDetails(inc))
	case errors.As(err, &exh):
		return detailed(protocol.CameraResourceExhausted, exhaustedDetails(exh))
	case errors.As(err, &use):
		return detailed(protocol.CameraStreamInUse, inUseDetails(use))
	case errors.Is(err, camera.ErrStreamInUse):
		return detailed(protocol.CameraStreamInUse, orderedJSON(kv{"message", streamInUseMessage}))
	case errors.As(err, &ns):
		return detailed(protocol.CameraNotSupported, orderedJSON(kv{"message", ns.Error()}, kv{"missing_clusters", nonNil(ns.Missing)}))
	case errors.As(err, &pr):
		return detailed(protocol.CameraPrivacyMode, privacyDetails(pr))
	case errors.As(err, &se):
		return protocol.Errorf(protocol.SDKStackError, "%s: %v", command, se)
	case errors.Is(err, camera.ErrInvalidArgument):
		return protocol.Errorf(protocol.InvalidArguments, "%s", err.Error())
	}
	return nodeError(command, err)
}

// The messages of 104 and 106, matterjs-server's (types.ts:317-333):
// go-fabric's own texts prefix the package and list the modes.
const (
	streamInUseMessage = "Stream is in use and cannot be released"
	privacyMessage     = "Camera privacy mode is enabled"
)

func detailed(code protocol.ErrorCode, details string) *protocol.Error {
	return &protocol.Error{ErrorCode: code, Details: details}
}

// kv is one key of a details object; orderedJSON writes them in order.
type kv struct {
	k string
	v any
}

func orderedJSON(pairs ...kv) string {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(p.k)
		v, err := protocol.Encode(p.v)
		if err != nil {
			v = []byte("null")
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(bytes.TrimSpace(v))
	}
	b.WriteByte('}')
	return b.String()
}

// incompatibleDetails is error 102's details: {message, reason, track?,
// feature?, device, requested, bound?, device_status?} — feature only for
// "feature", bound only for "bounds", no track for "no_media"
// (types.ts:274-301).
func incompatibleDetails(e *camera.IncompatibleError) string {
	pairs := []kv{{"message", e.Error()}, {"reason", string(e.Reason)}}
	if e.Track != "" && e.Reason != camera.ReasonNoMedia {
		pairs = append(pairs, kv{"track", string(e.Track)})
	}
	if e.Reason == camera.ReasonFeature {
		pairs = append(pairs, kv{"feature", e.Feature})
	}
	pairs = append(pairs, kv{"device", nonNil(e.Device)}, kv{"requested", nonNil(e.Requested)})
	if e.Reason == camera.ReasonBounds && e.Bound != nil {
		pairs = append(pairs, kv{"bound", json.RawMessage(orderedJSON(
			kv{"field", string(e.Bound.Field)}, kv{"requested", e.Bound.Requested}, kv{"limit", e.Bound.Limit}))})
	}
	if e.DeviceStatus != nil {
		pairs = append(pairs, kv{"device_status", int(*e.DeviceStatus)})
	}
	return orderedJSON(pairs...)
}

// exhaustedDetails is error 103's details: {message, allocated[{kind,
// stream_id, reference_count}], max_concurrent_encoders?,
// max_encoded_pixel_rate?} (types.ts:303-315).
func exhaustedDetails(e *camera.ExhaustedError) string {
	allocated := make([]json.RawMessage, 0, len(e.Allocated))
	for _, a := range e.Allocated {
		allocated = append(allocated, json.RawMessage(orderedJSON(kv{"kind", string(a.Kind)}, kv{"stream_id", a.StreamID}, kv{"reference_count", a.ReferenceCount})))
	}
	pairs := []kv{{"message", e.Error()}, {"allocated", allocated}}
	if e.MaxEncoders != nil {
		pairs = append(pairs, kv{"max_concurrent_encoders", *e.MaxEncoders})
	}
	if e.MaxPixelRate != nil {
		pairs = append(pairs, kv{"max_encoded_pixel_rate", *e.MaxPixelRate})
	}
	return orderedJSON(pairs...)
}

// inUseDetails is error 104's details: {message, stream_id,
// reference_count?}, the count only when above zero (types.ts:317-324).
func inUseDetails(e *camera.InUseError) string {
	pairs := []kv{{"message", streamInUseMessage}, {"stream_id", e.StreamID}}
	if e.ReferenceCount > 0 {
		pairs = append(pairs, kv{"reference_count", e.ReferenceCount})
	}
	return orderedJSON(pairs...)
}

// privacyDetails is error 106's details: {message, modes, device_status}
// (types.ts:326-333).
func privacyDetails(e *camera.PrivacyError) string {
	modes := make([]string, 0, len(e.Modes))
	for _, m := range e.Modes {
		modes = append(modes, string(m))
	}
	return orderedJSON(kv{"message", privacyMessage}, kv{"modes", modes}, kv{"device_status", int(e.DeviceStatus)})
}
