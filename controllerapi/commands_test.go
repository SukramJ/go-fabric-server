// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"

	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/node"

	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/protocol"
)

// wantCode fails unless err is a protocol error with code (and, when
// details is not empty, details containing it).
func wantCode(t *testing.T, err error, code protocol.ErrorCode, details string) *protocol.Error {
	t.Helper()
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		t.Fatalf("want error %d, got %v", code, err)
	}
	if pe.ErrorCode != code || !strings.Contains(pe.Details, details) {
		t.Fatalf("want error %d (%q), got %d %q", code, details, pe.ErrorCode, pe.Details)
	}
	return pe
}

// TestNodeCommands runs every node-targeted command of the S1 set against
// a commissioned light, one subtest each.
func TestNodeCommands(t *testing.T) {
	e := newEnv(t, envOptions{})
	n := e.commission()
	id := n.NodeID
	ep := e.lightEndpoint()
	ctx := e.ctx

	t.Run("get_nodes", func(t *testing.T) {
		nodes, err := e.client.GetNodes(ctx, false)
		if err != nil || len(nodes) != 1 || nodes[0].NodeID != id {
			t.Fatalf("%+v %v", nodes, err)
		}
		if _, err := e.client.GetNodes(ctx, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("read", func(t *testing.T) {
		rep, err := e.client.Read(ctx, id, []string{fmt.Sprintf("%d/6/0", ep), fmt.Sprintf("%d/6/9999", ep)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Attributes[fmt.Sprintf("%d/6/0", ep)] != false {
			t.Fatalf("attributes %v", rep.Attributes)
		}
		if _, ok := rep.AttributeStatus[fmt.Sprintf("%d/6/9999", ep)]; !ok {
			t.Fatalf("attribute_status %v: an unsupported attribute answers a status", rep.AttributeStatus)
		}
		if len(rep.DataVersions) == 0 {
			t.Fatal("data_versions missing")
		}
		_, err = e.client.Read(ctx, id, []string{"1/x/0"}, nil)
		wantCode(t, err, protocol.InvalidArguments, "neither a number")
		_, err = e.client.Read(ctx, id, nil, nil)
		wantCode(t, err, protocol.InvalidArguments, "at least one")
	})
	t.Run("write_attribute", func(t *testing.T) {
		res, err := e.client.WriteAttribute(ctx, id, fmt.Sprintf("%d/6/16385", ep), 10)
		if err != nil || len(res) != 1 || res[0].Status != 0 || res[0].Path.AttributeID != 16385 {
			t.Fatalf("%+v %v", res, err)
		}
		raw, err := e.client.CallRaw(ctx, "write_attribute", map[string]any{"node_id": id, "attribute_path": fmt.Sprintf("%d/6/16385", ep), "value": 3})
		if err != nil || !strings.Contains(string(raw), `"Path":{"EndpointId":`) || !strings.Contains(string(raw), `"AttributeId":16385}`) || !strings.Contains(string(raw), `"Status":0`) {
			t.Fatalf("raw %s %v", raw, err)
		}
		_, err = e.client.WriteAttribute(ctx, id, fmt.Sprintf("%d/*/0", ep), true)
		wantCode(t, err, protocol.InvalidArguments, "wildcard")
		res, err = e.client.WriteAttribute(ctx, id, fmt.Sprintf("%d/6/0", ep), true)
		if err != nil || len(res) != 1 || res[0].Status == 0 {
			t.Fatalf("a write of the read-only OnOff: %+v %v", res, err)
		}
	})
	t.Run("device_command", func(t *testing.T) {
		if _, err := e.client.DeviceCommand(ctx, id, ep, onoffdef.ClusterID, "Toggle", nil); err != nil {
			t.Fatal(err)
		}
		_, err := e.client.DeviceCommand(ctx, id, ep, onoffdef.ClusterID, "explode", nil)
		wantCode(t, err, protocol.InvalidArguments, "no command")
		_, err = e.client.DeviceCommand(ctx, id, ep, onoffdef.ClusterID, "OffWithEffect", map[string]any{"bogus": 1})
		wantCode(t, err, protocol.InvalidArguments, "")
		// A command the endpoint does not serve answers the device's status.
		_, err = e.client.DeviceCommand(ctx, id, ep, 8, "MoveToLevel", map[string]any{"level": 1, "transitionTime": 0, "optionsMask": 0, "optionsOverride": 0})
		pe := wantCode(t, err, protocol.UnknownError, "")
		if pe.Status == nil {
			t.Fatalf("a device status carries status: %+v", pe)
		}
	})
	t.Run("interview_node", func(t *testing.T) {
		if err := e.client.InterviewNode(ctx, id); err != nil {
			t.Fatal(err)
		}
		e.waitEvent(e.client, "node_updated", nil)
	})
	t.Run("ping_node", func(t *testing.T) {
		res, err := e.client.PingNode(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for ip, ok := range res {
			if !ok {
				t.Fatalf("%s unreachable: %v", ip, res)
			}
		}
	})
	t.Run("get_node_ip_addresses", func(t *testing.T) {
		if _, err := e.client.GetNodeIPAddresses(ctx, id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("get_matter_fabrics", func(t *testing.T) {
		fabrics, err := e.client.GetMatterFabrics(ctx, id)
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("%v %v", fabrics, err)
		}
		if fmt.Sprint(fabrics[0]["fabric_id"]) != fmt.Sprint(e.client.ServerInfo().FabricID) {
			t.Fatalf("fabric %v, server_info %d", fabrics[0], e.client.ServerInfo().FabricID)
		}
	})
	t.Run("remove_matter_fabric", func(t *testing.T) {
		// A fabric index the node does not have: the node answers in its
		// NOCResponse, the call itself succeeds.
		if err := e.client.RemoveMatterFabric(ctx, id, 9); err != nil {
			t.Fatal(err)
		}
		err := e.client.Call(ctx, "remove_matter_fabric", map[string]any{"node_id": id}, nil)
		wantCode(t, err, protocol.InvalidArguments, "fabric_index")
	})
	t.Run("open_commissioning_window", func(t *testing.T) {
		w, err := e.client.OpenCommissioningWindow(ctx, id, 300)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"setup_pin_code", "setup_manual_code", "setup_qr_code", "discriminator", "vendor_id", "product_id", "commissioning_timeout"} {
			if _, ok := w[k]; !ok {
				t.Fatalf("the window lacks %s: %v", k, w)
			}
		}
		if fmt.Sprint(w["commissioning_timeout"]) != "300" || !strings.HasPrefix(fmt.Sprint(w["setup_qr_code"]), "MT:") {
			t.Fatalf("window %v", w)
		}
		// The window's QR code decodes to the passcode it names.
		var p struct {
			Payloads []struct {
				SetupPinCode  uint32 `json:"setup_pin_code"`
				Discriminator uint16 `json:"discriminator"`
			} `json:"payloads"`
		}
		if err := e.client.Call(ctx, "parse_onboarding_payload", map[string]any{"code": w["setup_qr_code"]}, &p); err != nil || len(p.Payloads) != 1 ||
			fmt.Sprint(p.Payloads[0].SetupPinCode) != fmt.Sprint(w["setup_pin_code"]) || fmt.Sprint(p.Payloads[0].Discriminator) != fmt.Sprint(w["discriminator"]) {
			t.Fatalf("the window's QR code %+v %v", p, err)
		}
		err = e.client.Call(ctx, "open_commissioning_window", map[string]any{"node_id": id, "timeout": 0}, nil)
		wantCode(t, err, protocol.InvalidArguments, "positive number of seconds")
		_, err = e.client.OpenCommissioningWindow(ctx, 4242, 300)
		wantCode(t, err, protocol.NodeNotExists, "")
	})
	t.Run("batched_device_commands", func(t *testing.T) {
		var res []map[string]any
		err := e.client.Call(ctx, "batched_device_commands", map[string]any{"node_id": id, "commands": []any{
			map[string]any{"endpoint_id": ep, "cluster_id": 6, "command_name": "On", "payload": map[string]any{}},
		}}, &res)
		if err != nil || len(res) != 1 || fmt.Sprint(res[0]["status"]) != "0" {
			t.Fatalf("%v %v", res, err)
		}
		e.waitEvent(e.client, "attribute_updated", func(raw json.RawMessage) bool { return strings.Contains(string(raw), fmt.Sprintf(`"%d/6/0",true`, ep)) })
		many := make([]any, 300)
		for i := range many {
			many[i] = map[string]any{"endpoint_id": ep, "cluster_id": 6, "command_name": "Toggle"}
		}
		err = e.client.Call(ctx, "batched_device_commands", map[string]any{"node_id": id, "commands": many}, nil)
		wantCode(t, err, protocol.InvalidArguments, "per invoke")
		for _, bad := range []struct {
			cmds any
			want string
		}{
			{[]any{}, "non-empty commands list"},
			{[]any{1}, "command 0 must be an object"},
			{[]any{map[string]any{"cluster_id": 6, "command_name": "On"}}, "needs endpoint_id"},
			{[]any{map[string]any{"endpoint_id": ep, "cluster_id": 6, "command_name": "Fly"}}, `Command "Fly" does not exist`},
			{[]any{map[string]any{"endpoint_id": ep, "cluster_id": 0xFFF1FC01, "command_name": "On"}}, "unknown"},
			{[]any{map[string]any{"endpoint_id": ep, "cluster_id": 8, "command_name": "MoveToLevel", "payload": map[string]any{"level": "x"}}}, "Invalid command fields"},
		} {
			err := e.client.Call(ctx, "batched_device_commands", map[string]any{"node_id": id, "commands": bad.cmds}, nil)
			wantCode(t, err, protocol.InvalidArguments, bad.want)
		}
	})
	t.Run("subscribe outside the mode", func(t *testing.T) {
		_, err := e.client.Subscribe(ctx, id, []string{"1/6/0"}, 0, 10)
		wantCode(t, err, protocol.InvalidArguments, "--disable-auto-subscription")
		wantCode(t, e.client.Unsubscribe(ctx, id, 1), protocol.InvalidArguments, "--disable-auto-subscription")
		for _, cmd := range []string{"enable_auto_subscription", "disable_auto_subscription"} {
			wantCode(t, e.client.Call(ctx, cmd, map[string]any{"node_id": id}, nil), protocol.InvalidArguments, "--disable-auto-subscription")
		}
	})
	t.Run("diagnostics", func(t *testing.T) {
		d, err := e.client.Diagnostics(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"info", "nodes", "events"} {
			if _, ok := d[k]; !ok {
				t.Fatalf("diagnostics lacks %s", k)
			}
		}
	})
	t.Run("remove_node", func(t *testing.T) {
		if err := e.client.RemoveNode(ctx, id); err != nil {
			t.Fatal(err)
		}
		e.waitEvent(e.client, "node_removed", func(raw json.RawMessage) bool { return string(raw) == fmt.Sprint(id) })
		_, err := e.client.GetNode(ctx, id)
		wantCode(t, err, protocol.NodeNotExists, "")
	})
}

func TestServerCommands(t *testing.T) {
	e := newEnv(t, envOptions{})
	ctx := e.ctx
	t.Run("credentials", func(t *testing.T) {
		if err := e.client.SetWifiCredentials(ctx, "home", "secret"); err != nil {
			t.Fatal(err)
		}
		var info fsclient.ServerInfo
		decode(t, e.waitEvent(e.client, "server_info_updated", nil), &info)
		if !info.WifiCredentialsSet || info.WifiSSID == nil || *info.WifiSSID != "home" {
			t.Fatalf("server_info_updated %+v", info)
		}
		if err := e.client.SetThreadDataset(ctx, "0e080000000000010000"); err != nil {
			t.Fatal(err)
		}
		wantCode(t, e.client.SetThreadDataset(ctx, "zz"), protocol.InvalidArguments, "hex")
		wantCode(t, e.client.Call(ctx, "set_wifi_credentials", map[string]any{"ssid": "other"}, nil), protocol.InvalidArguments, "")
		all, err := e.client.GetAllCredentials(ctx)
		if err != nil || strings.Contains(fmt.Sprint(all), "secret") {
			t.Fatalf("get_all_credentials %v %v (no secret may leave)", all, err)
		}
		if err := e.client.Call(ctx, "remove_wifi_credentials", nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := e.client.Call(ctx, "remove_thread_dataset", map[string]any{"id": "default"}, nil); err != nil {
			t.Fatal(err)
		}
		if e.api.FabricLabel() != "HomeAssistant" {
			t.Fatal("the default label is matterjs-server's")
		}
	})
	t.Run("fabric label", func(t *testing.T) {
		if err := e.client.SetDefaultFabricLabel(ctx, "Home"); err != nil {
			t.Fatal(err)
		}
		if l, err := e.client.GetFabricLabel(ctx); err != nil || l != "Home" {
			t.Fatalf("%q %v", l, err)
		}
		// The label the store and the fabric hold is not pushed again
		// (Home Assistant sets it on every connect); a new one is.
		pushes := e.api.LabelPushes()
		if err := e.client.SetDefaultFabricLabel(ctx, "Home"); err != nil {
			t.Fatal(err)
		}
		if n := e.api.LabelPushes(); n != pushes {
			t.Fatalf("an unchanged label started %d pushes", n-pushes)
		}
		if err := e.client.SetDefaultFabricLabel(ctx, " Home "); err != nil {
			t.Fatal(err)
		}
		if n := e.api.LabelPushes(); n != pushes {
			t.Fatalf("the normalized unchanged label started %d pushes", n-pushes)
		}
		if err := e.client.SetDefaultFabricLabel(ctx, "Home2"); err != nil {
			t.Fatal(err)
		}
		if n := e.api.LabelPushes(); n != pushes+1 {
			t.Fatalf("a new label started %d pushes", n-pushes)
		}
		if err := e.client.SetDefaultFabricLabel(ctx, "Home"); err != nil {
			t.Fatal(err)
		}
		// A second connection does not own the label.
		c2 := e.dial(fsclient.Options{NoListen: true})
		if err := c2.SetDefaultFabricLabel(ctx, "Other"); err != nil {
			t.Fatal(err)
		}
		if l, _ := e.client.GetFabricLabel(ctx); l != "Home" {
			t.Fatalf("label %q: the owner's label stays", l)
		}
		wantCode(t, e.client.Call(ctx, "set_default_fabric_label", map[string]any{}, nil), protocol.InvalidArguments, "label")
	})
	t.Run("log level", func(t *testing.T) {
		l, err := e.client.GetLogLevel(ctx)
		if err != nil || l.Console != "info" || l.File != nil {
			t.Fatalf("%+v %v", l, err)
		}
		if l, err = e.client.SetLogLevel(ctx, "warn"); err != nil || l.Console != "info" {
			// No LevelVar in the test server: the level stays.
			t.Fatalf("%+v %v", l, err)
		}
		_, err = e.client.SetLogLevel(ctx, "loud")
		wantCode(t, err, protocol.InvalidArguments, "console_loglevel")
	})
	t.Run("parse_onboarding_payload", func(t *testing.T) {
		// matterjs-server's documented examples (the compat fixtures).
		for _, c := range []struct{ code, fixture string }{
			{"MT:Y.K90AFN00KA0648G00", "parse_onboarding_payload.value.json"},
			{"34970112332", "parse_onboarding_payload.value.2.json"},
		} {
			raw, err := e.client.CallRaw(ctx, "parse_onboarding_payload", map[string]any{"code": c.code})
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile("../protocol/testdata/compat/" + c.fixture)
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, want, raw) {
				t.Fatalf("%s:\nwant %s\ngot  %s", c.code, want, raw)
			}
		}
		for _, bad := range []string{"MT:invalid", "1234", "34970112333"} {
			err := e.client.Call(ctx, "parse_onboarding_payload", map[string]any{"code": bad}, nil)
			pe := wantCode(t, err, protocol.InvalidArguments, "Invalid pairing code")
			if pe.Reason == nil || *pe.Reason != protocol.ReasonInvalidPayload {
				t.Fatalf("%s: reason %v", bad, pe.Reason)
			}
		}
		wantCode(t, e.client.Call(ctx, "parse_onboarding_payload", nil, nil), protocol.InvalidArguments, "code")
	})
	t.Run("discover", func(t *testing.T) {
		found, err := e.client.Discover(ctx)
		if err != nil || len(found) != 1 {
			t.Fatalf("%v %v", found, err)
		}
		if fmt.Sprint(found[0]["long_discriminator"]) != fmt.Sprint(e.dev.Discriminator()) {
			t.Fatalf("discovered %v", found[0])
		}
		if _, err := e.client.CallRaw(ctx, "discover", nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCommissionOnNetwork(t *testing.T) {
	e := newEnv(t, envOptions{})
	n, err := e.client.CommissionOnNetwork(e.ctx, e.dev.Passcode(), 2, uint64(e.dev.Discriminator()), "")
	if err != nil || n.NodeID == 0 {
		t.Fatalf("%+v %v", n, err)
	}
	err = e.client.Call(e.ctx, "commission_on_network", map[string]any{"setup_pin_code": 1, "filter_type": 4}, nil)
	wantCode(t, err, protocol.InvalidArguments, "needs a filter")
	err = e.client.Call(e.ctx, "commission_on_network", map[string]any{"filter_type": 0}, nil)
	wantCode(t, err, protocol.InvalidArguments, "setup_pin_code")
}

func TestCommissionFailures(t *testing.T) {
	e := newEnv(t, envOptions{})
	_, err := e.client.CommissionWithCode(e.ctx, "1234", false)
	pe := wantCode(t, err, protocol.InvalidArguments, "")
	if pe.Reason == nil || *pe.Reason != protocol.ReasonInvalidPayload {
		t.Fatalf("reason %v", pe.Reason)
	}
	_, err = e.client.CommissionWithCode(e.ctx, "34970112333", false)
	pe = wantCode(t, err, protocol.InvalidArguments, "Invalid pairing code")
	if pe.Reason == nil || *pe.Reason != protocol.ReasonInvalidPayload {
		t.Fatalf("reason %v", pe.Reason)
	}
	// A valid code no device answers (the test code of discriminator
	// 3840 short 15, which the site device does not use).
	if e.dev.Discriminator()>>8 == 15 {
		t.Skip("the site device takes discriminator 15")
	}
	err = e.client.Call(e.ctx, "commission_with_code", map[string]any{"code": "34970112332", "network_only": true, "discovery_timeout_s": 2}, nil)
	pe = wantCode(t, err, protocol.NodeCommissionFailed, "")
	if pe.Reason == nil || *pe.Reason != protocol.ReasonNotDiscovered {
		t.Fatalf("reason %v", pe.Reason)
	}
	_, err = e.client.CommissionOnNetwork(e.ctx, e.dev.Passcode()+1, 2, uint64(e.dev.Discriminator()), "")
	pe = wantCode(t, err, protocol.NodeCommissionFailed, "")
	if pe.Reason == nil || *pe.Reason != protocol.ReasonPasscodeMismatch {
		t.Fatalf("reason %v (%s)", pe.Reason, pe.Details)
	}
}

func TestErrors(t *testing.T) {
	e := newEnv(t, envOptions{})
	ctx := e.ctx
	wantCode(t, e.client.Call(ctx, "no_such_command", nil, nil), protocol.InvalidCommand, "Unknown command: no_such_command")
	for _, cmd := range []string{"check_node_update", "update_node", "initiate_ota_upload"} {
		wantCode(t, e.client.Call(ctx, cmd, nil, nil), protocol.InvalidCommand, "milestone 5 phase 25")
	}
	for _, cmd := range []string{"get_thread_border_routers", "get_thread_diagnostics"} {
		wantCode(t, e.client.Call(ctx, cmd, nil, nil), protocol.InvalidCommand, "milestone 6, ADR 0027")
	}
	wantCode(t, e.client.Call(ctx, "camera_snapshot", nil, nil), protocol.InvalidCommand, "not planned")
	wantCode(t, e.client.Call(ctx, "send_webrtc_provider_command", nil, nil), protocol.InvalidCommand, "not planned")
	wantCode(t, e.client.Call(ctx, "get_node", nil, nil), protocol.InvalidArguments, "node_id is required")
	wantCode(t, e.client.Call(ctx, "get_node", map[string]any{"node_id": 4242}, nil), protocol.NodeNotExists, "4242")
	wantCode(t, e.client.Call(ctx, "get_node", map[string]any{"node_id": "x"}, nil), protocol.InvalidArguments, "")
	wantCode(t, e.client.Call(ctx, "get_node", map[string]any{"node_id": 1.5}, nil), protocol.InvalidArguments, "")
	_, err := e.client.SendRaw(ctx, "7", []byte(`{"message_id":"7","command":"get_nodes","args":[1]}`))
	wantCode(t, err, protocol.InvalidArguments, "get_nodes: args must be a JSON object")
	_, err = e.client.SendRaw(ctx, "8", []byte(`{"message_id":"8","command":"get_nodes","args":null}`))
	if err != nil {
		t.Fatalf("null args are the empty set: %v", err)
	}
	_, err = e.client.SendRaw(ctx, "", []byte(`not json`))
	wantCode(t, err, protocol.InvalidArguments, "not a JSON object")
	// A large node id travels exactly.
	wantCode(t, e.client.Call(ctx, "get_node", map[string]any{"node_id": uint64(18446744069414584320)}, nil), protocol.NodeNotExists, "18446744069414584320")
}

func TestToken(t *testing.T) {
	e := newEnv(t, envOptions{token: "s3cret", noDial: true})
	if _, err := fsclient.Dial(e.ctx, e.url, fsclient.Options{}); err == nil {
		t.Fatal("a connection without the token is refused")
	}
	if _, err := fsclient.Dial(e.ctx, e.url, fsclient.Options{Token: "wrong"}); err == nil {
		t.Fatal("a connection with a wrong token is refused")
	}
	c := e.dial(fsclient.Options{Token: "s3cret"})
	if c.ServerInfo().SchemaVersion != protocol.SchemaVersion {
		t.Fatal("server_info")
	}
	// A browser page of another origin is refused even with the token.
	h := http.Header{"Origin": []string{"http://evil.example"}, "Authorization": []string{"Bearer s3cret"}}
	if ws, resp, err := websocket.Dial(e.ctx, e.url, &websocket.DialOptions{HTTPHeader: h}); err == nil {
		_ = ws.CloseNow()
		t.Fatal("a cross-origin handshake is refused")
	} else if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// The query form.
	c2, err := fsclient.Dial(e.ctx, e.url+"?token=s3cret", fsclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.Close()
}

func TestSubscribeMode(t *testing.T) {
	e := newEnv(t, envOptions{server: controllerapi.Options{DisableAutoSubscription: true}})
	n := e.commission()
	ep := e.lightEndpoint()
	key := fmt.Sprintf("%d/6/0", ep)
	sub, err := e.client.Subscribe(e.ctx, n.NodeID, []string{key}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if sub.SubscriptionID == 0 || sub.Attributes[key] != false {
		t.Fatalf("priming %+v", sub)
	}
	if err := e.dev.Act(func(*node.Node) error { return e.light.OnOff().Set(onoffdef.Patch{OnOff: new(true)}) }); err != nil {
		t.Fatal(err)
	}
	raw := e.waitEvent(e.client, "subscription_report", nil)
	var rep fsclient.Report
	decode(t, raw, &rep)
	if rep.SubscriptionID != sub.SubscriptionID || rep.NodeID != n.NodeID || rep.Attributes[key] != true {
		t.Fatalf("report %s", raw)
	}
	if err := e.client.Unsubscribe(e.ctx, n.NodeID, sub.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.client.Unsubscribe(e.ctx, n.NodeID, sub.SubscriptionID), protocol.InvalidArguments, "holds no subscription")
	// enable_auto_subscription: the server's own subscription; a client
	// subscription is refused while it is on and ended by it.
	sub2, err := e.client.Subscribe(e.ctx, n.NodeID, []string{key}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.client.Call(e.ctx, "enable_auto_subscription", map[string]any{"node_id": n.NodeID}, nil); err != nil {
		t.Fatal(err)
	}
	e.waitEvent(e.client, "subscription_ended", func(raw json.RawMessage) bool {
		return strings.Contains(string(raw), fmt.Sprintf(`"subscription_id":%d`, sub2.SubscriptionID))
	})
	_, err = e.client.Subscribe(e.ctx, n.NodeID, []string{key}, 0, 10)
	wantCode(t, err, protocol.InvalidArguments, "enable_auto_subscription")
	if err := e.client.Call(e.ctx, "disable_auto_subscription", map[string]any{"node_id": n.NodeID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Subscribe(e.ctx, n.NodeID, []string{key}, 0, 10); err != nil {
		t.Fatalf("subscribe after disable_auto_subscription: %v", err)
	}
	wantCode(t, e.client.Call(e.ctx, "enable_auto_subscription", map[string]any{"node_id": 4242}, nil), protocol.NodeNotExists, "")
}

// sameJSON compares two JSON documents as values, numbers by their text.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	for _, x := range []struct {
		b []byte
		v *any
	}{{a, &va}, {b, &vb}} {
		dec := json.NewDecoder(bytes.NewReader(x.b))
		dec.UseNumber()
		if err := dec.Decode(x.v); err != nil {
			t.Fatalf("%s: %v", x.b, err)
		}
	}
	return reflect.DeepEqual(va, vb)
}
