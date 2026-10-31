// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// listWriteResult is set_acl_entry's and set_node_binding's result entry —
// the snake_case AttributeWriteResult (ws-client model.ts:1498-1501).
type listWriteResult struct {
	Path struct {
		EndpointID  uint16 `json:"endpoint_id"`
		ClusterID   uint32 `json:"cluster_id"`
		AttributeID uint32 `json:"attribute_id"`
	} `json:"path"`
	Status int `json:"status"`
}

// writeList writes a whole list attribute, named by cluster and attribute
// name, in one write, and answers matterjs-server's one-entry result with
// the status the node answered
// (ControllerCommandHandler.ts:2661-2673, :2696-2702; #writeAttribute
// :1515-1545 returns the first status).
func (s *Server) writeList(ctx context.Context, command string, n *client.Node, ep uint16, cluster, attribute string, entries []map[string]any) (any, *protocol.Error) {
	cl := schema.ClusterByName(cluster)
	a := cl.AttributeByName(attribute)
	if a == nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: the snapshot has no %s.%s", command, cluster, attribute)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, invalidArgs(command, "%v", err)
	}
	value, timed, err := encodeAttribute(cl.ID, a.ID, raw)
	if err != nil {
		return nil, invalidArgs(command, "%v", err)
	}
	path := client.AttributePath{Endpoint: ep, Cluster: cl.ID, Attribute: a.ID, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	statuses, err := n.Write(ctx, []client.WriteRequest{{Path: path, Value: value}}, client.WriteOptions{Timed: timed})
	if err != nil && len(statuses) == 0 {
		return nil, nodeError(command, err)
	}
	var r listWriteResult
	r.Path.EndpointID, r.Path.ClusterID, r.Path.AttributeID = ep, cl.ID, a.ID
	if len(statuses) > 0 {
		r.Status = int(statuses[0].Status)
	}
	return []listWriteResult{r}, nil
}

// fabricIndexOf is our fabric's index on the node, from its
// OperationalCredentials CurrentFabricIndex in the node data, as
// matterjs-server reads it from the node's state
// (ControllerCommandHandler.ts:2615-2618); false when the data has none,
// and the entries then go without one — the node fills a fabric-scoped
// entry's index with the accessing fabric's.
func (s *Server) fabricIndexOf(n *client.Node) (uint64, bool) {
	cl := schema.ClusterByName("OperationalCredentials")
	a := cl.AttributeByName("CurrentFabricIndex")
	if a == nil {
		return 0, false
	}
	s.refresh(n.ID())
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.nodes[n.ID()]
	if st == nil {
		return 0, false
	}
	idx, ok := uintOf(st.attrs[attrKey(0, cl.ID, a.ID)])
	return idx, ok && idx <= 0xFF
}

// cmdSetACLEntry replaces our fabric's ACL entries on the node
// (ControllerCommandHandler.ts:2624-2674): a subject naming the node itself
// is left out, and an entry left without subjects dropped (a node always
// has access to itself), and the call still succeeds.
func cmdSetACLEntry(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Entry *[]struct {
			Privilege *json.Number       `json:"privilege"`
			AuthMode  *json.Number       `json:"auth_mode"`
			Subjects  *[]protocol.Uint64 `json:"subjects"`
			Targets   *[]struct {
				Cluster    *json.Number `json:"cluster"`
				Endpoint   *json.Number `json:"endpoint"`
				DeviceType *json.Number `json:"device_type"`
			} `json:"targets"`
		} `json:"entry"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if a.Entry == nil {
		return nil, invalidArgs(c.req.Command, "entry is required")
	}
	idx, hasIdx := s.fabricIndexOf(n)
	entries := make([]map[string]any, 0, len(*a.Entry))
	for i, e := range *a.Entry {
		if e.Privilege == nil || e.AuthMode == nil {
			return nil, invalidArgs(c.req.Command, "entry[%d] needs privilege and auth_mode", i)
		}
		m := map[string]any{"privilege": *e.Privilege, "authMode": *e.AuthMode, "subjects": nil, "targets": nil}
		if e.Subjects != nil {
			subjects := []uint64{}
			for _, sub := range *e.Subjects {
				if uint64(sub) != nodeID {
					subjects = append(subjects, uint64(sub))
				}
			}
			if len(subjects) == 0 && len(*e.Subjects) > 0 {
				s.log.Warn("fabricserver.acl.self_subject", "node_id", nodeID)
				continue
			}
			if len(subjects) < len(*e.Subjects) {
				s.log.Warn("fabricserver.acl.self_subject", "node_id", nodeID)
			}
			m["subjects"] = subjects
		}
		if e.Targets != nil {
			targets := make([]map[string]any, 0, len(*e.Targets))
			for _, t := range *e.Targets {
				targets = append(targets, map[string]any{"cluster": t.Cluster, "endpoint": t.Endpoint, "deviceType": t.DeviceType})
			}
			m["targets"] = targets
		}
		if hasIdx {
			m["fabricIndex"] = idx
		}
		entries = append(entries, m)
	}
	return s.writeList(ctx, c.req.Command, n, 0, "AccessControl", "acl", entries)
}

// cmdSetNodeBinding replaces our fabric's Binding entries on an endpoint
// of the node (ControllerCommandHandler.ts:2680-2703); a null member is
// left out.
func cmdSetNodeBinding(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Endpoint *uint16 `json:"endpoint"`
		Bindings *[]struct {
			Node     *protocol.Uint64 `json:"node"`
			Group    *json.Number     `json:"group"`
			Endpoint *json.Number     `json:"endpoint"`
			Cluster  *json.Number     `json:"cluster"`
		} `json:"bindings"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if a.Endpoint == nil {
		return nil, invalidArgs(c.req.Command, "endpoint is required")
	}
	if a.Bindings == nil {
		return nil, invalidArgs(c.req.Command, "bindings is required")
	}
	idx, hasIdx := s.fabricIndexOf(n)
	entries := make([]map[string]any, 0, len(*a.Bindings))
	for _, b := range *a.Bindings {
		m := map[string]any{}
		if b.Node != nil {
			m["node"] = uint64(*b.Node)
		}
		if b.Group != nil {
			m["group"] = *b.Group
		}
		if b.Endpoint != nil {
			m["endpoint"] = *b.Endpoint
		}
		if b.Cluster != nil {
			m["cluster"] = *b.Cluster
		}
		if hasIdx {
			m["fabricIndex"] = idx
		}
		entries = append(entries, m)
	}
	return s.writeList(ctx, c.req.Command, n, *a.Endpoint, "Binding", "binding", entries)
}

