// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
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

// TestTwoNodes is issues #34's and #42's acceptance on a fabrictest
// site: the default node and a node the pages create in the registry
// (POST /nodes), each with its own bridge half as startBridge binds it
// (bridgeapi.Options.Node, the node's observers), served side by side by
// muxRoutes; the registry node's /bridge/<key> appears with its creation,
// each node is fed through its own route, advertises under its own
// discriminator and is commissioned on its own by the controller half's
// commission_with_code; the pages list both, rename the registry node and
// delete it, and its route is gone. The site builds the nodes (a node.New
// of the daemon's own cannot join a site, bridgeapi's harness), through
// the registry's start seam; Run's own nodes are TestRunRegistry's.
func TestTwoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	log := slog.New(slog.DiscardHandler)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: log})
	var halvesMu sync.Mutex
	var halves []bridgeHalf
	start := func(ctx context.Context, s nodeSpec) (bridgeHalf, error) {
		br := bridgeapi.New(bridgeapi.Options{Version: "test", GoFabricVersion: "v-test", Logger: log, SubscriptionPoll: 50 * time.Millisecond, Node: s.Key})
		dev := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Observers = br.Observers()
			cfg.Commissioning.Discriminator = uint16(s.Discriminator) //nolint:gosec // a 12-bit value
			cfg.BasicInformation.NodeLabel = s.Label
		}})
		if dev.Discriminator() != uint16(s.Discriminator) { //nolint:gosec // a 12-bit value
			return bridgeHalf{}, fmt.Errorf("node %s advertises discriminator %d", s.Key, dev.Discriminator())
		}
		if err := br.Start(ctx, dev.Node()); err != nil {
			return bridgeHalf{}, err
		}
		h := bridgeHalf{spec: s, node: dev.Node(), srv: br}
		halvesMu.Lock()
		halves = append(halves, h)
		halvesMu.Unlock()
		return h, nil
	}
	started := func() []bridgeHalf {
		halvesMu.Lock()
		defer halvesMu.Unlock()
		return slices.Clone(halves)
	}
	primary := nodeSpec{Key: "default", Listen: "[::1]:0", Label: "fabric-server bridge", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001, Store: "bridge.db"}
	def, err := start(ctx, primary)
	if err != nil {
		t.Fatal(err)
	}
	routes := newBridgeRoutes(BridgeHalf{Key: "default", Server: def.srv})
	dir := t.TempDir()
	reg, err := openRegistry(ctx, registryOptions{dir: dir, limit: 1, primary: primary, start: start, routes: routes, log: log})
	if err != nil {
		t.Fatal(err)
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
	srv := httptest.NewServer(muxRoutes(api, routes, "", "test"))
	pagesH, err := webHandler(Config{WebAdmin: true}, &def, reg, nil, fabricui.NewEvents(), nil, log)
	if err != nil {
		t.Fatal(err)
	}
	pages := httptest.NewServer(pagesH)
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		for _, h := range routes.all() {
			h.Server.Shutdown(sctx)
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
	if code := httpStatus(t, srv.URL+"/bridge/second"); code != http.StatusNotFound {
		t.Fatalf("/bridge/second before its creation: %d, want 404", code)
	}

	// The pages create the node: its key a slug of the label, its
	// discriminator allocated; /bridge/second answers from then on.
	token := csrfToken(t, pages.URL)
	var created struct {
		Key     string `json:"key"`
		Label   string `json:"label"`
		Managed bool   `json:"managed"`
	}
	if code := pageWrite(t, http.MethodPost, pages.URL+fabricui.DefaultPrefix+"/nodes", token, `{"label":"Second"}`, &created); code != http.StatusCreated || created.Key != "second" || !created.Managed {
		t.Fatalf("POST /nodes: %d %+v", code, created)
	}
	if code := pageWrite(t, http.MethodPost, pages.URL+fabricui.DefaultPrefix+"/nodes", token, `{"label":"Third"}`, nil); code != http.StatusConflict {
		t.Fatalf("POST /nodes above --node-limit: %d, want 409", code)
	}

	// /bridge is the default node's; /bridge/<key> each node's, so the
	// default's is reachable under both: with /bridge's client connected,
	// /bridge/default is busy (one client at a time per node).
	defClient, err := fsclient.DialBridge(ctx, base+"/bridge", fsclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = defClient.Close() })
	if defClient.Info().Node != "default" {
		t.Fatalf("/bridge feeds node %q", defClient.Info().Node)
	}
	if _, err := fsclient.DialBridge(ctx, base+"/bridge", fsclient.Options{Node: "default"}); err == nil || !strings.Contains(err.Error(), "one bridge client at a time") {
		t.Fatalf("/bridge/default beside /bridge's client: %v", err)
	}
	if code := httpStatus(t, srv.URL+"/bridge/nope"); code != http.StatusNotFound {
		t.Fatalf("/bridge/nope: %d, want 404", code)
	}
	keys := []string{"default", "second"}
	clients := map[string]*fsclient.Bridge{"default": defClient}
	b, err := fsclient.DialBridge(ctx, base+"/bridge", fsclient.Options{Node: "second"})
	if err != nil {
		t.Fatalf("/bridge/second: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if b.Info().Node != "second" {
		t.Fatalf("/bridge/second feeds node %q", b.Info().Node)
	}
	clients["second"] = b

	// Each client declares into its own node and commissions it by its
	// own code; neither commissioning touches the other node.
	ids := map[string]uint64{}
	for i, key := range keys {
		b := clients[key]
		if _, err := b.Declare(ctx, map[string]any{"devices": []any{map[string]any{"key": "light-" + key, "type": "OnOffLight"}}}); err != nil {
			t.Fatalf("declare on %s: %v", key, err)
		}
		qr, _, err := b.PairingCodes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n, err := ws.CommissionWithCode(ctx, qr, true)
		if err != nil {
			t.Fatalf("commission_with_code %s: %v", key, err)
		}
		ids[key] = n.NodeID
		for j, o := range started() {
			fabrics, err := o.node.FabricInfos(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if want := map[bool]int{true: 1, false: 0}[j <= i]; len(fabrics) != want {
				t.Fatalf("after commissioning %s, node %s has %d fabrics, want %d", key, o.spec.Key, len(fabrics), want)
			}
		}
	}
	if ids["default"] == ids["second"] {
		t.Fatalf("both commissionings answered node %d", ids["default"])
	}

	// The pages' selector: both nodes, the default first, each enabled,
	// the registry's managed; a rename shows at once, and the second's
	// routes serve the second node with its configuration block.
	if code := pageWrite(t, http.MethodPut, pages.URL+fabricui.DefaultPrefix+"/nodes/second/label", token, `{"label":"Kitchen"}`, nil); code != http.StatusNoContent {
		t.Fatalf("PUT /nodes/second/label: %d", code)
	}
	var list struct {
		Nodes []struct {
			Key     string `json:"key"`
			Label   string `json:"label"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
			Managed bool   `json:"managed"`
		} `json:"nodes"`
	}
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/nodes", &list)
	if len(list.Nodes) != 2 || list.Nodes[0].Key != "default" || list.Nodes[1].Key != "second" || list.Nodes[1].Label != "Kitchen" || list.Nodes[1].Kind != "bridge" ||
		!list.Nodes[0].Enabled || !list.Nodes[1].Enabled || list.Nodes[0].Managed || !list.Nodes[1].Managed {
		t.Fatalf("GET /nodes %+v", list)
	}
	var st struct {
		Enabled        bool   `json:"enabled"`
		ExposureReason string `json:"exposure_reason"`
		NodeRegistry   bool   `json:"node_registry"`
		Config         struct {
			Source        string `json:"source"`
			Label         string `json:"label"`
			Discriminator uint16 `json:"discriminator"`
			Store         string `json:"store"`
		} `json:"config"`
	}
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/nodes/second/status", &st)
	if !st.Enabled || st.ExposureReason != exposureReason || st.Config.Source != "registry" || st.Config.Label != "Kitchen" || st.Config.Store != "bridge-second.db" ||
		uint(st.Config.Discriminator) != started()[1].spec.Discriminator {
		t.Fatalf("GET /nodes/second/status %+v", st)
	}
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/node/status", &st)
	if !st.NodeRegistry || st.Config.Source != "configuration" || st.Config.Store != "bridge.db" || st.Config.Discriminator != 3840 {
		t.Fatalf("GET /node/status %+v", st)
	}

	// Delete: the node leaves every fabric and stops, its client is shut
	// down, /bridge/second is gone and so is its entry.
	if code := pageWrite(t, http.MethodDelete, pages.URL+fabricui.DefaultPrefix+"/nodes/default", token, "", nil); code != http.StatusConflict {
		t.Fatalf("DELETE /nodes/default: %d, want 409", code)
	}
	if code := pageWrite(t, http.MethodDelete, pages.URL+fabricui.DefaultPrefix+"/nodes/second", token, "", nil); code != http.StatusNoContent {
		t.Fatalf("DELETE /nodes/second: %d", code)
	}
	if code := httpStatus(t, srv.URL+"/bridge/second"); code != http.StatusNotFound {
		t.Fatalf("/bridge/second after its deletion: %d, want 404", code)
	}
	awaitShutdown(ctx, t, clients["second"])
	getJSON(t, pages.URL+fabricui.DefaultPrefix+"/nodes", &list)
	if len(list.Nodes) != 1 || len(reg.List()) != 0 {
		t.Fatalf("GET /nodes after the deletion %+v", list)
	}
	if fabrics, err := started()[0].node.FabricInfos(ctx); err != nil || len(fabrics) != 1 {
		t.Fatalf("the default node keeps its fabric: %d %v", len(fabrics), err)
	}
}

// awaitShutdown waits for b's server_shutdown, past the events before it.
func awaitShutdown(ctx context.Context, t *testing.T, b *fsclient.Bridge) {
	t.Helper()
	for {
		select {
		case ev, ok := <-b.Events():
			if !ok {
				t.Fatal("the client's events ended without server_shutdown")
			}
			if ev.Event == "server_shutdown" {
				return
			}
		case <-ctx.Done():
			t.Fatal("no server_shutdown")
		}
	}
}

// csrfToken is the pages' token-mode admin token (--web-admin).
func csrfToken(t *testing.T, base string) string {
	t.Helper()
	var st struct {
		CSRFToken string `json:"csrf_token"`
	}
	getJSON(t, base+fabricui.DefaultPrefix+"/node/status", &st)
	if st.CSRFToken == "" {
		t.Fatal("no csrf_token in token mode")
	}
	return st.CSRFToken
}

// pageWrite sends a writing request to the pages and decodes a JSON
// answer into v (nil skips it).
func pageWrite(t *testing.T, method, url, token, body string, v any) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body)) //nolint:noctx // a test against httptest
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(fabricui.TokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("%s %s: %v", method, url, err)
		}
	}
	return resp.StatusCode
}

// httpStatus is a GET's status code.
func httpStatus(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // a test against httptest
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
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
