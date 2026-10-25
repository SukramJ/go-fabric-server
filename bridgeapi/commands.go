// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/protocol"
)

type serverInfoData struct {
	BridgeSchemaVersion int      `json:"bridge_schema_version"`
	SDKVersion          string   `json:"sdk_version"`
	Capabilities        []string `json:"capabilities"`
}

func (s *Server) serverInfo() serverInfoData {
	return serverInfoData{
		BridgeSchemaVersion: BridgeSchemaVersion,
		SDKVersion:          fmt.Sprintf("go-fabric-server/%s (go-fabric/%s)", s.opts.Version, s.opts.GoFabricVersion),
		Capabilities:        Capabilities(),
	}
}

func cmdServerInfo(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	return s.serverInfo(), nil
}

// originName is a change's origin on the wire.
func originName(o contract.Origin) string {
	switch o {
	case contract.OriginHostSet:
		return "host_set"
	case contract.OriginControllerWrite:
		return "controller_write"
	case contract.OriginCommand:
		return "command"
	default:
		return "unknown"
	}
}

// follow sends a changed event after every change of one of b's servers
// the host did not make itself. It follows each server through
// device.Device.OnChangedAsync: the event is built on the device's own
// goroutine, after the change, never inside the server's change.
func (s *Server) follow(b *bridged) {
	for _, srv := range b.dev.Servers() {
		id := srv.MatterClusterID()
		k, ok := kits[id]
		if !ok || k.err() != nil {
			continue
		}
		b.unsub = append(b.unsub, b.dev.OnChangedAsync(id, func(c contract.Change) {
			if c.Origin == contract.OriginHostSet {
				return
			}
			cl := k.cluster()
			names := make([]string, 0, len(c.Attributes))
			for _, a := range c.Attributes {
				if at := cl.Attribute(a); at != nil {
					names = append(names, at.Property)
				}
			}
			data := map[string]any{
				"device": b.key, "cluster": cl.Name, "state": k.state(srv),
				"attributes": names, "origin": originName(c.Origin),
			}
			if ep, ok := b.dev.EndpointNumber(); ok {
				data["endpoint"] = ep
			}
			s.emit("changed", data)
		}))
	}
}

