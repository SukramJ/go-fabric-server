// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Command fabric-server is the go-fabric-server daemon: a WebSocket/JSON
// API over go-fabric. The controller half at /ws is wire-compatible with
// matterjs-server (schema 14); the bridge half arrives with S2. Run with
// -h for the flags; docs/operations.md describes files, ports and the
// token.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/SukramJ/go-fabric-server/internal/daemon"
)

func main() {
	cfg, err := daemon.ParseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fabric-server:", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, cfg, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "fabric-server:", err)
		stop()
		os.Exit(1) //nolint:gocritic // stop ran
	}
}
