// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
	"github.com/SukramJ/go-fabric/cluster/spec"
)

// ErrValue is a JSON value that does not fit its type; the message says
// where and why.
var ErrValue = errors.New("wire: value does not fit its type")

// EncodeFields encodes a command's request payload, an object of its
// fields by name (camelCase or the snapshot's PascalCase, compared without
// regard to case) or by decimal field id. It is matterjs-server's
// convertCommandDataToMatter (Converters.ts:183-231): a null for an
// optional field that is not nullable counts as left out, bytes are
// base64. A nil or empty raw is the empty payload. The encoding itself is
// go-fabric's (spec.EncodeJSON).
func EncodeFields(fields []schema.Field, raw json.RawMessage) (spec.Node, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		raw = json.RawMessage("{}")
	}
	t := schema.Type{Meta: schema.MetaStruct, Struct: &schema.Struct{Fields: fields}}
	return Encode(t, false, raw)
}

// Encode encodes raw as a value of t: an attribute's value of a write.
func Encode(t schema.Type, nullable bool, raw json.RawMessage) (spec.Node, error) {
	v, err := decodeJSON(raw)
	if err != nil {
		return spec.Node{}, err
	}
	v = prepare(t, v)
	b, err := json.Marshal(v)
	if err != nil {
		return spec.Node{}, err
	}
	f := &spec.Field{Type: specType(t), Quality: spec.Quality{Nullable: nullable}}
	n, err := spec.EncodeJSON(f, b)
	if err != nil {
		return spec.Node{}, errors.Join(ErrValue, err)
	}
	return n, nil
}

// EncodeAttribute encodes the value of a write to an attribute: by the
// attribute's type when the snapshot knows it, untyped otherwise. timed
// reports an attribute whose write must be timed.
func EncodeAttribute(cluster, attribute uint32, raw json.RawMessage) (n spec.Node, timed bool, err error) {
	a := schema.ClusterByID(cluster).Attribute(attribute)
	if a == nil {
		n, err = EncodeUntyped(raw)
		return n, false, err
	}
	n, err = Encode(a.Type, a.Nullable, raw)
	return n, a.Timed, err
}

// EncodeUntyped encodes raw without a model — a value of a cluster or
// attribute the snapshot does not know, in go-fabric's untyped JSON form
// (an object is a struct keyed by context tag).
func EncodeUntyped(raw json.RawMessage) (spec.Node, error) {
	n, err := spec.EncodeJSON(nil, raw)
	if err != nil {
		return spec.Node{}, errors.Join(ErrValue, err)
	}
	return n, nil
}

func decodeJSON(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.Join(ErrValue, err)
	}
	return v, nil
}

// prepare rewrites a decoded JSON value of the protocol into the form
// spec.EncodeJSON reads: base64 bytes get the "base64:" prefix, a null for
// an optional non-nullable struct member is dropped.
func prepare(t schema.Type, v any) any {
	switch t.Meta {
	case schema.MetaBytes:
		if s, ok := v.(string); ok {
			return "base64:" + s
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
			out[k] = prepare(f.Type, val)
		}
		return out
	case schema.MetaList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return v
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = prepare(*t.Entry, e)
		}
		return out
	case schema.MetaAny, schema.MetaBool, schema.MetaUint, schema.MetaInt, schema.MetaFloat, schema.MetaDouble,
		schema.MetaString, schema.MetaEnum, schema.MetaBitmap:
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

var structs sync.Map // *schema.Struct → *spec.Struct

func specType(t schema.Type) spec.Type {
	st := spec.Type{Name: t.Name, Bits: t.Bits}
	switch t.Meta {
	case schema.MetaBool:
		st.Kind = spec.KindBool
	case schema.MetaUint:
		st.Kind = spec.KindUint
	case schema.MetaInt:
		st.Kind = spec.KindInt
	case schema.MetaFloat:
		st.Kind = spec.KindFloat32
	case schema.MetaDouble:
		st.Kind = spec.KindFloat64
	case schema.MetaString:
		st.Kind = spec.KindString
	case schema.MetaBytes:
		st.Kind = spec.KindBytes
	case schema.MetaEnum:
		st.Kind = spec.KindEnum
	case schema.MetaBitmap:
		st.Kind = spec.KindBitmap
	case schema.MetaStruct:
		st.Kind = spec.KindStruct
		if t.Struct != nil {
			st.Struct = specStruct(t.Struct)
		}
	case schema.MetaList:
		st.Kind = spec.KindList
		if t.Entry != nil {
			st.Entry = &spec.Field{Type: specType(*t.Entry)}
		}
	case schema.MetaAny:
		st.Kind = spec.KindAny
	}
	return st
}

// specStruct converts a struct of the tables, cached by its pointer; an
// anonymous struct (a payload EncodeFields wraps) is built each time.
func specStruct(s *schema.Struct) *spec.Struct {
	if s.Name == "" {
		return buildStruct(s)
	}
	if v, ok := structs.Load(s); ok {
		return v.(*spec.Struct) //nolint:errcheck,forcetypeassert // the map holds only *spec.Struct
	}
	v, _ := structs.LoadOrStore(s, buildStruct(s))
	return v.(*spec.Struct) //nolint:errcheck,forcetypeassert // as above
}

func buildStruct(s *schema.Struct) *spec.Struct {
	out := &spec.Struct{Name: s.Name, Fields: make([]spec.Field, len(s.Fields))}
	for i, f := range s.Fields {
		sf := spec.Field{ID: f.ID, Name: f.Name, Type: specType(f.Type), Quality: spec.Quality{Nullable: f.Nullable}}
		if f.Mandatory {
			sf.Conformance = spec.Conformance{Text: "M", Op: spec.ConfMandatory}
		}
		out.Fields[i] = sf
	}
	return out
}
