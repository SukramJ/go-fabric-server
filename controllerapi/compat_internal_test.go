// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// compatDir holds matterjs-server's documented examples
// (protocol/testdata/compat/README.md).
const compatDir = "../protocol/testdata/compat"

// extensions are the keys go-fabric-server adds to a shape; an old client
// ignores them (concept v8 C.1).
var extensions = map[string]bool{"capabilities": true, "ble_proxy_enabled": true}

func normalize(t *testing.T, b []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return v
}

// sameModuloExtensions compares two decoded JSON values; got may carry the
// extension keys besides want's.
func sameModuloExtensions(want, got any) bool {
	wm, wok := want.(map[string]any)
	gm, gok := got.(map[string]any)
	if wok && gok {
		for k, wv := range wm {
			if !sameModuloExtensions(wv, gm[k]) {
				return false
			}
		}
		for k := range gm {
			if _, ok := wm[k]; !ok && !extensions[k] {
				return false
			}
		}
		return true
	}
	wl, wok := want.([]any)
	gl, gok := got.([]any)
	if wok && gok {
		if len(wl) != len(gl) {
			return false
		}
		for i := range wl {
			if !sameModuloExtensions(wl[i], gl[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// roundTrip decodes b into a new value of v's type and encodes it again.
func roundTrip[T any](t *testing.T, b []byte) []byte {
	t.Helper()
	var v T
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode into %T: %v", v, err)
	}
	out, err := protocol.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type subscriptionResult struct {
	SubscriptionID uint32 `json:"subscription_id"`
	reportData
}

type subscriptionReport struct {
	NodeID         uint64 `json:"node_id"`
	SubscriptionID uint32 `json:"subscription_id"`
	reportData
}

type subscriptionEnded struct {
	NodeID         uint64 `json:"node_id"`
	SubscriptionID uint32 `json:"subscription_id"`
}

type endpointEvent struct {
	NodeID     uint64 `json:"node_id"`
	EndpointID uint16 `json:"endpoint_id"`
}

type logLevelData struct {
	Console string  `json:"console_loglevel"`
	File    *string `json:"file_loglevel"`
}

type result[T any] struct {
	MessageID string `json:"message_id"`
	Result    T      `json:"result"`
}

type event[T any] struct {
	Event string `json:"event"`
	Data  T      `json:"data"`
}

// typed are the fixtures of S1 results and events, by file name, with the
// type the server encodes them with.
var typed = map[string]func(*testing.T, []byte) []byte{
	"connection.value.json":                     roundTrip[serverInfoData],
	"request_response_format.response.json":     roundTrip[protocol.Error],
	"get_loglevel.response.json":                roundTrip[result[logLevelData]],
	"set_loglevel.response.json":                roundTrip[result[logLevelData]],
	"write_attribute.value.json":                roundTrip[[]writeResult],
	"subscriptions_and_generic_read.value.json": roundTrip[reportData],
	"read.response.json":                        roundTrip[result[reportData]],
	"subscribe.response.json":                   roundTrip[result[subscriptionResult]],
	"node_added.event.json":                     roundTrip[event[nodeData]],
	"node_removed.event.json":                   roundTrip[event[uint64]],
	"attribute_updated.event.json":              roundTrip[event[[]any]],
	"endpoint_added.event.json":                 roundTrip[event[endpointEvent]],
	"endpoint_removed.event.json":               roundTrip[event[endpointEvent]],
	"node_event.event.json":                     roundTrip[event[nodeEventData]],
	"subscription_report.event.json":            roundTrip[event[subscriptionReport]],
	"subscription_ended.event.json":             roundTrip[event[subscriptionEnded]],
	"server_info_updated.event.json":            roundTrip[event[serverInfoData]],
	"server_shutdown.event.json":                roundTrip[event[struct{}]],
	// S3.
	"parse_onboarding_payload.value.json":     roundTrip[qrResult],
	"parse_onboarding_payload.value.2.json":   roundTrip[manualPayload],
	"open_commissioning_window.response.json": roundTrip[result[windowData]],
	"batched_device_commands.value.json":      roundTrip[[]batchedResult],
	"get_icd_state.value.json":                roundTrip[icdState],
}

// pythonDir holds the S3 fixtures in the python client's shape
// (python/README.md).
const pythonDir = compatDir + "/python"

// typedPython are those fixtures' results, events and errors with the
// type the server encodes them with; the requests decode as above.
var typedPython = map[string]func(*testing.T, []byte) []byte{
	"parse_onboarding_payload.value.manual21.json": roundTrip[manualPayload],
	"batched_device_commands.value.response.json":  roundTrip[[]batchedResult],
	"icd_state.unsupported.value.json":             roundTrip[icdState],
	"register_icd.error.json":                      roundTrip[protocol.Error],
	"commission_with_code.error.attestation.json":  roundTrip[protocol.Error],
	"get_network_topology.value.json":              roundTrip[networkTopology],
	"network_topology_updated.event.json":          roundTrip[event[networkTopology]],
	"get_all_credentials.value.json":               roundTrip[map[string][]map[string]any],
	"discover.value.json":                          roundTrip[[]map[string]any],
}

// notInS1 are documented shapes of commands and events S1 does not
// implement (InvalidCommand or not emitted); they stay as fixtures for the
// bundle that brings them.
var notInS1 = []string{
	"initiate_ota_upload.value", "thread_diagnostics_updated.event",
	"webrtc_callback.event", "camera_session_ended.event", "camera_stream_evicted.event",
}

// TestCompatPythonFixtures holds the S3 fixtures of python/: every
// request decodes and round-trips, every result, event and error
// round-trips through the server's type.
func TestCompatPythonFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(pythonDir, "*.json"))
	if err != nil || len(files) < 10 {
		t.Fatalf("fixtures: %d %v", len(files), err)
	}
	checked := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			if strings.Contains(name, ".request") {
				req, perr := protocol.DecodeRequest(b)
				if perr != nil {
					t.Fatalf("the request does not decode: %v", perr)
				}
				if !sameModuloExtensions(normalize(t, b), normalize(t, protocol.MustEncode(req))) {
					t.Fatalf("request round trip %s", b)
				}
				return
			}
			rt := typedPython[name]
			if rt == nil {
				t.Fatalf("a fixture without a check: %s", name)
			}
			out := rt(t, b)
			if !sameModuloExtensions(normalize(t, b), normalize(t, out)) {
				t.Fatalf("shape round trip\nwant %s\ngot  %s", b, out)
			}
			checked[name] = true
		})
	}
	for name := range typedPython {
		if !checked[name] {
			t.Errorf("no fixture %s", name)
		}
	}
}

func TestCompatFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(compatDir, "*.json"))
	if err != nil || len(files) < 80 {
		t.Fatalf("fixtures: %d %v", len(files), err)
	}
	checked := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			switch {
			case strings.Contains(name, ".request"):
				req, perr := protocol.DecodeRequest(b)
				if perr != nil {
					t.Fatalf("the documented request does not decode: %v", perr)
				}
				out := protocol.MustEncode(req)
				want := normalize(t, b)
				if wm, ok := want.(map[string]any); ok {
					if _, has := wm["args"]; !has {
						wm["args"] = map[string]any{} // absent args are the empty set
					}
				}
				if !sameModuloExtensions(want, normalize(t, out)) {
					t.Fatalf("request round trip\nwant %s\ngot  %s", b, out)
				}
			case typed[name] != nil:
				out := typed[name](t, b)
				if !sameModuloExtensions(normalize(t, b), normalize(t, out)) {
					t.Fatalf("shape round trip\nwant %s\ngot  %s", b, out)
				}
				checked[name] = true
			default:
				for _, n := range notInS1 {
					if strings.HasPrefix(name, n) {
						t.Skipf("%s is not in the S1 set", n)
					}
				}
				t.Fatalf("a fixture without a check: %s", name)
			}
		})
	}
	for name := range typed {
		if !checked[name] {
			t.Errorf("no fixture %s", name)
		}
	}
}
