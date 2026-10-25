// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// nodeArg is the node_id argument every node-targeted command takes.
type nodeArg struct {
	NodeID *protocol.Uint64 `json:"node_id"`
}

// node resolves the node_id of a command: a missing one is
// InvalidArguments, one the registry does not hold NodeNotExists.
func (s *Server) node(command string, id *protocol.Uint64) (*client.Node, uint64, *protocol.Error) {
	if id == nil {
		return nil, 0, invalidArgs(command, "node_id is required")
	}
	nodeID := uint64(*id)
	b := s.b()
	n := b.Registry().GetByAddress(b.Fabric().AddressOf(nodeID))
	if n == nil || !n.IsCommissioned() {
		return nil, nodeID, protocol.Errorf(protocol.NodeNotExists, "Node %d does not exist or is not yet interviewed", nodeID)
	}
	return n, nodeID, nil
}

// nodeData is the node data of a node, refreshed from its image first.
func (s *Server) nodeDataOf(n *client.Node) (nodeData, bool) {
	s.refresh(n.ID())
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.nodes[n.ID()]
	if st == nil {
		return nodeData{}, false
	}
	return st.data(), true
}

func cmdGetNodes(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		OnlyAvailable bool `json:"only_available"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeListLocked(a.OnlyAvailable), nil
}

func cmdGetNode(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, id, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	d, ok := s.nodeDataOf(n)
	if !ok {
		return nil, protocol.Errorf(protocol.NodeNotExists, "Node %d does not exist or is not yet interviewed", id)
	}
	return d, nil
}

// cmdRemoveNode decommissions the node (client.Registry.Decommission: the
// node removes our fabric, and the registry deletes the node); node_removed
// follows from the registry's change.
func cmdRemoveNode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if err := s.b().Registry().Decommission(ctx, n); err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	s.refresh(n.ID())
	return nil, nil
}

// cmdInterviewNode reads the whole node again, without data version
// filters (ControllerCommandHandler.ts:1193-1208), and sends node_updated.
func cmdInterviewNode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, id, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	req := client.ReadRequest{IncludeKnownVersions: true}
	req.Attributes = []client.AttributePath{{}}
	if _, err := n.Read(ctx, req); err != nil {
		e := nodeError(c.req.Command, err)
		if e.ErrorCode == protocol.UnknownError {
			e.ErrorCode = protocol.NodeInterviewFailed
		}
		return nil, e
	}
	s.refresh(n.ID())
	s.mu.Lock()
	if st := s.nodes[n.ID()]; st != nil {
		st.lastInterview = time.Now()
	}
	s.mu.Unlock()
	s.broadcastNodeUpdated(id)
	return nil, nil
}

// addresses are a node's known IP addresses, from its record — the
// addresses of its last discovery; scoped keeps an IPv6 zone.
func addresses(n *client.Node, scoped bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, a := range n.Record().Addresses {
		ip := a.IP
		if !scoped {
			ip = ip.WithZone("")
		}
		s := ip.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// cmdGetNodeIPAddresses lists the node's addresses from its record. The
// fresh DNS-SD lookup matterjs-server makes without prefer_cache needs a
// discovery route the facade does not offer (Questions Q3); prefer_cache
// is accepted and the record answers either way.
func cmdGetNodeIPAddresses(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		PreferCache *bool `json:"prefer_cache"`
		Scoped      bool  `json:"scoped"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	return addresses(n, a.Scoped), nil
}

