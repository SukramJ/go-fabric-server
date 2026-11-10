// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric-ui/fabricui/gofabric"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/topology"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"

	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// pagesEnv is the daemon's composition of the controller half and the
// controller pages on a fabrictest site (go-fabric-ui's
// fabricui/gofabric/e2e_test.go is the pattern): one controller, the
// half's hooks with the pages' chained after them, /ws and the pages'
// handler side by side.
type pagesEnv struct {
	t   *testing.T
	ctx context.Context
	dev *fabrictest.Device
	api *controllerapi.Server
	ws  *fsclient.Client
	// httpURL is the API listener's (Mux: /ws, /health, /metrics).
	httpURL string
	pages   *httptest.Server
	token   string
	events  chan fabricui.ControllerEvent
	// log is the daemon's logger (newLogger over io.Discard), lvl its one
	// level source: the half's LogLevel and the pages' Operations.
	log *slog.Logger
	lvl *slog.LevelVar
}

func newPagesEnv(t *testing.T, c Config) *pagesEnv {
	t.Helper()
	return newPagesEnvWith(t, c, nil, nil)
}

// newPagesEnvWith is newPagesEnv with the device's configuration widened
// by configure and the half's BLE central made by central (nil: none), as
// Run hands bleCentral's to controllerOptions.
func newPagesEnvWith(t *testing.T, c Config, configure func(*node.Config), central func(*fabrictest.Device) ble.Central) *pagesEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	// The daemon's logger and level source, as Run builds them; the
	// site keeps a silent one, so the ring holds the daemon's records.
	lvl := new(slog.LevelVar)
	log, ring := newLogger(io.Discard, lvl)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: slog.New(slog.DiscardHandler)})
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug:1"), Name: "Plug"})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{plug}, Configure: configure})
	var bleCentral ble.Central
	if central != nil {
		bleCentral = central(dev)
	}
	api, err := controllerapi.New(controllerapi.Options{
		Version: "test", GoFabricVersion: "v-test", StoragePath: t.TempDir(), Logger: log, LogLevel: lvl,
		DefaultFabricLabel: c.DefaultFabricLabel,
		BLE:                bleCentral,
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks := gofabric.NewHooks()
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: slog.New(slog.DiscardHandler)})
	cfg.HooksFor = hooks.Wrap(api.Hooks) // as Run wires them
	controllerTelemetry(&cfg, c)         // as Run sets them
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
		topo.Close()  // as Run closes it
		hooks.Close() // as Run closes them
		_ = ctrl.Close(cctx)
	})
	if err := api.Start(ctx, ctrl, controllerapi.Services{Topology: topo}); err != nil {
		t.Fatal(err)
	}
	ws := httptest.NewServer(Mux(api, "", "test"))
	src := controllerSource(ctrl, topo, api, hooks, log)
	h, err := webHandler(c, nil, nil, nil, src, fabricui.NewEvents(), operations{ring: ring, started: time.Now()}, log)
	if err != nil {
		t.Fatal(err)
	}
	pages := httptest.NewServer(h)
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		api.Shutdown(sctx)
		ws.Close()
		pages.Close()
	})
	e := &pagesEnv{t: t, ctx: ctx, dev: dev, api: api, httpURL: ws.URL, pages: pages, events: make(chan fabricui.ControllerEvent, 256), log: log, lvl: lvl}
	e.ws, err = fsclient.Dial(ctx, "ws"+strings.TrimPrefix(ws.URL, "http")+"/ws", fsclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.ws.Close() })
	src.Observe(func(ev fabricui.ControllerEvent) {
		select {
		case e.events <- ev:
		default:
		}
	})
	var st struct {
		CSRFToken string `json:"csrf_token"`
	}
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, &st)
	e.token = st.CSRFToken
	return e
}

