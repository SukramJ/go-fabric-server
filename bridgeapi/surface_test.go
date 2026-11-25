// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/surface"

	"thingfabric.dev/go-fabric-server/bridgeapi"
	fsclient "thingfabric.dev/go-fabric-server/client"
	"thingfabric.dev/go-fabric-server/protocol"
	"thingfabric.dev/go-fabric-server/protocol/schema"
)

// schedulesRow is the "Ahead of matter.js" register ID of Thermostat's
// MatterScheduleConfiguration feature (go-fabric cluster/thermo/ahead.go).
const schedulesRow = "BD-chip-Thermostat-Schedules"

// schedulesDecl selects the feature with the attributes it mandates
// (ScheduleTypes: 1 to 3 entries, systemMode 4 is Heat, its
// scheduleTypeFeatures 2 is SupportsSetpoints — a type that supports
// neither presets nor setpoints is refused since go-fabric v0.25.0;
// NumberOfScheduleTransitions: min 1).
const schedulesDecl = `{"devices":[{"key":"t","type":"Thermostat","clusters":{"Thermostat":{
 "features":["Heating","MatterScheduleConfiguration"],
 "initial":{"scheduleTypes":[{"systemMode":4,"numberOfSchedules":1,"scheduleTypeFeatures":2}],"numberOfSchedules":1,"numberOfScheduleTransitions":4,"numberOfScheduleTransitionPerDay":2}}}}]}`

// surfaceReport is the surface command's answer, decoded by
// surface.Report's own UnmarshalJSON.
func surfaceReport(t *testing.T, e *env) surface.Report {
	t.Helper()
	var rep surface.Report
	if err := e.host.Call(e.ctx, "surface", nil, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestSurfaceExtensionRefused: under the zero policies a declaration that
// selects an ahead-of-matter.js feature is refused with InvalidArguments
// naming the device, the register ID and the flag that enables it; the
// surface command reports the pin's level and no extension.
func TestSurfaceExtensionRefused(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	_, err := e.host.Declare(e.ctx, doc(schedulesDecl))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.ErrorCode != protocol.InvalidArguments {
		t.Fatalf("declare: %v", err)
	}
	for _, want := range []string{"devices[0] (t)", schedulesRow, "enable them with --bridge-extensions " + schedulesRow + " (", "Ahead-of-matter.js register"} {
		if !strings.Contains(perr.Details, want) {
			t.Errorf("details %q lack %q", perr.Details, want)
		}
	}
	rep := surfaceReport(t, e)
	if rep.Level != surface.PinLevel() || rep.LevelOrigin != "pin" || len(rep.Extensions) != 0 {
		t.Fatalf("report level %q (%s), extensions %v", rep.Level, rep.LevelOrigin, rep.Extensions)
	}
}

// TestSurfaceExtensionEnabled: the same declaration under a node that
// enables the register ID is accepted, and the surface command lists the
// ID and keeps the feature.
func TestSurfaceExtensionEnabled(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Extensions: surface.NewExtensions(schedulesRow)}})
	res, err := e.host.Declare(e.ctx, doc(schedulesDecl))
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Error != "" {
		t.Fatalf("declare: %+v %v", res, err)
	}
	rep := surfaceReport(t, e)
	if rep.Level != surface.PinLevel() || !slices.Equal(rep.Extensions, []string{schedulesRow}) {
		t.Fatalf("report level %q, extensions %v", rep.Level, rep.Extensions)
	}
	if !slices.ContainsFunc(rep.Decisions, func(d surface.Decision) bool {
		return d.Endpoint == "t" && d.Reference.Ahead == schedulesRow && d.Verdict == surface.Keep
	}) {
		t.Errorf("no kept %s decision on t: %+v", schedulesRow, rep.Decisions)
	}
}

// withheldDecl declares a Closure, a type absent from the 1.4.2 model
// (go-fabric device/surface_level_test.go TestLevelWithholdsAnUnknownType),
// beside a light every level knows.
const withheldDecl = `{"devices":[{"key":"c","type":"Closure"},{"key":"l","type":"OnOffLight"}]}`

