// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"errors"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// statusOf is the Matter status a device answered with, when err is one —
// matterjs-server's statusResponseOf (WebSocketControllerHandler.ts:238-250).
func statusOf(err error) (status int, cluster *int, ok bool) {
	var se *client.StatusError
	if !errors.As(err, &se) {
		return 0, nil, false
	}
	if se.ClusterStatus != nil {
		c := int(*se.ClusterStatus)
		cluster = &c
	}
	return int(se.Status), cluster, true
}

// unreachable reports whether err says the node could not be reached:
// a timeout or a failure of the exchange layer.
func unreachable(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, exchange.ErrRetransmissionLimit) ||
		errors.Is(err, exchange.ErrNetwork) ||
		errors.Is(err, exchange.ErrTransient) ||
		errors.Is(err, exchange.ErrSessionClosed)
}

// nodeError maps an error of an interaction with a node: a device status
// is UnknownError with status and cluster_status (the error_code
// matterjs-server gives an error that is not one of its own,
// WebSocketControllerHandler.ts:1148-1163); a node not reached is
// NodeNotResolving; a node not (yet) commissioned NodeNotReady.
func nodeError(command string, err error) *protocol.Error {
	if status, cluster, ok := statusOf(err); ok {
		return protocol.Errorf(protocol.UnknownError, "%s: %v", command, err).WithStatus(status, cluster)
	}
	switch {
	case errors.Is(err, client.ErrNotCommissioned), errors.Is(err, client.ErrDisabled), errors.Is(err, client.ErrClosed):
		return protocol.Errorf(protocol.NodeNotReady, "%s: %v", command, err)
	case unreachable(err):
		return protocol.Errorf(protocol.NodeNotResolving, "%s: %v", command, err)
	}
	return protocol.Errorf(protocol.UnknownError, "%s: %v", command, err)
}

// commissionError maps a failed commissioning to NodeCommissionFailed with
// the reason (docs/websockets_api.md:82-90). go-fabric names no
// attestation rejection of its own (report S1, Questions Q6): it is
// "other".
func commissionError(err error) *protocol.Error {
	reason := protocol.ReasonOther
	switch {
	case errors.Is(err, controller.ErrNoDevice):
		reason = protocol.ReasonNotDiscovered
	case errors.Is(err, handshake.ErrPasscodeMismatch):
		reason = protocol.ReasonPasscodeMismatch
	case errors.Is(err, commissioner.ErrCommissioningTimeout), errors.Is(err, context.DeadlineExceeded), unreachable(err):
		reason = protocol.ReasonTimeout
	}
	e := protocol.Errorf(protocol.NodeCommissionFailed, "Commissioning failed: %v", err).WithReason(reason)
	if status, cluster, ok := statusOf(err); ok {
		e.WithStatus(status, cluster)
	}
	return e
}
