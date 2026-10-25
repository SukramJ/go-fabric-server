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
// through these routes and the client nodes they hand out.
type Backend interface {
	// Registry is the registry of the paired nodes.
	Registry() *client.Registry
	// Fabric is our fabric.
	Fabric() *commissioner.Fabric
	// Pair commissions the device a QR or manual pairing code names.
	Pair(ctx context.Context, code string, opts controller.PairOptions) (*client.Node, error)
	// Discover finds the commissionable devices id selects within
	// timeout (controller.Controller.Discover, go-fabric v0.12.1).
	Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]client.Device, error)
}

var _ Backend = (*controller.Controller)(nil)
