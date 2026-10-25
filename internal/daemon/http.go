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

// MuxWith is the server's HTTP surface: /ws (the controller half),
// /bridge (the bridge half), GET /health, GET /metrics (expvar), GET
// /schema (the generated JSON Schema of the values), GET
// /schema/declaration (the bridge half's declaration); every route behind
// the token when one is set. A nil half is not served, and /health is
// healthy when every half served is.
func MuxWith(api *controllerapi.Server, br *bridgeapi.Server, token, version string) http.Handler {
	mux := http.NewServeMux()
	if api != nil {
		mux.Handle("/ws", api)
	}
	if br != nil {
		mux.Handle("/bridge", br)
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		healthy, nodes := api != nil || br != nil, 0
		body := map[string]any{"version": version}
		if api != nil {
			var ok bool
			ok, nodes = api.Health()
			healthy = healthy && ok
		}
		body["node_count"] = nodes
		if br != nil {
			ok, devices := br.Health()
			healthy = healthy && ok
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
