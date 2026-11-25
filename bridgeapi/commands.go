// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/surface"

	"thingfabric.dev/go-fabric-server/protocol"
)

type serverInfoData struct {
	BridgeSchemaVersion int      `json:"bridge_schema_version"`
	SDKVersion          string   `json:"sdk_version"`
	Capabilities        []string `json:"capabilities"`
	Node                string   `json:"node"`
}

func (s *Server) serverInfo() serverInfoData {
	return serverInfoData{
		BridgeSchemaVersion: BridgeSchemaVersion,
		SDKVersion:          fmt.Sprintf("go-fabric-server/%s (go-fabric/%s)", s.opts.Version, s.opts.GoFabricVersion),
		Capabilities:        Capabilities(),
		Node:                cmp.Or(s.opts.Node, "default"),
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
	// withheldAtCall: the Add or Replace call answered the declaration's
	// *surface.WithheldError, so the node recorded it withheld then.
	withheldAtCall bool
}

// withheldDevice is one entry of declare's withheld list: a declared
// device the node's specification level withholds.
type withheldDevice struct {
	Key        string `json:"key"`
	DeviceType string `json:"device_type"`
	SpecLevel  string `json:"spec_level"`
}

// batchAdd is node.Batch.Add; a variable so a test can make an add fail.
var batchAdd = func(bt *node.Batch, ep node.Endpoint) error { return bt.Add(ep) }

// batchForget is node.Batch.Forget; a variable so a test can make a
// forget fail at its call.
var batchForget = func(bt *node.Batch, key device.SourceKey) error { return bt.Forget(key) }

// batchReplace is node.Batch.Replace; a variable so a test can make a
// replacement fail.
var batchReplace = func(bt *node.Batch, key device.SourceKey, ep node.Endpoint) error { return bt.Replace(key, ep) }

// took judges the answer of b's Add or Replace call. A device the
// specification level withheld is recorded by the node (node.Aggregator.Add,
// Replace), serves nothing and is no error: it is logged once as a
// warning, as go-fabric's reference daemon does
// (examples/reference-bridge/surface.go addAll).
func (s *Server) took(r *declared, b *bridged, err error) {
	var w *surface.WithheldError
	switch {
	case err == nil:
	case errors.As(err, &w):
		b.withheld, r.withheldAtCall = w, true
		s.logWithheld(b)
	default:
		r.Error = err.Error()
	}
}

func (s *Server) logWithheld(b *bridged) {
	s.log.Warn("fabricserver.bridge.withheld", slog.String("key", b.key),
		slog.String("device_type", b.typeName), slog.String("spec_level", string(b.withheld.Report.Level)))
}

// replace puts b's device in the place of prev's. A served prev is
// replaced in one step, whether b is served or withheld: since go-fabric
// v0.26.0 (#585) Batch.Replace by a withheld declaration takes the served
// endpoint off the wire, the key keeping its endpoint number, records the
// declaration withheld and answers its *surface.WithheldError, and a
// replacement it refuses leaves the served endpoint in place (go-fabric
// CHANGELOG.md:151 at v0.26.0, node/aggregator.go:696-805). A withheld
// prev holds no place in the Aggregator, so b is added.
func (s *Server) replace(bt *node.Batch, r *declared, prev, b *bridged) {
	if prev.withheld != nil {
		s.took(r, b, batchAdd(bt, b.dev))
		return
	}
	s.took(r, b, batchReplace(bt, device.StringKey(b.key), b.dev))
}

// commitErrors sorts the error of a Batch: since go-fabric v0.26.0 a
// batch is judged at its commit, and a call that returned nil can be
// refused there (go-fabric CHANGELOG.md:72 at v0.26.0,
// node/aggregator.go:523-617 Partition.Batch). withheld are the keys whose
// held declaration the node recorded withheld at the commit (no error);
// refusals the key refusals
// (ErrDuplicateKey, ErrUnknownKey, ErrReservedKey, ErrPolicyMismatch);
// other the rest — the store's, the reassembly's, a device's attach error.
func commitErrors(err error) (withheld map[string]*surface.WithheldError, refusals, other []error) {
	withheld = map[string]*surface.WithheldError{}
	var walk func(error)
	walk = func(err error) {
		if err == nil {
			return
		}
		if j, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range j.Unwrap() {
				walk(e)
			}
			return
		}
		var w *surface.WithheldError
		switch {
		case errors.As(err, &w):
			withheld[w.Endpoint] = w
		case errors.Is(err, node.ErrDuplicateKey), errors.Is(err, node.ErrUnknownKey),
			errors.Is(err, node.ErrReservedKey), errors.Is(err, node.ErrPolicyMismatch):
			refusals = append(refusals, err)
		default:
			other = append(other, err)
		}
	}
	walk(err)
	return withheld, refusals, other
}