// withheldAt reports whether rep lists a withheld element of endpoint key
// and whether its effective surface serves key.
func withheldAt(rep surface.Report, key string) (withheld, served bool) {
	withheld = slices.ContainsFunc(rep.Withheld, func(el surface.Element) bool { return el.Endpoint == key })
	served = slices.ContainsFunc(rep.Effective.Endpoints, func(ep surface.Endpoint) bool { return ep.Key == key })
	return withheld, served
}

// nodeKeys are the keys of the devices node_info lists: the endpoints the
// node serves.
func nodeKeys(t *testing.T, e *env) []string {
	t.Helper()
	ni, err := e.host.NodeInfo(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, d := range ni["devices"].([]any) {
		if k, ok := d.(map[string]any)["key"].(string); ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// TestSurfaceWithheld: a device type the node's specification level does
// not know runs on (go-fabric ADR 0030 decision 3): the declaration
// succeeds, the device is added without an endpoint, the answer's
// withheld list names it with its type and the level, the node serves
// only the light and the surface report lists the Closure withheld. The
// same declaration again leaves it as it is and still names it.
func TestSurfaceWithheld(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Level: "1.4.2"}})
	for _, want := range []string{"added", "unchanged"} {
		res, err := e.host.Declare(e.ctx, doc(withheldDecl))
		if err != nil || len(res.Devices) != 2 {
			t.Fatalf("declare: %+v %v", res, err)
		}
		c, l := res.Devices[0], res.Devices[1]
		if c.Key != "c" || c.Action != want || c.Error != "" || c.Endpoint != nil {
			t.Errorf("c: %+v, want %s without endpoint", c, want)
		}
		if l.Key != "l" || l.Action != want || l.Error != "" || l.Endpoint == nil {
			t.Errorf("l: %+v, want %s with endpoint", l, want)
		}
		if !slices.Equal(res.Withheld, []fsclient.WithheldDevice{{Key: "c", DeviceType: "Closure", SpecLevel: "1.4.2"}}) {
			t.Errorf("withheld %+v", res.Withheld)
		}
	}
	if keys := nodeKeys(t, e); !slices.Equal(keys, []string{"l"}) {
		t.Errorf("node_info keys %v, want [l]", keys)
	}
	rep := surfaceReport(t, e)
	if rep.Level != "1.4.2" || rep.LevelOrigin != "explicit" {
		t.Fatalf("report level %q (%s)", rep.Level, rep.LevelOrigin)
	}
	if withheld, served := withheldAt(rep, "c"); !withheld || served {
		t.Errorf("c withheld %v, served %v; report withheld %+v", withheld, served, rep.Withheld)
	}
	if withheld, served := withheldAt(rep, "l"); withheld || !served {
		t.Errorf("l withheld %v, served %v", withheld, served)
	}
}