// VendorSource lists the vendor names by vendor id — the DCL's vendor
// records ([dcl.Client.Vendors]) unless Options.Vendors sets another.
type VendorSource func(ctx context.Context) (map[uint16]string, error)

// dclVendors is the default VendorSource — matterjs-server's vendor info
// service (MatterController.ts:677-679, WebSocketControllerHandler.ts:1695-1728):
// the vendors of go-fabric's embedded ledger snapshot
// (controller/dcl/snapshot.Vendors, v0.12.1, itself over matter.js's
// static vendors), with the production ledger's live records over them,
// fetched once and kept for a day. Without the ledger the snapshot
// answers alone.
func dclVendors() VendorSource {
	var (
		mu      sync.Mutex
		cache   map[uint16]string
		fetched time.Time
	)
	ledger := dcl.NewClient("")
	base := map[uint16]string{}
	if snap, err := snapshot.Embedded(); err == nil && snap != nil {
		for _, v := range snap.Vendors() {
			base[v.ID] = v.Name
		}
	}
	return func(ctx context.Context) (map[uint16]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if cache != nil && time.Since(fetched) < 24*time.Hour {
			return cache, nil
		}
		vendors, err := ledger.Vendors(ctx)
		if err != nil {
			if cache != nil {
				return cache, nil
			}
			if len(base) > 0 {
				return base, nil
			}
			return nil, err
		}
		cache = make(map[uint16]string, len(base)+len(vendors))
		for id, name := range base {
			cache[id] = name
		}
		for _, v := range vendors {
			cache[v.VendorID] = v.VendorName
		}
		fetched = time.Now()
		return cache, nil
	}
}

// cmdGetVendorNames answers the vendor names by decimal vendor id, all of
// them or those filter_vendors names (WebSocketControllerHandler.ts:1695-1728).
// The names are the embedded snapshot's with the ledger's over them
// (dclVendors), as matterjs-server merges its static table under the
// DCL's.
func cmdGetVendorNames(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Filter []json.Number `json:"filter_vendors"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	names, err := s.vendors(ctx)
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: the DCL vendor list: %v", c.req.Command, err)
	}
	out := map[string]string{}
	if len(a.Filter) == 0 {
		for id, name := range names {
			out[strconv.FormatUint(uint64(id), 10)] = name
		}
		return out, nil
	}
	ids := make([]uint64, 0, len(a.Filter))
	for _, f := range a.Filter {
		id, err := strconv.ParseUint(f.String(), 10, 16)
		if err != nil {
			return nil, invalidArgs(c.req.Command, "filter_vendors: %s is not a vendor id", f)
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if name, ok := names[uint16(id)]; ok && name != "" { //nolint:gosec // parsed with bitSize 16
			out[strconv.FormatUint(id, 10)] = name
		}
	}
	return out, nil
}
