// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// BridgeInfo is the bridge half's server_info.
type BridgeInfo struct {
	BridgeSchemaVersion int      `json:"bridge_schema_version"`
	SDKVersion          string   `json:"sdk_version"`
	Capabilities        []string `json:"capabilities"`
}

// Bridge is a connection to the bridge half (/bridge).
type Bridge struct {
	*Client
	binfo   BridgeInfo
	devices []BridgedDevice
}

// BridgedDevice is a device of start_listening's dump.
type BridgedDevice struct {
	Key       string                    `json:"key"`
	Type      string                    `json:"type"`
	Endpoint  *uint16                   `json:"endpoint"`
	Reachable bool                      `json:"reachable"`
	Clusters  map[string]map[string]any `json:"clusters"`
}

// DialBridge connects to the bridge half: url is "ws://host:port/bridge"
// or "unix:/path". Unless opts.NoListen, it sends start_listening, whose
// answer is [Bridge.Devices]. A second client while one is connected gets
// the server's refusal as a *protocol.Error.
func DialBridge(ctx context.Context, url string, opts Options) (*Bridge, error) {
	c, first, err := open(ctx, url, "/bridge", opts)
	if err != nil {
		return nil, err
	}
	b := &Bridge{Client: c}
	if err := json.Unmarshal(first, &b.binfo); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("client: server_info: %w", err)
	}
	if !opts.NoListen {
		var dump struct {
			Devices []BridgedDevice `json:"devices"`
		}
		if err := c.Call(ctx, "start_listening", nil, &dump); err != nil {
			_ = c.Close()
			return nil, err
		}
		b.devices = dump.Devices
	}
	return b, nil
}

// Info is the server_info the bridge half sent on connect.
func (b *Bridge) Info() BridgeInfo { return b.binfo }

// Devices is the start_listening dump.
func (b *Bridge) Devices() []BridgedDevice { return b.devices }

// Declared is one device of declare's answer.
type Declared struct {
	Key      string  `json:"key"`
	Action   string  `json:"action"`
	Endpoint *uint16 `json:"endpoint"`
	Error    string  `json:"error"`
}

// DeclareResult is declare's answer.
type DeclareResult struct {
	Devices []Declared `json:"devices"`
	Removed []string   `json:"removed"`
}

// Declare sends the declaration document (an object with "devices").
func (b *Bridge) Declare(ctx context.Context, doc any) (DeclareResult, error) {
	return call[DeclareResult](ctx, b.Client, "declare", doc)
}

// Set sends a patch of one cluster of a device.
func (b *Bridge) Set(ctx context.Context, device, cluster string, patch any) error {
	return b.Call(ctx, "set", map[string]any{"device": device, "cluster": cluster, "patch": patch}, nil)
}

// HostStatement are the host's statements of the host command; nil
// fields are left as they are.
type HostStatement struct {
	Device         string  `json:"device"`
	Name           *string `json:"name,omitempty"`
	DeviceAddress  *string `json:"device_address,omitempty"`
	ChannelAddress *string `json:"channel_address,omitempty"`
	Reachable      *bool   `json:"reachable,omitempty"`
	Label          *string `json:"label,omitempty"`
}

// Host sends the host command.
func (b *Bridge) Host(ctx context.Context, st HostStatement) error {
	return b.Call(ctx, "host", st, nil)
}

// Remove removes a device.
func (b *Bridge) Remove(ctx context.Context, device string) error {
	return b.Call(ctx, "remove", map[string]any{"device": device}, nil)
}

// ResultError is the error of a result: the status to answer with.
type ResultError struct {
	Status        *int `json:"status,omitempty"`
	ClusterStatus *int `json:"cluster_status,omitempty"`
}

// Result answers a write or command event: nil err accepts (with the
// command's response, when it has one).
func (b *Bridge) Result(ctx context.Context, correlation uint64, response any, err *ResultError) error {
	args := map[string]any{"correlation": correlation}
	if err != nil {
		args["error"] = err
	} else {
		args["ok"] = true
		if response != nil {
			args["response"] = response
		}
	}
	return b.Call(ctx, "result", args, nil)
}

// PairingCodes are the node's QR payload and manual code.
func (b *Bridge) PairingCodes(ctx context.Context) (qr, manual string, err error) {
	r, err := call[struct {
		QR     string `json:"qr_code"`
		Manual string `json:"manual_code"`
	}](ctx, b.Client, "pairing_codes", nil)
	return r.QR, r.Manual, err
}

// NodeInfo is node_info's answer.
func (b *Bridge) NodeInfo(ctx context.Context) (map[string]any, error) {
	return call[map[string]any](ctx, b.Client, "node_info", nil)
}

// BridgeEvent is a write or command event (Command or Attribute set), a
// changed event (Cluster, State, Attributes, Origin) or a result_timeout.
type BridgeEvent struct {
	Correlation uint64          `json:"correlation"`
	Device      string          `json:"device"`
	Endpoint    *uint16         `json:"endpoint"`
	Cluster     string          `json:"cluster"`
	Command     string          `json:"command"`
	Attribute   string          `json:"attribute"`
	Fields      json.RawMessage `json:"fields"`
	Value       json.RawMessage `json:"value"`
	Deadline    string          `json:"deadline"`
	TimeoutMS   int64           `json:"timeout_ms"`
	State       map[string]any  `json:"state"`
	Attributes  []string        `json:"attributes"`
	Origin      string          `json:"origin"`
}
