// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/internal/wire"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// nodeState is the cache entry of one commissioned node: its attributes
// in wire form and what the node data derives from them — matterjs-server's
// AttributeDataCache NodeSnapshot (packages/ws-controller/src/controller/AttributeDataCache.ts:35-38)
// with the availability Nodes.ts keeps (:21-27).
type nodeState struct {
	nodeID uint64
	local  string
	attrs  map[string]any
	// clusters are the rendered attribute keys and the data version of
	// each "endpoint/cluster", so a report re-renders only the clusters it
	// changed.
	clusters  map[string]clusterState
	endpoints map[uint16]bool
	parents   map[uint16]uint16
	available bool
	// primed is set once the node was available since the server started:
	// before, a change is sent as node_updated only, as the values of a
	// first report are not sent as attribute_updated
	// (docs/websockets_api.md:2065).
	primed        bool
	commissioned  time.Time
	lastInterview time.Time
}

type clusterState struct {
	version    uint32
	hasVersion bool
	keys       []string
}

// The ids the node data derives fields from, by the snapshot's names.
var (
	basicInformation   = schema.ClusterByName("BasicInformation")
	descriptor         = schema.ClusterByName("Descriptor")
	aggregatorType     = schema.DeviceTypeByName("Aggregator")
	attrSpecVersion    = basicInformation.AttributeByName("SpecificationVersion")
	attrDataModelRev   = basicInformation.AttributeByName("DataModelRevision")
	attrDeviceTypeList = descriptor.AttributeByName("DeviceTypeList")
)

// snapshot renders a node's image; prev, when not nil, lends the rendered
// values of the clusters whose data version did not change. nil for a node
// that is not commissioned.
func (s *Server) snapshot(n *client.Node, prev *nodeState) *nodeState {
	addr, ok := n.PeerAddress()
	if !ok {
		return nil
	}
	st := &nodeState{
		nodeID: addr.NodeID, local: n.ID(),
		attrs: map[string]any{}, clusters: map[string]clusterState{},
		endpoints: map[uint16]bool{}, parents: map[uint16]uint16{},
		available:    n.ConnectionState() == client.Connected,
		commissioned: n.Record().CommissionedAt,
	}
	if prev != nil {
		st.primed, st.lastInterview = prev.primed, prev.lastInterview
		if !prev.commissioned.IsZero() {
			st.commissioned = prev.commissioned
		}
	}
	if st.lastInterview.IsZero() {
		st.lastInterview = time.Now()
	}
	if st.available {
		st.primed = true
	}
	for _, ep := range n.Endpoints() {
		if !ep.Installed() {
			continue
		}
		num := ep.Number()
		st.endpoints[num] = true
		if num != 0 {
			if o := ep.Owner(); o != nil {
				st.parents[num] = o.Number()
			}
		}
		for _, c := range ep.Clusters() {
			key := wire.ClusterKey(num, c.ID())
			version, hasVersion := c.Version()
			if prev != nil {
				if pc, ok := prev.clusters[key]; ok && hasVersion && pc.hasVersion && pc.version == version {
					for _, k := range pc.keys {
						if v, ok := prev.attrs[k]; ok {
							st.attrs[k] = v
						}
					}
					st.clusters[key] = pc
					continue
				}
			}
			cs := clusterState{version: version, hasVersion: hasVersion}
			for _, id := range c.AttributeList() {
				v, ok := c.Value(id)
				if !ok {
					continue
				}
				k := wire.Key(num, c.ID(), id)
				st.attrs[k] = wire.Attribute(c.ID(), id, v)
				cs.keys = append(cs.keys, k)
			}
			st.clusters[key] = cs
		}
	}
	return st
}

// nodeData is MatterNodeData (packages/ws-client/src/models/node.ts:9-33).
type nodeData struct {
	NodeID                 uint64            `json:"node_id"`
	DateCommissioned       string            `json:"date_commissioned"`
	LastInterview          string            `json:"last_interview"`
	InterviewVersion       int               `json:"interview_version"`
	Available              bool              `json:"available"`
	IsBridge               bool              `json:"is_bridge"`
	Attributes             map[string]any    `json:"attributes"`
	AttributeSubscriptions []any             `json:"attribute_subscriptions"`
	MatterVersion          *string           `json:"matter_version,omitempty"`
	EndpointParents        map[string]uint16 `json:"endpoint_parents"`
}

// interviewVersion is matterjs-server's constant
// (ControllerCommandHandler.ts:1264).
const interviewVersion = 6

