// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// get_network_topology builds matterjs-server's NetworkTopology from the
// node data (packages/ws-controller/src/controller/NetworkTopologyService.ts:304-430
// and packages/ws-client/src/topology/topology-utils.ts at a7922e85): a
// node per commissioned (and test) node, Thread links from the neighbor
// tables, unknown Thread neighbours, one pseudo-node per Wi-Fi access
// point. What matterjs-server adds from a Thread border router registry
// and from route tables needs go-fabric's milestone 6 (ADR 0027) and is
// left out (report S3, Left out). The ids, names and field ids come from
// protocol/schema by name.

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

// topologySource is one node's data for the build.
type topologySource struct {
	nodeID    uint64
	available bool
	isBridge  bool
	attrs     map[string]json.RawMessage
	addresses []string
}

// cmdGetNetworkTopology answers the topology and opts the connection in to
// network_topology_updated (WebSocketControllerHandler.ts:2220-2229).
// refresh re-reads the diagnostics clusters of every available Thread or
// Wi-Fi node first.
func cmdGetNetworkTopology(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Refresh bool `json:"refresh"`
	}
	c.conn.topology.Store(true)
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Refresh {
		s.refreshDiagnostics(ctx)
	}
	return s.topology(), nil
}

// refreshDiagnostics reads the network diagnostics clusters of every
// available node (30 s at most), as matterjs-server's refresh does.
func (s *Server) refreshDiagnostics(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, n := range s.b().Registry().Commissioned() {
		if n.ConnectionState() != client.Connected {
			continue
		}
		req := client.ReadRequest{IncludeKnownVersions: true}
		for _, cl := range []uint32{netCommissioning.ID, generalDiag.ID, threadDiag.ID, wifiDiag.ID} {
			req.Attributes = append(req.Attributes, client.AttributePath{Endpoint: 0, HasEndpoint: true, Cluster: cl, HasCluster: true})
		}
		_, _ = n.Read(ctx, req)
		s.refresh(n.ID())
	}
}

// topology builds the snapshot from the cache.
func (s *Server) topology() networkTopology {
	s.mu.Lock()
	srcs := make([]topologySource, 0, len(s.nodes)+len(s.testNodes))
	for _, st := range s.nodes {
		srcs = append(srcs, topologySource{nodeID: st.nodeID, available: st.available, isBridge: st.isBridge(), attrs: st.attrs})
	}
	for _, n := range s.testNodeListLocked() {
		var avail, bridge bool
		_ = json.Unmarshal(n.Available, &avail)
		_ = json.Unmarshal(n.IsBridge, &bridge)
		srcs = append(srcs, topologySource{nodeID: n.NodeID, available: avail, isBridge: bridge, attrs: n.Attributes, addresses: testNodeAddresses})
	}
	s.mu.Unlock()
	if b := s.b(); b != nil {
		for i := range srcs {
			if isTestNodeID(srcs[i].nodeID) {
				continue
			}
			if n := b.Registry().GetByAddress(b.Fabric().AddressOf(srcs[i].nodeID)); n != nil {
				srcs[i].addresses = addresses(n, false)
			}
		}
	}
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].nodeID < srcs[j].nodeID })
	return buildTopology(srcs, time.Now())
}