// TestSurfaceWithheldRemoved: a later declaration without the withheld
// device removes it as any other key — named in removed, gone from the
// withheld list — and the light stays.
func TestSurfaceWithheldRemoved(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Level: "1.4.2"}})
	if _, err := e.host.Declare(e.ctx, doc(withheldDecl)); err != nil {
		t.Fatal(err)
	}
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"l","type":"OnOffLight"}]}`))
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Action != "unchanged" {
		t.Fatalf("declare: %+v %v", res, err)
	}
	if !slices.Equal(res.Removed, []string{"c"}) || len(res.Withheld) != 0 {
		t.Errorf("removed %v, withheld %+v", res.Removed, res.Withheld)
	}
	if keys := nodeKeys(t, e); !slices.Equal(keys, []string{"l"}) {
		t.Errorf("node_info keys %v, want [l]", keys)
	}
	if err := e.host.Set(e.ctx, "c", "OnOff", map[string]any{"onOff": true}); err == nil {
		t.Error("set on the removed key: no error")
	}
}

// TestSurfaceWithheldReplaced: a key moves between a withheld and a
// served device by re-declaration: served, it gets an endpoint; withheld
// again, it leaves the node and the withheld list names it; served once
// more, it gets the endpoint number it had (the removal reserved it);
// remove and forget of the withheld key answer as for any key.
func TestSurfaceWithheldReplaced(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Level: "1.4.2"}})
	steps := []struct {
		decl     string
		endpoint bool
		keys     []string
	}{
		{`{"devices":[{"key":"c","type":"Closure"}]}`, false, nil},
		{`{"devices":[{"key":"c","type":"OnOffLight"}]}`, true, []string{"c"}},
		{`{"devices":[{"key":"c","type":"Closure"}]}`, false, nil},
		{`{"devices":[{"key":"c","type":"OnOffLight"}]}`, true, []string{"c"}},
	}
	var endpoints []uint16
	for i, st := range steps {
		res, err := e.host.Declare(e.ctx, doc(st.decl))
		if err != nil || len(res.Devices) != 1 || res.Devices[0].Error != "" {
			t.Fatalf("step %d: declare: %+v %v", i, res, err)
		}
		if got := res.Devices[0].Endpoint != nil; got != st.endpoint {
			t.Errorf("step %d: endpoint %v, want %v", i, got, st.endpoint)
		} else if got {
			endpoints = append(endpoints, *res.Devices[0].Endpoint)
		}
		if got := len(res.Withheld) == 1; got == st.endpoint {
			t.Errorf("step %d: withheld %+v", i, res.Withheld)
		}
		if keys := nodeKeys(t, e); !slices.Equal(keys, st.keys) {
			t.Errorf("step %d: node_info keys %v, want %v", i, keys, st.keys)
		}
	}
	if len(endpoints) != 2 || endpoints[0] != endpoints[1] {
		t.Errorf("endpoints served, withheld, served: %v, want one number", endpoints)
	}
	if err := e.host.Remove(e.ctx, "c"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := e.host.Forget(e.ctx, "c"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"c","type":"Closure"}]}`))
	if err != nil || res.Devices[0].Action != "added" || len(res.Withheld) != 1 {
		t.Fatalf("declare after forget: %+v %v", res, err)
	}
}

// TestSurfaceReplaceRefusedKeepsServed: a served device replaced by a
// withheld one in one step (go-fabric #585, Batch.Replace): when the
// replacement is refused, the served device stays — on the node at its
// number and declared here — and the answer names the error and removes
// nothing.
func TestSurfaceReplaceRefusedKeepsServed(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Level: "1.4.2"}})
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"c","type":"OnOffLight"},{"key":"d","type":"OnOffLight"}]}`))
	if err != nil || res.Devices[0].Endpoint == nil {
		t.Fatalf("declare: %+v %v", res, err)
	}
	first := *res.Devices[0].Endpoint
	refused := errors.New("the replacement is refused")
	restore := bridgeapi.SetBatchReplace(func(*node.Batch, device.SourceKey, node.Endpoint) error { return refused })
	res, err = e.host.Declare(e.ctx, doc(`{"devices":[{"key":"c","type":"Closure"},{"key":"d","type":"OnOffLight"}]}`))
	restore()
	if err != nil || !strings.Contains(res.Devices[0].Error, refused.Error()) || len(res.Removed) != 0 || len(res.Withheld) != 0 {
		t.Fatalf("the refused replace: %+v %v", res, err)
	}
	if keys := nodeKeys(t, e); !slices.Equal(keys, []string{"c", "d"}) {
		t.Fatalf("node_info keys %v, want [c d]", keys)
	}
	if err := e.host.Set(e.ctx, "c", "OnOff", map[string]any{"onOff": true}); err != nil {
		t.Errorf("set on the kept light: %v", err)
	}
	// The light is still the declared device: declared again, it is
	// unchanged and keeps its number.
	res, err = e.host.Declare(e.ctx, doc(`{"devices":[{"key":"c","type":"OnOffLight"},{"key":"d","type":"OnOffLight"}]}`))
	if err != nil || res.Devices[0].Action != "unchanged" || res.Devices[0].Endpoint == nil || *res.Devices[0].Endpoint != first {
		t.Fatalf("declare after the refused replace: %+v %v (the number %d)", res, err, first)
	}
}

// foreignLight adds an OnOffLight under key to the node's Aggregator
// directly, past the bridge half: an endpoint the half did not declare.
func foreignLight(t *testing.T, e *env, key string) node.Endpoint {
	t.Helper()
	typ, ok := device.Lookup(schema.DeviceTypeByName("OnOffLight").ID)
	if !ok {
		t.Fatal("no OnOffLight in the kit")
	}
	dev, err := device.New(typ, device.Options{StableKey: device.StringKey(key)})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dev.Node().Aggregator().Add(e.ctx, dev); err != nil {
		t.Fatal(err)
	}
	return dev
}

// TestDeclareCommitRefusal: go-fabric v0.26.0 judges a Batch at its
// commit, so an Add of a key another endpoint serves returns nil at the
// call and is refused with ErrDuplicateKey when the batch commits. The
// declaration answers that device's error and the others' results, the
// half does not take the refused device as declared, and a later
// declaration without the key leaves the other endpoint alone.
func TestDeclareCommitRefusal(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	foreign := foreignLight(t, e, "x")
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"x","type":"OnOffLight"},{"key":"l","type":"OnOffLight"}]}`))
	if err != nil || len(res.Devices) != 2 {
		t.Fatalf("declare: %+v %v", res, err)
	}
	x, l := res.Devices[0], res.Devices[1]
	if x.Action != "added" || x.Endpoint != nil || !strings.Contains(x.Error, node.ErrDuplicateKey.Error()) {
		t.Errorf("x: %+v, want added with the duplicate key refusal", x)
	}
	if l.Action != "added" || l.Error != "" || l.Endpoint == nil {
		t.Errorf("l: %+v, want added with an endpoint", l)
	}
	if err := e.host.Set(e.ctx, "x", "OnOff", map[string]any{"onOff": true}); err == nil {
		t.Error("set on the refused key: no error")
	}
	res, err = e.host.Declare(e.ctx, doc(`{"devices":[{"key":"l","type":"OnOffLight"}]}`))
	if err != nil || len(res.Removed) != 0 {
		t.Fatalf("declare without x: %+v %v", res, err)
	}
	if !slices.Contains(e.dev.Node().Aggregator().Endpoints(), foreign) {
		t.Error("the foreign endpoint left the node")
	}
}

