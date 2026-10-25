// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package arch holds the architecture guard of the module: go-fabric is
// used through its facades only, and the layers of the server keep their
// order (concept v8 B.1, E.3). It is the counterpart of go-fabric's
// script/archcheck, as a test.
package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	module   = "github.com/SukramJ/go-fabric-server"
	goFabric = "github.com/SukramJ/go-fabric/"
)

// facade are the go-fabric packages the server may import: the roles'
// facades and what their exported signatures hand out (the client/*
// packages the controller exposes, the attestation and commissioner types
// of controller.Config and controller.PairOptions), the snapshot, the
// generated schema and the <cluster>def packages under cluster/spec/.
var facade = map[string]bool{
	"node":                    true,
	"node/metrics/expvar":     true,
	"device":                  true,
	"contract":                true,
	"parity":                  true,
	"schema":                  true,
	"controller":              true,
	"controller/client":       true,
	"controller/commissioner": true,
	"controller/attestation":  true,
	// The trust anchors of a production attestation store: the facade has
	// no one-call production store (report S1, Questions Q10).
	"controller/dcl/snapshot": true,
	// The DCL client of get_vendor_names (brief S2 §2): the ledger's
	// vendor records, as matterjs-server's vendor info service reads them.
	"controller/dcl": true,
	// The BLE central type of controller.PairOptions.BLE, and the Thread
	// dataset codec the CJ report names as the dataset reader (go-fabric
	// notes/briefs/reports/CJ.md, 1.13: controller/thread.DecodeDataset).
	"controller/ble":     true,
	"ble/bluez":          true,
	"controller/thread":  true,
	"client/discovery":   true,
	"client/exchange":    true,
	"client/handshake":   true,
	"client/interaction": true,
	"client/peer":        true,
}

// exceptions are imports below the facade one package may make, each a
// reported facade gap. Empty since S3: go-fabric v0.12.1's JSON route
// (client.EncodeJSON/DecodeJSON) closed Q1, and internal/wire is gone.
var exceptions = map[string]map[string]string{}

// testOnly are the go-fabric packages a test may import besides.
var testOnly = map[string]bool{"fabrictest": true, "clock": true}

// layers are the module's own import rules: a package may import only the
// module packages its entry lists ("" is any).
var layers = map[string][]string{
	"protocol":        {},
	"protocol/schema": {},
	"internal/gen":    {},
	"internal/arch":   {},
	"client":          {"protocol"},
	"controllerapi":   {"protocol", "protocol/schema"},
	"bridgeapi":       {"protocol", "protocol/schema"},
}

// testLayers widen layers for a package's tests.
var testLayers = map[string][]string{
	"controllerapi": {"client", "internal/daemon"},
	"bridgeapi":     {"client", "internal/daemon", "controllerapi"},
}

func isDef(rel string) bool {
	return strings.HasPrefix(rel, "cluster/spec/") && !strings.Contains(strings.TrimPrefix(rel, "cluster/spec/"), "/")
}

func TestImports(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		files++
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := filepath.ToSlash(rel)
		isTest := strings.HasSuffix(path, "_test.go")
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			where := filepath.ToSlash(strings.TrimPrefix(path, root+string(os.PathSeparator)))
			switch {
			case strings.HasPrefix(p, goFabric):
				if !facadeImport(pkg, strings.TrimPrefix(p, goFabric), isTest) {
					t.Errorf("%s imports %s: below go-fabric's facade", where, p)
				}
			case p == module || strings.HasPrefix(p, module+"/"):
				dep := strings.TrimPrefix(strings.TrimPrefix(p, module), "/")
				allowed, ruled := layers[pkg]
				if !ruled {
					continue
				}
				ok := dep == pkg // an external test package imports its own
				for _, a := range append(allowed, testLayersFor(pkg, isTest)...) {
					if dep == a {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s imports %s: outside the layers of %s", where, p, pkg)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 {
		t.Fatalf("walked %d files: the root is wrong", files)
	}
}

// facadeImport reports whether package pkg may import go-fabric's dep.
func facadeImport(pkg, dep string, isTest bool) bool {
	if facade[dep] || isDef(dep) || isTest && testOnly[dep] {
		return true
	}
	_, ok := exceptions[pkg][dep]
	return ok
}

// TestGuardRefuses holds the guard itself: what it must refuse, it does.
func TestGuardRefuses(t *testing.T) {
	for _, c := range []struct {
		pkg, dep string
		test     bool
	}{
		{"controllerapi", "tlv", false},
		{"controllerapi", "cluster/spec", false},
		{"controllerapi", "im", false},
		{"controllerapi", "fabrictest", false},
		{"protocol", "transport/udp", false},
		{"client", "secure/setup", true},
		{"controllerapi", "cluster/spec/onoff/internal", false},
		{"bridgeapi", "cluster/spec", false},
		{"bridgeapi", "endpoint", false},
		{"bridgeapi", "bridge", false},
		{"bridgeapi", "im", false},
		// The Q1 exception is gone (S3): no package reads TLV.
		{"controllerapi", "tlv", false},
		{"internal/wire", "tlv", false},
	} {
		if facadeImport(c.pkg, c.dep, c.test) {
			t.Errorf("%s may not import %s", c.pkg, c.dep)
		}
	}
	for _, c := range []struct {
		pkg, dep string
		test     bool
	}{
		{"controllerapi", "controller/client", false},
		{"controllerapi", "cluster/spec/onoff", false},
		{"controllerapi", "fabrictest", true},
		{"bridgeapi", "cluster/spec/powersource", false},
		{"bridgeapi", "device", false},
		{"bridgeapi", "node", false},
	} {
		if !facadeImport(c.pkg, c.dep, c.test) {
			t.Errorf("%s may import %s", c.pkg, c.dep)
		}
	}
}

func testLayersFor(pkg string, isTest bool) []string {
	if !isTest {
		return nil
	}
	return testLayers[pkg]
}

// TestProtocolKnowsNoGoFabric: protocol and its schema tables import no
// go-fabric package at all (concept v8 E.3: "protocol kennt kein Matter").
func TestProtocolKnowsNoGoFabric(t *testing.T) {
	for _, dir := range []string{"protocol", "protocol/schema"} {
		entries, err := os.ReadDir(filepath.Join("..", "..", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "..", dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if strings.Contains(imp.Path.Value, goFabric) {
					t.Errorf("%s/%s imports %s", dir, e.Name(), imp.Path.Value)
				}
			}
		}
	}
}
