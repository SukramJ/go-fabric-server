// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"encoding/json"
)

// GetNodes is get_nodes.
func (c *Client) GetNodes(ctx context.Context, onlyAvailable bool) ([]Node, error) {
	var out []Node
	return out, c.Call(ctx, "get_nodes", map[string]any{"only_available": onlyAvailable}, &out)
}

// GetNode is get_node.
func (c *Client) GetNode(ctx context.Context, nodeID uint64) (Node, error) {
	var out Node
	return out, c.Call(ctx, "get_node", map[string]any{"node_id": nodeID}, &out)
}

// CommissionWithCode is commission_with_code.
func (c *Client) CommissionWithCode(ctx context.Context, code string, networkOnly bool) (Node, error) {
	var out Node
	return out, c.Call(ctx, "commission_with_code", map[string]any{"code": code, "network_only": networkOnly}, &out)
}

// CommissionOnNetwork is commission_on_network; filterType 0 is no filter.
func (c *Client) CommissionOnNetwork(ctx context.Context, pin uint32, filterType int, filter uint64, ip string) (Node, error) {
	args := map[string]any{"setup_pin_code": pin, "filter_type": filterType}
	if filterType != 0 {
		args["filter"] = filter
	}
	if ip != "" {
		args["ip_addr"] = ip
	}
	var out Node
	return out, c.Call(ctx, "commission_on_network", args, &out)
}

// Discover is discover_commissionable_nodes.
func (c *Client) Discover(ctx context.Context) ([]map[string]any, error) {
	var out []map[string]any
	return out, c.Call(ctx, "discover_commissionable_nodes", nil, &out)
}

// ReadAttribute is read_attribute with one or more paths.
func (c *Client) ReadAttribute(ctx context.Context, nodeID uint64, paths ...string) (map[string]any, error) {
	var path any = paths
	if len(paths) == 1 {
		path = paths[0]
	}
	var out map[string]any
	return out, c.Call(ctx, "read_attribute", map[string]any{"node_id": nodeID, "attribute_path": path}, &out)
}

// WriteResult is one entry of a write_attribute result.
type WriteResult struct {
	Path struct {
		EndpointID  uint16 `json:"EndpointId"`
		ClusterID   uint32 `json:"ClusterId"`
		AttributeID uint32 `json:"AttributeId"`
	} `json:"Path"`
	Status        int  `json:"Status"`
	ClusterStatus *int `json:"ClusterStatus,omitempty"`
}

// WriteAttribute is write_attribute.
func (c *Client) WriteAttribute(ctx context.Context, nodeID uint64, path string, value any) ([]WriteResult, error) {
	var out []WriteResult
	return out, c.Call(ctx, "write_attribute", map[string]any{"node_id": nodeID, "attribute_path": path, "value": value}, &out)
}

// DeviceCommand is device_command; the result is the response payload,
// null for a command answered with a status.
func (c *Client) DeviceCommand(ctx context.Context, nodeID uint64, endpoint uint16, cluster uint32, command string, payload any) (json.RawMessage, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	return c.CallRaw(ctx, "device_command", map[string]any{
		"node_id": nodeID, "endpoint_id": endpoint, "cluster_id": cluster, "command_name": command, "payload": payload,
	})
}

// Report is the result of read and subscribe, and a subscription_report.
type Report struct {
	SubscriptionID  uint32                    `json:"subscription_id,omitempty"`
	NodeID          uint64                    `json:"node_id,omitempty"`
	Attributes      map[string]any            `json:"attributes"`
	Events          []map[string]any          `json:"events"`
	AttributeStatus map[string]map[string]int `json:"attribute_status,omitempty"`
	EventStatus     map[string]map[string]int `json:"event_status,omitempty"`
	DataVersions    map[string]uint32         `json:"data_versions,omitempty"`
}

// Read is read.
func (c *Client) Read(ctx context.Context, nodeID uint64, attributePaths []string, eventPaths []string) (Report, error) {
	args := map[string]any{"node_id": nodeID}
	if len(attributePaths) > 0 {
		args["attribute_paths"] = attributePaths
	}
	if len(eventPaths) > 0 {
		ev := make([]map[string]any, len(eventPaths))
		for i, p := range eventPaths {
			ev[i] = map[string]any{"path": p}
		}
		args["event_paths"] = ev
	}
	var out Report
	return out, c.Call(ctx, "read", args, &out)
}

// Subscribe is subscribe on attribute paths.
func (c *Client) Subscribe(ctx context.Context, nodeID uint64, attributePaths []string, minInterval, maxInterval uint16) (Report, error) {
	var out Report
	return out, c.Call(ctx, "subscribe", map[string]any{
		"node_id": nodeID, "attribute_paths": attributePaths, "min_interval": minInterval, "max_interval": maxInterval,
	}, &out)
}

