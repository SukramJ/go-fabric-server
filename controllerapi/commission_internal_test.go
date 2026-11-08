// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// TestBLEChoice: commission_with_code's BLE decision over its four inputs
// — a central, the code (manual; QR with bit 1; QR without), network_only,
// a resolved Wi-Fi or Thread entry — and the reasons in their precedence,
// the names of go-fabric-ui's BLESkipReason (fabricui/controller.go:613-616
// at v0.7.1).
func TestBLEChoice(t *testing.T) {
	// A QR code without the BLE bit, decoded by go-fabric rather than
	// written as a bit: matterjs-server's documented example, whose
	// discovery_capabilities the compat fixture
	// protocol/testdata/compat/parse_onboarding_payload.value.json says is
	// 4 (on the IP network only).
	withoutBLE, err := controller.ParsePairingCode("MT:Y.K90AFN00KA0648G00")
	if err != nil || withoutBLE.Manual || withoutBLE.Capabilities != 4 {
		t.Fatalf("the documented QR code: %+v, %v", withoutBLE, err)
	}
	// A QR code with the BLE bit. go-fabric's facade has no encoder that
	// sets it (node/credentials.go:102 and controller/client/window.go:149
	// at v0.18.0 fix DiscoveryOnIP; secure/setup.QRCode is below the
	// facade, internal/arch), so this one is the decoded code above with
	// the bit go-fabric documents for BLE (controller/code.go:40-42 at
	// v0.18.0: bit 1) set — not a decoded payload.
	withBLE := withoutBLE
	withBLE.Capabilities |= capabilityBLE
	manual, err := controller.ParsePairingCode("34970112332")
	if err != nil || !manual.Manual || manual.Capabilities != 0 {
		t.Fatalf("the documented manual code: %+v, %v", manual, err)
	}
	wifi := &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"}
	thread := &commissioner.ThreadNetwork{OperationalDataset: []byte{0x0e, 0x08, 0x00}}

	for _, c := range []struct {
		name        string
		central     bool
		pc          controller.PairingCode
		networkOnly bool
		wifi        *commissioner.WiFiNetwork
		thread      *commissioner.ThreadNetwork
		use         bool
		reason      string
	}{
		{"manual code, Wi-Fi", true, manual, false, wifi, nil, true, ""},
		{"manual code, Thread", true, manual, false, nil, thread, true, ""},
		{"QR with bit 1, both networks", true, withBLE, false, wifi, thread, true, ""},
		{"no central", false, manual, false, wifi, nil, false, "no_central"},
		{"no central before every other reason", false, withoutBLE, true, nil, nil, false, "no_central"},
		{"network_only", true, manual, true, wifi, nil, false, "network_only"},
		{"network_only before the code and the network", true, withoutBLE, true, nil, nil, false, "network_only"},
		{"QR without bit 1", true, withoutBLE, false, wifi, thread, false, "code_without_ble"},
		{"QR without bit 1 before the network", true, withoutBLE, false, nil, nil, false, "code_without_ble"},
		{"manual code, no network", true, manual, false, nil, nil, false, "no_network"},
		{"QR with bit 1, no network", true, withBLE, false, nil, nil, false, "no_network"},
	} {
		use, reason := bleChoice(c.central, c.pc, c.networkOnly, c.wifi, c.thread)
		if use != c.use || reason != c.reason {
			t.Errorf("%s: bleChoice = (%v, %q), want (%v, %q)", c.name, use, reason, c.use, c.reason)
		}
	}
}

// TestBLELogAttrs: the two lines' attributes — the network kind and the
// entries' ids as named or "default", never the SSID, the password or the
// dataset; the reason, with the hint for no_network only.
func TestBLELogAttrs(t *testing.T) {
	wifi := &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"}
	thread := &commissioner.ThreadNetwork{OperationalDataset: []byte{0x0e, 0x08, 0x00}}
	named := "office"
	empty := ""
	for _, c := range []struct {
		name                      string
		wifi                      *commissioner.WiFiNetwork
		thread                    *commissioner.ThreadNetwork
		wifiArg, threadArg        *string
		network, wifiID, threadID string
	}{
		{"Wi-Fi by default", wifi, nil, nil, nil, "wifi", "default", ""},
		{"Wi-Fi by an empty id", wifi, nil, &empty, nil, "wifi", "default", ""},
		{"Wi-Fi by name", wifi, nil, &named, nil, "wifi", "office", ""},
		{"Thread by default", nil, thread, nil, nil, "thread", "", "default"},
		{"both", wifi, thread, &named, nil, "wifi+thread", "office", "default"},
	} {
		got := attrMap(bleUsed(c.wifi, c.thread, c.wifiArg, c.threadArg))
		if got["network"] != c.network || got["wifi_id"] != c.wifiID || got["thread_id"] != c.threadID {
			t.Errorf("%s: %v", c.name, got)
		}
		for k, v := range got {
			if strings.Contains(k, "ssid") || v == "lab" || v == "secret12" || strings.Contains(v, "0e08") {
				t.Errorf("%s: a credential in the log attributes: %s=%q", c.name, k, v)
			}
		}
	}
	for _, reason := range []string{bleSkipNoCentral, bleSkipNetworkOnly, bleSkipCode, bleSkipNoNetwork} {
		got := attrMap(bleSkipped(reason))
		if got["reason"] != reason || (got["hint"] != "") != (reason == bleSkipNoNetwork) {
			t.Errorf("%s: %v", reason, got)
		}
	}
}

func attrMap(attrs []slog.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = a.Value.String()
	}
	return m
}
