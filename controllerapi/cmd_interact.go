// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// readBatch is matterjs-server's batch of read_attribute paths per Read
// (ControllerCommandHandler.ts:1283).
const readBatch = 9

func attributePath(p wirePath) client.AttributePath {
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

func eventPath(p wirePath, urgent bool) client.EventPath {
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
		wp, err := parsePath(p, 3, false)
		if err != nil {
			return nil, invalidArgs(c.req.Command, "%v", err)
		}
		parsed = append(parsed, attributePath(wp))
	}
	out := map[string]json.RawMessage{}
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
			out[attrKey(d.Path.Endpoint, d.Path.Cluster, d.Path.Attribute)] = attributeJSON(d.Path.Cluster, d.Path.Attribute, d.Value)
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
		wp, err := parsePath(p, 3, true)
		if err != nil {
			return r, invalidArgs(command, "%v", err)
		}
		r.Attributes = append(r.Attributes, attributePath(wp))
	}
	for _, e := range a.EventPaths {
		wp, err := parsePath(e.Path, 3, true)
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
		wp, err := parsePath(f.Path, 2, true)
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
	Attributes      map[string]json.RawMessage `json:"attributes"`
	Events          []nodeEventData            `json:"events"`
	AttributeStatus map[string]pathStatus      `json:"attribute_status,omitempty"`
	EventStatus     map[string]pathStatus      `json:"event_status,omitempty"`
	DataVersions    map[string]uint32          `json:"data_versions,omitempty"`
}

type pathStatus struct {
	Status        int  `json:"status"`
	ClusterStatus *int `json:"cluster_status,omitempty"`
}

func newReportData() reportData {
	return reportData{Attributes: map[string]json.RawMessage{}, Events: []nodeEventData{}}
}

