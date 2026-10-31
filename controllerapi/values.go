// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// The values of the protocol go through go-fabric's JSON route
// (controller/client json.go, v0.12.1: client.DecodeJSON, client.EncodeJSON,
// typed by the generated cluster definitions). It renders in
// matterjs-server's forms (packages/ws-controller/src/server/Converters.ts:430-531
// at 0bd06f22): attribute values tag-based (a struct keyed by field id,
// members the model does not name left out), command responses and event
// data name-based (camelCase), bytes base64, enums and bitmaps numbers,
// epoch times the raw TLV value (report S1, D5), and a value the model does
// not type as decoded from the TLV (a structure keyed by tag number). The
// server keeps one rule of its own: the FeatureMap of a cluster the
// snapshot does not know is {} (docs/websockets_api.md:2385-2390 of the
// clone; go-fabric types FeatureMap globally as a bitmap).
//
// The route types only the clusters go-fabric has a generated definition
// of (cluster/spec); for the 16 others of the snapshot — AccessControl,
// OperationalCredentials, GroupKeyManagement, the Thread, camera, TLS and
// Joint Fabric clusters — client.AttributeField and its siblings are
// untyped (report S3, Questions Q20). For those the server shapes the
// untyped form by protocol/schema, the tables generated from the same
// snapshot: a rendered value is re-keyed by field name or stripped of
// members the model does not name, and a value to send is put into the
// untyped encoder's form (decimal member ids, chip's CustomArgument
// prefixes "base64:", "s:", "f:", "d:" for bytes, signed and float
// values). The fallback goes the moment go-fabric types the cluster: a
// typed field is never reshaped.

// featureMapID is the FeatureMap attribute id, as the snapshot names it.
var featureMapID, _ = schema.GlobalAttributeID("FeatureMap")

var jsonNull = json.RawMessage("null")

// attributeJSON renders an attribute value of the node data (tag-based).
func attributeJSON(cluster, attribute uint32, v client.Value) json.RawMessage {
	c := schema.ClusterByID(cluster)
	if attribute == featureMapID && c == nil {
		return json.RawMessage("{}")
	}
	var t *schema.Type
	if a := c.Attribute(attribute); a != nil {
		t = &a.Type
	}
	return renderJSON(client.AttributeField(cluster, attribute), t, v, client.TagBased)
}

// eventJSON renders an event's data by field name.
func eventJSON(cluster, event uint32, v client.Value) json.RawMessage {
	var t *schema.Type
	if e := schema.ClusterByID(cluster).Event(event); e != nil {
		t = structOf(e.Fields)
	}
	return renderJSON(client.EventField(cluster, event), t, v, client.NameBased)
}

// responseJSON renders a command response's fields by field name; command
// is the response command's id.
func responseJSON(cluster, command uint32, v client.Value) json.RawMessage {
	var t *schema.Type
	if r := schema.ClusterByID(cluster).Response(command); r != nil {
		t = structOf(r.Fields)
	}
	return renderJSON(client.ResponseField(cluster, command), t, v, client.NameBased)
}

func structOf(fields []schema.Field) *schema.Type {
	return &schema.Type{Meta: schema.MetaStruct, Struct: &schema.Struct{Fields: fields}}
}

// renderJSON renders v by f; an untyped f the snapshot types as t is
// reshaped (the Q20 fallback).
func renderJSON(f client.Field, t *schema.Type, v client.Value, form client.Form) json.RawMessage {
	b, err := client.DecodeJSON(f, v, form)
	if err != nil {
		return jsonNull
	}
	if f.Typed() || t == nil {
		return b
	}
	out, err := json.Marshal(reshape(*t, decodeAny(b), form == client.NameBased))
	if err != nil {
		return b
	}
	return out
}

// reshape keys an untyped rendering's structs as t names their members:
// by property name when nameBased, else by field id with the members t
// does not name left out.
func reshape(t schema.Type, v any, nameBased bool) any {
	switch t.Meta {
	case schema.MetaStruct:
		m, ok := v.(map[string]any)
		if !ok || t.Struct == nil {
			return v
		}
		out := make(map[string]any, len(m))
		for i := range t.Struct.Fields {
			f := &t.Struct.Fields[i]
			val, ok := m[strconv.FormatUint(uint64(f.ID), 10)]
			if !ok {
				continue
			}
			key := strconv.FormatUint(uint64(f.ID), 10)
			if nameBased {
				key = f.Property
			}
			out[key] = reshape(f.Type, val, nameBased)
		}
		return out
	case schema.MetaList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return v
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = reshape(*t.Entry, e, nameBased)
		}
		return out
	case schema.MetaAny, schema.MetaBool, schema.MetaUint, schema.MetaInt, schema.MetaFloat, schema.MetaDouble,
		schema.MetaString, schema.MetaBytes, schema.MetaEnum, schema.MetaBitmap:
	}
	return v
}

