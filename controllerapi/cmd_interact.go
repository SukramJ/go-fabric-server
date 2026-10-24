// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/internal/wire"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// readBatch is matterjs-server's batch of read_attribute paths per Read
// (ControllerCommandHandler.ts:1283).
const readBatch = 9

func attributePath(p wire.Path) client.AttributePath {
	var a client.AttributePath
	if p.Endpoint != nil {
		a.Endpoint, a.HasEndpoint = *p.Endpoint, true
	}
	if p.Cluster != nil {
		a.Cluster, a.HasCluster = *p.Cluster, true
	}
	if p.Element != nil {
		a.Attribute, a.HasAttribute = *p.Element, true
	}
	return a
}

func eventPath(p wire.Path, urgent bool) client.EventPath {
	var e client.EventPath
	if p.Endpoint != nil {
		e.Endpoint, e.HasEndpoint = *p.Endpoint, true
	}
	if p.Cluster != nil {
		e.Cluster, e.HasCluster = *p.Cluster, true
	}
	if p.Element != nil {
		e.Event, e.HasEvent = *p.Element, true
	}
	e.IsUrgent = urgent
	return e
}

// stringOrList reads read_attribute's attribute_path: one path or a list.
func stringOrList(raw json.RawMessage) ([]string, bool) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}, true
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many, true
	}
	return nil, false
}

