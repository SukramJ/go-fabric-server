// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/protocol"
)

type listResult struct {
	Path struct {
		EndpointID  uint16 `json:"endpoint_id"`
		ClusterID   uint32 `json:"cluster_id"`
		AttributeID uint32 `json:"attribute_id"`
	} `json:"path"`
	Status int `json:"status"`
}

func TestSetACLEntryAndBinding(t *testing.T) {
	e := newEnv(t, envOptions{})
	n := e.commission()
	ctrlID := *e.client.ServerInfo().ControllerNodeID
	var res []listResult
	err := e.client.Call(e.ctx, "set_acl_entry", map[string]any{"node_id": n.NodeID, "entry": []any{
		map[string]any{"privilege": 5, "auth_mode": 2, "subjects": []uint64{ctrlID}, "targets": nil},
		// A self-ACL: dropped, the call still succeeds.
		map[string]any{"privilege": 3, "auth_mode": 2, "subjects": []uint64{n.NodeID}, "targets": nil},
		map[string]any{"privilege": 1, "auth_mode": 2, "subjects": []uint64{ctrlID, n.NodeID}, "targets": []any{map[string]any{"cluster": 6, "endpoint": nil, "device_type": nil}}},
	}}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Path.EndpointID != 0 || res[0].Path.ClusterID != 31 || res[0].Path.AttributeID != 0 || res[0].Status != 0 {
		t.Fatalf("set_acl_entry %+v", res)
	}
	vals, err := e.client.ReadAttribute(e.ctx, n.NodeID, "0/31/0")
	if err != nil {
		t.Fatal(err)
	}
	acl, _ := vals["0/31/0"].([]any)
	if len(acl) != 2 {
		t.Fatalf("acl %v", vals)
	}
	if first, _ := acl[0].(map[string]any); fmt.Sprint(first["1"]) != "5" {
		t.Fatalf("acl entry %v", acl[0])
	}
	if second, _ := acl[1].(map[string]any); fmt.Sprint(second["3"]) != fmt.Sprintf("[%d]", ctrlID) {
		t.Fatalf("self subject kept: %v", acl[1])
	}

	ep := e.lightEndpoint()
	err = e.client.Call(e.ctx, "set_node_binding", map[string]any{"node_id": n.NodeID, "endpoint": ep, "bindings": []any{
		map[string]any{"node": ctrlID, "group": nil, "endpoint": 1, "cluster": 6},
	}}, &res)
	if err != nil {
		t.Fatal(err)
	}
	// The light serves no Binding: the node's status is the answer.
	if len(res) != 1 || res[0].Path.EndpointID != ep || res[0].Path.ClusterID != 30 || res[0].Status == 0 {
		t.Fatalf("set_node_binding %+v", res)
	}

	for _, c := range []struct {
		command string
		args    map[string]any
		want    string
	}{
		{"set_acl_entry", map[string]any{"node_id": n.NodeID}, "entry is required"},
		{"set_acl_entry", map[string]any{"node_id": n.NodeID, "entry": []any{map[string]any{"privilege": 5}}}, "entry[0] needs privilege and auth_mode"},
		{"set_acl_entry", map[string]any{"node_id": n.NodeID, "entry": []any{map[string]any{"privilege": 5, "auth_mode": 2, "subjects": []any{"x"}}}}, ""},
		{"set_acl_entry", map[string]any{"entry": []any{}}, "node_id is required"},
		{"set_node_binding", map[string]any{"node_id": n.NodeID, "bindings": []any{}}, "endpoint is required"},
		{"set_node_binding", map[string]any{"node_id": n.NodeID, "endpoint": 1}, "bindings is required"},
		{"set_node_binding", map[string]any{"node_id": 4242, "endpoint": 1, "bindings": []any{}}, ""},
	} {
		err := e.client.Call(e.ctx, c.command, c.args, nil)
		var pe *protocol.Error
		if !errors.As(err, &pe) || pe.ErrorCode == 0 && c.want != "" || c.want != "" && !strings.Contains(pe.Details, c.want) {
			t.Errorf("%s %v: %v, want %q", c.command, c.args, err, c.want)
		}
	}
}

func TestGetVendorNames(t *testing.T) {
	fail := errors.New("offline")
	var down atomic.Bool
	src := func(context.Context) (map[uint16]string, error) {
		if down.Load() {
			return nil, fail
		}
		return map[uint16]string{4874: "Vendor A", 65521: "Test Vendor", 1: ""}, nil
	}
	e := newEnv(t, envOptions{server: controllerapi.Options{Vendors: src}})
	var all map[string]string
	if err := e.client.Call(e.ctx, "get_vendor_names", nil, &all); err != nil || len(all) != 3 || all["4874"] != "Vendor A" {
		t.Fatalf("all %v %v", all, err)
	}
	var some map[string]string
	if err := e.client.Call(e.ctx, "get_vendor_names", map[string]any{"filter_vendors": []int{65521, 1, 9}}, &some); err != nil || len(some) != 1 || some["65521"] != "Test Vendor" {
		t.Fatalf("filtered %v %v", some, err)
	}
	wantCode(t, e.client.Call(e.ctx, "get_vendor_names", map[string]any{"filter_vendors": []int{70000}}, nil), protocol.InvalidArguments, "70000")
	down.Store(true)
	wantCode(t, e.client.Call(e.ctx, "get_vendor_names", nil, nil), protocol.UnknownError, "offline")
	info := e.client.ServerInfo()
	for _, c := range []string{"set_acl_entry", "set_node_binding", "get_vendor_names"} {
		found := false
		for _, have := range info.Capabilities {
			found = found || have == c
		}
		if !found {
			t.Errorf("capabilities lack %s", c)
		}
	}
}