// cmdDeclare applies the declaration document as one Aggregator.Batch: a
// new key is added, a changed device replaced, an unchanged one left as it
// is, a key the document no longer names removed and its endpoint number
// forgotten (Batch.Forget). A device the specification level withholds is
// declared like any other and named in the answer's withheld list.
//
// The half's devices follow what the node serves once the batch
// committed: a call the batch held and refused at its commit leaves its
// device undeclared (an Add) or the replaced device declared (a Replace),
// with the refusal as that device's error. A key the batch could not
// forget (at the call or at the commit) stays declared and answers
// UnknownError, as does an error of the commit that is no key refusal or
// a key refusal no device carries. A device refused at the commit carries
// every key refusal of the batch: go-fabric's refusals name their key
// only in their text (go-fabric #616 asks for a typed per-call error).
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
	// forgetErrs are the Forgets that failed at their call: their keys
	// stay declared.
	var forgetErrs []error
	batchErr := n.Aggregator().Batch(ctx, func(bt *node.Batch) error {
		for i, b := range devs {
			named[b.key] = true
			r := &results[i]
			r.Key = b.key
			prev, ok := old[b.key]
			switch {
			case ok && prev.canon == b.canon:
				r.Action = "unchanged"
			case ok:
				r.Action = "replaced"
				s.replace(bt, r, prev, b)
			default:
				r.Action = "added"
				s.took(r, b, batchAdd(bt, b.dev))
			}
		}
		for key := range old {
			if named[key] {
				continue
			}
			// Forget, not Remove: a key the document no longer names is gone
			// for good, so its endpoint number is freed (go-fabric v0.13.0
			// removed Batch.MarkComplete; a stored number is kept until
			// Forget erases it, as matter.js keeps it until its store is
			// erased, ServerEndpointInitializer.ts:59-65 eraseDescendant).
			if err := batchForget(bt, device.StringKey(key)); err != nil {
				forgetErrs = append(forgetErrs, fmt.Errorf("forget %s: %w", key, err))
				continue
			}
			removed = append(removed, key)
		}
		return nil
	})
	withheldAtCommit, refusals, other := commitErrors(batchErr)
	other = append(forgetErrs, other...)
	// served by the device, not by the node.Endpoint: a host's endpoint
	// on the same Aggregator may be a value Go cannot compare (go-fabric
	// CHANGELOG.md:171 at v0.26.0), which a map key would panic on.
	served := map[*device.Device]bool{}
	for _, ep := range n.Aggregator().Endpoints() {
		if d, ok := ep.(*device.Device); ok {
			served[d] = true
		}
	}
	// taken reports whether the node took b's declaration: served, or
	// recorded withheld at the call or at the commit.
	taken := func(r *declared, b *bridged) bool {
		if b.withheld == nil {
			return served[b.dev]
		}
		if r.withheldAtCall {
			return true
		}
		if w, ok := withheldAtCommit[b.key]; ok {
			b.withheld = w
			s.logWithheld(b)
			return true
		}
		return false
	}
	refusal := errors.Join(refusals...)
	refusedAtCommit := false
	s.mu.Lock()
	order := make([]string, 0, len(devs))
	for i, b := range devs {
		r := &results[i]
		if r.Action != "unchanged" && r.Error == "" && !taken(r, b) {
			// Held at the call, refused at the commit.
			refusedAtCommit = true
			r.Error = "the node refused it when the batch committed"
			if refusal != nil {
				r.Error += ": " + refusal.Error()
			}
		}
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
	// A key the document no longer names that the batch did not forget
	// (its Forget failed at the call, or was held and refused at the
	// commit: the node still serves the device) stays declared.
	gone := removed[:0]
	forgetRefused := false
	for _, key := range removed {
		if prev := old[key]; prev.withheld == nil && served[prev.dev] {
			forgetRefused = true
			continue
		}
		prev := old[key]
		prev.release()
		delete(s.devices, key)
		gone = append(gone, key)
	}
	removed = gone
	kept := make([]string, 0, len(old))
	for key := range old {
		if _, ok := s.devices[key]; ok && !named[key] {
			kept = append(kept, key)
		}
	}
	sort.Strings(kept)
	order = append(order, kept...)
	s.order = order
	var withheld []withheldDevice
	for _, key := range order {
		if w := s.devices[key].withheld; w != nil {
			withheld = append(withheld, withheldDevice{Key: key, DeviceType: s.devices[key].typeName, SpecLevel: string(w.Report.Level)})
		}
	}
	s.mu.Unlock()
	sort.Strings(removed)
	// A key refusal is claimed by the devices refused at the commit; a
	// refused Forget has no device entry to carry it.
	if unclaimed := refusal != nil && (!refusedAtCommit || forgetRefused); len(other) > 0 || unclaimed {
		var errs []error
		if unclaimed {
			errs = append(errs, refusal)
		}
		return nil, protocol.Errorf(protocol.UnknownError, "declare: the node's batch: %v", errors.Join(append(errs, other...)...))
	}
	if removed == nil {
		removed = []string{}
	}
	out := map[string]any{"devices": results, "removed": removed}
	if withheld != nil {
		out["withheld"] = withheld
	}
	return out, nil
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

// cmdHost changes the host's statements about a device: a persisted label
// (Rename), its name and addresses (device.Device.Update) and its
// reachability (SetReachable), in this order, stopping at the first
// refusal. The label goes first because it is the one go-fabric may
// refuse for a reason of the node's: Rename returns the node's refusal to
// store it, and the label is then not served and the device keeps its name
// (go-fabric v0.25.0 device/lifecycle.go:289-309) — so a refused label
// changes nothing. ErrNotBridged (a part, :34-40) is the host's mistake,
// InvalidArguments; any other refusal is UnknownError.
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
	if a.Label != nil {
		if err := b.dev.Rename(ctx, *a.Label); err != nil {
			return nil, refusedStatement(c.req.Command, err)
		}
	}
	if a.Name != nil || a.DeviceAddress != nil || a.ChannelAddress != nil {
		err := b.dev.Update(ctx, func(st *device.HostStatement) {
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
		if err != nil {
			return nil, refusedStatement(c.req.Command, err)
		}
	}
	if a.Reachable != nil {
		if err := b.dev.SetReachable(ctx, *a.Reachable); err != nil {
			return nil, refusedStatement(c.req.Command, err)
		}
	}
	return struct{}{}, nil
}

// refusedStatement is the command error of a host statement go-fabric refused.
func refusedStatement(command string, err error) *protocol.Error {
	if errors.Is(err, device.ErrNotBridged) {
		return invalidArgs(command, "%v", err)
	}
	return protocol.Errorf(protocol.UnknownError, "%s: %v", command, err)
}

// cmdRemove removes one device (Aggregator.Remove); its endpoint number
// stays stored under its key, so a later declare of the key gets it back
// (go-fabric v0.13.0: a stored number is kept until Forget).
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
	s.drop(a.Device)
	s.mu.Unlock()
	b.release()
	return struct{}{}, nil
}

// cmdForget removes one device if it is still declared and frees the
// endpoint number stored under its key (Aggregator.Forget): a later
// declare of the key gets a fresh number, as a new device would. It is
// idempotent: a key that is not declared (one remove dropped, one a past
// run knew, or none at all) is forgotten too and answers the same, since
// go-fabric's Forget of a key with nothing stored is no error and nothing
// tells whether a number was stored; only an empty key is refused. It
// mirrors go-fabric's Forget beside
// Remove (node/reserve.go, matter.js Endpoint.delete beside close,
// endpoint/Endpoint.ts:987-992, :1024-1032 at 862ad75f).
func cmdForget(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Device string `json:"device"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	s.declMu.Lock()
	defer s.declMu.Unlock()
	if a.Device == "" {
		return nil, invalidArgs(c.req.Command, "device is required")
	}
	s.mu.Lock()
	b := s.devices[a.Device]
	s.mu.Unlock()
	if err := s.node.Load().Aggregator().Forget(ctx, device.StringKey(a.Device)); err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "forget: %v", err)
	}
	s.mu.Lock()
	s.drop(a.Device)
	s.mu.Unlock()
	if b != nil {
		b.release()
	}
	return map[string]any{"device": a.Device}, nil
}

// drop deletes key from the declared devices and their order; s.mu is
// held.
func (s *Server) drop(key string) {
	delete(s.devices, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i:i], s.order[i+1:]...)
			break
		}
	}
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
	p, err := s.node.Load().SetupPayload()
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "pairing_codes: %v", err)
	}
	return map[string]any{"qr_code": p.QR, "manual_code": p.Manual}, nil
}

// cmdNodeInfo is the node as the controllers see it: the bridged devices,
// the fabrics, the commissioning window and the subscription count.
func cmdNodeInfo(ctx context.Context, s *Server, _ *call) (any, *protocol.Error) {
	n := s.node.Load()
	topo := n.Topology()
	devs := make([]map[string]any, 0, len(topo.Devices))
	for i := range topo.Devices {
		d := &topo.Devices[i]
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
	w := n.Window()
	out["commissioning_window"] = map[string]any{"open": w.Open, "own": w.Own}
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
