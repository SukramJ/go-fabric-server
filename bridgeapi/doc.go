// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package bridgeapi is the bridge half of go-fabric-server: the /bridge
// API through which a host that is not written in Go declares devices,
// sends state deltas and answers what a controller asks of them (concept
// v8 C.3, brief S2 §1).
//
// The half maps go-fabric's device kit one-to-one: a declaration is
// device.New with each cluster's generated <cluster>def.Declare, its
// initial state decoded into the generated Patch (clusters_gen.go, written
// by internal/gen from the snapshot's cluster list, holds one kit per
// typed package); the whole document is one Aggregator.Batch. set is the
// typed Server.Set, host is Device.Update, SetReachable and Rename. A
// change the host did not make is the changed event; a write or a command
// reaches the host as a write or command event only when the declaration
// opted in (OnWrite, Handle<Command>), with a correlation and a deadline.
//
// Lifecycle: [New] → wire [Server.Observers] into node.Config.Observers →
// node.New and Start → [Server.Start] with the node → serve
// [Server.ServeHTTP] at /bridge → [Server.Shutdown] → stop the node. One
// bridge client at a time.
package bridgeapi
