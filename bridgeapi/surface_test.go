// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	"github.com/SukramJ/go-fabric-server/protocol"
)

// schedulesRow is the "Ahead of matter.js" register ID of Thermostat's
// MatterScheduleConfiguration feature (go-fabric cluster/thermo/ahead.go).
const schedulesRow = "BD-chip-Thermostat-Schedules"

// schedulesDecl selects the feature with the attributes it mandates
// (ScheduleTypes: 1 to 3 entries, systemMode 4 is Heat;
// NumberOfScheduleTransitions: min 1).
const schedulesDecl = `{"devices":[{"key":"t","type":"Thermostat","clusters":{"Thermostat":{
 "features":["Heating","MatterScheduleConfiguration"],
 "initial":{"scheduleTypes":[{"systemMode":4,"numberOfSchedules":1,"scheduleTypeFeatures":0}],"numberOfSchedules":1,"numberOfScheduleTransitions":4,"numberOfScheduleTransitionPerDay":2}}}}]}`

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

// TestSurfaceWithheld: a device type the node's specification level does
// not know (Closure, absent from the 1.4.2 model: go-fabric
// device/surface_level_test.go TestLevelWithholdsAnUnknownType) is
// refused with InvalidArguments naming the level, the flag and the
// endpoint.
func TestSurfaceWithheld(t *testing.T) {
	e := newEnv(t, bridgeapi.Options{Surface: surface.Policies{Level: "1.4.2"}})
	_, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"c","type":"Closure"}]}`))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.ErrorCode != protocol.InvalidArguments {
		t.Fatalf("declare: %v", err)
	}
	for _, want := range []string{"devices[0] (c)", "specification level 1.4.2 (--bridge-spec-level)", `withholds endpoint "c"`} {
		if !strings.Contains(perr.Details, want) {
			t.Errorf("details %q lack %q", perr.Details, want)
		}
	}
	if rep := surfaceReport(t, e); rep.Level != "1.4.2" || rep.LevelOrigin != "explicit" {
		t.Fatalf("report level %q (%s)", rep.Level, rep.LevelOrigin)
	}
}