// cmdReadAttribute reads attribute paths, wildcards allowed, in batches of
// nine without data version filters, unfiltered unless fabric_filtered
// (ControllerCommandHandler.ts:1271-1320, WebSocketControllerHandler.ts:1494-1508).
func cmdReadAttribute(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		AttributePath  json.RawMessage `json:"attribute_path"`
		FabricFiltered bool            `json:"fabric_filtered"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	paths, ok := stringOrList(a.AttributePath)
	if !ok || len(paths) == 0 {
		return nil, invalidArgs(c.req.Command, "attribute_path must be a path or a list of paths")
	}
	parsed := make([]client.AttributePath, 0, len(paths))
	for _, p := range paths {
		wp, err := wire.ParsePath(p, 3, false)
		if err != nil {
			return nil, invalidArgs(c.req.Command, "%v", err)
		}
		parsed = append(parsed, attributePath(wp))
	}
	out := map[string]any{}
	for i := 0; i < len(parsed); i += readBatch {
		req := client.ReadRequest{IncludeKnownVersions: true}
		req.Attributes = parsed[i:min(i+readBatch, len(parsed))]
		req.NoFabricFilter = !a.FabricFiltered
		rep, err := n.Read(ctx, req)
		if err != nil {
			return nil, nodeError(c.req.Command, err)
		}
		for i := range rep.Attributes {
			d := &rep.Attributes[i]
			out[wire.Key(d.Path.Endpoint, d.Path.Cluster, d.Path.Attribute)] = wire.Attribute(d.Path.Cluster, d.Path.Attribute, d.Value)
		}
	}
	if len(out) == 0 {
		return nil, protocol.Errorf(protocol.SDKStackError, "Failed to read attribute: no values returned")
	}
	return out, nil
}

// readArgs are the arguments read and subscribe share (InteractionReadArgs,
// model.ts:411-423).
type readArgs struct {
	nodeArg
	AttributePaths []string `json:"attribute_paths"`
	EventPaths     []struct {
		Path     string `json:"path"`
		IsUrgent *bool  `json:"is_urgent"`
	} `json:"event_paths"`
	EventMin           *protocol.Uint64 `json:"event_min"`
	FabricFiltered     *bool            `json:"fabric_filtered"`
	DataVersionFilters []struct {
		Path        string  `json:"path"`
		DataVersion *uint32 `json:"data_version"`
	} `json:"data_version_filters"`
}

// paths checks and converts the read arguments
// (docs/websockets_api.md:2932-2951): strict paths, at least one path,
// event_min only with event paths, version filters only with attribute
// paths.
func (a *readArgs) request(command string) (client.ReadPaths, *protocol.Error) {
	var r client.ReadPaths
	for _, p := range a.AttributePaths {
		wp, err := wire.ParsePath(p, 3, true)
		if err != nil {
			return r, invalidArgs(command, "%v", err)
		}
		r.Attributes = append(r.Attributes, attributePath(wp))
	}
	for _, e := range a.EventPaths {
		wp, err := wire.ParsePath(e.Path, 3, true)
		if err != nil {
			return r, invalidArgs(command, "%v", err)
		}
		r.Events = append(r.Events, eventPath(wp, e.IsUrgent != nil && *e.IsUrgent))
	}
	if len(r.Attributes) == 0 && len(r.Events) == 0 {
		return r, invalidArgs(command, "at least one attribute or event path is required")
	}
	if a.EventMin != nil {
		if len(r.Events) == 0 {
			return r, invalidArgs(command, "event_min needs at least one event path")
		}
		r.EventFilters = []client.EventMinimumNumber{{EventMin: uint64(*a.EventMin)}}
	}
	if len(a.DataVersionFilters) > 0 && len(r.Attributes) == 0 {
		return r, invalidArgs(command, "data_version_filters need at least one attribute path")
	}
	for _, f := range a.DataVersionFilters {
		wp, err := wire.ParsePath(f.Path, 2, true)
		if err != nil || wp.Endpoint == nil || wp.Cluster == nil {
			return r, invalidArgs(command, "data version filter path %q must be endpoint/cluster, both numbers", f.Path)
		}
		if f.DataVersion == nil {
			return r, invalidArgs(command, "data_version is required")
		}
		r.DataVersionFilters = append(r.DataVersionFilters, client.DataVersionFilter{Endpoint: *wp.Endpoint, Cluster: *wp.Cluster, DataVersion: *f.DataVersion})
	}
	r.NoFabricFilter = a.FabricFiltered != nil && !*a.FabricFiltered
	return r, nil
}

// reportData is InteractionReportData (model.ts:435-444).
type reportData struct {
	Attributes      map[string]any        `json:"attributes"`
	Events          []nodeEventData       `json:"events"`
	AttributeStatus map[string]pathStatus `json:"attribute_status,omitempty"`
	EventStatus     map[string]pathStatus `json:"event_status,omitempty"`
	DataVersions    map[string]uint32     `json:"data_versions,omitempty"`
}

type pathStatus struct {
	Status        int  `json:"status"`
	ClusterStatus *int `json:"cluster_status,omitempty"`
}

func newReportData() reportData {
	return reportData{Attributes: map[string]any{}, Events: []nodeEventData{}}
}

func (r *reportData) add(nodeID uint64, rep *client.Report) {
	for i := range rep.Attributes {
		d := &rep.Attributes[i]
		r.Attributes[wire.Key(d.Path.Endpoint, d.Path.Cluster, d.Path.Attribute)] = wire.Attribute(d.Path.Cluster, d.Path.Attribute, d.Value)
		if d.DataVersion != 0 {
			if r.DataVersions == nil {
				r.DataVersions = map[string]uint32{}
			}
			r.DataVersions[wire.ClusterKey(d.Path.Endpoint, d.Path.Cluster)] = d.DataVersion
		}
	}
	for _, st := range rep.Statuses {
		if r.AttributeStatus == nil {
			r.AttributeStatus = map[string]pathStatus{}
		}
		r.AttributeStatus[wire.Key(st.Path.Endpoint, st.Path.Cluster, st.Path.Attribute)] = statusEntry(int(st.Status), st.ClusterStatus)
	}
	for i := range rep.Events {
		e := &rep.Events[i]
		r.Events = append(r.Events, eventData(nodeID, e))
	}
	for _, st := range rep.EventStatuses {
		if r.EventStatus == nil {
			r.EventStatus = map[string]pathStatus{}
		}
		r.EventStatus[wire.Key(st.Path.Endpoint, st.Path.Cluster, st.Path.Event)] = statusEntry(int(st.Status), st.ClusterStatus)
	}
}

func statusEntry(status int, cluster *uint8) pathStatus {
	p := pathStatus{Status: status}
	if cluster != nil {
		c := int(*cluster)
		p.ClusterStatus = &c
	}
	return p
}

// cmdRead is one Read request as given: no data version filters of the
// server's own (docs/websockets_api.md:2972-3024).
func cmdRead(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a readArgs
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
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: paths, IncludeKnownVersions: true})
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	out := newReportData()
	out.add(nodeID, rep)
	return out, nil
}

// writeResult is one entry of the write_attribute result, in the casing
// docs/websockets_api.md:612 documents (PascalCase; the AttributeWriteResult
// type of model.ts:1498 is snake_case — read map 1498, report S1
// Decisions D4).
type writeResult struct {
	Path struct {
		EndpointID  uint16 `json:"EndpointId"`
		ClusterID   uint32 `json:"ClusterId"`
		AttributeID uint32 `json:"AttributeId"`
	} `json:"Path"`
	Status        int  `json:"Status"`
	ClusterStatus *int `json:"ClusterStatus,omitempty"`
}

// timedTimeout reads timed_request_timeout_ms: an integer from 0 to
// 65535, 0 and null as not given (docs/websockets_api.md:2183-2186).
func timedTimeout(command string, raw *json.Number) (time.Duration, *protocol.Error) {
	if raw == nil {
		return 0, nil
	}
	v, err := strconv.ParseUint(raw.String(), 10, 16)
	if err != nil {
		return 0, invalidArgs(command, "timed_request_timeout_ms must be an integer from 0 to 65535")
	}
	return time.Duration(v) * time.Millisecond, nil
}

// cmdWriteAttribute writes one attribute on one endpoint, or on every
// endpoint with "*" (WebSocketControllerHandler.ts:1653-1728).
func cmdWriteAttribute(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		AttributePath *string         `json:"attribute_path"`
		Value         json.RawMessage `json:"value"`
		TimedTimeout  *json.Number    `json:"timed_request_timeout_ms"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if a.AttributePath == nil {
		return nil, invalidArgs(c.req.Command, "attribute_path is required")
	}
	wp, err := wire.ParsePath(*a.AttributePath, 3, true)
	if err != nil {
		return nil, invalidArgs(c.req.Command, `write_attribute needs attribute_path as endpoint/cluster/attribute, each a number or "*"`)
	}
	if wp.Cluster == nil || wp.Element == nil {
		return nil, invalidArgs(c.req.Command, "write_attribute takes a wildcard endpoint only, not a wildcard cluster or attribute")
	}
	if len(a.Value) == 0 {
		return nil, invalidArgs(c.req.Command, "value is required")
	}
	timed, perr := timedTimeout(c.req.Command, a.TimedTimeout)
	if perr != nil {
		return nil, perr
	}
	value, attrTimed, err := wire.EncodeAttribute(*wp.Cluster, *wp.Element, a.Value)
	if err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	opts := client.WriteOptions{Timed: timed > 0 || attrTimed, TimedTimeout: timed}
	w := client.WriteRequest{Path: attributePath(wp), Value: value}
	statuses, err := n.Write(ctx, []client.WriteRequest{w}, opts)
	if err != nil && len(statuses) == 0 {
		return nil, nodeError(c.req.Command, err)
	}
	out := make([]writeResult, 0, len(statuses))
	for _, st := range statuses {
		var r writeResult
		r.Path.EndpointID, r.Path.ClusterID, r.Path.AttributeID = st.Path.Endpoint, st.Path.Cluster, st.Path.Attribute
		r.Status = int(st.Status)
		if st.ClusterStatus != nil {
			cs := int(*st.ClusterStatus)
			r.ClusterStatus = &cs
		}
		out = append(out, r)
	}
	return out, nil
}

