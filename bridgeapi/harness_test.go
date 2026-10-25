// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/internal/daemon"
)

// The tests run both halves of the daemon in one process on a fabrictest
// site: the bridge half drives the site device's node — a node the site
// builds (fabrictest.Device.Node), because a node.New of the daemon's own
// cannot join a site (the site wires its in-memory network and clock
// through seams below the facade) — and the controller half drives the
// site's controller, as in controllerapi's tests. The site runs on the
// wall clock: the host answers over a real WebSocket, whose round trip no
// site counter sees, so a fake clock would be stepped past a result's
// deadline while the answer is on its way.

type env struct {
	t    *testing.T
	ctx  context.Context
	site *fabrictest.Site
	dev  *fabrictest.Device
	br   *bridgeapi.Server
	api  *controllerapi.Server
	base string
	host *fsclient.Bridge
	ctl  *fsclient.Client
}

func logger() *slog.Logger {
	if os.Getenv("FABRIC_SERVER_TEST_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.DiscardHandler)
}

func newEnv(t *testing.T, opts bridgeapi.Options) *env {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: logger()})
	opts.Version, opts.GoFabricVersion, opts.Logger = "test", "v-test", logger()
	if opts.SubscriptionPoll == 0 {
		opts.SubscriptionPoll = 50 * time.Millisecond
	}
	br := bridgeapi.New(opts)
	dev := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) { cfg.Observers = br.Observers() }})
	if err := br.Start(ctx, dev.Node()); err != nil {
		t.Fatal(err)
	}
	api, err := controllerapi.New(controllerapi.Options{
		Version: "test", GoFabricVersion: "v-test", Logger: logger(), StoragePath: t.TempDir(),
		Attestation: attestation.Accept(), // the site device's test certificate
	})
	if err != nil {
		t.Fatal(err)
	}
	// The controller half on controller.New, wired to the site
	// (fabrictest.Site.ControllerConfig, go-fabric v0.12.1).
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: logger()})
	cfg.HooksFor = api.Hooks
	ctrl, err := controller.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_ = ctrl.Close(cctx)
	})
	if err := api.Start(ctx, ctrl); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.MuxWith(api, br, "", "test"))
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		br.Shutdown(sctx)
		api.Shutdown(sctx)
		srv.Close()
	})
	e := &env{t: t, ctx: ctx, site: site, dev: dev, br: br, api: api, base: "ws" + strings.TrimPrefix(srv.URL, "http")}
	e.host = e.dialBridge(fsclient.Options{})
	return e
}

func (e *env) dialBridge(o fsclient.Options) *fsclient.Bridge {
	e.t.Helper()
	b, err := fsclient.DialBridge(e.ctx, e.base+"/bridge", o)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = b.Close() })
	return b
}

// controller connects the controller half and commissions the bridge
// node through it; it returns the node id.
func (e *env) controller() uint64 {
	e.t.Helper()
	c, err := fsclient.Dial(e.ctx, e.base+"/ws", fsclient.Options{})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.Close() })
	e.ctl = c
	qr, _, err := e.host.PairingCodes(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	n, err := c.CommissionWithCode(e.ctx, qr, true)
	if err != nil {
		e.t.Fatalf("commission_with_code: %v", err)
	}
	return n.NodeID
}

// event waits for an event of name that match accepts, on c's events.
func event(t *testing.T, c *fsclient.Client, name string, match func(json.RawMessage) bool) json.RawMessage {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			if ev.Event == name && (match == nil || match(ev.Data)) {
				return ev.Data
			}
		case <-timeout:
			t.Fatalf("no %s event", name)
			return nil
		}
	}
}

// bridgeEvent waits for a bridge event of name for device.
func (e *env) bridgeEvent(name, device string) fsclient.BridgeEvent {
	e.t.Helper()
	var out fsclient.BridgeEvent
	event(e.t, e.host.Client, name, func(raw json.RawMessage) bool {
		var ev fsclient.BridgeEvent
		decode(e.t, raw, &ev)
		if ev.Device != device {
			return false
		}
		out = ev
		return true
	})
	return out
}

func decode(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func doc(s string) json.RawMessage { return json.RawMessage(s) }
