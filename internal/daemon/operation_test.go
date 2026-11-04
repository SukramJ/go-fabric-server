// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
)

// TestOperationPages: the pages' Operation page and /ws share one level
// source and one log — a level set on the pages is get_loglevel's, a
// level set_loglevel sets is the page's, and the page's log shows the
// daemon's records.
func TestOperationPages(t *testing.T) {
	e := newPagesEnv(t, Config{WebAdmin: true})
	type level struct {
		Level string `json:"level"`
	}
	type levels struct {
		Console string  `json:"console_loglevel"`
		File    *string `json:"file_loglevel"`
	}
	e.json(http.MethodPut, "/operation/log/level", `{"level":"debug"}`, http.StatusOK, nil)
	var ws levels
	if err := e.ws.Call(e.ctx, "get_loglevel", nil, &ws); err != nil || ws.Console != "debug" || ws.File != nil {
		t.Fatalf("get_loglevel after the page's debug: %+v %v", ws, err)
	}
	if err := e.ws.Call(e.ctx, "set_loglevel", map[string]any{"console_loglevel": "warning"}, &ws); err != nil {
		t.Fatal(err)
	}
	var page level
	e.json(http.MethodGet, "/operation/log/level", "", http.StatusOK, &page)
	if page.Level != "warn" || e.lvl.Level() != slog.LevelWarn {
		t.Fatalf("the page's level after set_loglevel warning: %+v (LevelVar %v)", page, e.lvl.Level())
	}

	e.log.Info("fabricserver.test.below") // under the gate: neither ring nor stderr
	e.log.Warn("fabricserver.test.marker", slog.Group("peer", slog.Int("node", 7)))
	var log struct {
		Records []struct {
			Seq   uint64            `json:"seq"`
			Level string            `json:"level"`
			Msg   string            `json:"msg"`
			Attrs map[string]string `json:"attrs"`
		} `json:"records"`
	}
	e.json(http.MethodGet, "/operation/log", "", http.StatusOK, &log)
	var found bool
	for _, r := range log.Records {
		if r.Msg == "fabricserver.test.below" {
			t.Fatalf("a record under the gate is in the ring: %+v", r)
		}
		if r.Msg == "fabricserver.test.marker" {
			found = r.Level == "warn" && r.Attrs["peer.node"] == "7"
		}
	}
	if !found {
		t.Fatalf("the daemon's record is not in the page's log: %+v", log.Records)
	}

	var info struct {
		Version string `json:"version"`
	}
	e.json(http.MethodGet, "/operation/info", "", http.StatusOK, &info)
	if info.Version == "" {
		t.Fatalf("info %+v", info)
	}
}

// TestOperationsAdapter: the port maps the ring one to one and observes
// what arrives.
func TestOperationsAdapter(t *testing.T) {
	lvl := new(slog.LevelVar)
	log, ring := newLogger(io.Discard, lvl)
	ops := operations{ring: ring, started: time.Now().Add(-time.Minute)}
	got := make(chan fabricui.LogRecord, 1)
	stop := ops.ObserveLog(func(r fabricui.LogRecord) { got <- r })
	defer stop()
	log.With(slog.String("node", "second")).Info("fabricserver.bridge.ready")
	select {
	case r := <-got:
		if r.Seq != 1 || r.Message != "fabricserver.bridge.ready" || r.Attrs["node"] != "second" || r.Level != slog.LevelInfo {
			t.Fatalf("observed %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no record observed")
	}
	if recs := ops.Log(1, 0); len(recs) != 1 || recs[0].Seq != 1 {
		t.Fatalf("log %+v", recs)
	}
	if err := ops.SetLogLevel(slog.LevelError); err != nil || lvl.Level() != slog.LevelError || ops.LogLevel() != slog.LevelError {
		t.Fatalf("SetLogLevel: %v, LevelVar %v", err, lvl.Level())
	}
	if in := ops.Info(); in.Uptime < time.Minute || in.StartedAt.IsZero() {
		t.Fatalf("info %+v", in)
	}
	if ops.ObserveLog(nil) == nil {
		t.Fatal("ObserveLog(nil) returned no unsubscribe")
	}
}
