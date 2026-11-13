// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
)

// TestSurfacePolicies: the three flags become the bridge node's
// node.Config.Surface; an unknown register ID fails with every known one,
// an unknown level or profile with go-fabric's own error.
func TestSurfacePolicies(t *testing.T) {
	t.Setenv("FABRIC_SERVER_BRIDGE_SPEC_LEVEL", "1.4.2")
	c, err := ParseFlags([]string{"--bridge-extensions", " BD-chip-Thermostat-Schedules, BD-chip-Thermostat-Events ,", "--bridge-profile", "apple-home"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	p, err := surfacePolicies(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Extensions.IDs(), []string{"BD-chip-Thermostat-Events", "BD-chip-Thermostat-Schedules"}) || p.Level != "1.4.2" || !slices.Equal(p.Profiles, []string{"apple-home"}) {
		t.Fatalf("%+v", p)
	}
	cfg, err := bridgeConfig(c, primaryNode(c), nil, nil)
	if err != nil || cfg.Surface.Digest() != p.Digest() || cfg.Network.MDNS.Interfaces != nil {
		t.Fatalf("bridgeConfig: %v %+v", err, cfg.Network)
	}
	if p, err := surfacePolicies(Config{}); err != nil || p.Extensions != nil || p.Level != "" || p.Profiles != nil {
		t.Fatalf("zero flags: %+v %v", p, err)
	}
	_, err = surfacePolicies(Config{BridgeExtensions: "BD-chip-Thermostat-Schedules,BD-typo"})
	if err == nil || !strings.Contains(err.Error(), `"BD-typo"`) {
		t.Fatalf("unknown ID: %v", err)
	}
	for _, id := range extensionList() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("%v does not list %s", err, id)
		}
	}
	if _, err := surfacePolicies(Config{BridgeSpecLevel: "9.9"}); !errors.Is(err, surface.ErrLevelAbovePin) {
		t.Errorf("level above the pin: %v", err)
	}
	if _, err := surfacePolicies(Config{BridgeProfile: "nope"}); !errors.Is(err, surface.ErrUnknownProfile) || !strings.Contains(err.Error(), "apple-home") {
		t.Errorf("unknown profile: %v", err)
	}
}

// TestBridgeMDNSInterfaces: --bridge-mdns-interfaces reaches every device
// node's node.MDNS.Interfaces — the configured node's and a registry
// node's — trimmed, an empty name kept for Config.Validate to refuse.
func TestBridgeMDNSInterfaces(t *testing.T) {
	c, err := ParseFlags([]string{"--bridge-mdns-interfaces", " enp1s0, wlan0"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []nodeSpec{primaryNode(c), {Listen: ":5541", Passcode: 20202022, Discriminator: 3841, VendorID: 0xFFF1, ProductID: 0x8001, Store: "nodes/n1"}} {
		cfg, err := bridgeConfig(c, s, nil, nil)
		if err != nil || !slices.Equal(cfg.Network.MDNS.Interfaces, []string{"enp1s0", "wlan0"}) || cfg.Network.Listen != s.Listen {
			t.Fatalf("%+v: %+v %v", s, cfg.Network, err)
		}
	}
	if got := mdnsInterfaces("eth0,,eth1"); !slices.Equal(got, []string{"eth0", "", "eth1"}) {
		t.Fatalf("an empty name dropped: %q", got)
	}
}
