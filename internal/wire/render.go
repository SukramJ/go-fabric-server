// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package wire converts Matter values between go-fabric's decoded TLV
// trees and the JSON forms of the protocol, and parses the protocol's
// paths. The types come from protocol/schema, the generated tables of
// go-fabric's snapshot; the tree type is go-fabric's spec.Node, the type
// controller/client hands out for every value.
//
// It is the one package that reads a TLV element (cluster/spec's Node and
// tlv's element types): controller/client exposes values only as those
// trees and has no JSON route of its own (report S1, Questions Q1). The
// architecture test allows the two imports here and nowhere else.
//
// The rendering is matterjs-server's convertMatterToWebSocket
// (packages/ws-controller/src/server/Converters.ts:430-531 at a7922e85):
//
//   - attribute values are tag-based: a struct is an object keyed by the
//     field id, members the model does not name are left out;
//   - command responses and event data are name-based: keyed by the
//     camelCase property name;
//   - bytes are base64, enums and bitmaps numbers, epoch times the raw
//     TLV value (matter.js decodes them to Unix time and matterjs-server
//     subtracts the offset again, :466-472), lists arrays;
//   - a value the model does not type is sent as decoded from the TLV: a
//     structure an object keyed by tag number, bytes base64, lists arrays
//     (docs/websockets_api.md:2385-2390), and the FeatureMap of an unknown
//     cluster is {}.
package wire

import (
	"encoding/base64"
	"strconv"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// featureMapID is the FeatureMap attribute id, as the snapshot names it.
var featureMapID, _ = schema.GlobalAttributeID("FeatureMap")

// Attribute renders an attribute value of the node data (tag-based).
func Attribute(cluster, attribute uint32, n spec.Node) any {
	c := schema.ClusterByID(cluster)
	if c == nil {
		if attribute == featureMapID {
			return map[string]any{}
		}
		return Untyped(n)
	}
	a := c.Attribute(attribute)
	if a == nil {
		return Untyped(n)
	}
	return Value(a.Type, n, true)
}

// Fields renders a command response's or an event's payload by field
// name. A payload without fields of the model is rendered untyped, keyed
// by tag.
func Fields(fields []schema.Field, n spec.Node) any {
	if fields == nil {
		return Untyped(n)
	}
	return Value(schema.Type{Meta: schema.MetaStruct, Struct: &schema.Struct{Fields: fields}}, n, false)
}

// Value renders n as a value of t; tagBased keys structs by field id,
// otherwise by property name.
func Value(t schema.Type, n spec.Node, tagBased bool) any {
	el := n.El
	if el.IsNull || el.Type == tlv.TypeNull {
		return nil
	}
	switch t.Meta {
	case schema.MetaBool:
		if isBool(el.Type) {
			return el.Bool
		}
	case schema.MetaUint, schema.MetaEnum, schema.MetaBitmap, schema.MetaInt, schema.MetaFloat, schema.MetaDouble:
		if isNumber(el.Type) {
			return number(el)
		}
	case schema.MetaString:
		if isString(el.Type) {
			return el.String
		}
	case schema.MetaBytes:
		if isBytes(el.Type) {
			return base64.StdEncoding.EncodeToString(el.Octets)
		}
	case schema.MetaStruct:
		if el.Type == tlv.TypeStructure && t.Struct != nil {
			out := map[string]any{}
			for _, child := range n.Children {
				f := fieldOf(t.Struct, child.El.Tag)
				if f == nil {
					continue
				}
				key := f.Property
				if tagBased {
					key = strconv.FormatUint(uint64(f.ID), 10)
				}
				if f.Nullable && (child.El.IsNull || child.El.Type == tlv.TypeNull) {
					out[key] = nil
					continue
				}
				out[key] = Value(f.Type, child, tagBased)
			}
			return out
		}
	case schema.MetaList:
		if el.IsContainer && el.Type != tlv.TypeStructure {
			out := make([]any, 0, len(n.Children))
			for _, child := range n.Children {
				if t.Entry != nil {
					out = append(out, Value(*t.Entry, child, tagBased))
				} else {
					out = append(out, Untyped(child))
				}
			}
			return out
		}
	case schema.MetaAny:
	}
	return Untyped(n)
}

func fieldOf(s *schema.Struct, tag tlv.Tag) *schema.Field {
	if tag.Kind != tlv.TagKindContext {
		return nil
	}
	for i := range s.Fields {
		if s.Fields[i].ID == tag.Number {
			return &s.Fields[i]
		}
	}
	return nil
}

// Untyped renders n as decoded from the TLV, without a model.
func Untyped(n spec.Node) any {
	el := n.El
	switch {
	case el.IsNull || el.Type == tlv.TypeNull:
		return nil
	case el.Type == tlv.TypeStructure:
		out := make(map[string]any, len(n.Children))
		for _, child := range n.Children {
			out[strconv.FormatUint(uint64(child.El.Tag.Number), 10)] = Untyped(child)
		}
		return out
	case el.IsContainer:
		out := make([]any, 0, len(n.Children))
		for _, child := range n.Children {
			out = append(out, Untyped(child))
		}
		return out
	case isBool(el.Type):
		return el.Bool
	case isNumber(el.Type):
		return number(el)
	case isString(el.Type):
		return el.String
	case isBytes(el.Type):
		return base64.StdEncoding.EncodeToString(el.Octets)
	}
	return nil
}

func number(el tlv.Element) any {
	switch {
	case el.Type <= tlv.TypeSignedInt8:
		return el.Int
	case el.Type <= tlv.TypeUnsignedInt8:
		return el.Uint
	}
	return el.Float
}

func isBool(t tlv.ElementType) bool { return t == tlv.TypeBoolFalse || t == tlv.TypeBoolTrue }

func isNumber(t tlv.ElementType) bool {
	return t <= tlv.TypeUnsignedInt8 || t == tlv.TypeFloat4 || t == tlv.TypeFloat8
}

func isString(t tlv.ElementType) bool { return t >= tlv.TypeUTF8Str1 && t <= tlv.TypeUTF8Str8 }

func isBytes(t tlv.ElementType) bool { return t >= tlv.TypeOctetStr1 && t <= tlv.TypeOctetStr8 }
