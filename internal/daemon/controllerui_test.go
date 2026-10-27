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
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric-ui/fabricui/gofabric"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
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
	t      *testing.T
	ctx    context.Context
	dev    *fabrictest.Device
	api    *controllerapi.Server
	ws     *fsclient.Client
	pages  *httptest.Server
	token  string
	events chan fabricui.ControllerEvent
}

func newPagesEnv(t *testing.T, c Config) *pagesEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	log := slog.New(slog.DiscardHandler)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: log})
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug:1"), Name: "Plug"})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{plug}})
	api, err := controllerapi.New(controllerapi.Options{
		Version: "test", GoFabricVersion: "v-test", StoragePath: t.TempDir(), Logger: log,
		DefaultFabricLabel: c.DefaultFabricLabel,
		// The site's device carries go-fabric's test attestation.
		Attestation: attestation.Accept(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks := gofabric.NewHooks()
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: log})
	cfg.HooksFor = hooks.Wrap(api.Hooks) // as Run wires them
	ctrl, err := controller.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		hooks.Close() // as Run closes them
		_ = ctrl.Close(cctx)
	})
	if err := api.Start(ctx, ctrl); err != nil {
		t.Fatal(err)
	}
	ws := httptest.NewServer(Mux(api, "", "test"))
	src := controllerSource(ctrl, api, hooks, log)
	h, err := webHandler(c, nil, src, fabricui.NewEvents(), log)
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
	e := &pagesEnv{t: t, ctx: ctx, dev: dev, api: api, pages: pages, events: make(chan fabricui.ControllerEvent, 256)}
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

	qr, _, err := e.dev.Node().PairingCodes()
	if err != nil {
		t.Fatal(err)
	}
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
