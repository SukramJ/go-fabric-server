// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// wsConn is one client connection.
type wsConn struct {
	s    *Server
	id   string
	ws   *websocket.Conn
	send *protocol.Conn
	sem  chan struct{}

	// listening is set once start_listening has answered; the server's
	// lock guards its reads and writes, so an event is emitted either
	// before the dump or after it.
	listening bool

	mu     sync.Mutex
	subs   map[subKey]*ownedSub
	closed atomic.Bool
	wg     sync.WaitGroup

	// topology is set once the connection asked for get_network_topology:
	// it gets network_topology_updated from then on
	// (WebSocketControllerHandler.ts:2220-2229).
	topology atomic.Bool
}

// run sends server_info, then reads requests until the connection ends.
// Each request runs on its own goroutine, at most MaxInFlight at once, as
// matterjs-server answers requests concurrently.
func (c *wsConn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-c.send.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	// On connect the bare server_info object, before any response
	// (WebSocketControllerHandler.ts:862-866).
	c.send.SendReliable(protocol.MustEncode(c.s.serverInfo()))
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			break
		}
		select {
		case c.sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		c.wg.Add(1)
		go func() {
			defer func() { <-c.sem; c.wg.Done() }()
			c.s.handle(ctx, c, data)
		}()
	}
	cancel()
	c.wg.Wait()
}

// reply sends a response.
func (c *wsConn) reply(v any) {
	b, err := protocol.Encode(v)
	if err != nil {
		c.s.log.Error("fabricserver.ws.encode", slog.String("conn", c.id), slog.Any("err", err))
		b = protocol.MustEncode(protocol.Errorf(protocol.UnknownError, "the response could not be encoded: %v", err))
	}
	c.send.SendReliable(b)
}

// close ends the connection: its subscriptions close without
// subscription_ended (docs/websockets_api.md:2232), the send path stops,
// the socket closes.
func (c *wsConn) close(code websocket.StatusCode, reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.s.mu.Lock()
	delete(c.s.conns, c)
	c.s.mu.Unlock()
	c.s.releaseLabel(c)
	c.mu.Lock()
	subs := c.subs
	c.subs = map[subKey]*ownedSub{}
	c.mu.Unlock()
	for _, o := range subs {
		o.close(false)
	}
	c.send.Close()
	_ = c.ws.Close(code, reason)
}
