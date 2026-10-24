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
	"strings"
	"testing"

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
	if c.Listen != "127.0.0.1:5580" || c.Token != "from-env" || !c.DisableAutoSubscription || !c.Controller || c.Trust != "production" {
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
		{Config{Listen: "127.0.0.1:0", LogLevel: "info", Bridge: true, StoragePath: t.TempDir()}, "S2"},
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
