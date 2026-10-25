// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed schema.json
var jsonSchema []byte

// JSONSchema is the JSON Schema (draft 2020-12) of the node data values,
// as GET /schema serves it. The caller must not modify it.
func JSONSchema() []byte { return jsonSchema }

//go:embed declaration.schema.json
var declarationSchema []byte

// DeclarationSchema is the JSON Schema (draft 2020-12) of the bridge
// half's declaration document and of a set's patch, as GET
// /schema/declaration serves it. The caller must not modify it.
func DeclarationSchema() []byte { return declarationSchema }

type index struct {
	byID     map[uint32]*Cluster
	byName   map[string]*Cluster
	dtByID   map[uint32]*DeviceType
	dtByName map[string]*DeviceType
	global   map[string]uint32
}

var (
	once sync.Once
	idx  index
)

func lookup() *index {
	once.Do(func() {
		idx = index{
			byID: map[uint32]*Cluster{}, byName: map[string]*Cluster{},
			dtByID: map[uint32]*DeviceType{}, dtByName: map[string]*DeviceType{},
			global: map[string]uint32{},
		}
		for _, c := range clusters {
			idx.byID[c.ID] = c
			idx.byName[strings.ToLower(c.Name)] = c
		}
		for i := range deviceTypes {
			d := &deviceTypes[i]
			idx.dtByID[d.ID] = d
			idx.dtByName[strings.ToLower(d.Name)] = d
		}
		for _, g := range globalAttributes {
			idx.global[g.Name] = g.ID
		}
	})
	return &idx
}

// Clusters are the snapshot's clusters, in its order.
func Clusters() []*Cluster { return clusters }

// ClusterByID is the cluster with id, or nil for one the snapshot does not
// know (a manufacturer cluster).
func ClusterByID(id uint32) *Cluster { return lookup().byID[id] }

// ClusterByName is the cluster with name, compared without regard to
// case, or nil.
func ClusterByName(name string) *Cluster { return lookup().byName[strings.ToLower(name)] }

// DeviceTypeByID is the device type with id, or nil.
func DeviceTypeByID(id uint32) *DeviceType { return lookup().dtByID[id] }

// DeviceTypeByName is the device type with name, compared without regard
// to case, or nil.
func DeviceTypeByName(name string) *DeviceType { return lookup().dtByName[strings.ToLower(name)] }

// GlobalAttributeID is the id of the global attribute the snapshot names
// name ("FeatureMap"); false when it names none.
func GlobalAttributeID(name string) (uint32, bool) {
	id, ok := lookup().global[name]
	return id, ok
}

// Attribute is the cluster's attribute with id, or nil.
func (c *Cluster) Attribute(id uint32) *Attribute {
	if c == nil {
		return nil
	}
	for i := range c.Attributes {
		if c.Attributes[i].ID == id {
			return &c.Attributes[i]
		}
	}
	return nil
}

// AttributeByName is the cluster's attribute with name (or property
// name), compared without regard to case, or nil.
func (c *Cluster) AttributeByName(name string) *Attribute {
	if c == nil {
		return nil
	}
	for i := range c.Attributes {
		if strings.EqualFold(c.Attributes[i].Name, name) || strings.EqualFold(c.Attributes[i].Property, name) {
			return &c.Attributes[i]
		}
	}
	return nil
}

// Command is the cluster's request command with id, or nil.
func (c *Cluster) Command(id uint32) *Command { return c.command(id, true) }

// Response is the cluster's response command with id, or nil.
func (c *Cluster) Response(id uint32) *Command { return c.command(id, false) }

func (c *Cluster) command(id uint32, request bool) *Command {
	if c == nil {
		return nil
	}
	for i := range c.Commands {
		if c.Commands[i].ID == id && c.Commands[i].Request == request {
			return &c.Commands[i]
		}
	}
	return nil
}

// CommandByName is the cluster's request command with name, compared
// without regard to case ("on", "On", "moveToLevelWithOnOff"), or nil —
// matterjs-server camelizes command_name and looks it up the same way
// (WebSocketControllerHandler.ts:1783).
func (c *Cluster) CommandByName(name string) *Command {
	if c == nil {
		return nil
	}
	for i := range c.Commands {
		x := &c.Commands[i]
		if x.Request && (strings.EqualFold(x.Name, name) || strings.EqualFold(x.Property, name)) {
			return x
		}
	}
	return nil
}

// ResponseByName is the cluster's response command with name, or nil.
func (c *Cluster) ResponseByName(name string) *Command {
	if c == nil {
		return nil
	}
	for i := range c.Commands {
		x := &c.Commands[i]
		if !x.Request && strings.EqualFold(x.Name, name) {
			return x
		}
	}
	return nil
}

// Event is the cluster's event with id, or nil.
func (c *Cluster) Event(id uint32) *Event {
	if c == nil {
		return nil
	}
	for i := range c.Events {
		if c.Events[i].ID == id {
			return &c.Events[i]
		}
	}
	return nil
}
