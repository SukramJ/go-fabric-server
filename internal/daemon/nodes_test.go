// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
)

// testRegistry is a registry on a temporary directory whose start must
// not be reached.
func testRegistry(t *testing.T, limit int, primary nodeSpec) *nodeRegistry {
	t.Helper()
	life := newNodeLifecycle(context.Background(), func(context.Context, nodeSpec) (liveNode, error) {
		return liveNode{}, errors.New("no start in this test")
	}, newBridgeRoutes(), slog.New(slog.DiscardHandler))
	r, err := openRegistry(registryOptions{dir: t.TempDir(), limit: limit, primary: primary, life: life})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestAllocKey: a key is a slug of the label within nodeKey, never the
// default's, a page name, a registry node's or a store left on disk.
func TestAllocKey(t *testing.T) {
	r := testRegistry(t, 8, nodeSpec{Discriminator: 3840})
	r.entries = []registryEntry{{Key: "kitchen"}}
	if err := os.WriteFile(filepath.Join(r.dir, "bridge-garden.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for label, want := range map[string]string{
		"Living Room":                          "living-room",
		"  Küche / Süd  ":                      "kueche-sued",
		"Kitchen":                              "kitchen-2",
		"garden":                               "garden-2",
		"Default":                              "default-2",
		"Status":                               "status-2",
		"🙂":                                    "node",
		strings.Repeat("a", 40):                strings.Repeat("a", slugMax),
		"Apple Home – Ground floor, east wing": "apple-home-ground-floor",
	} {
		got := r.allocKey(label)
		if got != want || !nodeKey.MatchString(got) {
			t.Fatalf("allocKey(%q) = %q, want %q", label, got, want)
		}
	}
}

// TestAllocListenAndCredentials: the next free port after the default
// node's, skipping the registry's and one the host cannot bind; an
// ephemeral default gives an ephemeral port; a discriminator no other
// node holds and a passcode node accepts.
func TestAllocListenAndCredentials(t *testing.T) {
	ctx := context.Background()
	r := testRegistry(t, 8, nodeSpec{Listen: "", Discriminator: 3840})
	r.entries = []registryEntry{{Key: "a", Listen: ":5541"}}
	r.probe = func(_ context.Context, addr string) bool { return addr != ":5542" }
	if got, err := r.allocListen(ctx); err != nil || got != ":5543" {
		t.Fatalf("allocListen %q %v", got, err)
	}
	r.primary.Listen = "[::1]:0"
	if got, err := r.allocListen(ctx); err != nil || got != "[::1]:0" {
		t.Fatalf("ephemeral %q %v", got, err)
	}
	r.primary.Listen = "nope"
	if _, err := r.allocListen(ctx); err == nil {
		t.Fatal("a malformed --bridge-listen")
	}
	r.primary.Listen = ":65535"
	if _, err := r.allocListen(ctx); err == nil {
		t.Fatal("no port after 65535")
	}
	for range 32 {
		d, p, err := r.allocCredentials()
		if err != nil || d == 3840 || d > 0xFFF || node.ValidateSetupPIN(p) != nil {
			t.Fatalf("credentials %d %d %v", d, p, err)
		}
		r.entries = append(r.entries, registryEntry{Discriminator: d})
	}
	// Every discriminator taken: the draw gives up.
	r.entries = r.entries[:0]
	for d := range uint16(0x1000) {
		r.entries = append(r.entries, registryEntry{Discriminator: d})
	}
	if _, _, err := r.allocCredentials(); err == nil {
		t.Fatal("a discriminator although all are taken")
	}
}

// TestRegistryRefuses: the limit, the default node and an unknown key;
// a malformed nodes.json stops the open, naming the entry.
func TestRegistryRefuses(t *testing.T) {
	ctx := context.Background()
	r := testRegistry(t, 0, nodeSpec{Discriminator: 3840})
	if _, err := r.Create(ctx, fabricui.NodeSpec{Label: "x"}); !errors.Is(err, fabricui.ErrNodeLimit) {
		t.Fatalf("Create above the limit: %v", err)
	}
	if err := r.Rename(ctx, "default", "x"); !errors.Is(err, fabricui.ErrDefaultNode) {
		t.Fatalf("Rename default: %v", err)
	}
	if err := r.Delete(ctx, "default"); !errors.Is(err, fabricui.ErrDefaultNode) {
		t.Fatalf("Delete default: %v", err)
	}
	if err := r.Rename(ctx, "nope", "x"); err == nil {
		t.Fatal("Rename of an unknown node")
	}
	if err := r.Delete(ctx, "nope"); err == nil {
		t.Fatal("Delete of an unknown node")
	}
	// A start that fails leaves no entry and no store.
	r.limit = 1
	if _, err := r.Create(ctx, fabricui.NodeSpec{Label: "x"}); err == nil || len(r.List()) != 0 {
		t.Fatalf("a failed start: %v %d", err, len(r.List()))
	}
	if _, err := os.Stat(filepath.Join(r.dir, registryFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a failed start persisted: %v", err)
	}

	primary := nodeSpec{Listen: ":5539", Discriminator: 3840}
	for _, c := range []struct{ file, want string }{
		{`{"nodes":[{"key":"default","label":"x","discriminator":1}]}`, "the key must match"},
		{`{"nodes":[{"key":"status","label":"x","discriminator":1}]}`, "the key must match"},
		{`{"nodes":[{"key":"two words","label":"x","discriminator":1}]}`, "the key must match"},
		{`{"nodes":[{"key":"a","label":" ","discriminator":1}]}`, "the label is empty"},
		{`{"nodes":[{"key":"a","label":"x","discriminator":3840}]}`, "the default node's"},
		{`{"nodes":[{"key":"a","label":"x","listen":":5539","discriminator":1}]}`, "the default node's port"},
		{`{"nodes":[{"key":"a","label":"x","discriminator":1},{"key":"a","label":"y","discriminator":2}]}`, "listed twice"},
		{`{"nodes":[{"key":"a","label":"x","discriminator":1},{"key":"b","label":"y","discriminator":1}]}`, `node "a"'s as well`},
		{`{"nodes":[{"key":"a","label":"x","listen":":5541","discriminator":1},{"key":"b","label":"y","listen":"[::1]:5541","discriminator":2}]}`, `takes node "a"'s port`},
		{`[]`, "cannot unmarshal"},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, registryFile), []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := openRegistry(registryOptions{dir: dir, primary: primary, life: newNodeLifecycle(ctx, nil, newBridgeRoutes(), slog.New(slog.DiscardHandler))})
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), registryFile) {
			t.Fatalf("%s: %v, want %q", c.file, err, c.want)
		}
	}
	// Two ephemeral ports never collide.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, registryFile), []byte(`{"nodes":[{"key":"a","label":"x","listen":"[::1]:0","discriminator":1},{"key":"b","label":"y","listen":"[::1]:0","discriminator":2}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := openRegistry(registryOptions{dir: dir, primary: nodeSpec{Listen: "[::1]:0", Discriminator: 3840}, life: newNodeLifecycle(ctx, nil, newBridgeRoutes(), slog.New(slog.DiscardHandler))})
	if err != nil || len(r.List()) != 2 || r.List()[0].Source != nil || r.List()[1].Config.Source != fabricui.NodeConfigSourceRegistry {
		t.Fatalf("two ephemeral ports: %v", err)
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

// TestBridgeRoutes: the default half answers "" (/bridge), a replaced
// one in place; a removed half is gone.
func TestBridgeRoutes(t *testing.T) {
	b := newBridgeRoutes()
	if b.get("") != nil {
		t.Fatal("an empty set has a default")
	}
	b.add(BridgeHalf{Key: "default"})
	b.add(BridgeHalf{Key: "second"})
	// An erase's new half takes its node's place: the default stays first.
	srv := bridgeapi.New(bridgeapi.Options{})
	b.replace("default", srv)
	b.replace("nope", nil)
	if b.get("") != srv || b.all()[0].Key != "default" || len(b.all()) != 2 {
		t.Fatalf("after replace %+v", b.all())
	}
	b.remove("second")
	b.remove("nope")
	if len(b.all()) != 1 || b.all()[0].Key != "default" {
		t.Fatalf("%+v", b.all())
	}
}

// TestEraseRestartFailed: an erase whose new node does not start keeps
// the daemon up — the half's switch keeps the stopped node, the failure
// is one node_restart_failed warning naming the key, kept from its first
// sighting across a failed retry — and an erase again that starts clears
// it and serves the new node. An ended half is not erased.
func TestEraseRestartFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	log := slog.New(slog.DiscardHandler)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: log})
	fail := false
	start := func(_ context.Context, s nodeSpec) (liveNode, error) {
		if fail {
			return liveNode{}, errors.New("listen: address in use")
		}
		return liveNode{node: site.AddDevice(fabrictest.Topology{}).Node(), srv: bridgeapi.New(bridgeapi.Options{Logger: log, Node: s.Key})}, nil
	}
	routes := newBridgeRoutes()
	life := newNodeLifecycle(ctx, start, routes, log)
	def, err := life.startHalf(nodeSpec{Key: "default"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := life.startHalf(nodeSpec{Key: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if ws := life.warnings(ctx); len(ws) != 0 {
		t.Fatalf("warnings before an erase: %+v", ws)
	}
	stopped := h.live()

	fail = true
	if err := life.erase(ctx, h); err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("erase with a failing start: %v", err)
	}
	ws := life.warnings(ctx)
	if len(ws) != 1 || ws[0].Code != restartFailedCode || !strings.HasPrefix(ws[0].Message, "second: ") || ws[0].Since.IsZero() {
		t.Fatalf("warnings after the failed restart: %+v", ws)
	}
	if h.sw.Current() != fabricui.NodeSource(stopped.node) || h.live() != stopped || routes.get("second") != stopped.srv {
		t.Fatal("a failed restart moved the switch, the half or the route")
	}
	if err := life.erase(ctx, h); err == nil {
		t.Fatal("the retry with a failing start succeeded")
	}
	if again := life.warnings(ctx); len(again) != 1 || !again[0].Since.Equal(ws[0].Since) {
		t.Fatalf("warnings after a failed retry: %+v, since %v", again, ws[0].Since)
	}

	fail = false
	if err := life.erase(ctx, h); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if ws := life.warnings(ctx); len(ws) != 0 {
		t.Fatalf("warnings after the restart: %+v", ws)
	}
	run := h.live()
	if run == stopped || h.sw.Current() != fabricui.NodeSource(run.node) || routes.get("second") != run.srv || routes.get("") != def.live().srv {
		t.Fatal("the restart did not serve the new node")
	}

	// A failed FactoryReset is the half's warning as well, by its own
	// code; a failure of the other kind is a new sighting.
	h.fail(eraseFailedCode, errors.New("store: disk I/O error"), time.Now())
	first := h.failed.Load().since
	h.fail(eraseFailedCode, errors.New("again"), first.Add(time.Minute))
	if ws := life.warnings(ctx); len(ws) != 1 || ws[0].Code != eraseFailedCode || ws[0].Message != "second: again" || !ws[0].Since.Equal(first) {
		t.Fatalf("warnings after a failed erase: %+v", ws)
	}
	h.fail(restartFailedCode, errors.New("x"), first.Add(time.Hour))
	if ws := life.warnings(ctx); len(ws) != 1 || ws[0].Code != restartFailedCode || !ws[0].Since.Equal(first.Add(time.Hour)) {
		t.Fatalf("warnings after a failure of the other kind: %+v", ws)
	}
	if err := life.erase(ctx, h); err != nil || len(life.warnings(ctx)) != 0 {
		t.Fatalf("an erase that succeeds clears the warning: %v", err)
	}

	life.end(ctx, h, false)
	if err := life.erase(ctx, h); err == nil || routes.get("second") != nil {
		t.Fatalf("erase of an ended half: %v", err)
	}
	def.stop(ctx)
	if err := life.erase(ctx, def); err == nil {
		t.Fatal("erase of a stopped half")
	}
}
