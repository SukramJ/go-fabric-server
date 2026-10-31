// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// Test nodes are node dumps imported with import_test_node, answered from
// the server's memory without a network peer — matterjs-server's
// TestNodeCommandHandler (packages/ws-controller/src/controller/TestNodeCommandHandler.ts
// at 0bd06f22). go-fabric's registry has no import route, and a test node
// is not a node of our fabric, so they live here, in memory only, lost on
// a restart as there.

// The Temporary Local node id range test nodes take
// (packages/ws-client/src/models/model.ts:1896-1907).
const (
	testNodeStart uint64 = 0xFFFF_FFFE_0000_0000
	testNodeEnd   uint64 = testNodeStart + (1 << 32) - 1
)

func isTestNodeID(id uint64) bool { return id >= testNodeStart && id <= testNodeEnd }

// testNode is a stored dump node: MatterNodeData with the dump's values
// kept as they are.
type testNode struct {
	NodeID                 uint64                     `json:"node_id"`
	DateCommissioned       json.RawMessage            `json:"date_commissioned"`
	LastInterview          json.RawMessage            `json:"last_interview"`
	InterviewVersion       json.RawMessage            `json:"interview_version"`
	Available              json.RawMessage            `json:"available"`
	IsBridge               json.RawMessage            `json:"is_bridge"`
	Attributes             map[string]json.RawMessage `json:"attributes"`
	AttributeSubscriptions []any                      `json:"attribute_subscriptions"`
	EndpointParents        json.RawMessage            `json:"endpoint_parents,omitempty"`
}

// testNodeAddresses are the addresses matterjs-server answers for a test
// node (TestNodeCommandHandler.ts:228-241).
var testNodeAddresses = []string{"0.0.0.0", "0000:1111:2222:3333:4444"}

