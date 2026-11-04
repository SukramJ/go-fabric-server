// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/topology"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// TestTwoNodes is issue #34's acceptance on a fabrictest site: two
// device nodes, each with its own bridge half as startBridge binds it
// (bridgeapi.Options.Node, the node's observers), served side by side by
// MuxNodes; each is fed through its own /bridge/<key>, advertises under
// its own discriminator and is commissioned on its own by the
// controller half's commission_with_code; the pages list both, the
// default first. The site builds the nodes (a node.New of the daemon's
// own cannot join a site, bridgeapi's harness), so Run's own loop over
// bridgeNodes is TestBridgeNodes' and TestRunBridge's.
func TestTwoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	log := slog.New(slog.DiscardHandler)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: log})
	specs := []nodeSpec{{Key: "default", Label: "fabric-server bridge", Discriminator: 3840}, {Key: "second", Label: "Second", Discriminator: 3841}}
	halves := make([]bridgeHalf, 0, len(specs))
	mounts := make([]BridgeHalf, 0, len(specs))
	for _, s := range specs {
		br := bridgeapi.New(bridgeapi.Options{Version: "test", GoFabricVersion: "v-test", Logger: log, SubscriptionPoll: 50 * time.Millisecond, Node: s.Key})
		dev := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Observers = br.Observers()
			cfg.Commissioning.Discriminator = uint16(s.Discriminator) //nolint:gosec // a test constant
			cfg.BasicInformation.NodeLabel = s.Label
		}})
		if dev.Discriminator() != uint16(s.Discriminator) { //nolint:gosec // a test constant
			t.Fatalf("node %s advertises discriminator %d", s.Key, dev.Discriminator())
		}
		if err := br.Start(ctx, dev.Node()); err != nil {
			t.Fatal(err)
		}
		halves = append(halves, bridgeHalf{spec: s, node: dev.Node(), srv: br})
		mounts = append(mounts, BridgeHalf{Key: s.Key, Server: br})
	}

	api, err := controllerapi.New(controllerapi.Options{
		Version: "test", GoFabricVersion: "v-test", Logger: log, StoragePath: t.TempDir(),
		Attestation: attestation.Accept(), // the site devices' test certificates
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: log})
	cfg.HooksFor = api.Hooks
	var changes topology.Changes
	changes.Install(&cfg)
	ctrl, err := controller.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	topo := topology.New(ctrl, topology.Config{Changes: &changes, Clock: cfg.Clock})
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		topo.Close()
		_ = ctrl.Close(cctx)
	})
	if err := api.Start(ctx, ctrl, topo); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(MuxNodes(api, mounts, "", "test"))
	pagesH, err := webHandler(Config{WebAdmin: true}, halves, nil, fabricui.NewEvents(), nil, log)
	if err != nil {
		t.Fatal(err)
	}
	pages := httptest.NewServer(pagesH)
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		for _, h := range halves {
			h.srv.Shutdown(sctx)
		}
		api.Shutdown(sctx)
		srv.Close()
		pages.Close()
	})
	base := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, err := fsclient.Dial(ctx, base+"/ws", fsclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })

	// /bridge is the default node's; /bridge/<key> each node's, so the
	// default's is reachable under both (one client at a time per node).
	def, err := fsclient.DialBridge(ctx, base+"/bridge", fsclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if def.Info().Node != "default" {
		t.Fatalf("/bridge feeds node %q", def.Info().Node)
	}
	_ = def.Close()
	resp, err := http.Get(srv.URL + "/bridge/nope") //nolint:noctx // a test against httptest
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/bridge/nope: %d, want 404", resp.StatusCode)
	}
	clients := map[string]*fsclient.Bridge{}
	for _, s := range specs {
		b, err := fsclient.DialBridge(ctx, base+"/bridge", fsclient.Options{Node: s.Key})
		if err != nil {
			t.Fatalf("/bridge/%s: %v", s.Key, err)
		}
		t.Cleanup(func() { _ = b.Close() })
		if b.Info().Node != s.Key {
			t.Fatalf("/bridge/%s feeds node %q", s.Key, b.Info().Node)
		}
		clients[s.Key] = b
	}

	// Each client declares into its own node and commissions it by its
	// own code; neither commissioning touches the other node.
	ids := map[string]uint64{}
	for i, s := range specs {
		b := clients[s.Key]
		if _, err := b.Declare(ctx, map[string]any{"devices": []any{map[string]any{"key": "light-" + s.Key, "type": "OnOffLight"}}}); err != nil {
			t.Fatalf("declare on %s: %v", s.Key, err)
		}
		qr, _, err := b.PairingCodes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n, err := ws.CommissionWithCode(ctx, qr, true)
		if err != nil {
			t.Fatalf("commission_with_code %s: %v", s.Key, err)
		}
		ids[s.Key] = n.NodeID
		for j, o := range halves {
			fabrics, err := o.node.FabricInfos(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if want := map[bool]int{true: 1, false: 0}[j <= i]; len(fabrics) != want {
				t.Fatalf("after commissioning %s, node %s has %d fabrics, want %d", s.Key, o.spec.Key, len(fabrics), want)
			}
		}
	}
	if ids["default"] == ids["second"] {
		t.Fatalf("both commissionings answered node %d", ids["default"])
	}

	// The pages' selector: both nodes, the default first, each enabled;
	// the second's routes serve the second node, with the exposure reason.
	var list struct {
		Nodes []struct {
			Key     string `json:"key"`
			Label   string `json:"label"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
		} `json:"nodes"`
	}
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/nodes", &list)
	if len(list.Nodes) != 2 || list.Nodes[0].Key != "default" || list.Nodes[1].Key != "second" || list.Nodes[1].Label != "Second" || list.Nodes[1].Kind != "bridge" || !list.Nodes[0].Enabled || !list.Nodes[1].Enabled {
		t.Fatalf("GET /nodes %+v", list)
	}
	var st struct {
		Enabled        bool   `json:"enabled"`
		ExposureReason string `json:"exposure_reason"`
	}
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/nodes/second/status", &st)
	if !st.Enabled || st.ExposureReason != exposureReason {
		t.Fatalf("GET /nodes/second/status %+v", st)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // a test against httptest
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}
