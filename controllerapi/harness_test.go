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

	"thingfabric.dev/go-fabric/clock"
	onoffdef "thingfabric.dev/go-fabric/cluster/spec/onoff"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/camera"
	"thingfabric.dev/go-fabric/controller/camera/cameratest"
	"thingfabric.dev/go-fabric/controller/topology"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"

	fsclient "thingfabric.dev/go-fabric-server/client"
	"thingfabric.dev/go-fabric-server/controllerapi"
	"thingfabric.dev/go-fabric-server/internal/daemon"
)

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
	ctrl   *controller.Controller
	cam    *camera.Service
	// camera is the site's camera and port, with envOptions.camera:
	// go-fabric's test camera (controller/camera/cameratest).
	camera *cameratest.Camera
	port   *cameratest.Port
	// subscribed: the server subscribes to a commissioned node itself
	// (Options.DisableAutoSubscription unset), so the node becomes
	// available on its own.
	subscribed bool
}

type envOptions struct {
	server controllerapi.Options
	token  string
	// noDial skips the client connection.
	noDial bool
	// configure edits the controller's configuration.
	configure func(*controller.Config)
	// camera adds a camera beside the light (cameratest): the site
	// runs on loopback sockets then, since the camera's offers need the
	// device's TCP.
	camera bool
	// noCameraService starts the server without its camera service.
	noCameraService bool
	// onEvicted, when set, runs on the eviction path before the server
	// hears of an evicted stream — before the start that evicted it
	// allocates again.
	onEvicted func(camera.StreamEvicted)
}

func logger() *slog.Logger {
	if os.Getenv("FABRIC_SERVER_TEST_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.DiscardHandler)
}

func newEnv(t *testing.T, o envOptions) *env {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: logger(), Loopback: o.camera})
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light:1"), Name: "light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(false)}})
	if err != nil {
		t.Fatal(err)
	}
	top := fabrictest.Topology{Bridged: []node.Endpoint{light}}
	var (
		cam  *cameratest.Camera
		port *cameratest.Port
	)
	if o.camera {
		if cam, err = cameratest.New(cameratest.Options{}); err != nil {
			t.Fatal(err)
		}
		port = cam.Port()
		top.Bridged = append(top.Bridged, cam.Device())
		top.Configure = func(cfg *node.Config) { cfg.Network.TCP = node.TCPConfig{Incoming: true} }
	}
	dev := site.AddDevice(top)
	if o.server.Version == "" {
		o.server.Version = "test"
	}
	if o.server.GoFabricVersion == "" {
		o.server.GoFabricVersion = "v-test"
	}
	if o.server.Logger == nil {
		o.server.Logger = logger()
	}
	// The site's devices carry test certificates, which the site's trust
	// store accepts; their test Certification Declaration is an info
	// finding the server's policy proceeds on.
	if o.server.StoragePath == "" {
		o.server.StoragePath = t.TempDir()
	}
	api, err := controllerapi.New(o.server)
	if err != nil {
		t.Fatal(err)
	}
	// The daemon's own composition on the site (go-fabric v0.12.1,
	// Site.ControllerConfig): controller.New with the site's network,
	// DNS-SD and clock, the server's hooks per node, the camera side with
	// outgoing TCP and the cameras' reports (brief S8 §1).
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{Logger: logger()})
	cfg.HooksFor = api.Hooks
	if o.configure != nil {
		o.configure(&cfg)
	}
	var changes topology.Changes
	changes.Install(&cfg)
	cfg.Camera = &controller.CameraConfig{}
	cfg.TCP = controller.TCPConfig{Outgoing: true}
	var reports camera.Reports
	reports.Install(&cfg)
	ctrl, err := controller.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	topo := topology.New(ctrl, topology.Config{Changes: &changes, Clock: cfg.Clock})
	observer := api.CameraObserver()
	if hook, forward := o.onEvicted, observer.StreamEvicted; hook != nil {
		observer.StreamEvicted = func(ev camera.StreamEvicted) {
			hook(ev)
			forward(ev)
		}
	}
	svc, err := camera.New(ctx, ctrl, camera.Config{Reports: &reports, Observer: observer, Logger: logger()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_ = svc.Close(cctx)
		topo.Close()
		_ = ctrl.Close(cctx)
	})
	services := controllerapi.Services{Topology: topo, Camera: svc}
	if o.noCameraService {
		services.Camera = nil
	}
	if err := api.Start(ctx, ctrl, services); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(daemon.Mux(api, o.token, "test"))
	t.Cleanup(func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		api.Shutdown(sctx)
		srv.Close()
	})
	e := &env{t: t, site: site, light: light, dev: dev, api: api, ctrl: ctrl, cam: svc, camera: cam, port: port, url: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws", ctx: ctx, subscribed: !o.server.DisableAutoSubscription}
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
// QR code and returns the node data the command answered — with the
// server's own subscription, once the node is primed (waitPrimed).
func (e *env) commission() fsclient.Node {
	e.t.Helper()
	payload, err := e.dev.Node().SetupPayload()
	if err != nil {
		e.t.Fatal(err)
	}
	qr := payload.QR
	n, err := e.client.CommissionWithCode(e.ctx, qr, true)
	if err != nil {
		e.t.Fatalf("commission_with_code: %v", err)
	}
	if e.subscribed {
		e.waitPrimed(n.NodeID)
	}
	return n
}

// waitPrimed waits until the server's entry of the node is primed. The
// commissioning answers before the node's subscription is up: the node is
// already available then (Reconnecting counts, #81), but a change it
// reports before the server saw it Connected is sent as node_updated only,
// never as attribute_updated (cache.go nodeState.primed,
// docs/websockets_api.md:2065), so a test that waits for attribute_updated
// first waits for this (#78). get_node refreshes the node's entry, so the
// poll also primes it.
func (e *env) waitPrimed(nodeID uint64) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		got, err := e.client.GetNode(e.ctx, nodeID)
		if err == nil && got.Available && e.api.Primed(nodeID) {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("node %d not primed: %+v %v", nodeID, got, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
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
