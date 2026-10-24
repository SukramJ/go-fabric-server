// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"

	"github.com/SukramJ/go-fabric-server/internal/wire"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

func u(tag tlv.Tag, v uint64) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tag, Type: tlv.TypeUnsignedInt1, Uint: v}}
}

func structure(tag tlv.Tag, children ...spec.Node) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tag, Type: tlv.TypeStructure, IsContainer: true}, Children: children}
}

func array(children ...spec.Node) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tlv.AnonymousTag(), Type: tlv.TypeArray, IsContainer: true}, Children: children}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	return string(protocol.MustEncode(v))
}

func TestAttributeRendering(t *testing.T) {
	desc := schema.ClusterByName("Descriptor")
	dtl := desc.AttributeByName("DeviceTypeList")
	agg := schema.DeviceTypeByName("Aggregator")
	v := wire.Attribute(desc.ID, dtl.ID, array(structure(tlv.AnonymousTag(), u(tlv.ContextTag(0), uint64(agg.ID)), u(tlv.ContextTag(1), 2), u(tlv.ContextTag(9), 1))))
	if got := jsonOf(t, v); got != `[{"0":14,"1":2}]` {
		t.Fatalf("tag-based struct, unknown member left out: %s", got)
	}
	// An unknown cluster: tag keys, bytes base64, FeatureMap {}.
	fm, _ := schema.GlobalAttributeID("FeatureMap")
	if got := jsonOf(t, wire.Attribute(0xFFF1FC01, fm, u(tlv.AnonymousTag(), 3))); got != `{}` {
		t.Fatalf("FeatureMap of an unknown cluster: %s", got)
	}
	raw := structure(tlv.AnonymousTag(), u(tlv.ContextTag(1), 5), spec.Node{El: tlv.Element{Tag: tlv.ContextTag(3), Type: tlv.TypeOctetStr1, Octets: []byte{1, 2}}})
	if got := jsonOf(t, wire.Attribute(0xFFF1FC01, 0, raw)); got != `{"1":5,"3":"AQI="}` {
		t.Fatalf("untyped struct: %s", got)
	}
	// A known cluster's FeatureMap is a number; null is null.
	onoff := schema.ClusterByName("OnOff")
	if got := jsonOf(t, wire.Attribute(onoff.ID, fm, u(tlv.AnonymousTag(), 1))); got != `1` {
		t.Fatalf("FeatureMap: %s", got)
	}
	null := spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}}
	if got := jsonOf(t, wire.Attribute(onoff.ID, onoff.AttributeByName("StartUpOnOff").ID, null)); got != `null` {
		t.Fatalf("null: %s", got)
	}
	// Signed, float, string, bool.
	if got := jsonOf(t, wire.Untyped(spec.Node{El: tlv.Element{Type: tlv.TypeSignedInt2, Int: -5}})); got != `-5` {
		t.Fatal(got)
	}
	if got := jsonOf(t, wire.Untyped(spec.Node{El: tlv.Element{Type: tlv.TypeFloat8, Float: 1.5}})); got != `1.5` {
		t.Fatal(got)
	}
	if got := jsonOf(t, wire.Untyped(array(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "a"}}, spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}}))); got != `["a",true]` {
		t.Fatal(got)
	}
	// A value of the wrong TLV type renders untyped rather than lying.
	if got := jsonOf(t, wire.Attribute(onoff.ID, onoff.AttributeByName("OnOff").ID, u(tlv.AnonymousTag(), 7))); got != `7` {
		t.Fatal(got)
	}
}

func TestFieldsByName(t *testing.T) {
	sw := schema.ClusterByName("Switch")
	ev := sw.Event(1) // InitialPress
	got := jsonOf(t, wire.Fields(ev.Fields, structure(tlv.AnonymousTag(), u(tlv.ContextTag(0), 1))))
	if got != `{"newPosition":1}` {
		t.Fatalf("name-based: %s", got)
	}
	if got := jsonOf(t, wire.Fields(nil, structure(tlv.AnonymousTag(), u(tlv.ContextTag(4), 1)))); got != `{"4":1}` {
		t.Fatalf("no model: %s", got)
	}
}

