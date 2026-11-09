// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/controller/topology"
)

func attrsOf(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestWireTopology holds the wire shape of get_network_topology to the
// one the server's own build answered before go-fabric's
// controller/topology took over: nodes decoded from dump attributes
// (testNodeImage), built by go-fabric (topology.Build, which its own
// tests hold to matterjs-server's rules, NetworkTopologyService.ts:304-430),
// mapped by wireTopology — two Thread nodes that see each other, an
// unknown router neighbour, a dead link dropped, a Wi-Fi station and its
// access point, an Ethernet node without links.
func TestWireTopology(t *testing.T) {
	// Node 1: a Thread router, ext address 0x0102030405060708 (base64
	// AQIDBAUGBwg=), rloc16 0x400, neighbours node 2 (by ext address, lqi
	// 3), an unknown router (lqi 1), a dead one (lqi 0).
	n1 := attrsOf(t, `{"0/49/65532":2,"0/53/1":5,"0/53/64":1024,"0/53/4":1229782938247303441,"0/53/2":"Mesh",
		"0/51/0":[{"4":"AQIDBAUGBwg=","7":4}],
		"0/53/7":[{"0":1157726452361532951,"2":2048,"5":3,"6":-40,"10":true},{"0":3735928559,"2":0,"5":1,"10":true},{"0":4276215469,"2":0,"5":0}]}`)
	// Node 2: ext address 0x1011121314151617, rloc16 0x800, sees node 1
	// by rloc16 (lqi 2).
	n2 := attrsOf(t, `{"0/49/65532":2,"0/53/1":2,"0/53/64":2048,"0/51/0":[{"4":"EBESExQVFhc=","7":4}],
		"0/53/7":[{"0":0,"2":1024,"5":2}]}`)
	// Node 3: Wi-Fi, BSSID 00:11:22:33:44:55, RSSI -60, connected SSID
	// "home".
	n3 := attrsOf(t, `{"0/49/65532":1,"0/54/0":"ABEiM0RV","0/54/4":-60,"0/49/1":[{"0":"aG9tZQ==","1":true}]}`)
	// Node 4: Ethernet.
	n4 := attrsOf(t, `{"0/49/65532":4}`)
	img3 := testNodeImage(3, true, false, n3)
	img3.Addresses = []netip.Addr{netip.MustParseAddr("fe80::1%eth0"), netip.MustParseAddr("192.168.1.3"), netip.MustParseAddr("fe80::1%eth1")}
	topo := wireTopology(topology.Build([]topology.Image{
		testNodeImage(1, true, false, n1),
		testNodeImage(2, false, true, n2),
		img3,
		testNodeImage(4, true, false, n4),
	}, nil, time.UnixMilli(1735689600000)))
	b, err := json.Marshal(topo)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"collected_at":1735689600000`,
		`{"id":"1","kind":"matter","network_type":"thread","node_id":1,"role":"router","available":true,"ext_address":"0102030405060708","rloc16":1024,"ext_pan_id":"1111111111111111","network_name":"Mesh"}`,
		`{"id":"2","kind":"matter","network_type":"thread","node_id":2,"role":"sleepy_end_device","available":false,"is_bridge":true,"ext_address":"1011121314151617","rloc16":2048}`,
		`{"id":"3","kind":"matter","network_type":"wifi","node_id":3,"role":"station","available":true,"ssid":"home","bssid":"00:11:22:33:44:55","ip_addresses":["192.168.1.3","fe80::1"]}`,
		`{"id":"4","kind":"matter","network_type":"ethernet","node_id":4,"available":true}`,
		`{"source":"1","target":"2","network":"thread","strength":"strong","source_to_target":{"strength":"strong","lqi":3,"rssi":-40},"target_to_source":{"strength":"medium","lqi":2}}`,
		`{"id":"unknown_00000000DEADBEEF","kind":"thread_unknown","network_type":"thread","role":"router","ext_address":"00000000DEADBEEF","ext_pan_id":"1111111111111111"}`,
		`{"source":"1","target":"unknown_00000000DEADBEEF","network":"thread","strength":"weak","source_to_target":{"strength":"weak","lqi":1}}`,
		`{"id":"ap_001122334455","kind":"wifi_ap","network_type":"wifi","role":"ap","network_name":"00:11:22:33:44:55","ssid":"home","bssid":"00:11:22:33:44:55"}`,
		`{"source":"3","target":"ap_001122334455","network":"wifi","strength":"strong","source_to_target":{"strength":"strong","rssi":-60}}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the topology lacks\n%s\nin\n%s", want, got)
		}
	}
	if strings.Contains(got, "FEE1DEAD") || strings.Contains(got, "00000000FEE1DEAD") {
		t.Errorf("a dead neighbour is shown: %s", got)
	}
}

