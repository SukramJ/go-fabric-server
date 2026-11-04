// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"io"
	"strings"
	"testing"
)

// TestBridgeNodes: the --bridge-* node stays the default under its old
// identity, a --bridge-node entry takes what it leaves out from the
// --bridge-* flags, and the entries are validated before any node starts.
func TestBridgeNodes(t *testing.T) {
	t.Setenv(env("bridge-node"), "third,listen=:5542,discriminator=0x0F02;fourth,listen=[::1]:0,discriminator=3843,label=Kitchen")
	c, err := ParseFlags([]string{"--bridge", "--bridge-passcode=34567890", "--bridge-node", "second,listen=:5541,discriminator=3841,product-id=0x8002"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.BridgeNodes) != 1 {
		t.Fatalf("the flag replaces the environment: %q", c.BridgeNodes)
	}
	c.BridgeNodes = append(c.BridgeNodes, "third,listen=:5542,discriminator=0x0F02", "fourth,listen=[::1]:0,discriminator=3843,label=Kitchen")
	nodes, err := bridgeNodes(c)
	if err != nil {
		t.Fatal(err)
	}
	want := []nodeSpec{
		{Key: "default", Label: "fabric-server bridge", Passcode: 34567890, Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001, Store: "bridge.db", Serial: "FABRIC-SERVER-BRIDGE"},
		{Key: "second", Listen: ":5541", Label: "second", Passcode: 34567890, Discriminator: 3841, VendorID: 0xFFF1, ProductID: 0x8002, Store: "bridge-second.db", Serial: "FSB-second"},
		{Key: "third", Listen: ":5542", Label: "third", Passcode: 34567890, Discriminator: 0x0F02, VendorID: 0xFFF1, ProductID: 0x8001, Store: "bridge-third.db", Serial: "FSB-third"},
		{Key: "fourth", Listen: "[::1]:0", Label: "Kitchen", Passcode: 34567890, Discriminator: 3843, VendorID: 0xFFF1, ProductID: 0x8001, Store: "bridge-fourth.db", Serial: "FSB-fourth"},
	}
	if len(nodes) != len(want) {
		t.Fatalf("nodes %+v", nodes)
	}
	for i := range want {
		if nodes[i] != want[i] {
			t.Fatalf("node %d %+v, want %+v", i, nodes[i], want[i])
		}
	}

	// The environment alone carries several entries.
	ce, err := ParseFlags([]string{"--bridge"}, io.Discard)
	if err != nil || len(ce.BridgeNodes) != 2 || ce.BridgeNodes[0] != "third,listen=:5542,discriminator=0x0F02" {
		t.Fatalf("environment entries %q %v", ce.BridgeNodes, err)
	}

	// --node-default moves its node first; the rest keep their order.
	c.NodeDefault = "third"
	nodes, err = bridgeNodes(c)
	if err != nil || nodes[0].Key != "third" || nodes[1].Key != "default" || nodes[2].Key != "second" || nodes[3].Key != "fourth" {
		t.Fatalf("--node-default third: %+v %v", nodes, err)
	}

	base, err := ParseFlags([]string{"--bridge"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		entries []string
		want    string
	}{
		{[]string{"second,listen=:5541,discriminator=3840"}, `discriminator 3840 is node "default"'s as well`},
		{[]string{"second,listen=:5540,discriminator=3841"}, `takes node "default"'s port`},
		{[]string{"second,listen=:5541,discriminator=3841", "second,listen=:5542,discriminator=3842"}, "configured twice"},
		{[]string{"default,listen=:5541"}, "the --bridge-* node's key"},
		{[]string{"status,listen=:5541"}, "a page name of the node pages"},
		{[]string{"two words,listen=:5541"}, "the key must match"},
		{[]string{strings.Repeat("k", 29) + ",listen=:5541"}, "the key must match"},
		{[]string{"second,listen=:5541,colour=red"}, `unknown field "colour"`},
		{[]string{"second,listen=:5541,discriminator=x"}, "discriminator: "},
		{[]string{"second,:5541"}, "is not name=value"},
	} {
		cfg := base
		cfg.BridgeNodes = c.entries
		if _, err := bridgeNodes(cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%q: %v, want %q", c.entries, err, c.want)
		}
	}
	// Two ephemeral ports never collide.
	cfg := base
	cfg.BridgeListen = "[::1]:0"
	cfg.BridgeNodes = []string{"second,listen=[::1]:0,discriminator=3841"}
	if _, err := bridgeNodes(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeMetricsPrefixAndName(t *testing.T) {
	if bridgeMetricsPrefix("default") != "fabricserver.bridge." || bridgeMetricsPrefix("second") != "fabricserver.bridge.second." {
		t.Fatal("prefix")
	}
	if nodeName("default") != "bridge node" || nodeName("second") != "bridge node second" {
		t.Fatal("name")
	}
}
