// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

func TestParseFlagsAndEnvironment(t *testing.T) {
	t.Setenv("FABRIC_SERVER_TOKEN", "from-env")
	t.Setenv("FABRIC_SERVER_LISTEN", "127.0.0.1:1")
	t.Setenv("FABRIC_SERVER_DISABLE_AUTO_SUBSCRIPTION", "true")
	c, err := ParseFlags([]string{"--listen", "127.0.0.1:5580"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:5580" || c.Token != "from-env" || !c.DisableAutoSubscription || !c.Controller || c.Trust != "production" ||
		c.Bridge || c.BridgePasscode != 20202021 || c.BridgeDiscriminator != 3840 || c.BridgeResultTimeout.Seconds() != 5 {
		t.Fatalf("%+v", c)
	}
	if c.NodeLimit != defaultNodeLimit {
		t.Fatalf("--node-limit %d", c.NodeLimit)
	}
	// The flags the node registry replaced stop the start with their
	// migration, on the command line and through the environment.
	for _, args := range [][]string{{"--bridge-node", "second,listen=:5541"}, {"--node-default", "second"}} {
		if _, err := ParseFlags(args, io.Discard); err == nil || !strings.Contains(err.Error(), "create further device nodes in the pages") {
			t.Fatalf("%q: %v", args, err)
		}
	}
	t.Setenv("FABRIC_SERVER_BRIDGE_NODE", "second,listen=:5541")
	if _, err := ParseFlags(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "FABRIC_SERVER_BRIDGE_NODE: removed: --bridge-node") {
		t.Fatalf("environment: %v", err)
	}
	t.Setenv("FABRIC_SERVER_BRIDGE_NODE", "")
	if _, err := ParseFlags(nil, io.Discard); err == nil {
		t.Fatal("an empty FABRIC_SERVER_BRIDGE_NODE is still the removed flag")
	}
	t.Setenv("FABRIC_SERVER_VENDOR_ID", "x")
	if _, err := ParseFlags(nil, io.Discard); err == nil {
		t.Fatal("a malformed environment value is an error")
	}
}

func TestRunRefuses(t *testing.T) {
	ctx := context.Background()
	stubCentral(t, nil, errors.New("bluez: no such adapter"))
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{Listen: "0.0.0.0:5580", LogLevel: "info", StoragePath: t.TempDir()}, "needs --token"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "loud", StoragePath: t.TempDir()}, "--log-level"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, StoragePath: t.TempDir()}, "bridge node: "},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, BridgeVendorID: 70000, StoragePath: t.TempDir()}, "wider than its field"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, BridgeExtensions: "BD-nope", StoragePath: t.TempDir()}, `--bridge-extensions: "BD-nope" is no extension go-fabric declares; known: `},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, BridgeSpecLevel: "1.5", StoragePath: t.TempDir()}, "--bridge-spec-level: "},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, BridgeProfile: "nope", StoragePath: t.TempDir()}, "--bridge-profile: "},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, NodeLimit: -1, StoragePath: t.TempDir()}, "--node-limit -1 is negative"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Controller: true, Trust: "none", StoragePath: t.TempDir()}, "--trust"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", VendorID: 70000, StoragePath: t.TempDir()}, "--vendor-id"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Controller: true, Trust: "production", BluetoothAdapter: "0", StoragePath: t.TempDir()}, "--bluetooth-adapter 0: bluez: no such adapter"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Web: "127.0.0.1:0", WebAdminSource: "nope", StoragePath: t.TempDir()}, "--web-admin-source"},
	} {
		err := Run(ctx, c.cfg, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%+v: %v", c.cfg, err)
		}
	}
	var out bytes.Buffer
	if err := Run(ctx, Config{ShowVersion: true}, &out); err != nil || !strings.Contains(out.String(), "go-fabric-server") {
		t.Fatalf("%q %v", out.String(), err)
	}
}

