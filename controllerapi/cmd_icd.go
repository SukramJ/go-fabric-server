// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/SukramJ/go-fabric/controller/client"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// The four ICD commands run on go-fabric's ICD client
// (controller/client.ICDClient, v0.12.0, after matter.js IcdClient.ts) —
// matterjs-server's ControllerCommandHandler.ts:2426-2602 and
// WebSocketControllerHandler.ts:2112-2150 at a7922e85.

var (
	icdManagement     = schema.ClusterByName("IcdManagement")
	attrOperatingMode = icdManagement.AttributeByName("OperatingMode")
)

// icdState is IcdStateData (python client models.py:293-303): every key
// present; next_expected_checkin in Unix milliseconds.
type icdState struct {
	Supported           bool    `json:"supported"`
	LITSupported        bool    `json:"lit_supported"`
	Registered          bool    `json:"registered"`
	OperatingMode       *string `json:"operating_mode"`
	Awake               *bool   `json:"awake"`
	Available           *bool   `json:"available"`
	NextExpectedCheckIn *int64  `json:"next_expected_checkin"`
}

// icdNode resolves the node of an ICD command; a test node is refused as
// matterjs-server refuses it (WebSocketControllerHandler.ts:2107-2111).
func (s *Server) icdNode(command string, id *protocol.Uint64) (*client.Node, uint64, *protocol.Error) {
	if id != nil && isTestNodeID(uint64(*id)) {
		return nil, 0, protocol.Errorf(protocol.InvalidArguments, "ICD commands are not supported for test nodes")
	}
	return s.node(command, id)
}

// icdStateOf is getIcdState (ControllerCommandHandler.ts:2575-2600): a
// node without ICD Management is not supported, not an error; the
// operating mode is the node's OperatingMode from the cache, LIT or SIT.
func (s *Server) icdStateOf(n *client.Node) icdState {
	st := icdState{}
	mode, has := s.cachedValue(n, 0, icdManagement.ID, attrOperatingMode.ID)
	if !has {
		return st
	}
	c := n.ICD()
	if c == nil {
		return st
	}
	st.Supported, st.LITSupported, st.Registered = true, c.PeerSupportsLIT(), c.Registered()
	m := "SIT"
	if v, ok := uintOf(mode); ok && v == 1 {
		m = "LIT"
	}
	awake, available := c.Awake(), c.Available()
	st.OperatingMode, st.Awake, st.Available = &m, &awake, &available
	if t, ok := c.NextExpectedCheckIn(); ok {
		ms := t.UnixMilli()
		st.NextExpectedCheckIn = &ms
	}
	return st
}

// cachedValue is a rendered attribute value of the node from the cache;
// has is false when the node data has none.
func (s *Server) cachedValue(n *client.Node, ep uint16, cluster, attribute uint32) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.nodes[n.ID()]
	if st == nil {
		return nil, false
	}
	v, ok := st.attrs[attrKey(ep, cluster, attribute)]
	return v, ok
}

func cmdGetICDState(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, _, perr := s.icdNode(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	return s.icdStateOf(n), nil
}

// icdError maps an ICD client error: a multi-admin refusal is
// IcdMultiAdmin (100) with matterjs-server's JSON details
// (WebSocketMessageTypes.ts:266-272).
func icdError(command string, err error) *protocol.Error {
	var ma *client.ICDMultiAdminError
	if errors.As(err, &ma) {
		ids := make([]int, len(ma.AdminVendorIDs))
		for i, v := range ma.AdminVendorIDs {
			ids[i] = int(v)
		}
		details, _ := json.Marshal(struct {
			Message        string `json:"message"`
			AdminVendorIDs []int  `json:"admin_vendor_ids"`
		}{"Peer has administrators from other vendors that may not support LIT", ids})
		return &protocol.Error{ErrorCode: protocol.IcdMultiAdmin, Details: string(details)}
	}
	return nodeError(command, err)
}

// cmdRegisterICD registers us as the node's check-in client
// (ICDClient.Register) and answers the state; node_updated follows.
func cmdRegisterICD(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		AllowMultiAdmin *bool     `json:"allow_multi_admin"`
		IgnoredVendors  *[]uint16 `json:"ignored_vendors"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.icdNode(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if n.ICD() == nil {
		return nil, protocol.Errorf(protocol.NodeNotReady, "%s: node %d has no ICD client", c.req.Command, nodeID)
	}
	var o client.ICDRegisterOptions
	if a.AllowMultiAdmin != nil {
		o.AllowMultiAdmin = *a.AllowMultiAdmin
	}
	if a.IgnoredVendors != nil {
		o.IgnoredVendors = *a.IgnoredVendors
	}
	if err := n.ICD().Register(ctx, o); err != nil {
		return nil, icdError(c.req.Command, err)
	}
	s.broadcastNodeUpdated(nodeID)
	return s.icdStateOf(n), nil
}

// cmdUnregisterICD unregisters us on the node (ICDClient.Unregister), or
// with force forgets the registration locally only (ICDClient.Forget).
func cmdUnregisterICD(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		nodeArg
		Force bool `json:"force"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.icdNode(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if n.ICD() == nil {
		return nil, protocol.Errorf(protocol.NodeNotReady, "%s: node %d has no ICD client", c.req.Command, nodeID)
	}
	var err error
	if a.Force {
		err = n.ICD().Forget(ctx)
	} else {
		err = n.ICD().Unregister(ctx)
	}
	if err != nil {
		return nil, icdError(c.req.Command, err)
	}
	s.broadcastNodeUpdated(nodeID)
	return s.icdStateOf(n), nil
}

// cmdResyncICD forgets the local registration and re-establishes the
// server's subscription, so a LIT peer registers again
// (ControllerCommandHandler.ts:2452-2477); with the subscription off it
// only forgets.
func cmdResyncICD(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a nodeArg
	if err := c.args(&a); err != nil {
		return nil, err
	}
	n, nodeID, perr := s.icdNode(c.req.Command, a.NodeID)
	if perr != nil {
		return nil, perr
	}
	if n.ICD() == nil {
		return nil, protocol.Errorf(protocol.NodeNotReady, "%s: node %d has no ICD client", c.req.Command, nodeID)
	}
	if err := n.ICD().Forget(ctx); err != nil {
		return nil, icdError(c.req.Command, err)
	}
	if !n.AutoSubscribe() {
		return nil, nil
	}
	if err := n.SetAutoSubscribe(ctx, false); err != nil {
		return nil, nodeError(c.req.Command, err)
	}
	if err := n.SetAutoSubscribe(ctx, true); err != nil {
		return nil, protocol.Errorf(protocol.NodeNotReady, "%s: %v", c.req.Command, err)
	}
	return nil, nil
}
