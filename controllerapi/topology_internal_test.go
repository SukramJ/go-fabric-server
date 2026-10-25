// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func attrsOf(t *testing.T, doc string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestBuildTopology holds the build to matterjs-server's rules
// (NetworkTopologyService.ts:304-430): two Thread nodes that see each
// other, an unknown router neighbour, a dead link dropped, a Wi-Fi
// station and its access point, an Ethernet node without links.
func TestBuildTopology(t *testing.T) {
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
	topo := buildTopology([]topologySource{
		{nodeID: 1, available: true, attrs: n1},
		{nodeID: 2, available: false, isBridge: true, attrs: n2},
		{nodeID: 3, available: true, attrs: n3, addresses: []string{"192.168.1.3"}},
		{nodeID: 4, available: true, attrs: n4},
	}, time.UnixMilli(1735689600000))
	b, err := json.Marshal(topo)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"collected_at":1735689600000`,
		`{"id":"1","kind":"matter","network_type":"thread","node_id":1,"role":"router","available":true,"ext_address":"0102030405060708","rloc16":1024,"ext_pan_id":"1111111111111111","network_name":"Mesh"}`,
		`{"id":"2","kind":"matter","network_type":"thread","node_id":2,"role":"sleepy_end_device","available":false,"is_bridge":true,"ext_address":"1011121314151617","rloc16":2048}`,
		`{"id":"3","kind":"matter","network_type":"wifi","node_id":3,"role":"station","available":true,"ssid":"home","bssid":"00:11:22:33:44:55","ip_addresses":["192.168.1.3"]}`,
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
