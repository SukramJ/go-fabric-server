// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package daemon is the composition root of fabric-server: the flags, the
// controller, the controller half of the API, the HTTP listener and its
// routes. cmd/fabric-server is a thin main around [Run].
package daemon

import (
	"encoding/json"
	"expvar"
	"net/http"

	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// Mux is the server's HTTP surface: /ws (the controller half), GET
// /health, GET /metrics (expvar), GET /schema (the generated JSON Schema);
// every route behind the token when one is set.
func Mux(api *controllerapi.Server, token, version string) http.Handler {
	mux := http.NewServeMux()
	if api != nil {
		mux.Handle("/ws", api)
	}
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		healthy, nodes := false, 0
		if api != nil {
			healthy, nodes = api.Health()
		}
		body := map[string]any{"version": version, "node_count": nodes, "healthy": healthy}
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
	return protocol.RequireToken(token, mux)
}
