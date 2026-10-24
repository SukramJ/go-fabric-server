// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// subKey names a subscription: an id is only unique per node
// (docs/websockets_api.md:3070-3072).
type subKey struct {
	node uint64
	id   uint32
}

// ownedSub is a subscription a connection opened with subscribe. The
// priming report answers subscribe; later reports go to the owner as
// subscription_report, also when it does not listen, and only after the
// subscribe response was sent.
type ownedSub struct {
	conn   *wsConn
	nodeID uint64

	mu        sync.Mutex
	sub       *client.Subscription
	key       subKey
	opened    bool // Subscribe returned
	responded bool // the subscribe response was sent
	priming   []*client.Report
	pending   []*client.Report
	byUs      bool // closed by unsubscribe, the connection or shutdown
	ended     bool
}

func (o *ownedSub) onReport(rep *client.Report) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case !o.opened:
		o.priming = append(o.priming, rep)
	case !o.responded:
		o.pending = append(o.pending, rep)
	default:
		o.sendReportLocked(rep)
	}
}

// sendReportLocked sends subscription_report, droppable under
// backpressure (docs/websockets_api.md:540-544).
func (o *ownedSub) sendReportLocked(rep *client.Report) {
	if o.ended {
		return
	}
	d := newReportData()
	d.add(o.nodeID, rep)
	data := struct {
		NodeID         uint64 `json:"node_id"`
		SubscriptionID uint32 `json:"subscription_id"`
		reportData
	}{o.nodeID, o.key.id, d}
	o.conn.send.SendOrdered(protocol.MustEncode(protocol.Event{Event: "subscription_report", Data: data}), true)
}

// responded flushes the reports that arrived between Subscribe and its
// response.
func (o *ownedSub) respondedNow() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.responded = true
	for _, rep := range o.pending {
		o.sendReportLocked(rep)
	}
	o.pending = nil
}

// onClosed is the subscription's end: one the connection did not ask for
// is announced with subscription_ended, never dropped
// (docs/websockets_api.md:546-555).
func (o *ownedSub) onClosed() {
	o.mu.Lock()
	if o.ended {
		o.mu.Unlock()
		return
	}
	o.ended = true
	byUs, opened := o.byUs, o.opened
	o.mu.Unlock()
	if !opened {
		return
	}
	o.conn.mu.Lock()
	if o.conn.subs[o.key] == o {
		delete(o.conn.subs, o.key)
	}
	o.conn.mu.Unlock()
	if byUs {
		return
	}
	data := map[string]any{"node_id": o.nodeID, "subscription_id": o.key.id}
	o.conn.send.SendOrdered(protocol.MustEncode(protocol.Event{Event: "subscription_ended", Data: data}), false)
}

// close ends the subscription; announce sends subscription_ended (a node
// removal), otherwise the end is the connection's own.
func (o *ownedSub) close(announce bool) {
	o.mu.Lock()
	o.byUs = !announce
	sub := o.sub
	o.mu.Unlock()
	if sub != nil {
		sub.Close()
	}
	o.onClosed()
}

// endNodeSubscriptions ends every subscription to a removed node; the
// caller holds s.mu.
func (s *Server) endNodeSubscriptions(nodeID uint64) {
	for c := range s.conns {
		c.mu.Lock()
		var subs []*ownedSub
		for k, o := range c.subs {
			if k.node == nodeID {
				subs = append(subs, o)
			}
		}
		c.mu.Unlock()
		for _, o := range subs {
			go o.close(true)
		}
	}
}

// cmdSubscribe opens a subscription owned by the connection
// (docs/websockets_api.md:3026-3089); only with --disable-auto-subscription.
func cmdSubscribe(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	if !s.opts.DisableAutoSubscription {
		return nil, invalidArgs(c.req.Command, "only available with the server running with --disable-auto-subscription")
	}
	var a struct {
		readArgs
		MinInterval       *uint16 `json:"min_interval"`
		MaxInterval       *uint16 `json:"max_interval"`
		KeepSubscriptions bool    `json:"keep_subscriptions"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	paths, perr := a.request(c.req.Command)
	if perr != nil {
		return nil, perr
	}
	if a.MinInterval == nil || a.MaxInterval == nil {
		return nil, invalidArgs(c.req.Command, "min_interval and max_interval are required")
	}
	if *a.MaxInterval < *a.MinInterval {
		return nil, invalidArgs(c.req.Command, "max_interval must not be below min_interval")
	}
	o := &ownedSub{conn: c.conn, nodeID: nodeID}
	req := client.SubscribeRequest{
		Attributes: paths.Attributes, Events: paths.Events, EventFilters: paths.EventFilters,
		DataVersionFilters: paths.DataVersionFilters, NoFabricFilter: paths.NoFabricFilter,
		ReplaceSubscriptions: !a.KeepSubscriptions,
		MinIntervalFloor:     client.Interval(time.Duration(*a.MinInterval) * time.Second),
		MaxIntervalCeiling:   client.Interval(time.Duration(*a.MaxInterval) * time.Second),
		Updated:              o.onReport,
		Closed:               o.onClosed,
	}
	sub, err := n.Subscribe(ctx, req)
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	o.mu.Lock()
	o.sub, o.opened, o.key = sub, true, subKey{node: nodeID, id: sub.ID()}
	priming := o.priming
	o.priming = nil
	o.mu.Unlock()
	c.conn.mu.Lock()
	c.conn.subs[o.key] = o
	c.conn.mu.Unlock()
	c.after = o.respondedNow
	d := newReportData()
	for _, rep := range priming {
		d.add(nodeID, rep)
	}
	return struct {
		SubscriptionID uint32 `json:"subscription_id"`
		reportData
	}{sub.ID(), d}, nil
}

// cmdUnsubscribe ends a subscription of this connection; Matter has no
// unsubscribe message, the device ends it at its next report
// (docs/websockets_api.md:3091-3106).
func cmdUnsubscribe(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	if !s.opts.DisableAutoSubscription {
		return nil, invalidArgs(c.req.Command, "only available with the server running with --disable-auto-subscription")
	}
	var a struct {
		nodeArg
		SubscriptionID *uint32 `json:"subscription_id"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.NodeID == nil || a.SubscriptionID == nil {
		return nil, invalidArgs(c.req.Command, "node_id and subscription_id are required")
	}
	key := subKey{node: uint64(*a.NodeID), id: *a.SubscriptionID}
	c.conn.mu.Lock()
	o := c.conn.subs[key]
	c.conn.mu.Unlock()
	if o == nil {
		return nil, invalidArgs(c.req.Command, "this connection holds no subscription %s of node %d", strconv.FormatUint(uint64(key.id), 10), key.node)
	}
	o.close(false)
	return nil, nil
}