// Unsubscribe is unsubscribe.
func (c *Client) Unsubscribe(ctx context.Context, nodeID uint64, subscriptionID uint32) error {
	return c.Call(ctx, "unsubscribe", map[string]any{"node_id": nodeID, "subscription_id": subscriptionID}, nil)
}

// RemoveNode is remove_node.
func (c *Client) RemoveNode(ctx context.Context, nodeID uint64) error {
	return c.Call(ctx, "remove_node", map[string]any{"node_id": nodeID}, nil)
}

// InterviewNode is interview_node.
func (c *Client) InterviewNode(ctx context.Context, nodeID uint64) error {
	return c.Call(ctx, "interview_node", map[string]any{"node_id": nodeID}, nil)
}

// PingNode is ping_node.
func (c *Client) PingNode(ctx context.Context, nodeID uint64) (map[string]bool, error) {
	var out map[string]bool
	return out, c.Call(ctx, "ping_node", map[string]any{"node_id": nodeID}, &out)
}

// GetNodeIPAddresses is get_node_ip_addresses.
func (c *Client) GetNodeIPAddresses(ctx context.Context, nodeID uint64) ([]string, error) {
	var out []string
	return out, c.Call(ctx, "get_node_ip_addresses", map[string]any{"node_id": nodeID}, &out)
}

// OpenCommissioningWindow is open_commissioning_window.
func (c *Client) OpenCommissioningWindow(ctx context.Context, nodeID uint64, timeout int) (map[string]any, error) {
	var out map[string]any
	return out, c.Call(ctx, "open_commissioning_window", map[string]any{"node_id": nodeID, "timeout": timeout}, &out)
}

// GetMatterFabrics is get_matter_fabrics.
func (c *Client) GetMatterFabrics(ctx context.Context, nodeID uint64) ([]map[string]any, error) {
	var out []map[string]any
	return out, c.Call(ctx, "get_matter_fabrics", map[string]any{"node_id": nodeID}, &out)
}

// RemoveMatterFabric is remove_matter_fabric.
func (c *Client) RemoveMatterFabric(ctx context.Context, nodeID uint64, fabricIndex uint8) error {
	return c.Call(ctx, "remove_matter_fabric", map[string]any{"node_id": nodeID, "fabric_index": fabricIndex}, nil)
}

// SetWifiCredentials is set_wifi_credentials for the default entry.
func (c *Client) SetWifiCredentials(ctx context.Context, ssid, credentials string) error {
	return c.Call(ctx, "set_wifi_credentials", map[string]any{"ssid": ssid, "credentials": credentials}, nil)
}

// SetThreadDataset is set_thread_dataset for the default entry.
func (c *Client) SetThreadDataset(ctx context.Context, dataset string) error {
	return c.Call(ctx, "set_thread_dataset", map[string]any{"dataset": dataset}, nil)
}

// GetAllCredentials is get_all_credentials.
func (c *Client) GetAllCredentials(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	return out, c.Call(ctx, "get_all_credentials", nil, &out)
}

// SetDefaultFabricLabel is set_default_fabric_label.
func (c *Client) SetDefaultFabricLabel(ctx context.Context, label string) error {
	return c.Call(ctx, "set_default_fabric_label", map[string]any{"label": label}, nil)
}

// GetFabricLabel is get_fabric_label.
func (c *Client) GetFabricLabel(ctx context.Context) (string, error) {
	var out struct {
		Label string `json:"fabric_label"`
	}
	return out.Label, c.Call(ctx, "get_fabric_label", nil, &out)
}

// LogLevels is the result of get_loglevel and set_loglevel.
type LogLevels struct {
	Console string  `json:"console_loglevel"`
	File    *string `json:"file_loglevel"`
}

// GetLogLevel is get_loglevel.
func (c *Client) GetLogLevel(ctx context.Context) (LogLevels, error) {
	var out LogLevels
	return out, c.Call(ctx, "get_loglevel", nil, &out)
}

// SetLogLevel is set_loglevel for the console.
func (c *Client) SetLogLevel(ctx context.Context, console string) (LogLevels, error) {
	var out LogLevels
	return out, c.Call(ctx, "set_loglevel", map[string]any{"console_loglevel": console}, &out)
}

// Diagnostics is diagnostics.
func (c *Client) Diagnostics(ctx context.Context) (map[string]json.RawMessage, error) {
	var out map[string]json.RawMessage
	return out, c.Call(ctx, "diagnostics", nil, &out)
}