// sliceEndpoint is a host endpoint that is a value Go cannot compare (a
// slice field), which go-fabric's Aggregator takes (CHANGELOG.md:171 at
// v0.26.0).
type sliceEndpoint struct {
	*device.Device
	tags []string
}

// TestDeclareBesideNonComparableEndpoint: a declaration on a node whose
// Aggregator also holds an endpoint that cannot be compared answers as
// usual (review S15 L1: the half's served set must not hash it).
func TestDeclareBesideNonComparableEndpoint(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	typ, _ := device.Lookup(schema.DeviceTypeByName("OnOffLight").ID)
	dev, err := device.New(typ, device.Options{StableKey: device.StringKey("y")})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dev.Node().Aggregator().Add(e.ctx, sliceEndpoint{Device: dev, tags: []string{"t"}}); err != nil {
		t.Fatal(err)
	}
	res, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"l","type":"OnOffLight"}]}`))
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Error != "" || res.Devices[0].Endpoint == nil {
		t.Fatalf("declare: %+v %v", res, err)
	}
}

// TestDeclareForgetRefusedAtCall: a key the document no longer names whose
// Forget fails at its call stays declared — in the next listening
// client's dump — and the declare answers UnknownError naming the
// failure (review S15 L2); a later declare forgets it.
func TestDeclareForgetRefusedAtCall(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{})
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight"},{"key":"b","type":"OnOffLight"}]}`)); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("the forget is refused")
	restore := bridgeapi.SetBatchForget(func(*node.Batch, device.SourceKey) error { return refused })
	_, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight"}]}`))
	restore()
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.ErrorCode != protocol.UnknownError || !strings.Contains(perr.Details, refused.Error()) {
		t.Fatalf("declare with the refused forget: %v", err)
	}
	e.host.Close()
	time.Sleep(50 * time.Millisecond)
	h := e.dialBridge(fsclient.Options{})
	var keys []string
	for _, d := range h.Devices() {
		keys = append(keys, d.Key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"a", "b"}) {
		t.Fatalf("dump keys %v, want [a b]", keys)
	}
	res, err := h.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight"}]}`))
	if err != nil || !slices.Equal(res.Removed, []string{"b"}) {
		t.Fatalf("declare after: %+v %v", res, err)
	}
}