// TestTestNodeImage: a test node's dump attributes become the typed
// cluster states of its topology image — the Thread dump of
// TestNetworkTopologyCommand and a Wi-Fi one.
func TestTestNodeImage(t *testing.T) {
	img := testNodeImage(0xFFFF_FFFE_0000_0000, true, false, attrsOf(t, `{"0/49/65532":2,"0/53/1":5,"0/53/64":1024}`))
	td := img.ThreadNetworkDiagnostics
	if img.NetworkFeatures&netcommdef.FeatureThreadNetworkInterface == 0 || td.RoutingRole == nil || *td.RoutingRole != threaddiagdef.RoutingRoleRouter ||
		td.Rloc16 == nil || *td.Rloc16 != 1024 || !img.Available || img.NodeID != 0xFFFF_FFFE_0000_0000 {
		t.Fatalf("thread image %+v", img)
	}
	if len(img.Addresses) != 0 || img.WiFiNetworkDiagnostics.Bssid != nil || td.NetworkName != nil {
		t.Errorf("thread image carries what the dump lacks: %+v", img)
	}

	img = testNodeImage(7, false, true, attrsOf(t, `{"0/49/65532":1,"0/54/0":"ABEiM0RV","0/54/3":6,"0/54/4":-60,"0/49/1":[{"0":"aG9tZQ==","1":true}],
		"0/51/0":[{"4":"ABEiM0RV","7":1}]}`))
	wd := img.WiFiNetworkDiagnostics
	if img.NetworkFeatures&netcommdef.FeatureWiFiNetworkInterface == 0 || wd.Bssid == nil || string(*wd.Bssid) != "\x00\x11\x22\x33\x44\x55" ||
		wd.Rssi == nil || *wd.Rssi != -60 || wd.ChannelNumber == nil || *wd.ChannelNumber != 6 || !img.IsBridge {
		t.Fatalf("wifi image %+v", img)
	}
	if n := img.NetworkCommissioning.Networks; len(n) != 1 || string(n[0].NetworkId) != "home" || !n[0].Connected {
		t.Errorf("networks %+v", n)
	}
	if n := img.GeneralDiagnostics.NetworkInterfaces; len(n) != 1 || n[0].Type != 1 || len(n[0].HardwareAddress) != 6 {
		t.Errorf("interfaces %+v", n)
	}

	// A neighbour's RSSI: the average, else the last; null when neither.
	nbs := neighborTable(json.RawMessage(`[{"0":1,"2":1024,"5":300,"6":null,"7":-70,"10":true,"13":true},{"0":2}]`))
	if len(nbs) != 2 || nbs[0].Lqi != 255 || !nbs[0].AverageRssi.Null || nbs[0].LastRssi.Null || nbs[0].LastRssi.Value != -70 ||
		!nbs[0].RxOnWhenIdle || !nbs[0].IsChild || nbs[0].Rloc16 != 1024 || !nbs[1].AverageRssi.Null || !nbs[1].LastRssi.Null {
		t.Errorf("neighbours %+v", nbs)
	}
}
