// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/protocol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/node"
)

func TestServerInfoShape(t *testing.T) {
	e := newEnv(t, envOptions{})
	info := e.client.ServerInfo()
	if info.SchemaVersion != protocol.SchemaVersion || info.MinSupportedSchemaVersion != protocol.MinSupportedSchemaVersion {
		t.Fatalf("schema %d/%d", info.SchemaVersion, info.MinSupportedSchemaVersion)
	}
	if info.SDKVersion != "go-fabric-server/test (go-fabric/v-test)" {
		t.Fatalf("sdk_version %q", info.SDKVersion)
	}
	if info.FabricID == 0 || info.CompressedFabricID == 0 || info.ControllerNodeID == nil {
		t.Fatalf("fabric %+v", info)
	}
	for _, want := range []string{"start_listening", "commission_with_code", "device_command", "read_attribute"} {
		if !slices.Contains(info.Capabilities, want) {
			t.Fatalf("capabilities %v lack %s", info.Capabilities, want)
		}
	}
	if slices.Contains(info.Capabilities, "batched_device_commands") {
		t.Fatal("a command without a facade route is in capabilities")
	}
	// The same object as a result.
	var again fsclient.ServerInfo
	if err := e.client.Call(e.ctx, "server_info", nil, &again); err != nil || again.FabricID != info.FabricID {
		t.Fatalf("server_info %+v %v", again, err)
	}
	// The raw frame has exactly the keys of the contract plus ours.
	raw, err := e.client.CallRaw(e.ctx, "server_info", nil)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	decode(t, raw, &keys)
	for _, k := range []string{"fabric_id", "compressed_fabric_id", "schema_version", "min_supported_schema_version", "sdk_version", "wifi_credentials_set", "thread_credentials_set", "bluetooth_enabled", "capabilities"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("server_info lacks %s: %s", k, raw)
		}
	}
}

func TestCommissionReadCommandAndEvents(t *testing.T) {
	e := newEnv(t, envOptions{})
	n := e.commission()
	if n.NodeID == 0 || n.InterviewVersion != 6 || n.AttributeSubscriptions == nil {
		t.Fatalf("node %+v", n)
	}
	ep := e.lightEndpoint()
	key := fmt.Sprintf("%d/6/0", ep)

	// node_added reaches the listening client.
	e.waitEvent(e.client, "node_added", nil)

	// A second client's start_listening dump holds the node.
	c2 := e.dial(fsclient.Options{})
	if len(c2.Nodes()) != 1 || c2.Nodes()[0].NodeID != n.NodeID {
		t.Fatalf("dump %+v", c2.Nodes())
	}
	dump := c2.Nodes()[0]
	if !dump.IsBridge {
		t.Fatal("a node with an Aggregator is a bridge")
	}
	if dump.EndpointParents[fmt.Sprint(ep)] == 0 {
		t.Fatalf("endpoint_parents %v: the light sits below the aggregator", dump.EndpointParents)
	}
	if v, ok := dump.Attributes[key]; !ok || v != false {
		t.Fatalf("%s = %v in the dump", key, v)
	}

	// read_attribute
	vals, err := e.client.ReadAttribute(e.ctx, n.NodeID, key)
	if err != nil || vals[key] != false {
		t.Fatalf("read_attribute %v %v", vals, err)
	}
	wild, err := e.client.ReadAttribute(e.ctx, n.NodeID, fmt.Sprintf("%d/6/*", ep))
	if err != nil || len(wild) < 3 {
		t.Fatalf("read_attribute wildcard %v %v", wild, err)
	}

	// device_command On → attribute_updated [node, path, true].
	res, err := e.client.DeviceCommand(e.ctx, n.NodeID, ep, onoffdef.ClusterID, "on", nil)
	if err != nil || string(res) != "null" {
		t.Fatalf("device_command %s %v", res, err)
	}
	match := func(v any) func(json.RawMessage) bool {
		return func(raw json.RawMessage) bool {
			var d []any
			decode(t, raw, &d)
			return len(d) == 3 && fmt.Sprint(d[0]) == fmt.Sprint(n.NodeID) && d[1] == key && d[2] == v
		}
	}
	e.waitEvent(c2, "attribute_updated", match(true))

	// The host toggles locally → attribute_updated false.
	if err := e.dev.Act(func(*node.Node) error { return e.light.OnOff().Set(onoffdef.Patch{OnOff: new(false)}) }); err != nil {
		t.Fatal(err)
	}
	e.waitEvent(c2, "attribute_updated", match(false))
	got, err := e.client.GetNode(e.ctx, n.NodeID)
	if err != nil || got.Attributes[key] != false {
		t.Fatalf("get_node %v %v", got.Attributes[key], err)
	}
	if !strings.Contains(got.DateCommissioned, "T") {
		t.Fatalf("date_commissioned %q", got.DateCommissioned)
	}
}