func buildTopology(srcs []topologySource, now time.Time) networkTopology {
	out := networkTopology{CollectedAt: now.UnixMilli(), Nodes: []topologyNode{}, Connections: []topologyConnection{}}
	type radio struct {
		id, ssid, bssid string
		rssi            *int64
	}
	var radios []radio
	var thread []topologySource
	extToID := map[uint64]string{}
	rlocToID := map[uint64]string{}
	for _, src := range srcs {
		id := strconv.FormatUint(src.nodeID, 10)
		nodeID, available := src.nodeID, src.available
		n := topologyNode{ID: id, Kind: "matter", NodeID: &nodeID, Available: &available, IPAddresses: src.addresses}
		if src.isBridge {
			t := true
			n.IsBridge = &t
		}
		n.NetworkType = networkTypeOf(src.attrs)
		switch n.NetworkType {
		case "thread":
			thread = append(thread, src)
			n.Role = threadRole(src.attrs)
			if ext, ok := threadExtAddress(src.attrs); ok {
				n.ExtAddress = hex16(ext)
				extToID[ext] = id
			}
			if r, ok := uintOf(src.attrs[attrKey(0, threadDiag.ID, attrRloc16.ID)]); ok {
				n.Rloc16 = &r
				if r != 0 {
					rlocToID[r] = id
				}
			}
			if xp, ok := uintOf(src.attrs[attrKey(0, threadDiag.ID, attrExtPanID.ID)]); ok {
				n.ExtPanID = hex16(xp)
			}
			var name string
			if json.Unmarshal(src.attrs[attrKey(0, threadDiag.ID, attrThreadName.ID)], &name) == nil {
				n.NetworkName = name
			}
		case "wifi":
			n.Role = "station"
			n.SSID = wifiSSID(src.attrs)
			n.BSSID = bssidOf(src.attrs)
			r := radio{id: id, ssid: n.SSID, bssid: n.BSSID}
			if v, ok := intOf(src.attrs[attrKey(0, wifiDiag.ID, attrWifiRssi.ID)]); ok {
				r.rssi = &v
			}
			radios = append(radios, r)
		}
		out.Nodes = append(out.Nodes, n)
	}
	// Thread: one connection per pair, each direction from the observer's
	// neighbor table (buildThreadEdgePairs, topology-utils.ts:843-969;
	// mapThreadPair, NetworkTopologyService.ts:485-500).
	type pair struct {
		a, b   string
		ab, ba *topologyDirection
	}
	pairs := map[string]*pair{}
	var order []string
	externals := map[string]topologyNode{}
	for _, src := range thread {
		from := strconv.FormatUint(src.nodeID, 10)
		var xpHex string
		if xp, ok := uintOf(src.attrs[attrKey(0, threadDiag.ID, attrExtPanID.ID)]); ok {
			xpHex = hex16(xp)
		}
		for _, nb := range neighbors(src.attrs) {
			to, ok := extToID[nb.ext]
			if !ok && nb.rloc16 != 0 {
				to, ok = rlocToID[nb.rloc16]
			}
			if !ok {
				to = "unknown_" + hex16(nb.ext)
				if _, seen := externals[to]; !seen {
					externals[to] = topologyNode{ID: to, Kind: "thread_unknown", NetworkType: "thread", ExtAddress: hex16(nb.ext), ExtPanID: xpHex}
				}
				if nb.rxOnWhenIdle {
					e := externals[to]
					e.Role = "router"
					externals[to] = e
				}
			}
			if to == from {
				continue
			}
			a, b := from, to
			if b < a {
				a, b = b, a
			}
			key := a + "|" + b
			p := pairs[key]
			if p == nil {
				p = &pair{a: a, b: b}
				pairs[key] = p
				order = append(order, key)
			}
			// An LQI is one byte; a device's larger value is held to it.
			lqi := int64(0xFF)
			if nb.lqi <= 0xFF {
				lqi = int64(nb.lqi)
			}
			d := &topologyDirection{Strength: lqiStrength(nb.lqi), LQI: &lqi, RSSI: nb.rssi}
			if from == p.a && p.ab == nil {
				p.ab = d
			} else if from == p.b && p.ba == nil {
				p.ba = d
			}
		}
	}
	linked := map[string]bool{}
	for _, key := range order {
		p := pairs[key]
		best := "none"
		for _, d := range []*topologyDirection{p.ab, p.ba} {
			if d != nil && levelRank[d.Strength] > levelRank[best] {
				best = d.Strength
			}
		}
		if best == "none" {
			continue // a dead link (mapThreadPair)
		}
		out.Connections = append(out.Connections, topologyConnection{Source: p.a, Target: p.b, Network: "thread", Strength: best, SourceToTarget: p.ab, TargetToSource: p.ba})
		linked[p.a], linked[p.b] = true, true
	}
	extIDs := make([]string, 0, len(externals))
	for id := range externals {
		if linked[id] {
			extIDs = append(extIDs, id)
		}
	}
	sort.Strings(extIDs)
	for _, id := range extIDs {
		out.Nodes = append(out.Nodes, externals[id])
	}
	// Wi-Fi: an access point per BSSID, an edge from each station
	// (NetworkTopologyService.ts:397-430).
	aps := map[string]int{}
	for _, r := range radios {
		if r.bssid == "" {
			continue
		}
		apID := "ap_" + strings.ReplaceAll(r.bssid, ":", "")
		i, ok := aps[apID]
		if !ok {
			out.Nodes = append(out.Nodes, topologyNode{ID: apID, Kind: "wifi_ap", NetworkType: "wifi", Role: "ap", NetworkName: r.bssid, BSSID: r.bssid})
			i = len(out.Nodes) - 1
			aps[apID] = i
		}
		if out.Nodes[i].SSID == "" {
			out.Nodes[i].SSID = r.ssid
		}
		strength := rssiStrength(r.rssi)
		conn := topologyConnection{Source: r.id, Target: apID, Network: "wifi", Strength: strength}
		if r.rssi != nil {
			conn.SourceToTarget = &topologyDirection{Strength: strength, RSSI: r.rssi}
		}
		out.Connections = append(out.Connections, conn)
	}
	return out
}