func TestMuxRoutes(t *testing.T) {
	srv := httptest.NewServer(Mux(nil, "", "v-test"))
	defer srv.Close()
	get := func(path string) (int, http.Header, []byte) {
		t.Helper()
		resp, err := http.Get(srv.URL + path) //nolint:noctx // a test
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, b
	}
	code, _, body := get("/health")
	if code != http.StatusServiceUnavailable || !strings.Contains(string(body), `"version":"v-test"`) {
		t.Fatalf("health without a controller: %d %s", code, body)
	}
	_, header, body := get("/schema")
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil || header.Get("X-Go-Fabric-Version") != schema.GoFabricVersion || doc["$schema"] == nil {
		t.Fatalf("schema: %v %v", header, err)
	}
	_, _, body = get("/schema/declaration")
	if err := json.Unmarshal(body, &doc); err != nil || doc["title"] != "go-fabric-server bridge declaration" {
		t.Fatalf("declaration schema: %v %v", doc["title"], err)
	}
	if code, _, _ = get("/metrics"); code != http.StatusOK {
		t.Fatalf("metrics %d", code)
	}
	if err := Run(context.Background(), Config{HealthCheck: srv.URL + "/health"}, io.Discard); err == nil {
		t.Fatal("an unhealthy server fails the probe")
	}
	if err := Run(context.Background(), Config{HealthCheck: srv.URL + "/metrics"}, io.Discard); err != nil {
		t.Fatalf("a 200 passes the probe: %v", err)
	}
	guarded := httptest.NewServer(Mux(nil, "t", "v-test"))
	defer guarded.Close()
	r, err := http.Get(guarded.URL + "/health") //nolint:noctx // a test
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the token guards every route: %d", r.StatusCode)
	}
}

