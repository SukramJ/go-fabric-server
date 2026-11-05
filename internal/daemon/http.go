// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package daemon is the composition root of fabric-server: the flags, the
// controller and the controller half of the API, the node and the bridge
// half, the HTTP listener and its routes. cmd/fabric-server is a thin
// main around [Run].
package daemon

import (
	"encoding/json"
	"expvar"
	"net/http"

	"github.com/SukramJ/go-fabric-ui/fabricui"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// Mux is the server's HTTP surface with the controller half only; see
// [MuxWith].
func Mux(api *controllerapi.Server, token, version string) http.Handler {
	return MuxWith(api, nil, token, version)
}

// MuxWith is [MuxNodes] with one bridge half, the default node's; a nil
// br serves none.
func MuxWith(api *controllerapi.Server, br *bridgeapi.Server, token, version string) http.Handler {
	if br == nil {
		return MuxNodes(api, nil, token, version)
	}
	return MuxNodes(api, []BridgeHalf{{Key: fabricui.DefaultNodeKey, Server: br}}, token, version)
}

// MuxNodes is the server's HTTP surface over a fixed set of bridge
// halves, the default node's first; see [muxRoutes].
func MuxNodes(api *controllerapi.Server, bridges []BridgeHalf, token, version string) http.Handler {
	return muxRoutes(api, newBridgeRoutes(bridges...), token, version)
}

// muxRoutes is the server's HTTP surface: /ws (the controller half),
// /bridge (the default node's bridge half, the first of bridges) and
// /bridge/<key> (each node's, the default's too, looked up per request:
// a registry node's appears when it is created and is gone, 404, once it
// is deleted), GET /health, GET /metrics (expvar), GET /schema (the
// generated JSON Schema of the values), GET /schema/declaration (the
// bridge half's declaration); every route behind the token when one is
// set. A nil api is not served, nor /bridge without a default half; /health
// is healthy when every half served is, and its device_count sums the
// nodes'.
func muxRoutes(api *controllerapi.Server, bridges *bridgeRoutes, token, version string) http.Handler {
	mux := http.NewServeMux()
	if api != nil {
		mux.Handle("/ws", api)
	}
	if bridges.get("") != nil {
		mux.HandleFunc("/bridge", func(w http.ResponseWriter, r *http.Request) {
			bridges.get("").ServeHTTP(w, r)
		})
		mux.HandleFunc("/bridge/{key}", func(w http.ResponseWriter, r *http.Request) {
			key := r.PathValue("key")
			if key == "" {
				http.NotFound(w, r)
				return
			}
			srv := bridges.get(key)
			if srv == nil {
				http.NotFound(w, r)
				return
			}
			srv.ServeHTTP(w, r)
		})
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		halves := bridges.all()
		healthy, nodes := api != nil || len(halves) > 0, 0
		body := map[string]any{"version": version}
		if api != nil {
			var ok bool
			ok, nodes = api.Health()
			healthy = healthy && ok
		}
		body["node_count"] = nodes
		if len(halves) > 0 {
			devices := 0
			for _, b := range halves {
				ok, n := b.Server.Health()
				healthy = healthy && ok
				devices += n
			}
			body["device_count"] = devices
		}
		body["healthy"] = healthy
		w.Header().Set("Content-Type", "application/json")
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.Handle("GET /metrics", expvar.Handler())
	mux.HandleFunc("GET /schema", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/schema+json")
		w.Header().Set("X-Go-Fabric-Version", schema.GoFabricVersion)
		_, _ = w.Write(schema.JSONSchema())
	})
	mux.HandleFunc("GET /schema/declaration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/schema+json")
		w.Header().Set("X-Go-Fabric-Version", schema.GoFabricVersion)
		_, _ = w.Write(schema.DeclarationSchema())
	})
	return protocol.RequireToken(token, mux)
}