// call sends one request to the pages with the page's token.
func (e *pagesEnv) call(method, path, body string) (status int, out []byte) {
	e.t.Helper()
	var rd io.Reader = http.NoBody
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(e.ctx, method, e.pages.URL+fabricui.DefaultPrefix+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if e.token != "" {
		req.Header.Set(fabricui.TokenHeader, e.token)
	}
	resp, err := e.pages.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err = io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp.StatusCode, out
}

func (e *pagesEnv) json(method, path, body string, want int, v any) {
	e.t.Helper()
	status, out := e.call(method, path, body)
	if status != want {
		e.t.Fatalf("%s %s: %d, want %d: %s", method, path, status, want, out)
	}
	if v != nil {
		if err := json.Unmarshal(out, v); err != nil {
			e.t.Fatalf("%s %s: %v: %s", method, path, err, out)
		}
	}
}

// waitPageEvent waits for a controller stream event of the type.
func (e *pagesEnv) waitPageEvent(typ string) fabricui.ControllerEvent {
	e.t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev := <-e.events:
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			e.t.Fatalf("no %s on the controller stream", typ)
		}
	}
}

// TestControllerPages: the daemon's web handler serves the controller
// family over the daemon's controller — status, a node commissioned over
// /ws in the pages' list, and the composed hooks: node_added reaches both
// the /ws client and the pages' stream.
func TestControllerPages(t *testing.T) {
	e := newPagesEnv(t, Config{WebAdmin: true})
	var st struct {
		Enabled      bool   `json:"enabled"`
		Label        string `json:"label"`
		NodeCount    int    `json:"node_count"`
		Capabilities struct {
			BLE         bool `json:"ble"`
			Credentials bool `json:"credentials"`
		} `json:"capabilities"`
	}
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, &st)
	if !st.Enabled || st.Label != "HomeAssistant" || st.NodeCount != 0 || !st.Capabilities.Credentials || st.Capabilities.BLE {
		t.Fatalf("status %+v", st)
	}

	payload, err := e.dev.Node().SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	qr := payload.QR
	n, err := e.ws.CommissionWithCode(e.ctx, qr, true)
	if err != nil {
		t.Fatalf("commission_with_code: %v", err)
	}
	if ev := e.waitPageEvent(fabricui.EventNodeAdded); ev.Node != n.NodeID {
		t.Fatalf("node_added on the stream names %d, want %d", ev.Node, n.NodeID)
	}
	waitWSEvent(t, e.ws, "node_added")

	var list struct {
		Nodes []struct {
			NodeIDHex string `json:"node_id_hex"`
		} `json:"nodes"`
	}
	e.json(http.MethodGet, "/controller/nodes", "", http.StatusOK, &list)
	want := fmt.Sprintf("%016X", n.NodeID)
	if len(list.Nodes) != 1 || list.Nodes[0].NodeIDHex != want {
		t.Fatalf("nodes %+v, want %s", list.Nodes, want)
	}
	e.json(http.MethodGet, "/controller/nodes/"+want, "", http.StatusOK, nil)

	// The pages' graph is the one get_network_topology answers from
	// (ControllerOptions.Topology): served, with the node as a peer of
	// the controller.
	type graphNode struct {
		Kind      string `json:"kind"`
		NodeIDHex string `json:"node_id_hex"`
	}
	var graph struct {
		Nodes []graphNode `json:"nodes"`
	}
	e.json(http.MethodGet, "/controller/topology?refresh=true", "", http.StatusOK, &graph)
	if !slices.Contains(graph.Nodes, graphNode{Kind: "peer", NodeIDHex: want}) {
		t.Fatalf("topology %+v, want peer %s", graph, want)
	}

	// The pages' label is the half's: stored, read back over /ws.
	e.json(http.MethodPut, "/controller/fabric/label", `{"label":"Pages"}`, http.StatusNoContent, nil)
	var label struct {
		FabricLabel string `json:"fabric_label"`
	}
	if err := e.ws.Call(e.ctx, "get_fabric_label", nil, &label); err != nil || label.FabricLabel != "Pages" {
		t.Fatalf("get_fabric_label %+v, %v", label, err)
	}
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, &st)
	if st.Label != "Pages" {
		t.Fatalf("status label %q", st.Label)
	}

	// The pages' ping is go-fabric-ui's adapter's (ControllerNode.Ping,
	// the daemon passes nothing), /ws ping_node the half's: both reach
	// the node.
	var ping struct {
		Method    string `json:"method"`
		Reachable bool   `json:"reachable"`
	}
	e.json(http.MethodPost, "/controller/nodes/"+want+"/ping", "", http.StatusOK, &ping)
	if ping.Method != "matter-read" || !ping.Reachable {
		t.Fatalf("pages ping %+v", ping)
	}
	addrs, err := e.ws.PingNode(e.ctx, n.NodeID)
	if err != nil || len(addrs) == 0 {
		t.Fatalf("ping_node %v %v", addrs, err)
	}
	for a, ok := range addrs {
		if !ok {
			t.Fatalf("ping_node: %s unreachable", a)
		}
	}

	// The pages' decommission is remove_node: /ws hears node_removed.
	e.json(http.MethodDelete, "/controller/nodes/"+want, "", http.StatusNoContent, nil)
	waitWSEvent(t, e.ws, "node_removed")
}

