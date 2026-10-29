// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// declaration is the brief's shape: one ElectricalSensor with a
// PowerSource (go-fabric's embed-in-a-daemon cookbook, §3) and one
// OnOffLight whose Toggle the host answers. Since go-fabric v0.12.1 a
// bridged device's Identify takes the host's handlers (report S2, Q16);
// TestBridgeErrors holds the declaration and the refusal of anything else.
const declaration = `{"devices": [
 {"key": "zendure-1", "type": "ElectricalSensor", "name": "Zendure", "vendor": "Zendure", "device_address": "SN1",
  "clusters": {
   "PowerTopology": {"features": ["NodeTopology"]},
   "ElectricalPowerMeasurement": {"features": ["AlternatingCurrent"], "initial": {
     "powerMode": 2, "numberOfMeasurementTypes": 1, "activePower": 1234,
     "accuracy": [{"measurementType": 2, "measured": true, "minMeasuredValue": -10000000, "maxMeasuredValue": 10000000,
       "accuracyRanges": [{"rangeMin": -10000000, "rangeMax": 10000000, "fixedMax": 1000}]}]}},
   "PowerSource": {"features": ["Battery"], "initial": {"order": 0, "batReplacementNeeded": false, "batPercentRemaining": 180, "description": "Battery"}}
  }},
 {"key": "light-1", "type": "OnOffLight", "name": "Lamp",
  "clusters": {"OnOff": {"initial": {"onOff": false}, "handle_commands": ["Toggle"]}}}
]}`

func path(t *testing.T, ep uint16, cluster, attribute string) string {
	t.Helper()
	cl := schema.ClusterByName(cluster)
	a := cl.AttributeByName(attribute)
	if a == nil {
		t.Fatalf("%s.%s", cluster, attribute)
	}
	return fmt.Sprintf("%d/%d/%d", ep, cl.ID, a.ID)
}

func endpoints(t *testing.T, r fsclient.DeclareResult) map[string]uint16 {
	t.Helper()
	out := map[string]uint16{}
	for _, d := range r.Devices {
		if d.Error != "" || d.Endpoint == nil {
			t.Fatalf("declare %+v", d)
		}
		out[d.Key] = *d.Endpoint
	}
	return out
}

