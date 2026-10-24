// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/internal/daemon"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// siteBackend is the server's controller in the tests: fabrictest's
// controller of the site. controller.New cannot take the site's network
// and clock (it opens its own UDP socket and DNS-SD browser; report S1,
// Questions Q1), so the tests drive fabrictest.AddController's stack —
// the same registry and commissioner types controller.New builds — and
// pair through fabrictest's CommissionNode, which runs the registry route
// controller.Pair runs (discover by discriminator, Registry.ForDevice,
// Registry.Commission) on the site's DNS-SD.
type siteBackend struct {
	c   *fabrictest.Controller
	dev *fabrictest.Device
}

func (b *siteBackend) Registry() *client.Registry      { return b.c.Registry() }
func (b *siteBackend) Fabric() *commissioner.Fabric    { return b.c.Fabric() }
func (b *siteBackend) Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]client.Device, error) {
	return b.c.Discover(ctx, id, timeout)
}

func (b *siteBackend) Pair(ctx context.Context, code string, opts controller.PairOptions) (*client.Node, error) {
	qr, manual, err := b.dev.Node().PairingCodes()
	if err != nil {
		return nil, err
	}
	if code != qr && code != manual {
		return nil, controller.ErrNoDevice
	}
	return b.c.CommissionNode(ctx, b.dev, client.CommissionOptions{AutoSubscribe: opts.AutoSubscribe})
}

// env is one test's site, server and client.
type env struct {
	t      *testing.T
	site   *fabrictest.Site
	light  *device.OnOffLight
	dev    *fabrictest.Device
	api    *controllerapi.Server
	url    string
	ctx    context.Context
	client *fsclient.Client
}

type envOptions struct {
	server controllerapi.Options
	token  string
	// noDial skips the client connection.
	noDial bool
}

func logger(t *testing.T) *slog.Logger {
	if os.Getenv("FABRIC_SERVER_TEST_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.DiscardHandler)
}

func newEnv(t *testing.T, o envOptions) *env {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: logger(t)})
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light:1"), Name: "light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}})
	if err != nil {
		t.Fatal(err)
	}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{light}})
	if o.server.Version == "" {
		o.server.Version = "test"
	}
	if o.server.GoFabricVersion == "" {
		o.server.GoFabricVersion = "v-test"
	}
	if o.server.Logger == nil {
		o.server.Logger = logger(t)
	}
	if o.server.StoragePath == "" {
		o.server.StoragePath = t.TempDir()
	}
	api, err := controllerapi.New(o.server)
	if err != nil {
		t.Fatal(err)
	}
	ctrl := site.AddController(fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) { rc.HooksFor = api.Hooks }})
	if err := api.Start(ctx, &siteBackend{c: ctrl, dev: dev}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.Mux(api, o.token, "test"))
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		api.Shutdown(sctx)
		srv.Close()
	})
	e := &env{t: t, site: site, light: light, dev: dev, api: api, url: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", ctx: ctx}
	if !o.noDial {
		e.client = e.dial(fsclient.Options{Token: o.token})
	}
	return e
}

func (e *env) dial(o fsclient.Options) *fsclient.Client {
	e.t.Helper()
	c, err := fsclient.Dial(e.ctx, e.url, o)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.Close() })
	return c
}

// commission pairs the site's light through commission_with_code with its
// QR code and returns the node data.
func (e *env) commission() fsclient.Node {
	e.t.Helper()
	qr, _, err := e.dev.Node().PairingCodes()
	if err != nil {
		e.t.Fatal(err)
	}
	n, err := e.client.CommissionWithCode(e.ctx, qr, true)
	if err != nil {
		e.t.Fatalf("commission_with_code: %v", err)
	}
	return n
}

// lightEndpoint is the light's endpoint number on the site device.
func (e *env) lightEndpoint() uint16 {
	ep, ok := e.light.EndpointNumber()
	if !ok {
		e.t.Fatal("the light has no endpoint")
	}
	return ep
}

// waitEvent waits for an event of name that match accepts.
func (e *env) waitEvent(c *fsclient.Client, name string, match func(json.RawMessage) bool) json.RawMessage {
	e.t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			if ev.Event == name && (match == nil || match(ev.Data)) {
				return ev.Data
			}
		case <-timeout:
			e.t.Fatalf("no %s event", name)
			return nil
		}
	}
}

func decode(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}