// untypedForm puts a JSON value of the protocol's form, typed t by the
// snapshot, into the form of go-fabric's untyped encoder: struct members
// by decimal id (a null for an optional member that is not nullable left
// out), bytes "base64:…", signed integers "s:…", floats "f:…" and "d:…".
// A string the model calls a string goes as it is (a value starting with
// one of the prefixes would be read as one; none of the 16 clusters has
// such a value of its own).
func untypedForm(t schema.Type, v any) any {
	switch t.Meta {
	case schema.MetaBytes:
		if s, ok := v.(string); ok {
			return "base64:" + s
		}
	case schema.MetaInt:
		if n, ok := v.(json.Number); ok {
			return "s:" + n.String()
		}
	case schema.MetaFloat:
		if n, ok := v.(json.Number); ok {
			return "f:" + n.String()
		}
	case schema.MetaDouble:
		if n, ok := v.(json.Number); ok {
			return "d:" + n.String()
		}
	case schema.MetaStruct:
		m, ok := v.(map[string]any)
		if !ok || t.Struct == nil {
			return v
		}
		out := make(map[string]any, len(m))
		for k, val := range m {
			f := memberField(t.Struct, k)
			if f == nil {
				out[k] = val
				continue
			}
			if val == nil && !f.Mandatory && !f.Nullable {
				continue
			}
			out[strconv.FormatUint(uint64(f.ID), 10)] = untypedForm(f.Type, val)
		}
		return out
	case schema.MetaList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return v
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = untypedForm(*t.Entry, e)
		}
		return out
	case schema.MetaAny, schema.MetaBool, schema.MetaUint, schema.MetaString, schema.MetaEnum, schema.MetaBitmap:
	}
	return v
}

func memberField(s *schema.Struct, key string) *schema.Field {
	for i := range s.Fields {
		f := &s.Fields[i]
		if strings.EqualFold(key, f.Name) || strings.EqualFold(key, f.Property) || key == strconv.FormatUint(uint64(f.ID), 10) {
			return f
		}
	}
	return nil
}

// encodeBy encodes raw by f, or, when f is untyped and the snapshot types
// the value as t, through the untyped encoder in t's shape (Q20).
func encodeBy(f client.Field, t *schema.Type, raw json.RawMessage) (client.Encodable, error) {
	if f.Typed() || t == nil {
		return client.EncodeJSON(f, raw)
	}
	v, err := decodeStrict(raw)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(untypedForm(*t, v))
	if err != nil {
		return nil, err
	}
	return client.EncodeJSON(f, b)
}

func decodeStrict(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.Join(client.ErrJSONValue, err)
	}
	return v, nil
}

// encodeAttribute encodes the value of a write to an attribute: by the
// attribute's type when the model knows it, untyped otherwise. timed
// reports an attribute whose write must be timed.
func encodeAttribute(cluster, attribute uint32, raw json.RawMessage) (value client.Encodable, timed bool, err error) {
	var t *schema.Type
	if a := schema.ClusterByID(cluster).Attribute(attribute); a != nil {
		timed, t = a.Timed, &a.Type
	}
	value, err = encodeBy(client.AttributeField(cluster, attribute), t, raw)
	return value, timed, err
}

// encodeCommand encodes a request command's payload, an object of its
// fields by name or decimal id; nil, empty or null is the empty payload,
// and a mandatory field left out takes matter.js's fallback
// (client.EncodeJSON).
func encodeCommand(cluster, command uint32, raw json.RawMessage) (client.Encodable, error) {
	if t := bytes.TrimSpace(raw); len(t) == 0 || string(t) == "null" {
		raw = json.RawMessage("{}")
	}
	var t *schema.Type
	if c := schema.ClusterByID(cluster).Command(command); c != nil {
		t = structOf(c.Fields)
	}
	return encodeBy(client.CommandField(cluster, command), t, raw)
}

// decodeAny reads a rendered value back as a Go value, numbers as
// json.Number.
func decodeAny(raw json.RawMessage) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	return v
}

// uintOf reads a rendered integer.
func uintOf(raw json.RawMessage) (uint64, bool) {
	v, err := strconv.ParseUint(string(bytes.TrimSpace(raw)), 10, 64)
	return v, err == nil
}

// numberIs reports whether a decoded JSON value is the integer want.
func numberIs(v any, want uint64) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	u, err := strconv.ParseUint(n.String(), 10, 64)
	return err == nil && u == want
}