type declared struct {
	Key      string  `json:"key"`
	Action   string  `json:"action"`
	Endpoint *uint16 `json:"endpoint,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// cmdDeclare applies the declaration document as one Aggregator.Batch: a
// new key is added, a changed device replaced, an unchanged one left as it
// is, a key the document no longer names removed; the batch marks the
// topology complete.
func cmdDeclare(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	n := s.node.Load()
	devs, err := s.decodeDeclaration(ctx, c.req.Args)
	if err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	s.declMu.Lock()
	defer s.declMu.Unlock()
	s.mu.Lock()
	old := maps.Clone(s.devices)
	s.mu.Unlock()
	results := make([]declared, len(devs))
	named := map[string]bool{}
	var removed []string
	batchErr := n.Aggregator().Batch(ctx, func(bt *node.Batch) error {
		for i, b := range devs {
			named[b.key] = true
			results[i].Key = b.key
			prev, ok := old[b.key]
			switch {
			case ok && prev.canon == b.canon:
				results[i].Action = "unchanged"
			case ok:
				results[i].Action = "replaced"
				if err := bt.Replace(device.StringKey(b.key), b.dev); err != nil {
					results[i].Error = err.Error()
				}
			default:
				results[i].Action = "added"
				if err := bt.Add(b.dev); err != nil {
					results[i].Error = err.Error()
				}
			}
		}
		for key := range old {
			if named[key] {
				continue
			}
			if err := bt.Remove(device.StringKey(key)); err == nil {
				removed = append(removed, key)
			}
		}
		bt.MarkComplete()
		return nil
	})
	s.mu.Lock()
	order := make([]string, 0, len(devs))
	for i, b := range devs {
		r := &results[i]
		switch {
		case r.Error != "":
			b.release()
			if prev, ok := old[b.key]; ok && r.Action == "replaced" {
				order = append(order, prev.key)
			}
			continue
		case r.Action == "unchanged":
			b.release()
			b = old[b.key]
		case r.Action == "replaced":
			old[b.key].release()
		}
		s.devices[b.key] = b
		order = append(order, b.key)
		if ep, ok := b.dev.EndpointNumber(); ok {
			r.Endpoint = &ep
		}
	}
	for _, key := range removed {
		old[key].release()
		delete(s.devices, key)
	}
	s.order = order
	s.mu.Unlock()
	sort.Strings(removed)
	if batchErr != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "declare: the node's reassembly: %v", batchErr)
	}
	if removed == nil {
		removed = []string{}
	}
	return map[string]any{"devices": results, "removed": removed}, nil
}

// device is the declared device with key.
func (s *Server) device(command, key string) (*bridged, *protocol.Error) {
	if key == "" {
		return nil, invalidArgs(command, "device is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.devices[key]
	if !ok {
		return nil, invalidArgs(command, "no device %q is declared", key)
	}
	return b, nil
}

// cmdSet applies a patch through the cluster's typed Set: every value is
// checked by the kit, and all are stored or none.
func cmdSet(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Device  string          `json:"device"`
		Cluster string          `json:"cluster"`
		Patch   json.RawMessage `json:"patch"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	b, perr := s.device(c.req.Command, a.Device)
	if perr != nil {
		return nil, perr
	}
	k, err := kitByName(a.Cluster)
	if err != nil {
		return nil, invalidArgs(c.req.Command, "cluster: %v", err)
	}
	srv := b.dev.Server(k.cluster().ID)
	if srv == nil {
		return nil, invalidArgs(c.req.Command, "the device %q serves no %s cluster", a.Device, k.cluster().Name)
	}
	if len(a.Patch) == 0 {
		return nil, invalidArgs(c.req.Command, "patch is required")
	}
	p, err := k.decodePatch(a.Patch, "patch")
	if err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	if err := k.set(srv, p); err != nil {
		return nil, invalidArgs(c.req.Command, "%s: %v", k.cluster().Name, err)
	}
	return struct{}{}, nil
}

