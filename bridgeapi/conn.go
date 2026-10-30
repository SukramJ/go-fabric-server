// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// bconn is the bridge client's connection.
type bconn struct {
	s      *Server
	id     string
	remote string
	since  time.Time
	ws     *websocket.Conn
	send   *protocol.Conn
	sem    chan struct{}

	// listening is set once start_listening has answered; the server's
	// lock guards it.
	listening bool

	closed atomic.Bool
	wg     sync.WaitGroup
}

// run sends server_info, then reads requests until the connection ends;
// each request runs on its own goroutine, at most MaxInFlight at once, so
// a result is never stuck behind a declare.
func (c *bconn) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-c.send.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
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

func (c *bconn) reply(v any) {
	b, err := protocol.Encode(v)
	if err != nil {
		c.s.log.Error("fabricserver.bridge.encode", slog.String("conn", c.id), slog.Any("err", err))
		b = protocol.MustEncode(protocol.Errorf(protocol.UnknownError, "the response could not be encoded: %v", err))
	}
	c.send.SendReliable(b)
}

// close ends the connection; its pending results are answered, so the
// controllers waiting on them get their answer at once.
func (c *bconn) close(code websocket.StatusCode, reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.s.mu.Lock()
	if c.s.conn == c {
		c.s.conn = nil
		c.s.failPendingLocked()
	}
	c.s.mu.Unlock()
	c.s.log.Info("fabricserver.bridge.disconnected", slog.String("conn", c.id))
	c.send.Close()
	_ = c.ws.Close(code, reason)
}

// call is one request being answered.
type call struct {
	conn *bconn
	req  protocol.Request
}

func (c *call) args(dst any) *protocol.Error {
	return protocol.DecodeArgs(c.req.Command, c.req.Args, dst)
}

type handler func(ctx context.Context, s *Server, c *call) (any, *protocol.Error)

// handlers are the bridge half's commands (brief S2 §1.2); start_listening
// is answered by the dispatcher itself, under the server's lock.
var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"server_info":   cmdServerInfo,
		"declare":       cmdDeclare,
		"set":           cmdSet,
		"host":          cmdHost,
		"remove":        cmdRemove,
		"forget":        cmdForget,
		"result":        cmdResult,
		"pairing_codes": cmdPairingCodes,
		"node_info":     cmdNodeInfo,
		"surface":       cmdSurface,
	}
}

// Capabilities are the commands of the bridge half, sorted.
func Capabilities() []string {
	out := make([]string, 0, 1+len(handlers))
	out = append(out, "start_listening")
	for name := range handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *Server) handle(ctx context.Context, conn *bconn, frame []byte) {
	req, perr := protocol.DecodeRequest(frame)
	if perr != nil {
		conn.reply(perr)
		return
	}
	s.log.Debug("fabricserver.bridge.request", slog.String("conn", conn.id), slog.String("command", req.Command), slog.String("message_id", req.MessageID))
	if req.Command == "start_listening" {
		s.startListening(conn, req)
		return
	}
	result, err := s.dispatch(ctx, &call{conn: conn, req: req})
	if err != nil {
		err.MessageID = req.MessageID
		s.log.Info("fabricserver.bridge.error", slog.String("conn", conn.id), slog.String("command", req.Command), slog.Int("code", int(err.ErrorCode)), slog.String("details", err.Details))
		conn.reply(err)
		return
	}
	conn.reply(protocol.Result{MessageID: req.MessageID, Result: result})
}

func (s *Server) dispatch(ctx context.Context, c *call) (result any, err *protocol.Error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("fabricserver.bridge.panic", slog.String("command", c.req.Command), slog.Any("panic", r))
			result, err = nil, protocol.Errorf(protocol.UnknownError, "%s failed: %v", c.req.Command, r)
		}
	}()
	h, ok := handlers[c.req.Command]
	if !ok {
		return nil, protocol.Errorf(protocol.InvalidCommand, "Unknown command: %s", c.req.Command)
	}
	return h(ctx, s, c)
}

// startListening answers with the declared devices and their state and
// switches the events on once the answer is handed to the socket; the
// server's lock is held throughout, so no event comes between the dump
// and the switch.
func (s *Server) startListening(conn *bconn, req protocol.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := protocol.Encode(protocol.Result{MessageID: req.MessageID, Result: map[string]any{"devices": s.dumpLocked()}})
	if err != nil {
		b = protocol.MustEncode(&protocol.Error{MessageID: req.MessageID, ErrorCode: protocol.UnknownError, Details: err.Error()})
	}
	conn.send.SendReliable(b)
	conn.listening = true
}

// emit sends an event to the listening client.
func (s *Server) emit(event string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked(event, data)
}

func (s *Server) emitLocked(event string, data any) {
	c := s.conn
	if c == nil || !c.listening {
		return
	}
	b, err := protocol.Encode(protocol.Event{Event: event, Data: data})
	if err != nil {
		s.log.Error("fabricserver.bridge.encode", slog.String("event", event), slog.Any("err", err))
		return
	}
	c.send.SendOrdered(b, false)
}

func invalidArgs(command, format string, args ...any) *protocol.Error {
	return protocol.Errorf(protocol.InvalidArguments, "%s: %s", command, fmt.Sprintf(format, args...))
}
