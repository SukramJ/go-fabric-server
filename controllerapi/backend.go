// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// Backend is the controller the API drives — go-fabric's
// *controller.Controller satisfies it. Everything the API does goes
// through these three routes and the client nodes they hand out.
type Backend interface {
	// Registry is the registry of the paired nodes.
	Registry() *client.Registry
	// Fabric is our fabric.
	Fabric() *commissioner.Fabric
	// Pair commissions the device a QR or manual pairing code names.
	Pair(ctx context.Context, code string, opts controller.PairOptions) (*client.Node, error)
}

// Discoverer is the optional route of discover_commissionable_nodes and
// commission_on_network: find the commissionable devices id selects
// within timeout. go-fabric's controller facade has no such method yet
// (report S1, Questions Q3); fabrictest.Controller has it, so the
// in-process tests exercise the commands. A backend without it answers
// both commands with InvalidCommand.
type Discoverer interface {
	Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]client.Device, error)
}

var _ Backend = (*controller.Controller)(nil)
