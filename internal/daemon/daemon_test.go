// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	t.Setenv("FABRIC_SERVER_VENDOR_ID", "x")
	if _, err := ParseFlags(nil, io.Discard); err == nil {
		t.Fatal("a malformed environment value is an error")
	}
}

func TestRunRefuses(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{Listen: "0.0.0.0:5580", LogLevel: "info", StoragePath: t.TempDir()}, "needs --token"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "loud", StoragePath: t.TempDir()}, "--log-level"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, StoragePath: t.TempDir()}, "bridge node: "},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, BridgeVendorID: 70000, StoragePath: t.TempDir()}, "wider than its field"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Controller: true, Trust: "none", StoragePath: t.TempDir()}, "--trust"},
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", VendorID: 70000, StoragePath: t.TempDir()}, "--vendor-id"},
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

// TestRunBridge runs the daemon with the bridge half only, on loopback
// and a Unix socket: a client declares a device and reads the pairing
// codes; the daemon stops with its context.
func TestRunBridge(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "fs.sock")
	cfg := Config{
		Listen: "unix:" + sock, LogLevel: "info", StoragePath: dir, Bridge: true,
		BridgeListen: "[::1]:0", BridgePasscode: 20202021, BridgeDiscriminator: 3840,
		BridgeVendorID: 0xFFF1, BridgeProductID: 0x8001, BridgeLabel: "test", BridgeResultTimeout: time.Second,
	}
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
	rctx, rcancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer rcancel()
	if _, err := b.Declare(rctx, map[string]any{"devices": []any{map[string]any{"key": "l", "type": "OnOffLight"}}}); err != nil {
		t.Fatal(err)
	}
	if qr, manual, err := b.PairingCodes(rctx); err != nil || !strings.HasPrefix(qr, "MT:") || manual == "" {
		t.Fatalf("pairing codes %q %q %v", qr, manual, err)
	}
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