func (st *nodeState) data() nodeData {
	d := nodeData{
		NodeID:                 st.nodeID,
		DateCommissioned:       dateString(st.commissioned),
		LastInterview:          dateString(st.lastInterview),
		InterviewVersion:       interviewVersion,
		Available:              st.available,
		IsBridge:               st.isBridge(),
		Attributes:             st.attrs,
		AttributeSubscriptions: []any{},
		MatterVersion:          st.matterVersion(),
		EndpointParents:        map[string]uint16{},
	}
	// endpoint_parents names the endpoints in attributes other than 0
	// (AttributeDataCache.ts:233-255).
	for k := range st.attrs {
		ep, err := strconv.ParseUint(k[:strings.IndexByte(k, '/')], 10, 16)
		if err != nil || ep == 0 {
			continue
		}
		if p, ok := st.parents[uint16(ep)]; ok {
			d.EndpointParents[strconv.FormatUint(ep, 10)] = p
		}
	}
	return d
}

// dateString is matterjs-server's getDateAsString (Converters.ts:657-668):
// local time, milliseconds and three zero microsecond digits, no zone. A
// zero time is the current one, as getNodeDetails falls back to Date.now()
// (ControllerCommandHandler.ts:1261-1263).
func dateString(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.Local().Format("2006-01-02T15:04:05.000") + "000"
}

// isBridge is matterjs-server's isBridgeNode (packages/ws-controller/src/util/bridgeDetection.ts:28-38):
// any endpoint's DeviceTypeList holds the Aggregator device type.
func (st *nodeState) isBridge() bool {
	if aggregatorType == nil || attrDeviceTypeList == nil {
		return false
	}
	suffix := "/" + strconv.FormatUint(uint64(descriptor.ID), 10) + "/" + strconv.FormatUint(uint64(attrDeviceTypeList.ID), 10)
	for k, v := range st.attrs {
		if !strings.HasSuffix(k, suffix) || strings.Count(k, "/") != 2 {
			continue
		}
		list, _ := v.([]any)
		for _, e := range list {
			if m, ok := e.(map[string]any); ok && numberIs(m["0"], uint64(aggregatorType.ID)) {
				return true
			}
		}
	}
	return false
}

func numberIs(v any, want uint64) bool {
	switch x := v.(type) {
	case uint64:
		return x == want
	case int64:
		return x >= 0 && uint64(x) == want
	}
	return false
}

// matterVersion is matterjs-server's determineMatterVersion
// (ControllerCommandHandler.ts:193-215): the SpecificationVersion as
// major.minor.patch (its top three bytes), else an estimate from
// DataModelRevision.
func (st *nodeState) matterVersion() *string {
	if basicInformation == nil {
		return nil
	}
	if attrSpecVersion != nil {
		if v, ok := st.attrs[wire.Key(0, basicInformation.ID, attrSpecVersion.ID)].(uint64); ok && v > 0 {
			s := fmt.Sprintf("%d.%d.%d", v>>24&0xFF, v>>16&0xFF, v>>8&0xFF)
			return &s
		}
	}
	if attrDataModelRev != nil {
		if v, ok := st.attrs[wire.Key(0, basicInformation.ID, attrDataModelRev.ID)].(uint64); ok {
			var s string
			switch {
			case v <= 16:
				s = "<1.2.0"
			case v == 17:
				s = "1.2.0"
			default:
				return nil
			}
			return &s
		}
	}
	return nil
}

// nodeListLocked is the node data of every node in node id order; the
// caller holds s.mu.
func (s *Server) nodeListLocked(onlyAvailable bool) []nodeData {
	states := slices.Collect(maps.Values(s.nodes))
	sort.Slice(states, func(i, j int) bool { return states[i].nodeID < states[j].nodeID })
	out := make([]nodeData, 0, len(states))
	for _, st := range states {
		if onlyAvailable && !st.available {
			continue
		}
		out = append(out, st.data())
	}
	return out
}

// stateByNodeID is the cache entry of a node id; the caller holds s.mu.
func (s *Server) stateByNodeID(id uint64) *nodeState {
	for _, st := range s.nodes {
		if st.nodeID == id {
			return st
		}
	}
	return nil
}

// --- following the controller ---

func (s *Server) markDirty(id string) {
	s.dirtyMu.Lock()
	s.dirty[id] = true
	s.dirtyMu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// loop refreshes the nodes the hooks marked, one pass after the other.
func (s *Server) loop() {
	defer close(s.loopDone)
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		}
		s.dirtyMu.Lock()
		ids := s.dirty
		s.dirty = map[string]bool{}
		s.dirtyMu.Unlock()
		if ids[""] {
			s.refreshAll()
			continue
		}
		for id := range ids {
			s.refresh(id)
		}
	}
}

func (s *Server) refreshAll() {
	b := s.b()
	if b == nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range b.Registry().Commissioned() {
		seen[n.ID()] = true
		s.refresh(n.ID())
	}
	s.mu.Lock()
	var gone []string
	for id := range s.nodes {
		if !seen[id] {
			gone = append(gone, id)
		}
	}
	s.mu.Unlock()
	for _, id := range gone {
		s.refresh(id)
	}
}

