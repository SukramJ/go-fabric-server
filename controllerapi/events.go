// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/binary"
	"fmt"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// eventHistorySize is matterjs-server's EVENT_HISTORY_SIZE
// (WebSocketControllerHandler.ts:112), the events diagnostics returns.
const eventHistorySize = 25

// nodeEventData is MatterNodeEvent (packages/ws-client/src/models/model.ts:1504-1514).
type nodeEventData struct {
	NodeID        uint64 `json:"node_id"`
	EndpointID    uint16 `json:"endpoint_id"`
	ClusterID     uint32 `json:"cluster_id"`
	EventID       uint32 `json:"event_id"`
	EventNumber   uint64 `json:"event_number"`
	Priority      uint8  `json:"priority"`
	Timestamp     uint64 `json:"timestamp"`
	TimestampType int    `json:"timestamp_type"`
	Data          any    `json:"data"`
}

// eventData converts an event of a report — matterjs-server's
// convertEventToWebSocket (Converters.ts:759-800): the epoch timestamp
// (type 1), else the system timestamp (type 0), else the delta forms the
// event carried (the interaction layer's convenience Timestamp, type 2);
// the data name-based through the event's fields.
func eventData(nodeID uint64, e *client.EventData) nodeEventData {
	d := nodeEventData{
		NodeID: nodeID, EndpointID: e.Path.Endpoint, ClusterID: e.Path.Cluster, EventID: e.Path.Event,
		EventNumber: e.Number, Priority: uint8(e.Priority),
	}
	switch {
	case e.EpochTimestamp != nil:
		d.Timestamp, d.TimestampType = *e.EpochTimestamp, 1
	case e.SystemTimestamp != nil:
		d.Timestamp, d.TimestampType = *e.SystemTimestamp, 0
	default:
		d.Timestamp, d.TimestampType = e.Timestamp, 2
	}
	if e.HasValue {
		d.Data = eventJSON(e.Path.Cluster, e.Path.Event, e.Value)
	}
	return d
}

// nodeEvent forwards an event the default subscription reported as
// node_event (droppable under backpressure) and keeps it for diagnostics.
func (s *Server) nodeEvent(local string, endpoint uint16, cluster, event uint32, e client.EventData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.nodes[local]
	if st == nil || s.closing {
		return
	}
	e.Path.Endpoint, e.Path.Cluster, e.Path.Event = endpoint, cluster, event
	d := eventData(st.nodeID, &e)
	s.history = append(s.history, d)
	if len(s.history) > eventHistorySize {
		s.history = s.history[len(s.history)-eventHistorySize:]
	}
	frame := protocol.MustEncode(protocol.Event{Event: "node_event", Data: d})
	s.emitLocked(func(c *wsConn) { c.send.SendOrdered(frame, true) })
}

// serverInfoData is ServerInfoMessage (model.ts:1527-1544) with our
// capabilities extension.
type serverInfoData struct {
	FabricID                  uint64   `json:"fabric_id"`
	CompressedFabricID        uint64   `json:"compressed_fabric_id"`
	FabricIndex               *uint8   `json:"fabric_index,omitempty"`
	SchemaVersion             int      `json:"schema_version"`
	MinSupportedSchemaVersion int      `json:"min_supported_schema_version"`
	SDKVersion                string   `json:"sdk_version"`
	WifiCredentialsSet        bool     `json:"wifi_credentials_set"`
	WifiSSID                  *string  `json:"wifi_ssid,omitempty"`
	ThreadCredentialsSet      bool     `json:"thread_credentials_set"`
	BluetoothEnabled          bool     `json:"bluetooth_enabled"`
	BLEProxyEnabled           bool     `json:"ble_proxy_enabled"`
	ControllerNodeID          *uint64  `json:"controller_node_id,omitempty"`
	Capabilities              []string `json:"capabilities"`
}

// serverInfo builds server_info — WebSocketControllerHandler.ts:1165-1186.
// sdk_version has matterjs-server's form, "<server>/<version> (<stack>/<version>)".
func (s *Server) serverInfo() serverInfoData {
	info := serverInfoData{
		SchemaVersion:             protocol.SchemaVersion,
		MinSupportedSchemaVersion: protocol.MinSupportedSchemaVersion,
		SDKVersion:                fmt.Sprintf("go-fabric-server/%s (go-fabric/%s)", s.opts.Version, s.opts.GoFabricVersion),
		Capabilities:              s.Capabilities(),
		// matterjs-server's commandHandler.bleEnabled: a BLE backend is
		// configured (WebSocketControllerHandler.ts:1182). ble_proxy_enabled
		// stays false: it is matterjs-server's /ble endpoint, not ours.
		BluetoothEnabled: s.opts.BLE != nil,
	}
	if b := s.b(); b != nil {
		if f := b.Fabric(); f != nil {
			info.FabricID = f.FabricID
			if f.Identity != nil {
				info.CompressedFabricID = binary.BigEndian.Uint64(f.Identity.CompressedFabricID[:])
			}
			if f.FabricIndex != 0 {
				idx := f.FabricIndex
				info.FabricIndex = &idx
			}
			nodeID := f.RootNodeID
			info.ControllerNodeID = &nodeID
		}
	}
	wifi, thread := s.cfg.defaults()
	if wifi != nil && wifi.SSID != "" && wifi.Credentials != "" {
		info.WifiCredentialsSet = true
		ssid := wifi.SSID
		info.WifiSSID = &ssid
	}
	info.ThreadCredentialsSet = thread != nil && thread.Dataset != ""
	return info
}

// broadcastServerInfo sends server_info_updated to every connection
// (WebSocketControllerHandler.ts:1190-1217; reliable, not only to the
// listening ones).
func (s *Server) broadcastServerInfo() {
	frame := protocol.MustEncode(protocol.Event{Event: "server_info_updated", Data: s.serverInfo()})
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.send.SendReliable(frame)
	}
}
