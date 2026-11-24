// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package daemon is the composition root of fabric-server: the flags, the
// controller and the controller half of the API, the node and the bridge
// half, the HTTP listener and its routes. cmd/fabric-server is a thin
// main around [Run].
package daemon

import (
	"bytes"
	"encoding/json"
	"expvar"
	"net/http"
	"strings"

	"thingfabric.dev/go-fabric-ui/fabricui"

	"thingfabric.dev/go-fabric-server/bridgeapi"
	"thingfabric.dev/go-fabric-server/controllerapi"
	"thingfabric.dev/go-fabric-server/protocol"
	"thingfabric.dev/go-fabric-server/protocol/schema"
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
// is deleted), GET /health, GET /metrics (the daemon's expvar variables,
// metricsHandler), GET /schema (the
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
	mux.HandleFunc("GET /metrics", metricsHandler)
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

// metricsPrefix is the prefix of the daemon's own expvar variables (the
// controller's, each bridge half's, /ws's).
const metricsPrefix = "fabricserver."

// metricsHandler is GET /metrics: one JSON object in expvar's shape with
// the variables under metricsPrefix and memstats, nothing else.
// expvar.Handler would also publish cmdline — the daemon's arguments, so
// --bridge-passcode and --token when they are given as flags.
func metricsHandler(w http.ResponseWriter, _ *http.Request) {
	var b bytes.Buffer
	b.WriteString("{\n")
	first := true
	expvar.Do(func(kv expvar.KeyValue) {
		if kv.Key != "memstats" && !strings.HasPrefix(kv.Key, metricsPrefix) {
			return
		}
		key, err := json.Marshal(kv.Key)
		if err != nil {
			return
		}
		if !first {
			b.WriteString(",\n")
		}
		first = false
		b.Write(key)
		b.WriteString(": ")
		b.WriteString(kv.Value.String())
	})
	b.WriteString("\n}\n")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b.Bytes())
}
