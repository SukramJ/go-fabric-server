// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
)

// surfacePolicies are the bridge node's surface policies
// (node.Config.Surface, go-fabric ADR 0030) from --bridge-extensions,
// --bridge-spec-level and --bridge-profile. Mirrors go-fabric
// examples/reference-bridge/surface.go:33 surfacePolicies (its
// --extensions, --spec-level and --profile): every register ID must be
// one go-fabric declares (device.AheadRows, node.AheadRows), the level
// one of schema.Levels and every profile one of surface.Profiles; a typo
// stops the daemon with the list of the known ones.
func surfacePolicies(c Config) (surface.Policies, error) {
	known := extensionList()
	var p surface.Policies
	var ids []string
	for id := range strings.SplitSeq(c.BridgeExtensions, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if !slices.Contains(known, id) {
			return surface.Policies{}, fmt.Errorf("--bridge-extensions: %q is no extension go-fabric declares; known: %s", id, strings.Join(known, ", "))
		}
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		p.Extensions = surface.NewExtensions(ids...)
	}
	// "" is the pin's level; an unknown level stops the daemon with the
	// list (surface.Policies.Validate names the known levels).
	p.Level = surface.Level(strings.TrimSpace(c.BridgeSpecLevel))
	if err := (surface.Policies{Level: p.Level}).Validate(); err != nil {
		return surface.Policies{}, fmt.Errorf("--bridge-spec-level: %w", err)
	}
	for name := range strings.SplitSeq(c.BridgeProfile, ",") {
		if name = strings.TrimSpace(name); name != "" {
			p.Profiles = append(p.Profiles, name)
		}
	}
	// An unknown profile, or two whose levels differ without
	// --bridge-spec-level, stops the daemon.
	if err := p.Validate(); err != nil {
		return surface.Policies{}, fmt.Errorf("--bridge-profile: %w (profiles: %s)", err, profileList())
	}
	return p, nil
}

// extensionList is the "Ahead of matter.js" register IDs go-fabric
// declares (device.AheadRows, node.AheadRows), sorted, each once.
func extensionList() []string {
	known := slices.Concat(device.AheadRows(), node.AheadRows())
	slices.Sort(known)
	return slices.Compact(known)
}

// profileList names the selectable controller profiles (surface.Profiles)
// for the flag's help; reference-bridge/surface.go:76 profileList.
func profileList() string {
	names := make([]string, 0, len(surface.Profiles()))
	for _, p := range surface.Profiles() {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

// levelList names the selectable specification levels (schema.Levels)
// for the flag's help; reference-bridge/surface.go:86 levelList.
func levelList() string {
	names := make([]string, 0, len(schema.Levels()))
	for _, l := range schema.Levels() {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}
