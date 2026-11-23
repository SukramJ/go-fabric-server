// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
)

// webHandler is the pages of go-fabric-ui (0.13.0, docs/embedding.md):
// the node family over the device nodes — the default node of the
// --bridge-* flags (Options.Node, its configuration block
// Options.NodeConfig) and the node registry beside it (Options.Registry:
// the nodes the pages create, rename and delete), each erased in place by
// life (Options.Erase, NodeEntry.Erase) and a failed restart after an
// erase its warning (Options.Warnings) —, with no Exposure and
// its reason (the client decides what is exposed over /bridge), the Operation page over
// the daemon's log ring (ops), the controller family over ctrl (its
// graph, /controller/topology, the topology service /ws's
// get_network_topology answers from, controllerSource), and the
// admin rule of the flags for both — a controller route marked admin
// follows the same Options.Admin as a node page that writes. No node or
// a nil ctrl serves that family as disabled, a nil ops no Operation page.
// The controller node's ping is go-fabric-ui's own (its gofabric
// adapter's ControllerNode.Ping); the daemon passes nothing for it.
//
// go-fabric-ui checks every request's Host: loopback names and IP
// addresses pass, a DNS name only when --web-allowed-hosts lists it
// (Options.AllowedHosts) — or, with --web-proxy-source, when the request
// comes from that address (Options.AllowHost). go-fabric-ui hands its
// host check to such a proxy (fabricui/options.go:320-329: it must answer
// from what only the proxy can produce; the proxy's authentication of the
// user and its server names are the check then), so the flag names an
// authenticating proxy that forwards the browser's Host, never an address
// a browser connects from itself; --web-admin-source alone keeps the
// check. done ends the event streams (Options.Done); nil never does.
func webHandler(c Config, life *nodeLifecycle, def *bridgeHalf, reg *nodeRegistry, ctrl fabricui.ControllerSource, ev *fabricui.Events, ops fabricui.Operations, log *slog.Logger, done <-chan struct{}) (http.Handler, error) {
	o := fabricui.Options{
		Events: ev, Logger: log, Controller: ctrl, Operations: ops,
		ExposureReason: exposureReason, Done: done,
		AllowedHosts: hostList(c.WebAllowedHosts),
	}
	if def != nil {
		// The pages read the half's switch, never a bare *node.Node, so
		// they follow the node an erase brings back (fabricui.NodeSwitch).
		o.Node = def.sw
		o.NodeConfig = defaultNodeConfig(def.spec)
		o.Erase = life.eraser(def)
		if reg != nil {
			o.Registry = reg
		}
	}
	if life != nil {
		o.Warnings = life.warnings
	}
	admin, err := webAdmin(c)
	if err != nil {
		return nil, err
	}
	o.Admin = admin
	if c.WebProxySource != "" {
		proxy, err := netip.ParseAddr(c.WebProxySource)
		if err != nil {
			return nil, fmt.Errorf("--web-proxy-source %q: %w", c.WebProxySource, err)
		}
		o.AllowHost = func(r *http.Request) bool { return fromAddr(r, proxy) }
	}
	h, err := fabricui.NewHandler(o)
	if err != nil {
		if c.WebAllowedHosts != "" {
			return nil, fmt.Errorf("--web-allowed-hosts: %w", err)
		}
		return nil, err
	}
	if c.WebAdminSource == "" {
		return h, nil
	}
	// Behind an Ingress the proxy is the only client: with host networking
	// the listener is on the LAN too, and every other source is refused
	// (the add-on's form, go-ha2fabric's WEB_ALLOW).
	src, _ := netip.ParseAddr(c.WebAdminSource) // checked by webAdmin
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fromAddr(r, src) {
			http.Error(w, "the pages answer the Ingress proxy only", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
}

// hostList is --web-allowed-hosts as a list, each entry trimmed; nil for
// none. The entries are go-fabric-ui's to validate (NewHandler).
func hostList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	for name := range strings.SplitSeq(value, ",") {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// defaultNodeConfig is the default node's configuration block on its
// status page: the --bridge-* flags.
func defaultNodeConfig(s nodeSpec) *fabricui.NodeConfigView {
	listen := s.Listen
	if listen == "" {
		listen = ":5540"
	}
	return &fabricui.NodeConfigView{
		Source: fabricui.NodeConfigSourceConfiguration, SourceNote: "the --bridge-* flags (FABRIC_SERVER_BRIDGE_*)",
		Label: s.Label, Listen: listen, Discriminator: uint16(s.Discriminator), //nolint:gosec // checked by bridgeConfig before the node starts
		VendorID: uint16(s.VendorID), VendorName: bridgeVendorName, //nolint:gosec // as above
		ProductID: uint16(s.ProductID), ProductName: bridgeProductName, //nolint:gosec // as above
		Store: s.Store,
	}
}

// webAdmin is Options.Admin: with --web-admin-source an admin is a request
// from that address (the Ingress proxy vouches for its user), with
// --web-admin nil (the page's token mode), else nobody (read-only). With
// --web-admin-source the pages answer that address only (webHandler).
func webAdmin(c Config) (func(*http.Request) bool, error) {
	switch {
	case c.WebAdminSource != "":
		src, err := netip.ParseAddr(c.WebAdminSource)
		if err != nil {
			return nil, fmt.Errorf("--web-admin-source %q: %w", c.WebAdminSource, err)
		}
		return func(r *http.Request) bool { return fromAddr(r, src) }, nil
	case c.WebAdmin:
		return nil, nil
	}
	return func(*http.Request) bool { return false }, nil
}

// fromAddr reports whether the request's peer is src.
func fromAddr(r *http.Request, src netip.Addr) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	return ap.Addr().Unmap() == src.Unmap()
}

// serveWeb serves the pages on --web until the returned stop, which ends
// the event streams first (Options.Done), so the server's shutdown does
// not wait for them.
func serveWeb(ctx context.Context, c Config, life *nodeLifecycle, def *bridgeHalf, reg *nodeRegistry, ctrl fabricui.ControllerSource, ev *fabricui.Events, ops fabricui.Operations, log *slog.Logger) (func(), error) {
	done := make(chan struct{})
	var once sync.Once
	end := func() { once.Do(func() { close(done) }) }
	h, err := webHandler(c, life, def, reg, ctrl, ev, ops, log, done)
	if err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", c.Web)
	if err != nil {
		return nil, fmt.Errorf("--web: %w", err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("fabricserver.web.serve", slog.Any("error", err))
		}
	}()
	log.Info("fabricserver.web.listening", slog.String("listen", ln.Addr().String()), slog.Bool("admin", c.WebAdmin || c.WebAdminSource != ""), slog.Bool("node", def != nil), slog.Bool("registry", reg != nil), slog.Bool("controller", ctrl != nil))
	return func() {
		end()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}, nil
}