// cmdPingNode answers {ip: reachable} for each address of the node.
// matterjs-server sends ICMP echoes (ControllerCommandHandler.ts:2261-2300);
// a non-root daemon in a distroless image has no raw socket, so each
// attempt is a Matter read of the node's DataModelRevision instead, and
// every address carries its outcome (report S1, by_design candidate B3).
func cmdPingNode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Attempts *int `json:"attempts"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	attempts := 1
	if a.Attempts != nil && *a.Attempts > 0 {
		attempts = min(*a.Attempts, 10)
	}
	ok := false
	for range attempts {
		req := client.ReadRequest{IncludeKnownVersions: true}
		req.Attributes = []client.AttributePath{client.AttributeAt(0, basicInformation.ID, attrDataModelRev.ID)}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := n.Read(rctx, req)
		cancel()
		if err == nil {
			ok = true
			break
		}
	}
	out := map[string]bool{}
	for _, ip := range addresses(n, true) {
		out[ip] = ok
	}
	return out, nil
}

// --- fabrics ---

var (
	opCreds          = schema.ClusterByName("OperationalCredentials")
	attrFabrics      = opCreds.AttributeByName("Fabrics")
	cmdRemoveFabricC = opCreds.CommandByName("RemoveFabric")
)

// cmdGetMatterFabrics reads the node's Fabrics unfiltered
// (ControllerCommandHandler.ts:2382-2410) and answers
// [{fabric_id, vendor_id, fabric_index, fabric_label}].
func cmdGetMatterFabrics(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	req := client.ReadRequest{IncludeKnownVersions: true}
	req.Attributes = []client.AttributePath{client.AttributeAt(0, opCreds.ID, attrFabrics.ID)}
	req.NoFabricFilter = true
	rep, err := n.Read(ctx, req)
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	out := []map[string]any{}
	for i := range rep.Attributes {
		d := &rep.Attributes[i]
		if d.Path.Cluster != opCreds.ID || d.Path.Attribute != attrFabrics.ID {
			continue
		}
		list, _ := decodeAny(renderJSON(client.AttributeField(opCreds.ID, attrFabrics.ID), &attrFabrics.Type, d.Value, client.NameBased)).([]any)
		for _, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, map[string]any{
				"fabric_id":    m["fabricId"],
				"vendor_id":    m["vendorId"],
				"fabric_index": m["fabricIndex"],
				"fabric_label": m["label"],
			})
		}
	}
	return out, nil
}

// cmdRemoveMatterFabric invokes OperationalCredentials RemoveFabric.
func cmdRemoveMatterFabric(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		FabricIndex *uint8 `json:"fabric_index"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if a.FabricIndex == nil {
		return nil, invalidArgs(c.req.Command, "fabric_index is required")
	}
	payload, err := encodeCommand(opCreds.ID, cmdRemoveFabricC.ID, []byte(`{"fabricIndex":`+strconv.Itoa(int(*a.FabricIndex))+`}`))
	if err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	if _, err := n.Invoke(ctx, client.CommandAt(0, opCreds.ID, cmdRemoveFabricC.ID), payload, client.InvokeOptions{}); err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	return struct{}{}, nil
}

// windowData is CommissioningParameters (python client models.py:280-290):
// every key present, commissioning_timeout in seconds.
type windowData struct {
	SetupPinCode         uint32 `json:"setup_pin_code"`
	SetupManualCode      string `json:"setup_manual_code"`
	SetupQRCode          string `json:"setup_qr_code"`
	Discriminator        uint16 `json:"discriminator"`
	VendorID             uint16 `json:"vendor_id"`
	ProductID            uint16 `json:"product_id"`
	CommissioningTimeout int64  `json:"commissioning_timeout"`
}

// cmdOpenCommissioningWindow opens an enhanced commissioning window on the
// node (client.Node.OpenCommissioningWindow, go-fabric v0.12.1) for
// timeout seconds — matterjs-server's #handleOpenCommissioningWindow
// (WebSocketControllerHandler.ts:2316-2330, ControllerCommandHandler.ts:2364-2379):
// iteration, option and discriminator are accepted and not used (the
// window takes a random discriminator and passcode), a timeout that is not
// a positive number is InvalidArguments, and the answer carries the
// pairing codes. The answer holds the passcode and is never logged.
func cmdOpenCommissioningWindow(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Timeout       *float64 `json:"timeout"`
		Iteration     *float64 `json:"iteration"`
		Option        *float64 `json:"option"`
		Discriminator *float64 `json:"discriminator"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	var timeout time.Duration
	if a.Timeout != nil {
		if *a.Timeout <= 0 || *a.Timeout > 0xFFFF {
			return nil, invalidArgs(c.req.Command, "Commissioning window timeout must be a positive number of seconds (at most 65535)")
		}
		timeout = time.Duration(*a.Timeout * float64(time.Second))
	}
	w, err := n.OpenCommissioningWindow(ctx, timeout, client.WindowOptions{})
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	return windowData{
		SetupPinCode: w.Passcode, SetupManualCode: w.ManualCode, SetupQRCode: w.QRCode,
		Discriminator: w.Discriminator, VendorID: w.VendorID, ProductID: w.ProductID,
		CommissioningTimeout: int64(w.Timeout / time.Second),
	}, nil
}
