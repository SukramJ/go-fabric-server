// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// TestEveryKitReads holds the reading of every typed package against the
// snapshot: the Patch sets exactly the cluster's attributes below the
// globals, every Handle field names a request command, and a declaration
// with every hook builds.
func TestEveryKitReads(t *testing.T) {
	if len(kits) < 100 {
		t.Fatalf("%d kits: the generated table is short", len(kits))
	}
	for id, k := range kits {
		if err := k.err(); err != nil {
			t.Errorf("0x%04X: %v", id, err)
			continue
		}
		cl := k.cluster()
		// Every attribute the Patch sets is the snapshot's, below the
		// globals; a cluster whose lists are fabric-scoped (Binding) sets
		// none.
		for _, id := range k.patchAttributes() {
			if a := cl.Attribute(id); a == nil || id >= 0xF000 {
				t.Errorf("%s: the Patch sets 0x%04X, not an attribute of the snapshot", cl.Name, id)
			}
		}
		h := &hooks{device: "d", cluster: cl.Name, writes: true}
		for _, c := range cl.Commands {
			if c.Request && k.handles(c.ID) {
				h.commands = append(h.commands, c.ID)
			}
		}
		p, err := k.decodePatch(nil, "initial")
		if err != nil {
			t.Fatalf("%s: %v", cl.Name, err)
		}
		if _, err := k.declare(p, 0, h); err != nil {
			t.Errorf("%s: %v", cl.Name, err)
		}
	}
}

func TestDecodePatch(t *testing.T) {
	k, err := kitByName("powersource")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ in, err string }{
		{`{"batPercentRemaining": 180, "Order": 0, "description": "Battery"}`, ""},
		{`{"batpercentremaining": 180}`, ""},
		{`{"BatTimeRemaining": null}`, ""},
		{`{"nope": 1}`, `initial.nope: the PowerSource cluster has no attribute "nope"`},
		{`{"order": null}`, "initial.order: the attribute is not nullable"},
		{`{"order": 300}`, "initial.order: number 300 is not a valid unsigned 8-bit integer"},
		{`{"order": 1, "Order": 2}`, `the attribute is also given as`},
		{`{"null": [1]}`, `has no attribute "null"`},
		{`[1]`, "a patch is an object"},
	} {
		_, err := k.decodePatch(json.RawMessage(c.in), "initial")
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", c.in, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: %v, want %q", c.in, err, c.err)
		}
	}
	// A struct member the datatype does not have.
	epm, _ := kitByName("ElectricalPowerMeasurement")
	if _, err := epm.decodePatch(json.RawMessage(`{"accuracy":[{"measurementType":5,"bogus":1}]}`), "initial"); err == nil || !strings.Contains(err.Error(), `unknown field "bogus"`) {
		t.Errorf("nested unknown member: %v", err)
	}
	if _, err := kitByName("AccessControl"); err == nil || !strings.Contains(err.Error(), "no typed package") {
		t.Errorf("AccessControl: %v", err)
	}
	if _, err := kitByName("Nope"); err == nil {
		t.Error("an unknown cluster")
	}
}

func TestWireNames(t *testing.T) {
	cl := schema.ClusterByName("ElectricalPowerMeasurement")
	a := cl.AttributeByName("accuracy")
	type rangeStruct struct{ RangeMin, RangeMax int64 }
	type accuracy struct {
		MeasurementType uint16
		Measured        bool
		AccuracyRanges  []rangeStruct
	}
	got := toWire(a.Type, []accuracy{{MeasurementType: 5, Measured: true, AccuracyRanges: []rangeStruct{{1, 2}}}})
	b, _ := json.Marshal(got)
	if want := `[{"accuracyRanges":[{"rangeMax":2,"rangeMin":1}],"measured":true,"measurementType":5}]`; string(b) != want {
		t.Fatalf("toWire %s, want %s", b, want)
	}
}

func TestHookWithoutHost(t *testing.T) {
	s := New(Options{})
	h := &hooks{s: s, device: "d", cluster: "OnOff", writes: true}
	if err := h.write(context.Background(), 0, true); err != errNoHost { //nolint:errorlint // the sentinel itself
		t.Fatalf("write without a host: %v", err)
	}
}