func TestEncode(t *testing.T) {
	onoff := schema.ClusterByName("OnOff")
	n, timed, err := wire.EncodeAttribute(onoff.ID, onoff.AttributeByName("OnTime").ID, json.RawMessage(`10`))
	if err != nil || timed || n.El.Uint != 10 {
		t.Fatalf("%+v %v %v", n.El, timed, err)
	}
	if _, _, err := wire.EncodeAttribute(onoff.ID, onoff.AttributeByName("OnTime").ID, json.RawMessage(`"x"`)); !errors.Is(err, wire.ErrValue) {
		t.Fatalf("a string for a uint16: %v", err)
	}
	if _, _, err := wire.EncodeAttribute(onoff.ID, onoff.AttributeByName("OnTime").ID, json.RawMessage(`70000`)); err == nil {
		t.Fatal("out of range")
	}
	// An unknown attribute: untyped.
	if n, _, err := wire.EncodeAttribute(0xFFF1FC01, 1, json.RawMessage(`{"1": 5}`)); err != nil || n.El.Type != tlv.TypeStructure {
		t.Fatalf("%+v %v", n.El, err)
	}
	lc := schema.ClusterByName("LevelControl")
	cmd := lc.CommandByName("moveToLevelWithOnOff")
	if cmd == nil {
		t.Fatal("command by its camelCase name")
	}
	p, err := wire.EncodeFields(cmd.Fields, json.RawMessage(`{"level":128,"TransitionTime":10,"optionsMask":0,"optionsOverride":0}`))
	if err != nil || len(p.Children) != 4 || p.Children[0].El.Uint != 128 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := wire.EncodeFields(cmd.Fields, json.RawMessage(`{"level":128}`)); err == nil {
		t.Fatal("a missing mandatory field")
	}
	if p, err := wire.EncodeFields(nil, nil); err != nil || p.El.Type != tlv.TypeStructure || len(p.Children) != 0 {
		t.Fatalf("the empty payload: %+v %v", p, err)
	}
	// Bytes are base64; a null for an optional, not nullable field is left
	// out (Converters.ts:632-636).
	oc := schema.ClusterByName("OperationalCredentials")
	csr := oc.CommandByName("CSRRequest")
	p, err = wire.EncodeFields(csr.Fields, json.RawMessage(`{"csrNonce":"AQID","isForUpdateNoc":null}`))
	if err != nil || len(p.Children) != 1 || !reflect.DeepEqual(p.Children[0].El.Octets, []byte{1, 2, 3}) {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestParsePath(t *testing.T) {
	p, err := wire.ParsePath("1/6/0", 3, true)
	if err != nil || *p.Endpoint != 1 || *p.Cluster != 6 || *p.Element != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = wire.ParsePath("*/6/*", 3, true)
	if err != nil || p.Endpoint != nil || p.Element != nil || *p.Cluster != 6 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = wire.ParsePath("65535/4294967295/x", 3, false)
	if err != nil || p.Endpoint != nil || p.Cluster != nil || p.Element != nil {
		t.Fatalf("wildcard ids and a lenient non-number: %+v %v", p, err)
	}
	for _, bad := range []string{"1/6", "1/x/0", "1/6/0/1", ""} {
		if _, err := wire.ParsePath(bad, 3, true); err == nil {
			t.Fatalf("%q strict", bad)
		}
	}
	if _, err := wire.ParsePath("70000/6/0", 3, false); err == nil {
		t.Fatal("an endpoint out of range")
	}
	if _, err := wire.ParsePath("1/99999999999/0", 3, false); err == nil {
		t.Fatal("a cluster out of range")
	}
	if wire.Key(1, 6, 0) != "1/6/0" || wire.ClusterKey(2, 29) != "2/29" {
		t.Fatal("keys")
	}
}

func FuzzParsePath(f *testing.F) {
	for _, s := range []string{"1/6/0", "*/6/*", "65535/x/1", "1//", "/"} {
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, strict bool) {
		_, _ = wire.ParsePath(s, 3, strict)
	})
}

func FuzzEncodeFields(f *testing.F) {
	for _, s := range []string{`{}`, `{"level":1,"transitionTime":null,"optionsMask":0,"optionsOverride":0}`, `[1]`, `{"level":"x"}`, `null`} {
		f.Add([]byte(s))
	}
	cmd := schema.ClusterByName("LevelControl").CommandByName("MoveToLevel")
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = wire.EncodeFields(cmd.Fields, b)
		_, _ = wire.EncodeUntyped(b)
	})
}
