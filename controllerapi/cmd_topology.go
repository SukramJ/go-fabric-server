// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	generaldiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/controller/topology"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// get_network_topology answers go-fabric's controller/topology graph — a
// port of matterjs-server's NetworkTopologyService
// (packages/ws-controller/src/controller/NetworkTopologyService.ts at
// 0bd06f22: debounced rebuilds, a periodic one, a refresh of the
// diagnostics at a bounded concurrency, a hash-gated change event) — in
// the wire shape of matterjs-server's NetworkTopology. The imported test
// nodes join the graph as a topology.Source (testnodes.go). What
// matterjs-server adds from a Thread border router registry and from
// route tables is left out in go-fabric too (its controller/topology
// doc, "Left out").

// The test nodes' network attributes, by name from protocol/schema: the
// dump's JSON is decoded into the typed cluster states of a
// topology.Image (testNodeImage).
var (
	netCommissioning  = schema.ClusterByName("NetworkCommissioning")
	attrNetworks      = netCommissioning.AttributeByName("Networks")
	generalDiag       = schema.ClusterByName("GeneralDiagnostics")
	attrNetIfaces     = generalDiag.AttributeByName("NetworkInterfaces")
	threadDiag        = schema.ClusterByName("ThreadNetworkDiagnostics")
	attrRoutingRole   = threadDiag.AttributeByName("RoutingRole")
	attrThreadName    = threadDiag.AttributeByName("NetworkName")
	attrExtPanID      = threadDiag.AttributeByName("ExtendedPanId")
	attrNeighborTable = threadDiag.AttributeByName("NeighborTable")
	attrRloc16        = threadDiag.AttributeByName("Rloc16")
	wifiDiag          = schema.ClusterByName("WiFiNetworkDiagnostics")
	attrBssid         = wifiDiag.AttributeByName("Bssid")
	attrWifiChannel   = wifiDiag.AttributeByName("ChannelNumber")
	attrWifiRssi      = wifiDiag.AttributeByName("Rssi")
)

// topologyNode is NetworkTopologyNode (python client models.py:318-347),
// with our ip_addresses extension (the node's known addresses, brief S3
// §3; an old client ignores the key).
type topologyNode struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	NetworkType string   `json:"network_type"`
	NodeID      *uint64  `json:"node_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Available   *bool    `json:"available,omitempty"`
	IsBridge    *bool    `json:"is_bridge,omitempty"`
	ExtAddress  string   `json:"ext_address,omitempty"`
	Rloc16      *uint64  `json:"rloc16,omitempty"`
	ExtPanID    string   `json:"ext_pan_id,omitempty"`
	NetworkName string   `json:"network_name,omitempty"`
	SSID        string   `json:"ssid,omitempty"`
	BSSID       string   `json:"bssid,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
}

// topologyDirection is TopologyDirectionInfo (models.py:306-315).
type topologyDirection struct {
	Strength string `json:"strength"`
	LQI      *int64 `json:"lqi,omitempty"`
	RSSI     *int64 `json:"rssi,omitempty"`
}

// topologyConnection is NetworkTopologyConnection (models.py:350-366).
type topologyConnection struct {
	Source         string             `json:"source"`
	Target         string             `json:"target"`
	Network        string             `json:"network"`
	Strength       string             `json:"strength"`
	SourceToTarget *topologyDirection `json:"source_to_target,omitempty"`
	TargetToSource *topologyDirection `json:"target_to_source,omitempty"`
}

// networkTopology is NetworkTopology (models.py:369-380).
type networkTopology struct {
	CollectedAt int64                `json:"collected_at"`
	Nodes       []topologyNode       `json:"nodes"`
	Connections []topologyConnection `json:"connections"`
}

// cmdGetNetworkTopology answers the topology and opts the connection in to
// network_topology_updated (WebSocketControllerHandler.ts:2220-2230).
// refresh re-reads the diagnostics of every available Thread or Wi-Fi
// node first (Service.Refresh); a refresh that fails is logged and the
// graph as it stands is answered, as matterjs-server's refresh is best
// effort (NetworkTopologyService.ts:196-205).
func cmdGetNetworkTopology(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Refresh bool `json:"refresh"`
	}
	c.conn.topology.Store(true)
	if err := c.args(&a); err != nil {
		return nil, err
	}
	svc := s.topo()
	if svc == nil {
		return nil, protocol.Errorf(protocol.UnknownError, "get_network_topology: the server is not started")
	}
	if a.Refresh {
		t, err := svc.Refresh(ctx)
		if err == nil {
			return wireTopology(t), nil
		}
		s.log.Warn("fabricserver.topology.refresh", "err", err)
	}
	return wireTopology(svc.Topology()), nil
}

