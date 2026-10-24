// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package schema is the data model of the protocol, generated from
// go-fabric's snapshot of the matter.js model (parity/schema.json, read
// through go-fabric's parity package): every cluster with its attributes,
// commands, events and datatypes, every device type, by id and by name.
// The generator is internal/gen; tables_gen.go and schema.json are its
// committed output, and a test fails when they are stale.
//
// Nothing here is written by hand from Matter knowledge: the ids, names,
// types and value names are the snapshot's. The hand-written part is the
// shape of the tables (this file) and their lookups (lookup.go).
package schema

// Meta is the encoded shape of a value — the snapshot's metatype.
type Meta uint8

// Value shapes.
const (
	// MetaAny is a value the snapshot gives no type: it is rendered as
	// decoded from the TLV.
	MetaAny Meta = iota
	MetaBool
	MetaUint
	MetaInt
	MetaFloat
	MetaDouble
	MetaString
	MetaBytes
	MetaEnum
	MetaBitmap
	MetaStruct
	MetaList
)

// Type is a value's type.
type Type struct {
	// Name is the type the snapshot states ("uint8", "epoch-s",
	// "DeviceTypeStruct", "list").
	Name string
	Meta Meta
	// Bits is the width of an integer, enum or bitmap.
	Bits int
	// Struct, Enum and Bitmap describe a named datatype, when the snapshot
	// defines it.
	Struct *Struct
	Enum   *Enum
	Bitmap *Bitmap
	// Entry is the entry type of a list.
	Entry *Type
}

// Field is a member of a struct or of a command or event payload.
type Field struct {
	ID uint32
	// Name is the snapshot's name ("NewPosition"), Property the camelCase
	// property name matter.js gives it ("newPosition").
	Name, Property string
	Type           Type
	Nullable       bool
	// Mandatory is conformance "M".
	Mandatory bool
}

// Struct is a struct datatype.
type Struct struct {
	Name   string
	Fields []Field
}

// Enum is an enum datatype.
type Enum struct {
	Name   string
	Values []EnumValue
}

// EnumValue is one value of an enum.
type EnumValue struct {
	Value uint64
	Name  string
}

// Bitmap is a bitmap datatype.
type Bitmap struct {
	Name    string
	Members []BitmapMember
}

// BitmapMember is a bit, or a run of bits, of a bitmap: Width bits from
// Bit.
type BitmapMember struct {
	Name       string
	Bit, Width int
}

// Attribute is a cluster attribute.
type Attribute struct {
	ID             uint32
	Name, Property string
	Type           Type
	Nullable       bool
	Writable       bool
	// Timed is a write that must be timed.
	Timed bool
}

// Command is a cluster command: a request (client to server) or a
// response.
type Command struct {
	ID             uint32
	Name, Property string
	Request        bool
	// Response names the response command of a request, "status" for one
	// answered with a status only.
	Response string
	// Timed is a request that must be invoked timed.
	Timed  bool
	Fields []Field
}

// Event is a cluster event.
type Event struct {
	ID             uint32
	Name, Property string
	Priority       string
	Fields         []Field
}

// Feature is a FeatureMap bit.
type Feature struct {
	Name, Title string
	Bit         int
}

// Cluster is a cluster of the snapshot.
type Cluster struct {
	ID         uint32
	Name       string
	Revision   uint16
	Features   []Feature
	Attributes []Attribute
	Commands   []Command
	Events     []Event
}

// DeviceType is a device type of the snapshot.
type DeviceType struct {
	ID       uint32
	Name     string
	Revision uint16
}

// GlobalAttribute is an attribute id every cluster has (FeatureMap,
// ClusterRevision, …), as the snapshot names it.
type GlobalAttribute struct {
	ID   uint32
	Name string
}
