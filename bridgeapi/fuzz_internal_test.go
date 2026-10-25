// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"encoding/json"
	"testing"
)

// FuzzDecodeDeclaration: the declaration decoder never panics, and a
// document it accepts names each key once.
func FuzzDecodeDeclaration(f *testing.F) {
	for _, seed := range []string{
		`{"devices":[]}`,
		`{"devices":[{"key":"a","type":"OnOffLight","clusters":{"OnOff":{"initial":{"onOff":true},"handle_writes":true,"handle_commands":["Toggle"]}}}]}`,
		`{"devices":[{"key":"z","type":"ElectricalSensor","clusters":{"PowerTopology":{"features":["NodeTopology"]},"ElectricalPowerMeasurement":{"features":["ALTC"],"initial":{"activePower":-5}},"PowerSource":{"features":["Battery"],"initial":{"batPercentRemaining":180,"batTimeRemaining":null}}}}]}`,
		`{"devices":[{"key":"a","type":"DimmableLight","clusters":{"LevelControl":{"initial":{"currentLevel":null}}}}]}`,
		`{"devices":[{"key":"a","type":"Nope"}]}`,
		`{"devices":{}}`,
		`[]`,
	} {
		f.Add([]byte(seed))
	}
	s := New(Options{})
	f.Fuzz(func(t *testing.T, data []byte) {
		devs, err := s.decodeDeclaration(context.Background(), json.RawMessage(data))
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, b := range devs {
			if seen[b.key] || b.dev == nil {
				t.Fatalf("accepted %s: key %q twice or without a device", data, b.key)
			}
			seen[b.key] = true
			b.release()
		}
	})
}

// FuzzDecodePatch: a patch of every shape decodes or is refused, without
// a panic, for a cluster with lists of structs and nullable attributes.
func FuzzDecodePatch(f *testing.F) {
	for _, seed := range []string{
		`{"activePower":1}`, `{"accuracy":[{"measurementType":2,"accuracyRanges":[{"rangeMin":1}]}]}`,
		`{"voltage":null}`, `{"ACTIVEPOWER":"x"}`, `null`, `{}`, `{"ranges":[{}]}`,
	} {
		f.Add([]byte(seed))
	}
	k, err := kitByName("ElectricalPowerMeasurement")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = k.decodePatch(json.RawMessage(data), "patch")
	})
}