// wireTopology is go-fabric's graph in matterjs-server's wire shape:
// node_id and available on the commissioned nodes only, is_bridge only
// when set, the Thread addresses as 16 hex digits, lqi on Thread links
// only (go-fabric's Direction.LQI is 0 on Wi-Fi, which has none), the
// addresses without their zone, string-sorted, as get_node_ip_addresses
// answers them (cmd_node.go addresses).
func wireTopology(t topology.Topology) networkTopology {
	out := networkTopology{
		CollectedAt: t.CollectedAt.UnixMilli(),
		Nodes:       make([]topologyNode, 0, len(t.Nodes)),
		Connections: make([]topologyConnection, 0, len(t.Connections)),
	}
	for i := range t.Nodes {
		n := &t.Nodes[i]
		w := topologyNode{
			ID: n.ID, Kind: string(n.Kind), NetworkType: string(n.NetworkType), Role: string(n.Role),
			NetworkName: n.NetworkName, SSID: n.SSID, BSSID: n.BSSID, IPAddresses: ipStrings(n.Addresses),
		}
		if n.Kind == topology.KindMatter {
			id, avail := n.NodeID, n.Available
			w.NodeID, w.Available = &id, &avail
		}
		if n.IsBridge {
			w.IsBridge = new(true)
		}
		if n.ExtAddress != 0 {
			w.ExtAddress = hex16(n.ExtAddress)
		}
		if n.Rloc16 != nil {
			w.Rloc16 = new(uint64(*n.Rloc16))
		}
		if n.ExtPanID != 0 {
			w.ExtPanID = hex16(n.ExtPanID)
		}
		out.Nodes = append(out.Nodes, w)
	}
	for _, c := range t.Connections {
		out.Connections = append(out.Connections, topologyConnection{
			Source: c.Source, Target: c.Target, Network: string(c.Network), Strength: string(c.Strength),
			SourceToTarget: wireDirection(c.Network, c.SourceToTarget),
			TargetToSource: wireDirection(c.Network, c.TargetToSource),
		})
	}
	return out
}

func wireDirection(network topology.NetworkType, d *topology.Direction) *topologyDirection {
	if d == nil {
		return nil
	}
	w := &topologyDirection{Strength: string(d.Strength)}
	if network == topology.NetworkThread {
		w.LQI = new(int64(d.LQI))
	}
	if d.RSSI != nil {
		w.RSSI = new(int64(*d.RSSI))
	}
	return w
}

// ipStrings are addrs without their zone, each once, string-sorted; nil
// for none (ip_addresses then stays off the wire).
func ipStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if v := a.WithZone("").String(); !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// topologyChanged sends network_topology_updated to the connections that
// asked for the topology — the subscriber of Service.OnChanged, which
// tells only a graph that moved (matterjs-server's hashTopology gate,
// NetworkTopologyService.ts:279-285, :530-539), the latest one winning
// on a slow connection (the "network_topology" coalescing of
// WebSocketControllerHandler.ts:695-707).
func (s *Server) topologyChanged(t topology.Topology) {
	s.mu.Lock()
	want := false
	for c := range s.conns {
		if !s.closing && c.topology.Load() {
			want = true
			break
		}
	}
	s.mu.Unlock()
	if !want {
		return
	}
	frame := protocol.MustEncode(protocol.Event{Event: "network_topology_updated", Data: wireTopology(t)})
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		if !s.closing && c.topology.Load() {
			c.send.SendCoalescable("topology", func() []byte { return frame })
		}
	}
}

