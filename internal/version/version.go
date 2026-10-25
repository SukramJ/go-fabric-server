// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package version is the build metadata of go-fabric-server, set at link
// time (-ldflags -X); the defaults keep go run and the tests working.
package version

import "runtime/debug"

// Build-time information. Version tracks the latest release tag so a
// build without -ldflags still reports a sensible version; bump it with
// the release's changelog entry.
var (
	// Version is the release (e.g. "0.1.0").
	Version = "0.3.0"
	// Commit is the short Git SHA the binary was built from.
	Commit = "none"
	// BuildDate is the UTC RFC 3339 build time.
	BuildDate = "unknown"
)

// GoFabric is the version of the go-fabric module the binary is built
// with.
func GoFabric() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range info.Deps {
		if d.Path == "github.com/SukramJ/go-fabric" {
			if d.Replace != nil {
				return d.Replace.Version
			}
			return d.Version
		}
	}
	return "unknown"
}

// String is a one-line build banner.
func String() string {
	return "go-fabric-server " + Version + " (go-fabric " + GoFabric() + ", commit " + Commit + ", built " + BuildDate + ")"
}
