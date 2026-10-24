// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package client is a Go client of go-fabric-server's controller half
// (/ws): [Dial] connects, reads server_info and, unless told otherwise,
// sends start_listening; the typed methods send the S1 commands; events
// arrive on [Client.Events]. The tests of this repository use it; a Go
// program that wants Matter embeds go-fabric instead.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// Options configure [Dial].
type Options struct {
	// Token is sent as "Authorization: Bearer <token>".
	Token string
	// NoListen skips start_listening: no events but subscription reports.
	NoListen bool
	// EventBuffer is the capacity of the event channel; 0 is 1024. An event
	// that finds it full is dropped and counted ([Client.Dropped]).
	EventBuffer int
	// HTTPClient dials the connection; nil is http.DefaultClient (or a Unix
	// socket client for a "unix:" URL).
	HTTPClient *http.Client
}

// Event is a server push.
type Event struct {
	Event string
	Data  json.RawMessage
}

// ServerInfo is the server_info object.
type ServerInfo struct {
	FabricID                  uint64   `json:"fabric_id"`
	CompressedFabricID        uint64   `json:"compressed_fabric_id"`
	FabricIndex               *uint8   `json:"fabric_index,omitempty"`
	SchemaVersion             int      `json:"schema_version"`
	MinSupportedSchemaVersion int      `json:"min_supported_schema_version"`
	SDKVersion                string   `json:"sdk_version"`
	WifiCredentialsSet        bool     `json:"wifi_credentials_set"`
	WifiSSID                  *string  `json:"wifi_ssid,omitempty"`
	ThreadCredentialsSet      bool     `json:"thread_credentials_set"`
	BluetoothEnabled          bool     `json:"bluetooth_enabled"`
	BLEProxyEnabled           bool     `json:"ble_proxy_enabled"`
	ControllerNodeID          *uint64  `json:"controller_node_id,omitempty"`
	Capabilities              []string `json:"capabilities"`
}

// Node is a node's data (MatterNodeData). Attribute values keep their
// numbers exact (json.Number).
type Node struct {
	NodeID                 uint64            `json:"node_id"`
	DateCommissioned       string            `json:"date_commissioned"`
	LastInterview          string            `json:"last_interview"`
	InterviewVersion       int               `json:"interview_version"`
	Available              bool              `json:"available"`
	IsBridge               bool              `json:"is_bridge"`
	Attributes             map[string]any    `json:"attributes"`
	AttributeSubscriptions []any             `json:"attribute_subscriptions"`
	MatterVersion          *string           `json:"matter_version,omitempty"`
	EndpointParents        map[string]uint16 `json:"endpoint_parents"`
}

// Client is one connection. Its methods are safe for concurrent use.
type Client struct {
	ws      *websocket.Conn
	info    ServerInfo
	nodes   []Node
	events  chan Event
	dropped atomic.Uint64

	seq     atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan frame
	err     error
	done    chan struct{}
	cancel  context.CancelFunc
}

type frame struct {
	result json.RawMessage
	err    *protocol.Error
}

// Dial connects to url ("ws://host:port/ws", or "unix:/path" for the
// server's Unix socket).
func Dial(ctx context.Context, url string, opts Options) (*Client, error) {
	hc := opts.HTTPClient
	if p, ok := strings.CutPrefix(url, "unix:"); ok {
		if hc == nil {
			hc = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", p)
			}}}
		}
		url = "ws://localhost/ws"
	}
	h := http.Header{}
	if opts.Token != "" {
		h.Set("Authorization", "Bearer "+opts.Token)
	}
	ws, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: hc, HTTPHeader: h})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(64 << 20)
	n := opts.EventBuffer
	if n <= 0 {
		n = 1024
	}
	c := &Client{ws: ws, events: make(chan Event, n), pending: map[string]chan frame{}, done: make(chan struct{})}
	_, first, err := ws.Read(ctx)
	if err != nil {
		_ = ws.CloseNow()
		return nil, fmt.Errorf("client: server_info: %w", err)
	}
	if err := json.Unmarshal(first, &c.info); err != nil {
		_ = ws.CloseNow()
		return nil, fmt.Errorf("client: server_info: %w", err)
	}
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c.cancel = cancel
	go c.read(rctx)
	if !opts.NoListen {
		if err := c.Call(ctx, "start_listening", nil, &c.nodes); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// ServerInfo is the server_info the server sent on connect.
func (c *Client) ServerInfo() ServerInfo { return c.info }

// Nodes is the start_listening dump.
func (c *Client) Nodes() []Node { return c.nodes }

// Events are the server's pushes, in arrival order.
func (c *Client) Events() <-chan Event { return c.events }

// Dropped is the number of events dropped because Events was full.
func (c *Client) Dropped() uint64 { return c.dropped.Load() }

// Done is closed when the connection ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the connection.
func (c *Client) Close() error {
	err := c.ws.Close(websocket.StatusNormalClosure, "")
	if c.cancel != nil {
		c.cancel()
	}
	return err
}

func (c *Client) read(ctx context.Context) {
	defer close(c.done)
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			c.fail(err)
			return
		}
		var head struct {
			MessageID *string         `json:"message_id"`
			Event     *string         `json:"event"`
			Data      json.RawMessage `json:"data"`
			Result    json.RawMessage `json:"result"`
			ErrorCode *int            `json:"error_code"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			continue
		}
		switch {
		case head.Event != nil:
			select {
			case c.events <- Event{Event: *head.Event, Data: head.Data}:
			default:
				c.dropped.Add(1)
			}
		case head.MessageID != nil:
			var f frame
			if head.ErrorCode != nil {
				var e protocol.Error
				_ = json.Unmarshal(data, &e)
				f.err = &e
			} else {
				f.result = head.Result
			}
			c.mu.Lock()
			ch := c.pending[*head.MessageID]
			delete(c.pending, *head.MessageID)
			c.mu.Unlock()
			if ch != nil {
				ch <- f
			}
		}
	}
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

// ErrClosed is a call on a connection that ended.
var ErrClosed = errors.New("client: connection closed")

// Call sends command with args (nil for none) and decodes the result into
// result (nil to discard). A failure the server answered is a
// *protocol.Error.
func (c *Client) Call(ctx context.Context, command string, args, result any) error {
	raw, err := c.CallRaw(ctx, command, args)
	if err != nil || result == nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(result)
}

// CallRaw is [Client.Call] returning the raw result.
func (c *Client) CallRaw(ctx context.Context, command string, args any) (json.RawMessage, error) {
	id := strconv.FormatUint(c.seq.Add(1), 10)
	req := map[string]any{"message_id": id, "command": command}
	if args != nil {
		req["args"] = args
	}
	b, err := protocol.Encode(req)
	if err != nil {
		return nil, err
	}
	return c.SendRaw(ctx, id, b)
}

// SendRaw sends a frame as is and waits for the answer to id — for tests
// of malformed requests.
func (c *Client) SendRaw(ctx context.Context, id string, b []byte) (json.RawMessage, error) {
	ch := make(chan frame, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case f, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if f.err != nil {
			return nil, f.err
		}
		return f.result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}