// refresh brings one node's cache entry up to date and emits what changed:
// attribute_updated for changed and removed values, node_updated for a
// change of structure, availability or the set of attributes, then
// endpoint_added / endpoint_removed (a node_updated carrying an added
// endpoint goes first, WebSocketControllerHandler.ts:665-673), node_added
// for a node new to the cache and node_removed for one that left it.
func (s *Server) refresh(id string) {
	b := s.b()
	if b == nil {
		return
	}
	n := b.Registry().Get(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return
	}
	old := s.nodes[id]
	var st *nodeState
	if n != nil && n.IsCommissioned() {
		st = s.snapshot(n, old)
	}
	switch {
	case st == nil && old == nil:
		return
	case st == nil:
		delete(s.nodes, id)
		s.emitLocked(func(c *wsConn) {
			c.send.SendOrdered(protocol.MustEncode(protocol.Event{Event: "node_removed", Data: old.nodeID}), false)
		})
		s.endNodeSubscriptions(old.nodeID)
		return
	case old == nil:
		s.nodes[id] = st
		data := st.data()
		s.emitLocked(func(c *wsConn) {
			c.send.SendOrdered(protocol.MustEncode(protocol.Event{Event: "node_added", Data: data}), false)
		})
		return
	}
	s.nodes[id] = st
	structural := old.available != st.available || !maps.Equal(old.endpoints, st.endpoints) || !sameKeys(old.attrs, st.attrs)
	if old.primed {
		for k, v := range st.attrs {
			if ov, ok := old.attrs[k]; ok && !reflect.DeepEqual(ov, v) {
				s.attributeUpdatedLocked(st.nodeID, k, v)
			}
		}
		for k := range old.attrs {
			if _, ok := st.attrs[k]; ok {
				continue
			}
			// Attributes of an endpoint the device no longer has are not
			// sent this way (docs/websockets_api.md:2065).
			if ep, err := strconv.ParseUint(k[:strings.IndexByte(k, '/')], 10, 16); err == nil && !st.endpoints[uint16(ep)] {
				continue
			}
			s.attributeUpdatedLocked(st.nodeID, k, nil)
		}
	}
	if !structural && old.primed == st.primed {
		return
	}
	s.nodeUpdatedLocked(st)
	for ep := range st.endpoints {
		if !old.endpoints[ep] {
			s.endpointEventLocked("endpoint_added", st.nodeID, ep)
		}
	}
	for ep := range old.endpoints {
		if !st.endpoints[ep] {
			s.endpointEventLocked("endpoint_removed", st.nodeID, ep)
		}
	}
}

func sameKeys(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// emitLocked runs send for every listening connection; the caller holds
// s.mu.
func (s *Server) emitLocked(send func(c *wsConn)) {
	for c := range s.conns {
		if c.listening {
			send(c)
		}
	}
}

// attributeUpdatedLocked is attribute_updated, coalesced per node and path
// under backpressure (WebSocketControllerHandler.ts:705-725).
func (s *Server) attributeUpdatedLocked(nodeID uint64, key string, value any) {
	frame := protocol.MustEncode(protocol.Event{Event: "attribute_updated", Data: []any{nodeID, key, value}})
	coalesce := "attr:" + strconv.FormatUint(nodeID, 10) + "/" + key
	s.emitLocked(func(c *wsConn) { c.send.SendCoalescable(coalesce, func() []byte { return frame }) })
}

// nodeUpdatedLocked is node_updated, coalesced per node under
// backpressure (WebSocketControllerHandler.ts:636-645): a later
// node_updated of the node replaces a queued one, so the frame sent holds
// the state of the latest change.
func (s *Server) nodeUpdatedLocked(st *nodeState) {
	key := "node:" + strconv.FormatUint(st.nodeID, 10)
	frame := protocol.MustEncode(protocol.Event{Event: "node_updated", Data: st.data()})
	s.emitLocked(func(c *wsConn) { c.send.SendCoalescable(key, func() []byte { return frame }) })
}

func (s *Server) endpointEventLocked(event string, nodeID uint64, ep uint16) {
	frame := protocol.MustEncode(protocol.Event{Event: event, Data: map[string]any{"node_id": nodeID, "endpoint_id": ep}})
	s.emitLocked(func(c *wsConn) { c.send.SendOrdered(frame, false) })
}

// broadcastNodeUpdated sends node_updated for a node id, as after an
// interview.
func (s *Server) broadcastNodeUpdated(nodeID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.stateByNodeID(nodeID); st != nil {
		s.nodeUpdatedLocked(st)
	}
}
