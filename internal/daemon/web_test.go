// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
)

// testOps is the Operation page over a fresh log ring.
func testOps() (operations, *slog.Logger) {
	log, ring := newLogger(io.Discard, new(slog.LevelVar))
	return operations{ring: ring, started: time.Now()}, log
}

// serve answers one request built by the arguments; peer is its
// RemoteAddr, hdr its headers.
func serve(h http.Handler, method, url, peer, body string, hdr map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader = http.NoBody
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, url, rd)
	if peer != "" {
		req.RemoteAddr = peer
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestWebHosts: a DNS name in the Host is refused 403 host_not_allowed
// unless --web-allowed-hosts lists it; an entry with a port stops the
// start, naming the flag.
func TestWebHosts(t *testing.T) {
	const url = "http://fabric.example/api/v1/node/status"
	h, err := webHandler(Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(h, http.MethodGet, url, "", "", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "host_not_allowed") {
		t.Fatalf("an unlisted name: %d %s", rec.Code, rec.Body)
	}
	listed, err := webHandler(Config{WebAllowedHosts: " fabric.example , other.example"}, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(listed, http.MethodGet, url, "", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("a listed name: %d %s", rec.Code, rec.Body)
	}
	if _, err := webHandler(Config{WebAllowedHosts: "fabric.example:8080"}, nil, nil, nil, nil, nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "--web-allowed-hosts") {
		t.Fatalf("an entry with a port: %v", err)
	}
	err = Run(context.Background(), Config{Listen: "127.0.0.1:0", LogLevel: "info", StoragePath: t.TempDir(), Web: freeTCP(t), WebAllowedHosts: "fabric.example:8080"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--web-allowed-hosts") {
		t.Fatalf("Run with an entry with a port: %v", err)
	}
}

// TestWebRights: without --web-admin and --web-admin-source the pages
// show the status (an open route) and refuse an admin-only read 403
// forbidden. Behind the Ingress — the peer the admin source, the
// browser's Host and Origin forwarded unchanged, the client's header —
// an admin write passes; another peer is refused.
func TestWebRights(t *testing.T) {
	ops, log := testOps()
	h, err := webHandler(Config{}, nil, nil, nil, nil, nil, ops, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	ui := map[string]string{fabricui.RequestHeader: "1"}
	if rec := serve(h, http.MethodGet, "http://127.0.0.1/api/v1/node/status", "", "", ui); rec.Code != http.StatusOK {
		t.Fatalf("status without admin flags: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(h, http.MethodGet, "http://127.0.0.1/api/v1/operation/log", "", "", ui); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("an admin-only read without admin flags: %d %s", rec.Code, rec.Body)
	}

	ing, err := webHandler(Config{WebAdminSource: "172.30.32.2"}, nil, nil, nil, nil, nil, ops, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := map[string]string{
		fabricui.RequestHeader: "1", "Content-Type": "application/json", "Origin": "https://homeassistant.local:8123",
	}
	const write = "http://homeassistant.local:8123/api/v1/operation/log/level"
	if rec := serve(ing, http.MethodPut, write, "172.30.32.2:41000", `{"level":"debug"}`, ingress); rec.Code != http.StatusOK {
		t.Fatalf("the Ingress's admin write: %d %s", rec.Code, rec.Body)
	}
	if ops.LogLevel() != slog.LevelDebug {
		t.Fatalf("the write did not apply: %v", ops.LogLevel())
	}
	if rec := serve(ing, http.MethodPut, write, "192.168.1.9:41000", `{"level":"info"}`, ingress); rec.Code != http.StatusForbidden {
		t.Fatalf("another peer: %d %s", rec.Code, rec.Body)
	}
}

// TestWebStreamEndsAtStop: an open event stream ends when serveWeb's
// stop runs, and stop does not wait out its shutdown budget for it.
func TestWebStreamEndsAtStop(t *testing.T) {
	ops, log := testOps()
	addr := freeTCP(t)
	stop, err := serveWeb(context.Background(), Config{Web: addr, WebAdminSource: "127.0.0.1"}, nil, nil, nil, nil, fabricui.NewEvents(), ops, log)
	if err != nil {
		t.Fatal(err)
	}
	var resp *http.Response
	for i := 0; ; i++ {
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+fabricui.DefaultPrefix+"/operation/log/stream", http.NoBody) //nolint:noctx // closed by stop
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(fabricui.RequestHeader, "1")
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		if i > 50 {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d", resp.StatusCode)
	}
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, bufio.NewReader(resp.Body))
		close(ended)
	}()
	start := time.Now()
	stop()
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("stop took %v", d)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not end")
	}
}