// testNodeImage is a test node's topology.Image: its dump's network
// attributes of the root endpoint, decoded by the snapshot's member ids
// into go-fabric's typed cluster states — the inputs go-fabric's Build
// derives the node, its Thread links and its Wi-Fi access point from, as
// matterjs-server derives a TopologyNodeSource's nodes from their
// attributes (NetworkTopologyService.ts:72-81, :307). An attribute the
// dump lacks or that does not decode stays at its zero value. The dump
// carries no operational address (its placeholders are no IP
// addresses), so the image has none.
func testNodeImage(nodeID uint64, available, isBridge bool, attrs map[string]json.RawMessage) topology.Image {
	img := topology.Image{NodeID: nodeID, Available: available, IsBridge: isBridge}
	at := func(cl *schema.Cluster, a *schema.Attribute) json.RawMessage { return attrs[attrKey(0, cl.ID, a.ID)] }
	if fm, ok := uintOf(attrs[attrKey(0, netCommissioning.ID, featureMapID)]); ok && fm <= math.MaxUint32 {
		img.NetworkFeatures = netcommdef.Feature(fm)
	}
	idK, connK := member(attrNetworks.Type, "networkId"), member(attrNetworks.Type, "connected")
	for _, e := range entries(at(netCommissioning, attrNetworks)) {
		var n netcommdef.NetworkInfoStruct
		n.NetworkId, _ = bytesOf(e[idK])
		_ = json.Unmarshal(e[connK], &n.Connected)
		img.NetworkCommissioning.Networks = append(img.NetworkCommissioning.Networks, n)
	}
	typeK, hwK := member(attrNetIfaces.Type, "type"), member(attrNetIfaces.Type, "hardwareAddress")
	for _, e := range entries(at(generalDiag, attrNetIfaces)) {
		var n generaldiagdef.NetworkInterface
		if v, ok := uintOf(e[typeK]); ok && v <= math.MaxUint8 {
			n.Type = generaldiagdef.InterfaceTypeEnum(v)
		}
		n.HardwareAddress, _ = bytesOf(e[hwK])
		img.GeneralDiagnostics.NetworkInterfaces = append(img.GeneralDiagnostics.NetworkInterfaces, n)
	}
	td := &img.ThreadNetworkDiagnostics
	if v, ok := uintOf(at(threadDiag, attrRoutingRole)); ok && v <= math.MaxUint8 {
		td.RoutingRole = new(threaddiagdef.RoutingRoleEnum(v))
	}
	var name string
	if json.Unmarshal(at(threadDiag, attrThreadName), &name) == nil {
		td.NetworkName = &name
	}
	if v, ok := uintOf(at(threadDiag, attrExtPanID)); ok {
		td.ExtendedPanId = &v
	}
	if v, ok := uintOf(at(threadDiag, attrRloc16)); ok && v <= 0xFFFF {
		td.Rloc16 = new(uint16(v))
	}
	td.NeighborTable = neighborTable(at(threadDiag, attrNeighborTable))
	wd := &img.WiFiNetworkDiagnostics
	if b, ok := bytesOf(at(wifiDiag, attrBssid)); ok {
		wd.Bssid = &b
	}
	if v, ok := uintOf(at(wifiDiag, attrWifiChannel)); ok && v <= 0xFFFF {
		wd.ChannelNumber = new(uint16(v))
	}
	if v, ok := int8Of(at(wifiDiag, attrWifiRssi)); ok {
		wd.Rssi = &v
	}
	return img
}

// neighborTable decodes ThreadNetworkDiagnostics NeighborTable entries:
// the members go-fabric's Build reads (matterjs-server's
// parseNeighborTable, topology-utils.ts:251-298); a null or absent RSSI
// is null.
func neighborTable(raw json.RawMessage) []threaddiagdef.NeighborTableStruct {
	t := attrNeighborTable.Type
	extK, rlocK, lqiK := member(t, "extAddress"), member(t, "rloc16"), member(t, "lqi")
	avgK, lastK := member(t, "averageRssi"), member(t, "lastRssi")
	rxK, childK := member(t, "rxOnWhenIdle"), member(t, "isChild")
	list := entries(raw)
	out := make([]threaddiagdef.NeighborTableStruct, 0, len(list))
	for _, e := range list {
		var nb threaddiagdef.NeighborTableStruct
		nb.ExtAddress, _ = uintOf(e[extK])
		if v, ok := uintOf(e[rlocK]); ok && v <= 0xFFFF {
			nb.Rloc16 = uint16(v)
		}
		if v, ok := uintOf(e[lqiK]); ok {
			lqi := uint8(math.MaxUint8) // clamped, as before
			if v <= math.MaxUint8 {
				lqi = uint8(v)
			}
			nb.Lqi = lqi
		}
		nb.AverageRssi.Value, nb.AverageRssi.Null = nullableInt8(e[avgK])
		nb.LastRssi.Value, nb.LastRssi.Null = nullableInt8(e[lastK])
		_ = json.Unmarshal(e[rxK], &nb.RxOnWhenIdle)
		_ = json.Unmarshal(e[childK], &nb.IsChild)
		out = append(out, nb)
	}
	return out
}

// member is the decimal id of a struct member by its property name.
func member(t schema.Type, property string) string {
	if t.Entry != nil {
		t = *t.Entry
	}
	if t.Struct != nil {
		for _, f := range t.Struct.Fields {
			if f.Property == property {
				return strconv.FormatUint(uint64(f.ID), 10)
			}
		}
	}
	return ""
}

func entries(raw json.RawMessage) []map[string]json.RawMessage {
	var list []map[string]json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

// bytesOf is an octet string as the dump carries it, base64.
func bytesOf(raw json.RawMessage) ([]byte, bool) {
	var b64 string
	if json.Unmarshal(raw, &b64) != nil || b64 == "" {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	return b, err == nil
}

func intOf(raw json.RawMessage) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return v, err == nil
}

func int8Of(raw json.RawMessage) (int8, bool) {
	v, ok := intOf(raw)
	if !ok || v < -128 || v > 127 {
		return 0, false
	}
	return int8(v), true
}

// nullableInt8 is a nullable int8 member: null when absent, null or out
// of range.
func nullableInt8(raw json.RawMessage) (v int8, null bool) {
	v, ok := int8Of(raw)
	return v, !ok
}

func hex16(v uint64) string { return fmt.Sprintf("%016X", v) }