// TestControllerPagesPinnedLabel: --default-fabric-label refuses the
// pages' label change (409), as it makes set_default_fabric_label a no-op.
func TestControllerPagesPinnedLabel(t *testing.T) {
	e := newPagesEnv(t, Config{WebAdmin: true, DefaultFabricLabel: "Pinned"})
	e.json(http.MethodPut, "/controller/fabric/label", `{"label":"Pages"}`, http.StatusConflict, nil)
}

// TestControllerPagesReadOnly: without --web-admin the controller's admin
// routes are refused like the node pages' writes.
func TestControllerPagesReadOnly(t *testing.T) {
	e := newPagesEnv(t, Config{})
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, nil)
	e.json(http.MethodPut, "/controller/credentials", `{"kind":"wifi","id":"home","ssid":"HomeNet","passphrase":"secret-pass"}`, http.StatusForbidden, nil)
	e.json(http.MethodPut, "/controller/fabric/label", `{"label":"Pages"}`, http.StatusForbidden, nil)
}

// openThreadDemo is chip's OpenThreadDemo dataset (controllerapi's
// TestThreadDataset).
const openThreadDemo = "0e080000000000010000000300000f35060004001fffe0020811111111222222220708fd000000000000000510001122334455667788" +
	"99aabbccddeeff030e4f70656e54687265616444656d6f010212340410445f2b5ca6f2a93a55ce570a70efeecb0c0402a0f7f8"

