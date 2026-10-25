// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

func TestParsePath(t *testing.T) {
	p, err := parsePath("1/6/0", 3, true)
	if err != nil || *p.Endpoint != 1 || *p.Cluster != 6 || *p.Element != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = parsePath("*/6/*", 3, true)
	if err != nil || p.Endpoint != nil || p.Element != nil || *p.Cluster != 6 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = parsePath("65535/4294967295/x", 3, false)
	if err != nil || p.Endpoint != nil || p.Cluster != nil || p.Element != nil {
		t.Fatalf("wildcard ids and a lenient non-number: %+v %v", p, err)
	}
	for _, bad := range []string{"1/6", "1/x/0", "1/6/0/1", ""} {
		if _, err := parsePath(bad, 3, true); err == nil {
			t.Fatalf("%q strict", bad)
		}
	}
	if _, err := parsePath("70000/6/0", 3, false); err == nil {
		t.Fatal("an endpoint out of range")
	}
	if _, err := parsePath("1/99999999999/0", 3, false); err == nil {
		t.Fatal("a cluster out of range")
	}
	if attrKey(1, 6, 0) != "1/6/0" || clusterKey(2, 29) != "2/29" {
		t.Fatal("keys")
	}
}

// TestEncodeValues holds the server's use of go-fabric's JSON route:
// typed and untyped writes, the mandatory-field fallback of a command's
// fields, the empty payload, a value that does not fit.
func TestEncodeValues(t *testing.T) {
	onoff := schema.ClusterByName("OnOff")
	onTime := onoff.AttributeByName("OnTime").ID
	if _, timed, err := encodeAttribute(onoff.ID, onTime, json.RawMessage(`10`)); err != nil || timed {
		t.Fatalf("%v %v", timed, err)
	}
	if _, _, err := encodeAttribute(onoff.ID, onTime, json.RawMessage(`"x"`)); !errors.Is(err, client.ErrJSONValue) {
		t.Fatalf("a string for a uint16: %v", err)
	}
	if _, _, err := encodeAttribute(onoff.ID, onTime, json.RawMessage(`70000`)); err == nil {
		t.Fatal("out of range")
	}
	if _, _, err := encodeAttribute(0xFFF1FC01, 1, json.RawMessage(`{"1": 5}`)); err != nil {
		t.Fatalf("untyped: %v", err)
	}
	lc := schema.ClusterByName("LevelControl")
	cmd := lc.CommandByName("moveToLevelWithOnOff")
	if _, err := encodeCommand(lc.ID, cmd.ID, json.RawMessage(`{"level":128,"TransitionTime":10,"optionsMask":0,"optionsOverride":0}`)); err != nil {
		t.Fatal(err)
	}
	// A mandatory field left out takes matter.js's fallback (go-fabric
	// v0.12.1); an unknown one is refused.
	if _, err := encodeCommand(lc.ID, cmd.ID, json.RawMessage(`{"level":128}`)); err != nil {
		t.Fatalf("the fallback of the options: %v", err)
	}
	if _, err := encodeCommand(lc.ID, cmd.ID, json.RawMessage(`{"level":128,"nope":1}`)); err == nil {
		t.Fatal("an unknown field")
	}
	if _, err := encodeCommand(onoff.ID, onoff.CommandByName("On").ID, nil); err != nil {
		t.Fatalf("the empty payload: %v", err)
	}
	if got := string(attributeJSON(0xFFF1FC01, featureMapID, client.Value{})); got != `{}` {
		t.Fatalf("FeatureMap of an unknown cluster: %s", got)
	}
}

func FuzzParsePath(f *testing.F) {
	for _, s := range []string{"1/6/0", "*/6/*", "65535/x/1", "1//", "/"} {
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, strict bool) {
		_, _ = parsePath(s, 3, strict)
	})
}

func FuzzEncodeFields(f *testing.F) {
	for _, s := range []string{`{}`, `{"level":1,"transitionTime":null,"optionsMask":0,"optionsOverride":0}`, `[1]`, `{"level":"x"}`, `null`} {
		f.Add([]byte(s))
	}
	lc := schema.ClusterByName("LevelControl")
	cmd := lc.CommandByName("MoveToLevel")
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = encodeCommand(lc.ID, cmd.ID, b)
		_, _, _ = encodeAttribute(0xFFF1FC01, 1, b)
	})
}