// cmdDeviceCommand invokes a command by name with its payload by field
// names (WebSocketControllerHandler.ts:1730-1850, docs/websockets_api.md:864-914).
// A command the snapshot does not have for the cluster, or a payload that
// does not fit it, is InvalidArguments.
func cmdDeviceCommand(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		EndpointID         *uint16         `json:"endpoint_id"`
		ClusterID          *uint32         `json:"cluster_id"`
		CommandName        *string         `json:"command_name"`
		Payload            json.RawMessage `json:"payload"`
		TimedTimeout       *json.Number    `json:"timed_request_timeout_ms"`
		InteractionTimeout *json.Number    `json:"interaction_timeout_ms"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	switch {
	case a.EndpointID == nil:
		return nil, invalidArgs(c.req.Command, "device_command requires endpoint_id")
	case a.ClusterID == nil:
		return nil, invalidArgs(c.req.Command, "cluster_id is required")
	case a.CommandName == nil || *a.CommandName == "":
		return nil, invalidArgs(c.req.Command, "command_name is required")
	}
	timed, perr := timedTimeout(c.req.Command, a.TimedTimeout)
	if perr != nil {
		return nil, perr
	}
	var processing time.Duration
	if a.InteractionTimeout != nil {
		v, err := strconv.ParseUint(a.InteractionTimeout.String(), 10, 53)
		if err != nil {
			return nil, invalidArgs(c.req.Command, "interaction_timeout_ms must be an integer from 0 to 2^53-1")
		}
		processing = time.Duration(v) * time.Millisecond //nolint:gosec // bounded above
	}
	cl := schema.ClusterByID(*a.ClusterID)
	cmd := cl.CommandByName(*a.CommandName)
	if cmd == nil {
		return nil, invalidArgs(c.req.Command, "cluster %d has no command %q in the server's model", *a.ClusterID, *a.CommandName)
	}
	payload, err := wire.EncodeFields(cmd.Fields, a.Payload)
	if err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	opts := client.InvokeOptions{Timed: timed > 0 || cmd.Timed, TimedTimeout: timed, ExpectedProcessingTime: processing}
	res, err := n.Invoke(ctx, client.CommandAt(*a.EndpointID, *a.ClusterID, cmd.ID), payload, opts)
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	if !res.HasFields {
		return nil, nil
	}
	if resp := cl.Response(res.Command); resp != nil {
		return wire.Fields(resp.Fields, res.Fields), nil
	}
	return wire.Untyped(res.Fields), nil
}

// cmdBatchedDeviceCommands is answered by the dispatcher's gap check
// (Questions Q7).
func cmdBatchedDeviceCommands(context.Context, *Server, *call) (any, *protocol.Error) {
	return nil, protocol.Errorf(protocol.InvalidCommand, "batched_device_commands is not available")
}

// cmdSetAutoSubscription refuses outside --disable-auto-subscription, as
// matterjs-server does (docs/websockets_api.md:3118); in that mode the
// dispatcher's gap check answers (Questions Q9).
func cmdSetAutoSubscription(_ context.Context, _ *Server, c *call) (any, *protocol.Error) {
	return nil, invalidArgs(c.req.Command, "only available with the server running with --disable-auto-subscription")
}
