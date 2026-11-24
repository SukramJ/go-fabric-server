// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric-server/protocol/schema"
)

// argsJSON is the argument log of command over args, as JSON.
func argsJSON(t *testing.T, command string, args any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := loggedArgs(command, raw)
	if !ok {
		t.Fatalf("%s logs no args", command)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestLoggedArgs: the argument log of device_command and write_attribute
// redacts every value the model marks secret, by each spelling of the
// member the encoder accepts, nested in structs and lists; null stays
// null; a value without a mark is logged in full; what the model cannot
// judge is "[unjudged]"; every other command logs no argument.
func TestLoggedArgs(t *testing.T) {
	const pinValue = "OTE3MzU=" // base64 of a made-up PIN
	dl := schema.ClusterByName("DoorLock")
	lock := dl.CommandByName("LockDoor")
	var pin *schema.Field
	for i := range lock.Fields {
		if lock.Fields[i].Secret {
			pin = &lock.Fields[i]
		}
	}
	if pin == nil {
		t.Fatal("DoorLock LockDoor has no secret field in the model")
	}
	for _, key := range []string{pin.Name, pin.Property, strings.ToUpper(pin.Name), strconv.FormatUint(uint64(pin.ID), 10)} {
		got := argsJSON(t, "device_command", map[string]any{
			"node_id": 1, "endpoint_id": 1, "cluster_id": dl.ID, "command_name": lock.Name,
			"payload": map[string]any{key: pinValue},
		})
		if strings.Contains(got, pinValue) || !strings.Contains(got, `"`+key+`":"[redacted]"`) || !strings.Contains(got, `"command_name":"LockDoor"`) {
			t.Errorf("%s: %s", key, got)
		}
	}
	// The payload member by another case, as the args decoder reads it.
	if got := argsJSON(t, "device_command", map[string]any{
		"cluster_id": dl.ID, "command_name": lock.Name, "Payload": map[string]any{pin.Property: pinValue},
	}); strings.Contains(got, pinValue) {
		t.Errorf("Payload: %s", got)
	}
	// null stays null.
	if got := argsJSON(t, "device_command", map[string]any{
		"cluster_id": dl.ID, "command_name": lock.Name, "payload": map[string]any{pin.Property: nil},
	}); !strings.Contains(got, `"`+pin.Property+`":null`) {
		t.Errorf("null: %s", got)
	}

	nc := schema.ClusterByName("NetworkCommissioning")
	wifi := nc.CommandByName("AddOrUpdateWiFiNetwork")
	if got := argsJSON(t, "device_command", map[string]any{
		"cluster_id": nc.ID, "command_name": wifi.Name, "payload": map[string]any{"ssid": "aG9tZQ==", "credentials": pinValue, "breadcrumb": 7},
	}); strings.Contains(got, pinValue) || !strings.Contains(got, `"credentials":"[redacted]"`) || !strings.Contains(got, `"ssid":"aG9tZQ=="`) || !strings.Contains(got, `"breadcrumb":7`) {
		t.Errorf("wifi: %s", got)
	}

	// A struct of the command: GroupKeyManagement KeySetWrite's key set.
	gk := schema.ClusterByName("GroupKeyManagement")
	ksw := gk.CommandByName("KeySetWrite")
	set := ksw.Fields[0]
	if set.Type.Struct == nil {
		t.Fatalf("KeySetWrite's field %s is no struct", set.Name)
	}
	keySet := map[string]any{"groupKeySetId": 1, "epochKey0": pinValue, "epochStartTime0": 2, "epochKey1": nil}
	if got := argsJSON(t, "device_command", map[string]any{
		"cluster_id": gk.ID, "command_name": ksw.Name, "payload": map[string]any{set.Property: keySet},
	}); strings.Contains(got, pinValue) || !strings.Contains(got, `"epochKey0":"[redacted]"`) || !strings.Contains(got, `"epochKey1":null`) || !strings.Contains(got, `"groupKeySetId":1`) {
		t.Errorf("key set: %s", got)
	}
	// The same struct as the entry of a list, the model's own type.
	list := schema.Type{Meta: schema.MetaList, Entry: &set.Type}
	b, _ := json.Marshal(judgeValue(list, false, []any{keySet, keySet}))
	if strings.Contains(string(b), pinValue) || strings.Count(string(b), redacted) != 2 {
		t.Errorf("list: %s", b)
	}

	// write_attribute of the secret attribute; the path as given.
	alg := dl.AttributeByName("AliroGroupResolvingKey")
	path := fmt.Sprintf("1/%d/%d", dl.ID, alg.ID)
	if got := argsJSON(t, "write_attribute", map[string]any{"node_id": 1, "attribute_path": path, "value": pinValue}); strings.Contains(got, pinValue) ||
		!strings.Contains(got, `"value":"[redacted]"`) || !strings.Contains(got, path) {
		t.Errorf("write: %s", got)
	}
	if got := argsJSON(t, "write_attribute", map[string]any{"attribute_path": path, "value": nil}); !strings.Contains(got, `"value":null`) {
		t.Errorf("write null: %s", got)
	}

	// A command without a mark is logged in full.
	onoff := schema.ClusterByName("OnOff")
	if got := argsJSON(t, "device_command", map[string]any{
		"cluster_id": onoff.ID, "command_name": "OffWithEffect", "payload": map[string]any{"effectIdentifier": 0, "effectVariant": 1},
	}); !strings.Contains(got, `"effectIdentifier":0`) || !strings.Contains(got, `"effectVariant":1`) {
		t.Errorf("OnOff: %s", got)
	}

	// Fail closed.
	for name, c := range map[string]struct {
		command string
		args    map[string]any
	}{
		"unknown command":  {"device_command", map[string]any{"cluster_id": dl.ID, "command_name": "Nope", "payload": map[string]any{"x": pinValue}}},
		"unknown cluster":  {"device_command", map[string]any{"cluster_id": 0xFFF1FC00, "command_name": "LockDoor", "payload": map[string]any{"x": pinValue}}},
		"undefined member": {"device_command", map[string]any{"cluster_id": dl.ID, "command_name": lock.Name, "payload": map[string]any{"secret": pinValue}}},
		"no object":        {"device_command", map[string]any{"cluster_id": dl.ID, "command_name": lock.Name, "payload": pinValue}},
		"unknown attr":     {"write_attribute", map[string]any{"attribute_path": fmt.Sprintf("1/%d/65000", dl.ID), "value": pinValue}},
		"struct no object": {"device_command", map[string]any{"cluster_id": gk.ID, "command_name": ksw.Name, "payload": map[string]any{set.Property: []any{pinValue}}}},
	} {
		if got := argsJSON(t, c.command, c.args); strings.Contains(got, pinValue) || !strings.Contains(got, unjudged) {
			t.Errorf("%s: %s", name, got)
		}
	}

	if _, ok := loggedArgs("set_wifi_credentials", json.RawMessage(`{"ssid":"x","credentials":"y"}`)); ok {
		t.Error("another command logs its args")
	}
}

// TestLoggedDetails: the error details of a command whose target holds a
// secret, or of a write to an attribute the model does not know, are
// "[redacted]" in the log; others keep their text.
func TestLoggedDetails(t *testing.T) {
	dl := schema.ClusterByName("DoorLock")
	onoff := schema.ClusterByName("OnOff")
	args := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, c := range map[string]struct {
		command string
		args    json.RawMessage
		want    string
	}{
		"LockDoor":        {"device_command", args(map[string]any{"cluster_id": dl.ID, "command_name": "LockDoor"}), redacted},
		"On":              {"device_command", args(map[string]any{"cluster_id": onoff.ID, "command_name": "On"}), "x"},
		"unknown cmd":     {"device_command", args(map[string]any{"cluster_id": dl.ID, "command_name": "Nope"}), "x"},
		"secret attr":     {"write_attribute", args(map[string]any{"attribute_path": fmt.Sprintf("1/%d/%d", dl.ID, dl.AttributeByName("AliroGroupResolvingKey").ID)}), redacted},
		"unknown attr":    {"write_attribute", args(map[string]any{"attribute_path": fmt.Sprintf("1/%d/65000", dl.ID)}), redacted},
		"plain attr":      {"write_attribute", args(map[string]any{"attribute_path": fmt.Sprintf("1/%d/%d", onoff.ID, onoff.AttributeByName("OnTime").ID)}), "x"},
		"no path":         {"write_attribute", args(map[string]any{}), "x"},
		"another command": {"server_info", nil, "x"},
	} {
		if got := loggedDetails(c.command, c.args, "x"); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// TestLoggedArgsFailClosed: a top-level member no handler reads is
// "[unjudged]", one it reads is logged as given by every spelling the
// args decoder accepts; the SDP's ice-ufrag and ice-pwd lines are masked
// as matterjs-server masks them; the ICE server's credential (a mark
// under the snapshot's definitions) is "[redacted]"; a struct the tables
// do not resolve holds a secret for the error details.
func TestLoggedArgsFailClosed(t *testing.T) {
	const pin = "OTE3MzU="
	dl := schema.ClusterByName("DoorLock")
	got := argsJSON(t, "device_command", map[string]any{
		"Node_ID": 7, "cluster_id": dl.ID, "command_name": "LockDoor",
		"paylod": map[string]any{"pinCode": pin}, "payload": map[string]any{},
	})
	if strings.Contains(got, pin) || !strings.Contains(got, `"paylod":"[unjudged]"`) || !strings.Contains(got, `"Node_ID":7`) {
		t.Errorf("top-level members: %s", got)
	}
	if got := argsJSON(t, "write_attribute", map[string]any{"attribute_path": "1/6/0", "value": true, "extra": pin}); strings.Contains(got, pin) {
		t.Errorf("write top-level: %s", got)
	}

	wp := schema.ClusterByName("WebRtcTransportProvider")
	offer := wp.CommandByName("ProvideOffer")
	const ufrag, pwd = "Xq7ufragZ", "pwdSecret9876543210abcd"
	sdp := "v=0\r\na=ice-ufrag:" + ufrag + "\r\nA=ICE-PWD:" + pwd + "\r\na=fingerprint:sha-256 AB:CD\r\n"
	got = argsJSON(t, "device_command", map[string]any{
		"cluster_id": wp.ID, "command_name": offer.Name,
		"payload": map[string]any{
			"sdp":        sdp,
			"iceServers": []any{map[string]any{"urls": []any{"turn:example.org"}, "username": "u", "credential": pwd}},
		},
	})
	if strings.Contains(got, ufrag) || strings.Contains(got, pwd) || !strings.Contains(got, "a=ice-ufrag:[redacted]") ||
		!strings.Contains(got, "A=ICE-PWD:[redacted]") || !strings.Contains(got, "a=fingerprint") || !strings.Contains(got, `"credential":"[redacted]"`) {
		t.Errorf("WebRTC: %s", got)
	}

	if !holdsSecret(schema.Type{Meta: schema.MetaStruct}, false, map[*schema.Struct]bool{}) {
		t.Error("an unresolved struct holds no secret")
	}
	if got := judgeValue(schema.Type{Meta: schema.MetaStruct}, false, map[string]any{"x": 1}); got != unjudged {
		t.Errorf("an unresolved struct: %v", got)
	}
}