// cmdHost changes the host's statements about a device: its name and
// addresses (device.Device.Update), its reachability (SetReachable) and a
// persisted label (Rename).
func cmdHost(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Device         string  `json:"device"`
		Name           *string `json:"name"`
		DeviceAddress  *string `json:"device_address"`
		ChannelAddress *string `json:"channel_address"`
		Reachable      *bool   `json:"reachable"`
		Label          *string `json:"label"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	b, perr := s.device(c.req.Command, a.Device)
	if perr != nil {
		return nil, perr
	}
	if a.Name != nil || a.DeviceAddress != nil || a.ChannelAddress != nil {
		b.dev.Update(ctx, func(st *device.HostStatement) {
			if a.Name != nil {
				st.Name = *a.Name
			}
			if a.DeviceAddress != nil {
				st.DeviceAddress = *a.DeviceAddress
			}
			if a.ChannelAddress != nil {
				st.ChannelAddress = *a.ChannelAddress
			}
		})
	}
	if a.Reachable != nil {
		b.dev.SetReachable(ctx, *a.Reachable)
	}
	if a.Label != nil {
		b.dev.Rename(ctx, *a.Label)
	}
	return struct{}{}, nil
}

// cmdRemove removes one device (Aggregator.Remove); its endpoint number
// is forgotten once the topology is complete.
func cmdRemove(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Device string `json:"device"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	s.declMu.Lock()
	defer s.declMu.Unlock()
	b, perr := s.device(c.req.Command, a.Device)
	if perr != nil {
		return nil, perr
	}
	err := s.node.Load().Aggregator().Remove(ctx, device.StringKey(a.Device))
	if err != nil && !errors.Is(err, node.ErrUnknownKey) {
		return nil, protocol.Errorf(protocol.UnknownError, "remove: %v", err)
	}
	s.mu.Lock()
	delete(s.devices, a.Device)
	for i, key := range s.order {
		if key == a.Device {
			s.order = append(s.order[:i:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	b.release()
	return struct{}{}, nil
}

// cmdResult answers a write or command event before its deadline.
func cmdResult(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Correlation *protocol.Uint64 `json:"correlation"`
		OK          *bool            `json:"ok"`
		Error       *struct {
			Status        *int `json:"status"`
			ClusterStatus *int `json:"cluster_status"`
		} `json:"error"`
		Response json.RawMessage `json:"response"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Correlation == nil {
		return nil, invalidArgs(c.req.Command, "correlation is required")
	}
	id := uint64(*a.Correlation)
	s.mu.Lock()
	p, ok := s.pending[id]
	if !ok {
		s.mu.Unlock()
		return nil, invalidArgs(c.req.Command, "correlation %d is not pending: answered, past its deadline, or never sent", id)
	}
	var o outcome
	switch {
	case a.Error != nil:
		o.err = &hostError{status: a.Error.Status, clusterStatus: a.Error.ClusterStatus}
	case a.OK != nil && !*a.OK:
		o.err = &hostError{}
	case p.decode != nil:
		if err := p.decode(a.Response); err != nil {
			s.mu.Unlock()
			return nil, invalidArgs(c.req.Command, "%v", err)
		}
		o.response = a.Response
	}
	delete(s.pending, id)
	s.mu.Unlock()
	p.done <- o
	return struct{}{}, nil
}

func cmdPairingCodes(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	qr, manual, err := s.node.Load().PairingCodes()
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "pairing_codes: %v", err)
	}
	return map[string]any{"qr_code": qr, "manual_code": manual}, nil
}

// cmdNodeInfo is the node as the controllers see it: the bridged devices,
// the fabrics, the commissioning window and the subscription count.
func cmdNodeInfo(ctx context.Context, s *Server, _ *call) (any, *protocol.Error) {
	n := s.node.Load()
	topo := n.Topology()
	devs := make([]map[string]any, 0, len(topo.Devices))
	for _, d := range topo.Devices {
		e := map[string]any{"endpoint": d.Endpoint, "device_type": d.DeviceType, "name": d.Name, "reachable": d.Reachable}
		if d.Key != nil {
			e["key"] = d.Key.String()
		}
		devs = append(devs, e)
	}
	fabrics := []map[string]any{}
	infos, err := n.FabricInfos(ctx)
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "node_info: fabrics: %v", err)
	}
	for _, f := range infos {
		fabrics = append(fabrics, fabricData(f))
	}
	out := map[string]any{"aggregator_endpoint": topo.Aggregator, "devices": devs, "fabrics": fabrics}
	if w := n.CommissioningWindow(); w != nil {
		out["commissioning_window"] = map[string]any{"open": w.IsOpen(), "own": w.IsOwnWindow()}
	}
	if subs := n.Subscriptions(); subs != nil {
		out["subscriptions"] = subs.Active()
	}
	return out, nil
}

// dumpLocked is start_listening's answer: every declared device with the
// state of each cluster it serves.
func (s *Server) dumpLocked() []map[string]any {
	out := make([]map[string]any, 0, len(s.order))
	for _, key := range s.order {
		b := s.devices[key]
		if b == nil {
			continue
		}
		clusters := map[string]any{}
		for _, srv := range b.dev.Servers() {
			if k, ok := kits[srv.MatterClusterID()]; ok && k.err() == nil {
				clusters[k.cluster().Name] = k.state(srv)
			}
		}
		e := map[string]any{"key": b.key, "type": b.typeName, "reachable": b.dev.Reachable(), "clusters": clusters}
		if ep, ok := b.dev.EndpointNumber(); ok {
			e["endpoint"] = ep
		}
		out = append(out, e)
	}
	return out
}

func fabricData(f node.FabricInfo) map[string]any {
	return map[string]any{
		"fabric_index": f.FabricIndex, "fabric_id": f.FabricID, "node_id": f.NodeID, "vendor_id": f.VendorID,
		"label": f.Label, "compressed_fabric_id": binary.BigEndian.Uint64(f.CompressedFabricID[:]),
	}
}

func windowData(w node.WindowSnapshot) map[string]any {
	return map[string]any{
		"open": w.Open, "own": w.Own, "commissioning_mode": w.CommissioningMode, "discriminator": w.Discriminator,
		"window_status": uint8(w.Administrator.Status),
	}
}
