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
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/node"
)

// webHandler is the node pages of go-fabric-ui (v0.1.1, docs/embedding.md):
// the bridge node's family with no Exposure (the host decides what is
// exposed over /bridge) and the admin rule of the flags. A nil node serves
// the family as disabled. The controller pages come with go-fabric-ui's
// U2.
func webHandler(c Config, n *node.Node, ev *fabricui.Events, log *slog.Logger) (http.Handler, error) {
	o := fabricui.Options{Events: ev, Logger: log}
	if n != nil {
		o.Node = n
	}
	admin, err := webAdmin(c)
	if err != nil {
		return nil, err
	}
	o.Admin = admin
	h := fabricui.Handler(o)
	if c.WebAdminSource == "" {
		return h, nil
	}
	// Behind an Ingress the proxy is the only client: with host networking
	// the listener is on the LAN too, and every other source is refused
	// (the add-on's form, go-ha2fabric's WEB_ALLOW).
	src, _ := netip.ParseAddr(c.WebAdminSource) // checked by webAdmin
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fromAddr(r, src) {
			http.Error(w, "the node pages answer the Ingress proxy only", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
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

// serveWeb serves the node pages on --web until the returned stop.
func serveWeb(ctx context.Context, c Config, n *node.Node, ev *fabricui.Events, log *slog.Logger) (func(), error) {
	h, err := webHandler(c, n, ev, log)
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
	log.Info("fabricserver.web.listening", slog.String("listen", ln.Addr().String()), slog.Bool("admin", c.WebAdmin || c.WebAdminSource != ""))
	return func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}, nil
}
