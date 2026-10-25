// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCommittedOutputIsCurrent fails when protocol/schema does not match
// what the generator writes from the go-fabric module this build uses — a
// go-fabric bump without `go run ./internal/gen`, or a hand edit.
func TestCommittedOutputIsCurrent(t *testing.T) {
	files, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if v := goFabricVersion(); v == "unknown" || v == "" {
		t.Fatalf("go-fabric version %q: the build carries no module information", v)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		// A checkout that converted line endings is not stale.
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s is stale: run go run ./internal/gen and commit the output", name)
		}
	}
}

func TestCamelize(t *testing.T) {
	for in, want := range map[string]string{
		"OnOff": "onOff", "DSTOffset": "dstOffset", "PINCode": "pinCode", "VendorID": "vendorId",
		"NOCs": "nocs", "IPv6Addresses": "iPv6Addresses", "MeasuredValue100ths": "measuredValue100ths",
		"PIROccupiedToUnoccupiedDelay": "pirOccupiedToUnoccupiedDelay", "FabricIndex": "fabricIndex", "ACL": "acl",
	} {
		if got := camelize(in); got != want {
			t.Errorf("camelize(%q) = %q, want %q", in, got, want)
		}
	}
}