func TestBridgeEndToEnd(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{ResultTimeout: 2 * time.Second})
	info := e.host.Info()
	if info.BridgeSchemaVersion != bridgeapi.BridgeSchemaVersion || !slices.Contains(info.Capabilities, "declare") || info.SDKVersion != "go-fabric-server/test (go-fabric/v-test)" {
		t.Fatalf("server_info %+v", info)
	}
	res, err := e.host.Declare(e.ctx, doc(declaration))
	if err != nil {
		t.Fatalf("declare: %v", err)
	}
	eps := endpoints(t, res)
	if res.Devices[0].Action != "added" || len(res.Removed) != 0 {
		t.Fatalf("declare %+v", res)
	}
	// The same document again changes nothing.
	again, err := e.host.Declare(e.ctx, doc(declaration))
	if err != nil || again.Devices[0].Action != "unchanged" || *again.Devices[1].Endpoint != eps["light-1"] {
		t.Fatalf("declare again %+v %v", again, err)
	}

	nodeID := e.controller()
	zen, light := eps["zendure-1"], eps["light-1"]
	vals, err := e.ctl.ReadAttribute(e.ctx, nodeID, path(t, zen, "ElectricalPowerMeasurement", "activePower"), path(t, zen, "PowerSource", "batPercentRemaining"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(vals[path(t, zen, "ElectricalPowerMeasurement", "activePower")]) != "1234" || fmt.Sprint(vals[path(t, zen, "PowerSource", "batPercentRemaining")]) != "180" {
		t.Fatalf("read %v", vals)
	}

	// A controller's write: the server applies it, the host sees changed.
	onTime := path(t, light, "OnOff", "onTime")
	if _, err := e.ctl.WriteAttribute(e.ctx, nodeID, onTime, 30); err != nil {
		t.Fatal(err)
	}
	ch := e.bridgeEvent("changed", "light-1")
	if ch.Cluster != "OnOff" || ch.Origin != "controller_write" || !slices.Contains(ch.Attributes, "onTime") || fmt.Sprint(ch.State["onTime"]) != "30" {
		t.Fatalf("changed %+v", ch)
	}
	// A command the server answers from state: changed with its origin.
	if _, err := e.ctl.DeviceCommand(e.ctx, nodeID, light, schema.ClusterByName("OnOff").ID, "On", nil); err != nil {
		t.Fatal(err)
	}
	ch = e.bridgeEvent("changed", "light-1")
	if ch.Origin != "command" || ch.State["onOff"] != true {
		t.Fatalf("changed %+v", ch)
	}

	// A command the host opted into: the command event, the host's result.
	onOff := schema.ClusterByName("OnOff").ID
	done := make(chan error, 1)
	toggle := func() {
		go func() {
			_, err := e.ctl.DeviceCommand(e.ctx, nodeID, light, onOff, "Toggle", nil)
			done <- err
		}()
	}
	toggle()
	cmd := e.bridgeEvent("command", "light-1")
	if cmd.Cluster != "OnOff" || cmd.Command != "Toggle" || cmd.TimeoutMS != 2000 || cmd.Deadline == "" {
		t.Fatalf("command %+v %s", cmd, cmd.Fields)
	}
	if err := e.host.Result(e.ctx, cmd.Correlation, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	// A result twice is refused.
	if err := e.host.Result(e.ctx, cmd.Correlation, nil, nil); err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("second result: %v", err)
	}

	// The host refuses with a status: the controller gets that status
	// (contract.StatusError, go-fabric v0.12.1; Q15).
	toggle()
	cmd = e.bridgeEvent("command", "light-1")
	status := 0x87 // CONSTRAINT_ERROR
	if err := e.host.Result(e.ctx, cmd.Correlation, nil, &fsclient.ResultError{Status: &status}); err != nil {
		t.Fatal(err)
	}
	var perr *protocol.Error
	if err := <-done; !errors.As(err, &perr) || perr.Status == nil || *perr.Status != 0x87 {
		t.Fatalf("refused Toggle: %v", err)
	}

	// A late result: past the deadline the controller is answered, the
	// host gets result_timeout, and its result is refused.
	toggle()
	cmd = e.bridgeEvent("command", "light-1")
	to := e.bridgeEvent("result_timeout", "light-1")
	if to.Correlation != cmd.Correlation || to.Command != "Toggle" {
		t.Fatalf("result_timeout %+v for %d", to, cmd.Correlation)
	}
	if err := <-done; !errors.As(err, &perr) || perr.Status == nil || *perr.Status != 0x94 {
		t.Fatalf("timed-out Toggle: %v, want TIMEOUT (0x94)", err)
	}
	if err := e.host.Result(e.ctx, cmd.Correlation, nil, nil); err == nil {
		t.Fatal("a late result was accepted")
	}

	// The host's own set reaches the controller's subscription, and the
	// host sees no changed event for it.
	if err := e.host.Set(e.ctx, "zendure-1", "ElectricalPowerMeasurement", map[string]any{"activePower": -600000}); err != nil {
		t.Fatal(err)
	}
	ap := path(t, zen, "ElectricalPowerMeasurement", "activePower")
	event(t, e.ctl, "attribute_updated", func(raw json.RawMessage) bool {
		var u []any
		decode(t, raw, &u)
		return len(u) == 3 && u[1] == ap && fmt.Sprint(u[2]) == "-600000"
	})

	// Reachability.
	off := false
	if err := e.host.Host(e.ctx, fsclient.HostStatement{Device: "zendure-1", Reachable: &off}); err != nil {
		t.Fatal(err)
	}
	reach := path(t, zen, "BridgedDeviceBasicInformation", "reachable")
	event(t, e.ctl, "attribute_updated", func(raw json.RawMessage) bool {
		var u []any
		decode(t, raw, &u)
		return len(u) == 3 && u[1] == reach && u[2] == false
	})

	// node_info: the devices, the fabric, the subscription.
	ni, err := e.host.NodeInfo(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if devs, _ := ni["devices"].([]any); len(devs) != 2 {
		t.Fatalf("node_info %v", ni)
	}
	if fabrics, _ := ni["fabrics"].([]any); len(fabrics) != 1 {
		t.Fatalf("node_info fabrics %v", ni["fabrics"])
	}

	// Remove erases the light; a declare without it would too.
	if err := e.host.Remove(e.ctx, "light-1"); err != nil {
		t.Fatal(err)
	}
	event(t, e.ctl, "endpoint_removed", nil)
	if err := e.host.Remove(e.ctx, "light-1"); err == nil {
		t.Fatal("a second remove")
	}
	ni, _ = e.host.NodeInfo(e.ctx)
	if devs, _ := ni["devices"].([]any); len(devs) != 1 {
		t.Fatalf("node_info after remove %v", ni)
	}
}

// TestBridgeEvents: the node's own events reach the listening client.
func TestBridgeEvents(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"l","type":"OnOffLight"}]}`)); err != nil {
		t.Fatal(err)
	}
	e.controller()
	ev := event(t, e.host.Client, "fabric_added", nil)
	var f map[string]any
	decode(t, ev, &f)
	if fmt.Sprint(f["fabric_index"]) != "1" {
		t.Fatalf("fabric_added %s", ev)
	}
	event(t, e.host.Client, "commissioning_window", nil)
	event(t, e.host.Client, "subscriptions", func(raw json.RawMessage) bool { return !strings.Contains(string(raw), `"count":0`) })
	// The dump of a second listening start.
	e.host.Close()
	time.Sleep(50 * time.Millisecond)
	b := e.dialBridge(fsclient.Options{})
	if devs := b.Devices(); len(devs) != 1 || devs[0].Key != "l" || devs[0].Clusters["OnOff"] == nil {
		t.Fatalf("dump %+v", devs)
	}
}

// TestBridgeWrites: a cluster with handle_writes asks the host for every
// write.
func TestBridgeWrites(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{ResultTimeout: 5 * time.Second})
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"w","type":"OnOffLight","clusters":{"OnOff":{"handle_writes":true}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ep := endpoints(t, res)["w"]
	nodeID := e.controller()
	onTime := path(t, ep, "OnOff", "onTime")
	type wr struct {
		r   []fsclient.WriteResult
		err error
	}
	done := make(chan wr, 1)
	write := func(v int) {
		go func() {
			r, err := e.ctl.WriteAttribute(e.ctx, nodeID, onTime, v)
			done <- wr{r, err}
		}()
	}
	write(7)
	w := e.bridgeEvent("write", "w")
	if w.Attribute != "onTime" || string(w.Value) != "7" {
		t.Fatalf("write %+v %s", w, w.Value)
	}
	if err := e.host.Result(e.ctx, w.Correlation, nil, &fsclient.ResultError{}); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != nil || len(r.r) != 1 || r.r[0].Status != 0x01 {
		t.Fatalf("refused write without a status %+v, want FAILURE", r)
	}
	write(8)
	w = e.bridgeEvent("write", "w")
	if err := e.host.Result(e.ctx, w.Correlation, nil, nil); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != nil || len(r.r) != 1 || r.r[0].Status != 0 {
		t.Fatalf("accepted write %+v", r)
	}
	ch := e.bridgeEvent("changed", "w")
	if ch.Origin != "controller_write" || fmt.Sprint(ch.State["onTime"]) != "8" {
		t.Fatalf("changed %+v", ch)
	}
	// Without a listening client the write is refused at once.
	e.host.Close()
	time.Sleep(50 * time.Millisecond)
	write(9)
	if r := <-done; r.err != nil || len(r.r) != 1 || r.r[0].Status == 0 {
		t.Fatalf("write without a host %+v", r)
	}
}

func TestBridgeOneClient(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	_, err := fsclient.DialBridge(e.ctx, e.base+"/bridge", fsclient.Options{})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.ErrorCode != protocol.BridgeClientConnected || !strings.Contains(perr.Details, "bridge1") {
		t.Fatalf("second client: %v", err)
	}
	// Once the holder leaves, the next client is served.
	e.host.Close()
	time.Sleep(50 * time.Millisecond)
	e.dialBridge(fsclient.Options{NoListen: true})
}

func TestBridgeErrors(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	for _, c := range []struct{ doc, want string }{
		{`{}`, "devices is required"},
		{`{"devices":[], "extra": 1}`, `unknown field "extra"`},
		{`{"devices":[{"type":"OnOffLight"}]}`, "devices[0].key"},
		{`{"devices":[{"key":"a","type":"Toaster"}]}`, `no device type "Toaster"`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"Nope":{}}}]}`, `devices[0].clusters.Nope: no cluster "Nope"`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"AccessControl":{}}}]}`, "no typed package"},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"initial":{"nope":1}}}}]}`, `devices[0].clusters.OnOff.initial.nope`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"initial":{"onOff":3}}}}]}`, `initial.onOff: number is not a valid boolean`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"bogus":1}}}]}`, `unknown field "bogus"`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{},"onoff":{}}}]}`, "declared twice"},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"features":["Nope"]}}}]}`, `no feature "Nope"`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"handle_commands":["Fly"]}}}]}`, `no command "Fly"`},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"Thermostat":{}}}]}`, "devices[0] (a):"},
		// The kit's refusal, with its message: the assembler mounts a
		// bridged device's Identify and takes only its handlers (go-fabric
		// v0.12.1 closed Q16 for HandleIdentify/HandleTriggerEffect).
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"Identify":{"initial":{"identifyTime":1}}}}]}`, "Identify"},
		{`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"handle_commands":["Toggle","Nope"]}}}]}`, `no command "Nope"`},
		{`{"devices":[{"key":"a","type":"OnOffLight"},{"key":"a","type":"OnOffLight"}]}`, "also the key of devices[0]"},
	} {
		_, err := e.host.Declare(e.ctx, doc(c.doc))
		var perr *protocol.Error
		if !errors.As(err, &perr) || perr.ErrorCode != protocol.InvalidArguments || !strings.Contains(perr.Details, c.want) {
			t.Errorf("%s: %v, want %q", c.doc, err, c.want)
		}
	}
	// VideoDoorbell, a composite the kit did not build before go-fabric
	// v0.13.12 (CHANGELOG: device.NewVideoDoorbell; v0.14.0 generates every
	// snapshot device type, 91/91): accepted, no longer "does not build".
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"VideoDoorbell"}]}`)); err != nil {
		t.Fatalf("a VideoDoorbell: %v", err)
	}
	// The host's Identify handler on a bridged device (Q16, go-fabric
	// v0.12.1): accepted.
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"Identify":{"handle_commands":["Identify"]}}}]}`)); err != nil {
		t.Fatalf("an Identify handler: %v", err)
	}
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight"}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		command string
		args    any
		want    string
	}{
		{"set", map[string]any{"device": "b", "cluster": "OnOff", "patch": map[string]any{}}, `no device "b"`},
		{"set", map[string]any{"device": "a", "cluster": "Thermostat", "patch": map[string]any{}}, "serves no Thermostat"},
		{"set", map[string]any{"device": "a", "cluster": "Nope", "patch": map[string]any{}}, `no cluster "Nope"`},
		{"set", map[string]any{"device": "a", "cluster": "OnOff"}, "patch is required"},
		{"set", map[string]any{"device": "a", "cluster": "OnOff", "patch": map[string]any{"onOff": "x"}}, "patch.onOff"},
		{"set", map[string]any{"device": "a", "cluster": "OnOff", "patch": map[string]any{"startUpOnOff": 9}}, "OnOff:"},
		{"host", map[string]any{}, "device is required"},
		{"remove", map[string]any{"device": "zz"}, `no device "zz"`},
		{"forget", map[string]any{}, "device is required"},
		{"result", map[string]any{}, "correlation is required"},
		{"result", map[string]any{"correlation": 99}, "correlation 99 is not pending"},
		{"result", map[string]any{"correlation": "x"}, "not an unsigned integer literal"},
		{"nope", nil, "Unknown command: nope"},
	} {
		err := e.host.Call(e.ctx, c.command, c.args, nil)
		var perr *protocol.Error
		if !errors.As(err, &perr) || !strings.Contains(perr.Details, c.want) {
			t.Errorf("%s %v: %v, want %q", c.command, c.args, err, c.want)
		}
	}
	// host statements apply.
	name, addr, label, on := "Renamed", "addr", "Label", true
	if err := e.host.Host(e.ctx, fsclient.HostStatement{Device: "a", Name: &name, DeviceAddress: &addr, ChannelAddress: &addr, Label: &label, Reachable: &on}); err != nil {
		t.Fatal(err)
	}
	// An empty declaration removes every device.
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[]}`))
	if err != nil || len(res.Removed) != 1 || res.Removed[0] != "a" {
		t.Fatalf("empty declare %+v %v", res, err)
	}
	var si map[string]any
	if err := e.host.Call(e.ctx, "server_info", nil, &si); err != nil || si["bridge_schema_version"] == nil {
		t.Fatalf("server_info %v %v", si, err)
	}
}

