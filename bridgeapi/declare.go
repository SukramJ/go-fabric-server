// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SukramJ/go-fabric/device"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// bridged is one declared device.
type bridged struct {
	key      string
	typeName string
	dev      *device.Device
	// canon is the device's declaration in canonical JSON: a declare
	// that states the same leaves the device as it is.
	canon string
	// declared are the clusters the declaration names, by id.
	declared map[uint32]bool
	unsub    []func()
}

func (b *bridged) release() {
	for _, u := range b.unsub {
		u()
	}
	b.unsub = nil
}

// The declaration document (brief S2 §1.3): names, not ids.
type declDevice struct {
	Key            string                     `json:"key"`
	Type           string                     `json:"type"`
	Name           string                     `json:"name"`
	Vendor         string                     `json:"vendor"`
	DeviceAddress  string                     `json:"device_address"`
	ChannelAddress string                     `json:"channel_address"`
	Reachable      *bool                      `json:"reachable"`
	Clusters       map[string]json.RawMessage `json:"clusters"`
}

type declCluster struct {
	Initial        json.RawMessage `json:"initial"`
	Features       []string        `json:"features"`
	HandleWrites   bool            `json:"handle_writes"`
	HandleCommands []string        `json:"handle_commands"`
}

// strict decodes raw into dst, an unknown key refused, the error naming
// path.
func strict(raw json.RawMessage, path string, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%s: %s", path, decodeError(err))
	}
	if dec.More() {
		return fmt.Errorf("%s: data after the object", path)
	}
	return nil
}

// decodeDeclaration decodes a declare's args into devices built by the
// kit, nothing applied yet. A malformed document is an error naming the
// path; the kit's refusal of a device (device.New) is returned with the
// kit's message.
func (s *Server) decodeDeclaration(ctx context.Context, raw json.RawMessage) ([]*bridged, error) {
	var doc struct {
		Devices []json.RawMessage `json:"devices"`
	}
	if err := strict(raw, "declare", &doc); err != nil {
		return nil, err
	}
	if doc.Devices == nil {
		return nil, errors.New("declare: devices is required (an empty list removes every device)")
	}
	out := make([]*bridged, 0, len(doc.Devices))
	keys := map[string]int{}
	for i, rawDev := range doc.Devices {
		path := fmt.Sprintf("devices[%d]", i)
		b, err := s.buildDevice(ctx, rawDev, path)
		if err != nil {
			for _, done := range out {
				done.release()
			}
			return nil, err
		}
		if j, dup := keys[b.key]; dup {
			b.release()
			for _, done := range out {
				done.release()
			}
			return nil, fmt.Errorf("%s.key: %q is also the key of devices[%d]", path, b.key, j)
		}
		keys[b.key] = i
		out = append(out, b)
	}
	return out, nil
}

func (s *Server) buildDevice(ctx context.Context, raw json.RawMessage, path string) (*bridged, error) {
	var d declDevice
	if err := strict(raw, path, &d); err != nil {
		return nil, err
	}
	if d.Key == "" {
		return nil, fmt.Errorf("%s.key: a device needs its stable key", path)
	}
	dt := schema.DeviceTypeByName(d.Type)
	if dt == nil {
		return nil, fmt.Errorf("%s.type: no device type %q", path, d.Type)
	}
	typ, ok := device.Lookup(dt.ID)
	if !ok {
		return nil, fmt.Errorf("%s.type: go-fabric's device kit does not build %s devices", path, dt.Name)
	}
	names := make([]string, 0, len(d.Clusters))
	for name := range d.Clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	b := &bridged{key: d.Key, typeName: dt.Name, declared: map[uint32]bool{}}
	var decls []device.Declaration
	for _, name := range names {
		at := path + ".clusters." + name
		k, err := kitByName(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", at, err)
		}
		cl := k.cluster()
		if b.declared[cl.ID] {
			return nil, fmt.Errorf("%s: the %s cluster is declared twice", at, cl.Name)
		}
		b.declared[cl.ID] = true
		decl, err := s.clusterDeclaration(k, d.Clusters[name], at, d.Key)
		if err != nil {
			return nil, err
		}
		decls = append(decls, decl)
	}
	opts := device.Options{
		StableKey: device.StringKey(d.Key), Name: d.Name, Vendor: d.Vendor,
		DeviceAddress: d.DeviceAddress, ChannelAddress: d.ChannelAddress,
	}
	dev, err := device.New(typ, opts, decls...)
	if err != nil {
		return nil, fmt.Errorf("%s (%s): %w", path, d.Key, err)
	}
	if d.Reachable != nil {
		dev.SetReachable(ctx, *d.Reachable)
	}
	b.dev = dev
	canon, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	b.canon = string(canon)
	s.follow(b)
	return b, nil
}

// clusterDeclaration is one cluster's Declare: its initial patch through
// the package's typed Patch, the features by name, the host's hooks.
func (s *Server) clusterDeclaration(k clusterKit, raw json.RawMessage, path, key string) (device.Declaration, error) {
	cl := k.cluster()
	var c declCluster
	if err := strict(raw, path, &c); err != nil {
		return nil, err
	}
	patch, err := k.decodePatch(c.Initial, path+".initial")
	if err != nil {
		return nil, err
	}
	var features uint32
	for i, name := range c.Features {
		bit, ok := featureBit(cl, name)
		if !ok {
			return nil, fmt.Errorf("%s.features[%d]: the %s cluster has no feature %q", path, i, cl.Name, name)
		}
		features |= 1 << bit
	}
	if features != 0 && !k.hasFeatures() {
		return nil, fmt.Errorf("%s.features: the %s cluster takes no feature selection", path, cl.Name)
	}
	var h *hooks
	if c.HandleWrites || len(c.HandleCommands) > 0 {
		h = &hooks{s: s, device: key, cluster: cl.Name, writes: c.HandleWrites}
		for i, name := range c.HandleCommands {
			cmd := cl.CommandByName(name)
			if cmd == nil {
				return nil, fmt.Errorf("%s.handle_commands[%d]: the %s cluster has no command %q", path, i, cl.Name, name)
			}
			if !k.handles(cmd.ID) {
				return nil, fmt.Errorf("%s.handle_commands[%d]: go-fabric's %s server takes no host handler for %s", path, i, cl.Name, cmd.Name)
			}
			h.commands = append(h.commands, cmd.ID)
		}
	}
	decl, err := k.declare(patch, features, h)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return decl, nil
}

// featureBit resolves a feature by its title (Lighting) or its code (LT),
// without regard to case.
func featureBit(cl *schema.Cluster, name string) (int, bool) {
	for _, f := range cl.Features {
		if strings.EqualFold(f.Title, name) || strings.EqualFold(f.Name, name) {
			return f.Bit, true
		}
	}
	return 0, false
}
