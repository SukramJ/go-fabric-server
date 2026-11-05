// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strconv"
	"sync"
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

// pingArgs are ping_node's arguments: matterjs-server's node_id and
// attempts (map 1795-1806), and the details extension.
type pingArgs struct {
	nodeArg
	Attempts *int `json:"attempts"`
	// Details answers one pingResult per address instead of
	// matterjs-server's {ip: reachable} (an extension, docs/protocol.md).
	Details bool `json:"details"`
}

// attempts is the attempts argument, 1 to 10, 1 when absent.
func (a pingArgs) attempts() int {
	if a.Attempts != nil && *a.Attempts > 0 {
		return min(*a.Attempts, 10)
	}
	return 1
}

// pingResult is one address's outcome in ping_node's details.
type pingResult struct {
	IP        string `json:"ip"`
	Port      uint16 `json:"port"`
	Reachable bool   `json:"reachable"`
	// RoundTripMS is the round trip of the answered attempt, in
	// milliseconds; absent when no attempt was answered.
	RoundTripMS *float64 `json:"round_trip_ms,omitempty"`
	// Error is the last attempt's error; absent when reachable.
	Error string `json:"error,omitempty"`
}

// pingAttemptTimeout bounds one attempt; go-fabric's ping caps its own
// transmissions, so this only ends an attempt that would outlive them.
const pingAttemptTimeout = 10 * time.Second

// pingFunc probes one address — client.Node.PingAddress.
type pingFunc func(context.Context, netip.AddrPort) (time.Duration, error)

// cmdPingNode probes each address of the node's record with go-fabric's
// client.Node.PingAddress, all addresses in parallel, up to attempts
// times each, as matterjs-server pings each address in parallel
// (ControllerCommandHandler.ts:2267-2306). matterjs-server sends ICMP
// echoes; a non-root daemon in a distroless image has no raw socket, so an
// attempt is an empty Matter read over the session the server holds, sent
// to that address (B1). The answer is matterjs-server's NodePingResult,
// {ip: reachable} (ws-client models/model.ts:1889), an IP reachable when
// any of its ports answered; with details, one pingResult per address.
func cmdPingNode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a pingArgs
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	res := pingAddresses(ctx, n.Record().Addresses, a.attempts(), pingAttemptTimeout, n.PingAddress)
	if a.Details {
		return res, nil
	}
	return pingMap(res), nil
}

// pingAddresses probes each distinct address of addrs in parallel and
// answers in the order of their IP strings (as get_node_ip_addresses),
// then ports.
func pingAddresses(ctx context.Context, addrs []client.NetworkAddress, attempts int, timeout time.Duration, ping pingFunc) []pingResult {
	seen := map[netip.AddrPort]bool{}
	targets := []netip.AddrPort{}
	for _, a := range addrs {
		ap := netip.AddrPortFrom(a.IP, a.Port)
		if !seen[ap] {
			seen[ap] = true
			targets = append(targets, ap)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		si, sj := targets[i].Addr().String(), targets[j].Addr().String()
		if si != sj {
			return si < sj
		}
		return targets[i].Port() < targets[j].Port()
	})
	out := make([]pingResult, len(targets))
	var wg sync.WaitGroup
	for i, ap := range targets {
		wg.Go(func() { out[i] = pingOne(ctx, ap, attempts, timeout, ping) })
	}
	wg.Wait()
	return out
}

// pingOne probes ap until an attempt answers, at most attempts times. A
// node without a session, a closed node or the end of ctx ends the
// attempts early: a further attempt cannot answer.
func pingOne(ctx context.Context, ap netip.AddrPort, attempts int, timeout time.Duration, ping pingFunc) pingResult {
	r := pingResult{IP: ap.Addr().String(), Port: ap.Port()}
	var err error
	for range attempts {
		actx, cancel := context.WithTimeout(ctx, timeout)
		var rtt time.Duration
		rtt, err = ping(actx, ap)
		cancel()
		if err == nil {
			ms := float64(rtt.Microseconds()) / 1000
			r.Reachable, r.RoundTripMS = true, &ms
			return r
		}
		if ctx.Err() != nil || errors.Is(err, client.ErrNoSession) || errors.Is(err, client.ErrClosed) || errors.Is(err, client.ErrNotCommissioned) {
			break
		}
	}
	r.Error = err.Error()
	return r
}

// pingMap folds results into matterjs-server's {ip: reachable}.
func pingMap(res []pingResult) map[string]bool {
	out := map[string]bool{}
	for _, r := range res {
		out[r.IP] = out[r.IP] || r.Reachable
	}
	return out
}

// --- fabrics ---

var (
	opCreds          = schema.ClusterByName("OperationalCredentials")
	attrFabrics      = opCreds.AttributeByName("Fabrics")
	cmdRemoveFabricC = opCreds.CommandByName("RemoveFabric")
)

// cmdGetMatterFabrics reads the node's Fabrics unfiltered
// (ControllerCommandHandler.ts:2388-2416) and answers
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
// (WebSocketControllerHandler.ts:2316-2330, ControllerCommandHandler.ts:2370-2385):
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