func (r *reportData) add(nodeID uint64, rep *client.Report) {
	for i := range rep.Attributes {
		d := &rep.Attributes[i]
		r.Attributes[attrKey(d.Path.Endpoint, d.Path.Cluster, d.Path.Attribute)] = attributeJSON(d.Path.Cluster, d.Path.Attribute, d.Value)
		if d.DataVersion != 0 {
			if r.DataVersions == nil {
				r.DataVersions = map[string]uint32{}
			}
			r.DataVersions[clusterKey(d.Path.Endpoint, d.Path.Cluster)] = d.DataVersion
		}
	}
	for _, st := range rep.Statuses {
		if r.AttributeStatus == nil {
			r.AttributeStatus = map[string]pathStatus{}
		}
		r.AttributeStatus[attrKey(st.Path.Endpoint, st.Path.Cluster, st.Path.Attribute)] = statusEntry(int(st.Status), st.ClusterStatus)
	}
	for i := range rep.Events {
		e := &rep.Events[i]
		r.Events = append(r.Events, eventData(nodeID, e))
	}
	for _, st := range rep.EventStatuses {
		if r.EventStatus == nil {
			r.EventStatus = map[string]pathStatus{}
		}
		r.EventStatus[attrKey(st.Path.Endpoint, st.Path.Cluster, st.Path.Event)] = statusEntry(int(st.Status), st.ClusterStatus)
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
	wp, err := parsePath(*a.AttributePath, 3, true)
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
	value, attrTimed, err := encodeAttribute(*wp.Cluster, *wp.Element, a.Value)
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
	processing, perr := interactionTimeout(c.req.Command, a.InteractionTimeout)
	if perr != nil {
		return nil, perr
	}
	cl := schema.ClusterByID(*a.ClusterID)
	cmd := cl.CommandByName(*a.CommandName)
	if cmd == nil {
		return nil, invalidArgs(c.req.Command, "cluster %d has no command %q in the server's model", *a.ClusterID, *a.CommandName)
	}
	payload, err := encodeCommand(*a.ClusterID, cmd.ID, a.Payload)
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
	return responseJSON(*a.ClusterID, res.Command, res.Fields), nil
}

// batchedResult is BatchedCommandResult (python client models.py:903-909):
// the command's status, its cluster status when the device sent one, its
// response's fields by name when it answered with data.
type batchedResult struct {
	Status        int             `json:"status"`
	ClusterStatus *int            `json:"cluster_status,omitempty"`
	Response      json.RawMessage `json:"response,omitempty"`
}

// cmdBatchedDeviceCommands invokes several commands on one node in one
// InvokeRequest, in their order (client.Node.InvokeBatch, go-fabric
// v0.12.1) — matterjs-server's #handleBatchedDeviceCommands
// (WebSocketControllerHandler.ts:1821-1883, ControllerCommandHandler.ts:1832-1917):
// every entry is checked before anything is sent; more commands than the
// node's MaxPathsPerInvoke are refused, so the batch is never split; one
// command failing does not fail the others, a failed exchange fails the
// request.
func cmdBatchedDeviceCommands(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Commands           []json.RawMessage `json:"commands"`
		TimedTimeout       *json.Number      `json:"timed_request_timeout_ms"`
		InteractionTimeout *json.Number      `json:"interaction_timeout_ms"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if len(a.Commands) == 0 {
		return nil, invalidArgs(c.req.Command, "requires a non-empty commands list")
	}
	timed, perr := timedTimeout(c.req.Command, a.TimedTimeout)
	if perr != nil {
		return nil, perr
	}
	processing, perr := interactionTimeout(c.req.Command, a.InteractionTimeout)
	if perr != nil {
		return nil, perr
	}
	reqs, anyTimed, perr := batchRequests(c.req.Command, a.Commands)
	if perr != nil {
		return nil, perr
	}
	if most := n.MaxPathsPerInvoke(); len(reqs) > most {
		return nil, invalidArgs(c.req.Command, "has %d commands, but node %d accepts at most %d per invoke (its MaxPathsPerInvoke, 1 when it reports none)", len(reqs), nodeID, most)
	}
	opts := client.InvokeOptions{Timed: timed > 0 || anyTimed, TimedTimeout: timed, ExpectedProcessingTime: processing}
	results, err := n.InvokeBatch(ctx, reqs, opts)
	if err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	out := make([]batchedResult, len(results))
	for i := range results {
		r := &results[i]
		out[i].Status = int(r.Status)
		if r.ClusterStatus != nil {
			cs := int(*r.ClusterStatus)
			out[i].ClusterStatus = &cs
		}
		if r.HasFields {
			out[i].Response = responseJSON(reqs[i].Path.Cluster, r.Command, r.Fields)
		}
	}
	return out, nil
}

// batchRequests checks and encodes the entries of batched_device_commands
// (WebSocketControllerHandler.ts:1826-1850).
func batchRequests(command string, raw []json.RawMessage) (reqs []client.CommandRequest, anyTimed bool, perr *protocol.Error) {
	reqs = make([]client.CommandRequest, 0, len(raw))
	for i, r := range raw {
		var e struct {
			EndpointID  *uint16         `json:"endpoint_id"`
			ClusterID   *uint32         `json:"cluster_id"`
			CommandName *string         `json:"command_name"`
			Payload     json.RawMessage `json:"payload"`
		}
		if b := bytes.TrimSpace(r); len(b) == 0 || b[0] != '{' {
			return nil, false, invalidArgs(command, "command %d must be an object", i)
		}
		if json.Unmarshal(r, &e) != nil || e.EndpointID == nil || *e.EndpointID == 0xFFFF || e.ClusterID == nil || e.CommandName == nil {
			return nil, false, invalidArgs(command, "command %d needs endpoint_id, cluster_id and command_name", i)
		}
		cl := schema.ClusterByID(*e.ClusterID)
		if cl == nil {
			return nil, false, invalidArgs(command, "command %d: Cluster Id \"%d\" unknown", i, *e.ClusterID)
		}
		if cl.Name == "WebRtcTransportProvider" {
			return nil, false, invalidArgs(command, "command %d: send WebRTC Transport Provider commands with device_command or send_webrtc_provider_command", i)
		}
		cmd := cl.CommandByName(*e.CommandName)
		if cmd == nil {
			return nil, false, invalidArgs(command, "command %d: Command \"%s\" does not exist on cluster \"%s\"", i, *e.CommandName, cl.Name)
		}
		fields, err := encodeCommand(cl.ID, cmd.ID, e.Payload)
		if err != nil {
			return nil, false, invalidArgs(command, "command %d: Invalid command fields: %v", i, err)
		}
		anyTimed = anyTimed || cmd.Timed
		reqs = append(reqs, client.CommandRequest{Path: client.CommandAt(*e.EndpointID, cl.ID, cmd.ID), Fields: fields})
	}
	return reqs, anyTimed, nil
}

// interactionTimeout reads interaction_timeout_ms: an integer from 0 to
// 2^53-1.
func interactionTimeout(command string, raw *json.Number) (time.Duration, *protocol.Error) {
	if raw == nil {
		return 0, nil
	}
	v, err := strconv.ParseUint(raw.String(), 10, 53)
	if err != nil {
		return 0, invalidArgs(command, "interaction_timeout_ms must be an integer from 0 to 2^53-1")
	}
	return time.Duration(v) * time.Millisecond, nil //nolint:gosec // bounded above
}

// cmdSetAutoSubscription switches the server's own subscription to one
// node on or off (client.Node.SetAutoSubscribe, go-fabric v0.12.1) — only
// with --disable-auto-subscription, as matterjs-server
// (ControllerCommandHandler.ts:2494-2568): enabling ends the node's
// subscriptions opened with subscribe (each gets subscription_ended), and
// subscribe to the node is refused while it is on. go-fabric persists the
// choice in the node's settings, matterjs-server does not (report S3,
// by_design candidate).
func cmdSetAutoSubscription(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	if !s.opts.DisableAutoSubscription {
		return nil, invalidArgs(c.req.Command, "requires the server to run with --disable-auto-subscription")
	}
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.NodeID != nil && isTestNodeID(uint64(*a.NodeID)) {
		return nil, protocol.Errorf(protocol.InvalidArguments, "Auto subscription commands are not supported for test nodes")
	}
	n, nodeID, perr := s.node(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	on := c.req.Command == "enable_auto_subscription"
	if err := n.SetAutoSubscribe(ctx, on); err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	if on {
		s.mu.Lock()
		s.endNodeSubscriptions(nodeID)
		s.mu.Unlock()
	}
	return nil, nil
}
