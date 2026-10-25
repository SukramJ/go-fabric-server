// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command gen generates protocol/schema from go-fabric's snapshot of the
// matter.js model (parity.SchemaJSON, the bytes of go-fabric's
// parity/schema.json): tables_gen.go, the clusters, attributes, commands,
// events, datatypes and device types as Go tables, and schema.json, the
// JSON Schema (draft 2020-12) of the node data values that GET /schema
// serves. Both carry the go-fabric version they were generated from.
//
// Usage, from the module root:
//
//	go run ./internal/gen             # regenerate protocol/schema and bridgeapi/clusters_gen.go
//	go run ./internal/gen -root DIR   # below DIR instead of the module root
//
// It also writes declaration.schema.json, the JSON Schema of the bridge
// half's declaration, and bridgeapi/clusters_gen.go, the per-cluster table
// of go-fabric's typed cluster packages the declaration decoder goes
// through; for that it reads the package list of the go-fabric module
// (go list -m).
//
// The test of this package fails when the committed output is stale —
// after a go-fabric bump, run the generator and commit its output.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", ".", "the module root the output paths are relative to")
	flag.Parse()
	files, err := Generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(*root, filepath.FromSlash(name)), content, 0o644); err != nil { //nolint:gosec // generated sources are world-readable
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}