var levelRank = map[string]int{"unknown": -1, "none": 0, "weak": 1, "medium": 2, "strong": 3}

// lqiStrength is getSignalLevelFromLqi (topology-utils.ts:754-759).
func lqiStrength(lqi uint64) string {
	switch {
	case lqi == 0:
		return "none"
	case lqi > 2:
		return "strong"
	case lqi > 1:
		return "medium"
	}
	return "weak"
}

// rssiStrength is rssiToStrength (NetworkTopologyService.ts:517-522).
func rssiStrength(rssi *int64) string {
	switch {
	case rssi == nil:
		return "unknown"
	case *rssi > -70:
		return "strong"
	case *rssi > -85:
		return "medium"
	}
	return "weak"
}

// networkTypeOf is getNetworkTypeFromAttributes (topology-utils.ts:89-108):
// the root's NetworkCommissioning FeatureMap, Thread before Wi-Fi before
// Ethernet.
func networkTypeOf(attrs map[string]json.RawMessage) string {
	fm, ok := uintOf(attrs[attrKey(0, netCommissioning.ID, featureMapID)])
	if !ok {
		return "unknown"
	}
	for _, t := range []struct{ title, name string }{{"ThreadNetworkInterface", "thread"}, {"WiFiNetworkInterface", "wifi"}, {"EthernetNetworkInterface", "ethernet"}} {
		for _, f := range netCommissioning.Features {
			if f.Title == t.title && fm&(1<<uint(f.Bit)) != 0 { //nolint:gosec // a feature bit
				return t.name
			}
		}
	}
	return "unknown"
}

// threadRoles are the wire names of the RoutingRoleEnum values by their
// snapshot names (mapThreadRole, NetworkTopologyService.ts:435-452).
var threadRoles = map[string]string{
	"Leader": "leader", "Router": "router", "Reed": "reed", "EndDevice": "end_device",
	"SleepyEndDevice": "sleepy_end_device", "Unassigned": "unassigned",
}

