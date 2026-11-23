// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// TestBridgeLabelBound: --bridge-label beyond the NodeLabel's 32 bytes
// stops the start with --bridge, naming the flag, its variable and the
// byte count; without --bridge the unused flag blocks nothing; 32 bytes
// of multibyte runes pass.
func TestBridgeLabelBound(t *testing.T) {
	long := strings.Repeat("ü", 17) // 34 bytes, 17 runes
	err := checkBridgeLabel(Config{Bridge: true, BridgeLabel: long})
	if err == nil {
		t.Fatal("a 34-byte --bridge-label passed")
	}
	for _, want := range []string{"--bridge-label", "FABRIC_SERVER_BRIDGE_LABEL", "34 bytes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %q", err, want)
		}
	}
	if err := Run(context.Background(), Config{Listen: "127.0.0.1:0", LogLevel: "info", StoragePath: t.TempDir(), Bridge: true, BridgeLabel: long}, io.Discard); err == nil || !strings.Contains(err.Error(), "--bridge-label") {
		t.Fatalf("Run with a 34-byte --bridge-label: %v", err)
	}
	for _, c := range []Config{
		{Bridge: false, BridgeLabel: long},
		{Bridge: true, BridgeLabel: strings.Repeat("ü", 16)},
		{Bridge: true},
	} {
		if err := checkBridgeLabel(c); err != nil {
			t.Errorf("%q (bridge %v): %v", c.BridgeLabel, c.Bridge, err)
		}
	}
}

// TestRegistryLabelCut: a registry node with a 64-rune multibyte label
// (go-fabric-ui's bound, 128 bytes) starts, serving the label cut to 32
// bytes on a rune boundary as its NodeLabel; a rename keeps the full
// label in the registry and gives the running node the cut. Its
// commissionable record names the Aggregator as its device type before
// any device is declared.
func TestRegistryLabelCut(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the daemon's bridge node on the host network: linux only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := ParseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	c.StoragePath, c.BridgeListen = t.TempDir(), "[::1]:0"
	log := slog.New(slog.DiscardHandler)
	life := newNodeLifecycle(ctx, nodeStarter(c, nil, log), newBridgeRoutes(), log)
	reg, err := openRegistry(registryOptions{dir: c.StoragePath, limit: 1, primary: primaryNode(c), life: life})
	if err != nil {
		t.Fatal(err)
	}
	label := strings.Repeat("ü", 64)
	e, err := reg.Create(ctx, fabricui.NodeSpec{Label: label})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.stopAll(context.Background()) })
	n := reg.running[e.Key].live().node

	bi := schema.ClusterByName("BasicInformation")
	// The server answers a string with its byte bound, a struct of
	// go-fabric's tlv package (below the facade): its Value field.
	nodeLabel := func() string {
		t.Helper()
		v, err := n.ReadAttribute(ctx, 0, bi.ID, bi.AttributeByName("NodeLabel").ID)
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := v.Value.(string); ok {
			return s
		}
		return reflect.ValueOf(v.Value).FieldByName("Value").String()
	}
	if got, want := nodeLabel(), device.ComposeNodeLabel(label, ""); got != want || len(want) != 32 {
		t.Fatalf("NodeLabel %q, want %q (32 bytes)", got, want)
	}
	if e.Label != label {
		t.Fatalf("registry label %q, want the full one", e.Label)
	}

	renamed := strings.Repeat("é", 64)
	if err := reg.Rename(ctx, e.Key, renamed); err != nil {
		t.Fatal(err)
	}
	if got := nodeLabel(); got != device.ComposeNodeLabel(renamed, "") {
		t.Fatalf("NodeLabel after the rename %q", got)
	}
	i := slices.IndexFunc(reg.List(), func(x fabricui.NodeEntry) bool { return x.Key == e.Key })
	if i < 0 || reg.List()[i].Label != renamed {
		t.Fatalf("registry after the rename: %+v", reg.List())
	}

	want := strconv.Itoa(int(device.AggregatorType().ID))
	if dt, ok := commissionableTXT(n, "DT"); !ok || dt != want {
		t.Fatalf("commissionable DT %q (%v), want the Aggregator's %s; services %+v", dt, ok, want, n.MDNSServices())
	}
}

// commissionableTXT is the TXT value key of n's commissionable record.
func commissionableTXT(n *node.Node, key string) (string, bool) {
	services := n.MDNSServices()
	for i := range services {
		if services[i].ServiceType != "_matterc._udp" {
			continue
		}
		for _, r := range services[i].TXT {
			if r.Key == key {
				return r.Value, true
			}
		}
	}
	return "", false
}