// TestRunRegistry runs the daemon with the bridge half only, on loopback
// and a Unix socket, twice on one storage path: a client declares a
// device into the default node and reads the pairing codes; the pages
// create a registry node, whose /bridge/<key> a client dials with
// client.Options.Node; the restart keeps it; the pages delete it, and its
// route, store and entry are gone. The daemon stops with its context.
func TestRunRegistry(t *testing.T) {
	if runtime.GOOS != "linux" {
		// The bridge node opens real sockets and mDNS; the CI legs on
		// macOS and Windows run the half in process (bridgeapi's tests).
		t.Skip("the daemon's bridge node on the host network: linux only")
	}
	dir := t.TempDir()
	// A short socket path: sun_path holds about 100 bytes.
	sockDir, err := os.MkdirTemp("", "fsb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "fs.sock")
	web := freeTCP(t)
	pages := "http://" + web + "/api/v1"
	cfg := Config{
		Listen: "unix:" + sock, LogLevel: "info", StoragePath: dir, Bridge: true,
		BridgeListen: "[::1]:0", BridgePasscode: 20202021, BridgeDiscriminator: 3840,
		BridgeVendorID: 0xFFF1, BridgeProductID: 0x8001, BridgeLabel: "test", BridgeResultTimeout: time.Second,
		NodeLimit: defaultNodeLimit, Web: web, WebAdminSource: "127.0.0.1",
	}
	run := func() (*fsclient.Bridge, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, cfg, io.Discard) }()
		var b *fsclient.Bridge
		for i := 0; ; i++ {
			var err error
			dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
			b, err = fsclient.DialBridge(dctx, "unix:"+sock, fsclient.Options{})
			dcancel()
			if err == nil {
				break
			}
			if i > 100 {
				cancel()
				t.Fatalf("dial: %v (run: %v)", err, <-done)
			}
			time.Sleep(50 * time.Millisecond)
		}
		return b, func() {
			cancel()
			select {
			case ev := <-b.Events():
				if ev.Event != "server_shutdown" {
					t.Fatalf("event %s", ev.Event)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("no server_shutdown")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	}
	rctx, rcancel := context.WithTimeout(context.Background(), time.Minute)
	defer rcancel()

	b, stop := run()
	if _, err := b.Declare(rctx, map[string]any{"devices": []any{map[string]any{"key": "l", "type": "OnOffLight"}}}); err != nil {
		t.Fatal(err)
	}
	qr, manual, err := b.PairingCodes(rctx)
	if err != nil || !strings.HasPrefix(qr, "MT:") || manual == "" {
		t.Fatalf("pairing codes %q %q %v", qr, manual, err)
	}
	if _, err := fsclient.DialBridge(rctx, "unix:"+sock, fsclient.Options{Node: "second"}); err == nil {
		t.Fatal("/bridge/second before its creation")
	}
	var created struct {
		Key string `json:"key"`
	}
	if code := pageWrite(t, http.MethodPost, pages+"/nodes", "", `{"label":"Second"}`, &created); code != http.StatusCreated || created.Key != "second" {
		t.Fatalf("POST /nodes: %d %+v", code, created)
	}
	second, err := fsclient.DialBridge(rctx, "unix:"+sock, fsclient.Options{Node: "second"})
	if err != nil {
		t.Fatal(err)
	}
	qr2, _, err := second.PairingCodes(rctx)
	if err != nil || second.Info().Node != "second" || qr2 == qr {
		t.Fatalf("second node %q %q %v", second.Info().Node, qr2, err)
	}
	_ = second.Close()
	if _, err := os.Stat(filepath.Join(dir, "bridge-second.db")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, registryFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%s: %v %v", registryFile, fi, err)
	}
	stop()

	// The restart keeps the registry node: the same key, the same
	// pairing code.
	b, stop = run()
	defer func() { _ = b.Close() }()
	second, err = fsclient.DialBridge(rctx, "unix:"+sock, fsclient.Options{Node: "second"})
	if err != nil {
		t.Fatalf("after the restart: %v", err)
	}
	if again, _, err := second.PairingCodes(rctx); err != nil || again != qr2 {
		t.Fatalf("after the restart the second node's code %q, want %q (%v)", again, qr2, err)
	}
	var list struct {
		Nodes []struct {
			Key string `json:"key"`
		} `json:"nodes"`
	}
	getJSON(t, pages+"/nodes", &list)
	if len(list.Nodes) != 2 || list.Nodes[1].Key != "second" {
		t.Fatalf("GET /nodes after the restart %+v", list)
	}

	// Delete: the client is shut down, the route, the store and the
	// entry are gone.
	if code := pageWrite(t, http.MethodDelete, pages+"/nodes/second", "", "", nil); code != http.StatusNoContent {
		t.Fatalf("DELETE /nodes/second: %d", code)
	}
	awaitShutdown(rctx, t, second)
	_ = second.Close()
	if _, err := fsclient.DialBridge(rctx, "unix:"+sock, fsclient.Options{Node: "second"}); err == nil {
		t.Fatal("/bridge/second after its deletion")
	}
	if _, err := os.Stat(filepath.Join(dir, "bridge-second.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the deleted node's store: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, registryFile)); err != nil || strings.Contains(string(raw), "second") {
		t.Fatalf("%s after the deletion: %s %v", registryFile, raw, err)
	}
	stop()

	// A registry file the daemon cannot read stops the start, naming it.
	if err := os.WriteFile(filepath.Join(dir, registryFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(rctx, cfg, io.Discard); err == nil || !strings.Contains(err.Error(), "node registry") {
		t.Fatalf("a malformed %s: %v", registryFile, err)
	}
}

// freeTCP is a loopback address no listener holds now.
func freeTCP(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestTrustStore(t *testing.T) {
	for _, c := range []Config{{Trust: "production"}, {Trust: "development"}, {Trust: "production", EnableTestNetDCL: true}} {
		store, err := trustStore(c)
		if err != nil || store == nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if want := c.Trust == "development" || c.EnableTestNetDCL; store.AcceptsTestCertificates() != want {
			t.Fatalf("%+v: accepts test certificates %v", c, !want)
		}
	}
}

// TestWebAdmin: the node pages' admin rule — read-only by default, the
// page's token mode with --web-admin, the Ingress proxy's address with
// --web-admin-source.
func TestWebAdmin(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/node/window", http.NoBody)
	r.RemoteAddr = "172.30.32.2:41000"
	other := httptest.NewRequest(http.MethodPost, "/api/v1/node/window", http.NoBody)
	other.RemoteAddr = "192.168.1.9:41000"
	ro, err := webAdmin(Config{})
	if err != nil || ro == nil || ro(r) {
		t.Fatalf("read-only: %v", err)
	}
	tok, err := webAdmin(Config{WebAdmin: true})
	if err != nil || tok != nil {
		t.Fatalf("token mode is a nil Admin: %v", err)
	}
	src, err := webAdmin(Config{WebAdminSource: "172.30.32.2"})
	if err != nil || !src(r) || src(other) {
		t.Fatalf("source check: %v", err)
	}
	// The pages answer without a node: the family disabled.
	h, err := webHandler(Config{}, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/node/status", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<") {
		t.Fatalf("shell %d", rec.Code)
	}
	// Behind the Ingress only the proxy is answered.
	ing, err := webHandler(Config{WebAdminSource: "172.30.32.2"}, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]int{"172.30.32.2:1234": http.StatusOK, "192.168.1.9:1234": http.StatusForbidden} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/node/status", http.NoBody)
		req.RemoteAddr = addr
		rec = httptest.NewRecorder()
		ing.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: %d, want %d", addr, rec.Code, want)
		}
	}
}