// TestForgetAndRemove: remove keeps the endpoint number stored under the
// key, so a re-declared key gets it back; forget frees it, so a
// re-declared key gets a fresh one, whether the key is still declared or
// remove dropped it before; forget is idempotent.
func TestForgetAndRemove(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	const ab = `{"devices":[{"key":"a","type":"OnOffLight"},{"key":"b","type":"OnOffLight"}]}`
	declare := func() map[string]uint16 {
		t.Helper()
		res, err := e.host.Declare(e.ctx, doc(ab))
		if err != nil {
			t.Fatal(err)
		}
		return endpoints(t, res)
	}
	first := declare()

	// remove keeps the number.
	if err := e.host.Remove(e.ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if got := declare(); got["a"] != first["a"] || got["b"] != first["b"] {
		t.Fatalf("after remove %v, want %v", got, first)
	}

	// forget of a declared key frees its number.
	var res map[string]any
	if err := e.host.Call(e.ctx, "forget", map[string]any{"device": "a"}, &res); err != nil || res["device"] != "a" {
		t.Fatalf("forget %v %v", res, err)
	}
	if err := e.host.Forget(e.ctx, "a"); err != nil {
		t.Fatalf("a second forget: %v", err)
	}
	second := declare()
	if second["a"] == first["a"] || second["b"] != first["b"] {
		t.Fatalf("after forget %v, first %v", second, first)
	}

	// forget of a key remove dropped frees its number too.
	if err := e.host.Remove(e.ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if err := e.host.Forget(e.ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if err := e.host.Forget(e.ctx, "b"); err != nil {
		t.Fatalf("a forget of a forgotten key: %v", err)
	}
	// A key the daemon never saw is forgotten too.
	if err := e.host.Forget(e.ctx, "never-declared"); err != nil {
		t.Fatalf("a forget of an unseen key: %v", err)
	}
	third := declare()
	if third["b"] == first["b"] || third["a"] != second["a"] {
		t.Fatalf("after remove and forget %v, before %v", third, second)
	}
}