// cmdImportTestNode imports the nodes of a diagnostics dump: data.node
// (one node), data.server.nodes or data.nodes (TestNodeCommandHandler.ts:117-185),
// each under the next free test node id, node_added for each.
func cmdImportTestNode(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Dump *string `json:"dump"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Dump == nil {
		return nil, invalidArgs(c.req.Command, "dump is required, a JSON string")
	}
	var dump struct {
		Data struct {
			Node   json.RawMessage `json:"node"`
			Server struct {
				Nodes json.RawMessage `json:"nodes"`
			} `json:"server"`
			Nodes json.RawMessage `json:"nodes"`
		} `json:"data"`
	}
	dec := json.NewDecoder(strings.NewReader(*a.Dump))
	dec.UseNumber()
	if err := dec.Decode(&dump); err != nil {
		return nil, invalidArgs(c.req.Command, "Invalid dump format: %v", err)
	}
	var raws []json.RawMessage
	switch {
	case present(dump.Data.Node):
		raws = []json.RawMessage{dump.Data.Node}
	case present(dump.Data.Server.Nodes):
		raws = nodesOf(dump.Data.Server.Nodes)
	case present(dump.Data.Nodes):
		raws = nodesOf(dump.Data.Nodes)
	}
	if raws == nil {
		return nil, protocol.Errorf(protocol.InvalidArguments, "Invalid dump format: cannot find node data")
	}
	nodes := make([]*testNode, 0, len(raws))
	for i, r := range raws {
		var n testNode
		d := json.NewDecoder(bytes.NewReader(r))
		d.UseNumber()
		if err := d.Decode(&n); err != nil {
			return nil, invalidArgs(c.req.Command, "Invalid dump format: node %d: %v", i, err)
		}
		if n.Attributes == nil {
			n.Attributes = map[string]json.RawMessage{}
		}
		n.AttributeSubscriptions = []any{}
		if string(n.EndpointParents) == "null" {
			n.EndpointParents = nil
		}
		nodes = append(nodes, &n)
	}
	s.mu.Lock()
	defer s.topologyUpdated() // after the unlock below
	defer s.mu.Unlock()
	next := testNodeStart
	for id := range s.testNodes {
		if id >= next {
			next = id + 1
		}
	}
	for _, n := range nodes {
		n.NodeID = next
		next++
		s.testNodes[n.NodeID] = n
		frame := protocol.MustEncode(protocol.Event{Event: "node_added", Data: n})
		s.emitLocked(func(c *wsConn) { c.send.SendOrdered(frame, false) })
		s.log.Info("fabricserver.test_node.imported", "node_id", n.NodeID, "attributes", len(n.Attributes))
	}
	return nil, nil
}

func present(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && string(t) != "null"
}

// nodesOf reads a dump's nodes: an object of nodes (Object.values) or an
// array.
func nodesOf(raw json.RawMessage) []json.RawMessage {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]json.RawMessage, 0, len(keys))
	for _, k := range keys {
		out = append(out, obj[k])
	}
	return out
}

// testNodeListLocked is the test nodes in node id order; the caller holds
// s.mu.
func (s *Server) testNodeListLocked() []*testNode {
	out := make([]*testNode, 0, len(s.testNodes))
	for _, n := range s.testNodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// testCommands are the node commands a test node answers itself
// (WebSocketControllerHandler.ts:331-333 #handlerFor and its callers);
// every other node command takes the registry's route, where the node
// does not exist.
var testCommands = map[string]func(s *Server, c *call, n *testNode) (any, *protocol.Error){
	"get_node": func(_ *Server, _ *call, n *testNode) (any, *protocol.Error) { return n, nil },
	"read_attribute": func(_ *Server, c *call, n *testNode) (any, *protocol.Error) {
		var a struct {
			AttributePath json.RawMessage `json:"attribute_path"`
		}
		if err := c.args(&a); err != nil {
			return nil, err
		}
		paths, ok := stringOrList(a.AttributePath)
		if !ok || len(paths) == 0 {
			return nil, invalidArgs(c.req.Command, "attribute_path must be a path or a list of paths")
		}
		out := map[string]json.RawMessage{}
		for _, p := range paths {
			if !strings.Contains(p, "*") {
				out[p] = n.Attributes[p]
				continue
			}
			wp, err := parsePath(p, 3, false)
			if err != nil {
				return nil, invalidArgs(c.req.Command, "%v", err)
			}
			for k, v := range n.Attributes {
				if pathMatches(wp, k) {
					out[k] = v
				}
			}
		}
		return out, nil
	},
	"read":      refuseTestRead,
	"subscribe": refuseTestRead,
	"write_attribute": func(_ *Server, c *call, _ *testNode) (any, *protocol.Error) {
		var a struct {
			AttributePath *string `json:"attribute_path"`
		}
		if err := c.args(&a); err != nil {
			return nil, err
		}
		if a.AttributePath == nil {
			return nil, invalidArgs(c.req.Command, "attribute_path is required")
		}
		wp, err := parsePath(*a.AttributePath, 3, true)
		if err != nil || wp.Cluster == nil || wp.Element == nil {
			return nil, invalidArgs(c.req.Command, `write_attribute needs attribute_path as endpoint/cluster/attribute, each a number or "*"`)
		}
		if wp.Endpoint == nil {
			return nil, protocol.Errorf(protocol.InvalidArguments, "write_attribute to a test node needs a specific endpoint")
		}
		// Not written: matterjs-server answers success
		// (TestNodeCommandHandler.ts:190-202).
		var r writeResult
		r.Path.EndpointID, r.Path.ClusterID, r.Path.AttributeID = *wp.Endpoint, *wp.Cluster, *wp.Element
		return []writeResult{r}, nil
	},
	"device_command": func(*Server, *call, *testNode) (any, *protocol.Error) { return nil, nil },
	"batched_device_commands": func(_ *Server, c *call, _ *testNode) (any, *protocol.Error) {
		var a struct {
			Commands []json.RawMessage `json:"commands"`
		}
		if err := c.args(&a); err != nil {
			return nil, err
		}
		if len(a.Commands) == 0 {
			return nil, invalidArgs(c.req.Command, "requires a non-empty commands list")
		}
		out := make([]batchedResult, len(a.Commands))
		return out, nil
	},
	"get_node_ip_addresses": func(*Server, *call, *testNode) (any, *protocol.Error) { return testNodeAddresses, nil },
	"ping_node": func(*Server, *call, *testNode) (any, *protocol.Error) {
		out := map[string]bool{}
		for _, a := range testNodeAddresses {
			out[a] = true
		}
		return out, nil
	},
	"remove_node": func(s *Server, _ *call, n *testNode) (any, *protocol.Error) {
		s.mu.Lock()
		defer s.topologyUpdated() // after the unlock below
		defer s.mu.Unlock()
		delete(s.testNodes, n.NodeID)
		frame := protocol.MustEncode(protocol.Event{Event: "node_removed", Data: n.NodeID})
		s.emitLocked(func(c *wsConn) { c.send.SendOrdered(frame, false) })
		return nil, nil
	},
	"interview_node": func(s *Server, _ *call, n *testNode) (any, *protocol.Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		// matterjs-server's form: ISO time, "Z" replaced by "000"
		// (WebSocketControllerHandler.ts:2093).
		n.LastInterview, _ = json.Marshal(strings.Replace(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "Z", "000", 1))
		frame := protocol.MustEncode(protocol.Event{Event: "node_updated", Data: n})
		s.emitLocked(func(c *wsConn) {
			c.send.SendCoalescable("node:"+strconv.FormatUint(n.NodeID, 10), func() []byte { return frame })
		})
		return nil, nil
	},
}

func refuseTestRead(*Server, *call, *testNode) (any, *protocol.Error) {
	return nil, protocol.Errorf(protocol.InvalidArguments, "read and subscribe are not supported for test nodes")
}

// pathMatches reports whether the node data key k is on the path p.
func pathMatches(p wirePath, k string) bool {
	parts := strings.Split(k, "/")
	if len(parts) != 3 {
		return false
	}
	match := func(want *uint32, s string) bool {
		if want == nil {
			return true
		}
		v, err := strconv.ParseUint(s, 10, 32)
		return err == nil && v == uint64(*want)
	}
	if p.Endpoint != nil {
		v, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil || v != uint64(*p.Endpoint) {
			return false
		}
	}
	return match(p.Cluster, parts[1]) && match(p.Element, parts[2])
}

// testNodeFor is the test node a node command addresses, when it addresses
// one: a node_id in the test range the server holds. A test id it does
// not hold is NodeNotExists.
func (s *Server) testNodeFor(c *call) (*testNode, bool, *protocol.Error) {
	if _, routed := testCommands[c.req.Command]; !routed {
		return nil, false, nil
	}
	var a nodeArg
	if perr := protocol.DecodeArgs(c.req.Command, c.req.Args, &a); perr != nil || a.NodeID == nil || !isTestNodeID(uint64(*a.NodeID)) {
		return nil, false, nil //nolint:nilerr // the command's own handler answers bad arguments
	}
	id := uint64(*a.NodeID)
	s.mu.Lock()
	n := s.testNodes[id]
	s.mu.Unlock()
	if n == nil {
		return nil, true, protocol.Errorf(protocol.NodeNotExists, "Node %d does not exist", id)
	}
	return n, true, nil
}