// TestControllerPagesCredentials: one credential list — a Wi-Fi entry set
// over /ws is in the pages' list by name only, a Thread dataset set on the
// pages is in get_all_credentials, and the pages remove what /ws stored.
func TestControllerPagesCredentials(t *testing.T) {
	e := newPagesEnv(t, Config{WebAdmin: true})
	if err := e.ws.Call(e.ctx, "set_wifi_credentials", map[string]any{"id": "home", "ssid": "HomeNet", "credentials": "secret-pass"}, nil); err != nil {
		t.Fatal(err)
	}
	status, out := e.call(http.MethodGet, "/controller/credentials", "")
	if status != http.StatusOK || strings.Contains(string(out), "secret-pass") {
		t.Fatalf("credentials %d %s", status, out)
	}
	var list struct {
		Credentials []struct{ ID, Kind, Name string } `json:"credentials"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Credentials) != 1 || list.Credentials[0] != (struct{ ID, Kind, Name string }{"home", "wifi", "HomeNet"}) {
		t.Fatalf("credentials %+v", list.Credentials)
	}

	e.json(http.MethodPut, "/controller/credentials", `{"kind":"thread","id":"mesh","dataset_hex":"`+openThreadDemo+`"}`, http.StatusNoContent, nil)
	e.json(http.MethodPut, "/controller/credentials", `{"kind":"thread","id":"bad","dataset_hex":"0e"}`, http.StatusBadRequest, nil)
	e.json(http.MethodPut, "/controller/credentials", `{"kind":"wifi","id":"new","ssid":"Other"}`, http.StatusBadRequest, nil)
	all, err := e.ws.GetAllCredentials(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(all["thread"]); !strings.Contains(got, "id:mesh") || !strings.Contains(got, "networkName:OpenThreadDemo") || strings.Contains(got, "445f2b5c") {
		t.Fatalf("get_all_credentials thread %s", got)
	}
	if got := fmt.Sprint(all["wifi"]); !strings.Contains(got, "id:home") || !strings.Contains(got, "ssid:HomeNet") {
		t.Fatalf("get_all_credentials wifi %s", got)
	}
	e.json(http.MethodGet, "/controller/credentials", "", http.StatusOK, &list)
	if len(list.Credentials) != 2 || list.Credentials[1].Name != "OpenThreadDemo" {
		t.Fatalf("credentials %+v", list.Credentials)
	}
	if _, ok := e.api.ThreadDataset("mesh"); !ok {
		t.Fatal("the pages' dataset is not the half's")
	}

	e.json(http.MethodDelete, "/controller/credentials?kind=wifi&id=home", "", http.StatusNoContent, nil)
	e.json(http.MethodDelete, "/controller/credentials?kind=wifi&id=home", "", http.StatusNotFound, nil)
	all, err = e.ws.GetAllCredentials(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(all["wifi"]); strings.Contains(got, "home") {
		t.Fatalf("get_all_credentials wifi after the pages' remove %s", got)
	}
}

// waitWSEvent waits for a /ws event of the name.
func waitWSEvent(t *testing.T, c *fsclient.Client, name string) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			if ev.Event == name {
				return
			}
		case <-deadline:
			t.Fatalf("no %s on /ws", name)
		}
	}
}

// TestControllerTelemetry: --controller-diagnostics-capacity bounds the
// trace the pages' Diagnostics tab reads (GET /controller/events), the
// tab's routes answer through controllerSource, and GET /metrics carries
// go-fabric's controller metric names under fabricserver.controller.
func TestControllerTelemetry(t *testing.T) {
	const capacity = 3
	e := newPagesEnv(t, Config{WebAdmin: true, ControllerDiagnosticsCapacity: capacity})
	var st struct {
		Capabilities struct {
			Diagnostics bool `json:"diagnostics"`
		} `json:"capabilities"`
	}
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, &st)
	if !st.Capabilities.Diagnostics {
		t.Fatal("capabilities.diagnostics is false")
	}
	payload, err := e.dev.Node().SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ws.CommissionWithCode(e.ctx, payload.QR, true); err != nil {
		t.Fatalf("commission_with_code: %v", err)
	}
	var trace struct {
		Events []json.RawMessage `json:"events"`
	}
	e.json(http.MethodGet, "/controller/events", "", http.StatusOK, &trace)
	if n := len(trace.Events); n < 1 || n > capacity {
		t.Fatalf("%d events in the trace, want 1..%d", n, capacity)
	}
	var diagnostics struct {
		Peers []json.RawMessage `json:"peers"`
	}
	e.json(http.MethodGet, "/controller/diagnostics", "", http.StatusOK, &diagnostics)
	if len(diagnostics.Peers) != 1 {
		t.Fatalf("%d peers in the diagnostics, want 1", len(diagnostics.Peers))
	}

	resp, err := http.Get(e.httpURL + "/metrics") //nolint:noctx // a test
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var vars map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&vars); err != nil {
		t.Fatal(err)
	}
	for _, name := range contract.MetricNames() {
		if _, ok := vars[controllerMetricsPrefix+name]; !ok {
			t.Errorf("GET /metrics has no %s", controllerMetricsPrefix+name)
		}
	}
	for _, name := range []string{
		controller.MetricSessionsOpen, controller.MetricMRPRetransmits, controller.MetricPairFailures,
		controller.MetricCASEUnresponsive, controller.MetricReachabilityProbes, controller.MetricICDCheckInsMissed,
	} {
		if _, ok := vars[controllerMetricsPrefix+name]; !ok {
			t.Errorf("GET /metrics has no %s", controllerMetricsPrefix+name)
		}
	}
	var keys []string
	for k, v := range vars {
		if strings.HasPrefix(k, controllerMetricsPrefix) {
			keys = append(keys, k+"="+string(v))
		}
	}
	sort.Strings(keys)
	t.Logf("GET /metrics: %s", strings.Join(keys, " "))
}