func threadRole(attrs map[string]json.RawMessage) string {
	v, ok := uintOf(attrs[attrKey(0, threadDiag.ID, attrRoutingRole.ID)])
	if !ok || attrRoutingRole.Type.Enum == nil {
		return ""
	}
	for _, e := range attrRoutingRole.Type.Enum.Values {
		if e.Value == v {
			return threadRoles[e.Name]
		}
	}
	return ""
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

// threadExtAddress is getThreadExtendedAddress (topology-utils.ts:197-226):
// the hardware address of the Thread interface (else the first one) of
// GeneralDiagnostics NetworkInterfaces, its last 8 bytes.
func threadExtAddress(attrs map[string]json.RawMessage) (uint64, bool) {
	list := entries(attrs[attrKey(0, generalDiag.ID, attrNetIfaces.ID)])
	if len(list) == 0 {
		return 0, false
	}
	typeKey, hwKey := member(attrNetIfaces.Type, "type"), member(attrNetIfaces.Type, "hardwareAddress")
	threadType := enumValue(attrNetIfaces.Type, "type", "Thread")
	pick := list[0]
	for _, e := range list {
		if v, ok := uintOf(e[typeKey]); ok && threadType >= 0 && v == uint64(threadType) {
			pick = e
			break
		}
	}
	var b64 string
	if json.Unmarshal(pick[hwKey], &b64) != nil || b64 == "" {
		return 0, false
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, false
	}
	if len(b) > 8 {
		b = b[len(b)-8:]
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v, v != 0
}

// enumValue is the value of an enum member of a struct member, -1 when
// the snapshot has none.
func enumValue(t schema.Type, property, name string) int64 {
	if t.Entry != nil {
		t = *t.Entry
	}
	if t.Struct == nil {
		return -1
	}
	for _, f := range t.Struct.Fields {
		if f.Property == property && f.Type.Enum != nil {
			for _, e := range f.Type.Enum.Values {
				if e.Name == name {
					return int64(e.Value) //nolint:gosec // an enum value
				}
			}
		}
	}
	return -1
}

type neighbor struct {
	ext, rloc16, lqi uint64
	rssi             *int64
	rxOnWhenIdle     bool
}

// neighbors is parseNeighborTable (topology-utils.ts:251-298).
func neighbors(attrs map[string]json.RawMessage) []neighbor {
	t := attrNeighborTable.Type
	extK, rlocK, lqiK := member(t, "extAddress"), member(t, "rloc16"), member(t, "lqi")
	avgK, lastK, rxK := member(t, "averageRssi"), member(t, "lastRssi"), member(t, "rxOnWhenIdle")
	list := entries(attrs[attrKey(0, threadDiag.ID, attrNeighborTable.ID)])
	out := make([]neighbor, 0, len(list))
	for _, e := range list {
		var nb neighbor
		nb.ext, _ = uintOf(e[extK])
		nb.rloc16, _ = uintOf(e[rlocK])
		nb.lqi, _ = uintOf(e[lqiK])
		if v, ok := intOf(e[avgK]); ok {
			nb.rssi = &v
		} else if v, ok := intOf(e[lastK]); ok {
			nb.rssi = &v
		}
		_ = json.Unmarshal(e[rxK], &nb.rxOnWhenIdle)
		out = append(out, nb)
	}
	return out
}

// bssidOf is getWiFiDiagnostics' BSSID (topology-utils.ts:1033-1047),
// "AA:BB:…".
func bssidOf(attrs map[string]json.RawMessage) string {
	var b64 string
	if json.Unmarshal(attrs[attrKey(0, wifiDiag.ID, attrBssid.ID)], &b64) != nil || b64 == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02X", x)
	}
	return strings.Join(parts, ":")
}

// wifiSSID is getWiFiSsid (topology-utils.ts:1076-1110): the connected
// entry of NetworkCommissioning Networks, its network id as UTF-8 text of
// at most 32 bytes without control characters.
func wifiSSID(attrs map[string]json.RawMessage) string {
	idK, connK := member(attrNetworks.Type, "networkId"), member(attrNetworks.Type, "connected")
	for _, e := range entries(attrs[attrKey(0, netCommissioning.ID, attrNetworks.ID)]) {
		var connected bool
		if json.Unmarshal(e[connK], &connected) != nil || !connected {
			continue
		}
		var b64 string
		if json.Unmarshal(e[idK], &b64) != nil {
			return ""
		}
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(b) == 0 || len(b) > 32 || !utf8.Valid(b) {
			return ""
		}
		for _, r := range string(b) {
			if r < 0x20 || r == 0x7F {
				return ""
			}
		}
		return string(b)
	}
	return ""
}

func hex16(v uint64) string { return fmt.Sprintf("%016X", v) }

func intOf(raw json.RawMessage) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	return v, err == nil
}

// topologyUpdated sends network_topology_updated to the connections that
// asked for the topology, when it changed (NetworkTopologyService.ts:
// hash-gated, latest wins); the caller does not hold s.mu.
func (s *Server) topologyUpdated() {
	s.mu.Lock()
	want := false
	for c := range s.conns {
		if c.topology.Load() {
			want = true
			break
		}
	}
	s.mu.Unlock()
	if !want {
		return
	}
	t := s.topology()
	sig, _ := json.Marshal(struct {
		N []topologyNode
		C []topologyConnection
	}{t.Nodes, t.Connections})
	s.topoMu.Lock()
	same := string(sig) == s.topoSig
	s.topoSig = string(sig)
	s.topoMu.Unlock()
	if same {
		return
	}
	frame := protocol.MustEncode(protocol.Event{Event: "network_topology_updated", Data: t})
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		if c.topology.Load() {
			c.send.SendCoalescable("topology", func() []byte { return frame })
		}
	}
}
