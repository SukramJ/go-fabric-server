// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// call is one request being answered.
type call struct {
	conn *wsConn
	req  protocol.Request
	// after runs once the response is handed to the socket.
	after func()
}

func (c *call) args(dst any) *protocol.Error {
	return protocol.DecodeArgs(c.req.Command, c.req.Args, dst)
}

type handler func(ctx context.Context, s *Server, c *call) (any, *protocol.Error)

// handlers is the S1 command set — the dispatch of matterjs-server's
// #handleWebSocketRequest (WebSocketControllerHandler.ts:947-1130, read
// map §1.7) as far as S1 implements it. start_listening is answered by
// the dispatcher itself (it must hold the server's lock while it answers).
var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"server_info":                   cmdServerInfo,
		"diagnostics":                   cmdDiagnostics,
		"get_loglevel":                  cmdGetLogLevel,
		"set_loglevel":                  cmdSetLogLevel,
		"get_nodes":                     cmdGetNodes,
		"get_node":                      cmdGetNode,
		"commission_with_code":          cmdCommissionWithCode,
		"commission_on_network":         cmdCommissionOnNetwork,
		"parse_onboarding_payload":      cmdParseOnboardingPayload,
		"discover_commissionable_nodes": cmdDiscover,
		"discover":                      cmdDiscover,
		"read_attribute":                cmdReadAttribute,
		"read":                          cmdRead,
		"write_attribute":               cmdWriteAttribute,
		"device_command":                cmdDeviceCommand,
		"batched_device_commands":       cmdBatchedDeviceCommands,
		"subscribe":                     cmdSubscribe,
		"unsubscribe":                   cmdUnsubscribe,
		"enable_auto_subscription":      cmdSetAutoSubscription,
		"disable_auto_subscription":     cmdSetAutoSubscription,
		"remove_node":                   cmdRemoveNode,
		"interview_node":                cmdInterviewNode,
		"ping_node":                     cmdPingNode,
		"get_node_ip_addresses":         cmdGetNodeIPAddresses,
		"open_commissioning_window":     cmdOpenCommissioningWindow,
		"set_wifi_credentials":          cmdSetWifiCredentials,
		"remove_wifi_credentials":       cmdRemoveWifiCredentials,
		"set_thread_dataset":            cmdSetThreadDataset,
		"remove_thread_dataset":         cmdRemoveThreadDataset,
		"get_all_credentials":           cmdGetAllCredentials,
		"set_default_fabric_label":      cmdSetDefaultFabricLabel,
		"get_fabric_label":              cmdGetFabricLabel,
		"get_matter_fabrics":            cmdGetMatterFabrics,
		"remove_matter_fabric":          cmdRemoveMatterFabric,
	}
}

// planned names the bundle that brings a command of matterjs-server's
// dispatch the server does not implement (concept v8 C.2).
var planned = map[string]string{
	"set_acl_entry":                 "S2",
	"set_node_binding":              "S2",
	"check_node_update":             "S2",
	"update_node":                   "S2",
	"initiate_ota_upload":           "S2",
	"get_vendor_names":              "S2",
	"get_icd_state":                 "S2",
	"register_icd":                  "S2",
	"resync_icd":                    "S2",
	"unregister_icd":                "S2",
	"import_test_node":              "S3",
	"get_thread_border_routers":     "S3",
	"get_thread_diagnostics":        "S3",
	"get_network_topology":          "S3",
	"camera_get_capabilities":       "never",
	"camera_start_stream":           "never",
	"camera_stop_stream":            "never",
	"camera_snapshot":               "never",
	"camera_release_stream":         "never",
	"camera_provide_answer":         "never",
	"camera_provide_ice_candidates": "never",
	"send_webrtc_provider_command":  "never",
}

// gap is the answer of a command whose route go-fabric's controller
// facade does not offer yet (report S1, Questions): InvalidCommand naming
// what is missing. Such a command is not in capabilities.
type gap struct {
	needs string
}

// gaps are the S1 commands without a facade route on a backend; filled
// by availability checks at call time.
func (s *Server) gapOf(command string) *gap {
	switch command {
	case "discover_commissionable_nodes", "discover", "commission_on_network":
		if _, ok := s.b().(Discoverer); !ok {
			return &gap{needs: "a DNS-SD discovery route on the controller (controller.Controller.Discover)"}
		}
	case "batched_device_commands":
		return &gap{needs: "a multi-command invoke on client.Node (one InvokeRequest with several commands)"}
	case "open_commissioning_window":
		return &gap{needs: "an enhanced commissioning window route on client.Node (OpenCommissioningWindow with the PAKE verifier and the pairing codes)"}
	case "parse_onboarding_payload":
		return &gap{needs: "an exported onboarding payload decoder in the controller facade"}
	case "enable_auto_subscription", "disable_auto_subscription":
		if s.opts.DisableAutoSubscription {
			return &gap{needs: "switching a node's own subscription at run time (client.Node.SetAutoSubscribe)"}
		}
	}
	return nil
}

// Capabilities are the commands this server answers, sorted — the
// capabilities list of server_info (concept v8 C.1, our extension).
func (s *Server) Capabilities() []string {
	out := []string{"start_listening"}
	for name := range handlers {
		if s.gapOf(name) == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// handle answers one frame.
func (s *Server) handle(ctx context.Context, conn *wsConn, frame []byte) {
	req, perr := protocol.DecodeRequest(frame)
	if perr != nil {
		conn.reply(perr)
		return
	}
	s.log.Debug("fabricserver.ws.request", slog.String("conn", conn.id), slog.String("command", req.Command), slog.String("message_id", req.MessageID))
	if req.Command == "start_listening" {
		s.startListening(conn, req)
		return
	}
	c := &call{conn: conn, req: req}
	result, err := s.dispatch(ctx, c)
	if err != nil {
		err.MessageID = req.MessageID
		s.log.Info("fabricserver.ws.error", slog.String("conn", conn.id), slog.String("command", req.Command), slog.Int("code", int(err.ErrorCode)), slog.String("details", err.Details))
		conn.reply(err)
	} else {
		conn.reply(protocol.Result{MessageID: req.MessageID, Result: result})
	}
	if c.after != nil {
		c.after()
	}
}

func (s *Server) dispatch(ctx context.Context, c *call) (result any, err *protocol.Error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("fabricserver.ws.panic", slog.String("command", c.req.Command), slog.Any("panic", r))
			result, err = nil, protocol.Errorf(protocol.UnknownError, "%s failed: %v", c.req.Command, r)
		}
	}()
	h, ok := handlers[c.req.Command]
	if !ok {
		if bundle, ok := planned[c.req.Command]; ok {
			return nil, protocol.Errorf(protocol.InvalidCommand, "not implemented in go-fabric-server %s; planned for %s", s.opts.Version, bundle)
		}
		return nil, protocol.Errorf(protocol.InvalidCommand, "Unknown command: %s", c.req.Command)
	}
	if g := s.gapOf(c.req.Command); g != nil {
		return nil, protocol.Errorf(protocol.InvalidCommand, "not implemented in go-fabric-server %s; needs %s in go-fabric", s.opts.Version, g.needs)
	}
	return h(ctx, s, c)
}

// startListening answers start_listening with every node and switches the
// connection's events on once the answer is handed to the socket
// (WebSocketControllerHandler.ts:833-843, :1220-1226). It holds the
// server's lock throughout, so no event is emitted between the dump and
// the switch.
func (s *Server) startListening(conn *wsConn, req protocol.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := s.nodeListLocked(false)
	b, err := protocol.Encode(protocol.Result{MessageID: req.MessageID, Result: nodes})
	if err != nil {
		b = protocol.MustEncode(&protocol.Error{MessageID: req.MessageID, ErrorCode: protocol.UnknownError, Details: err.Error()})
	}
	conn.send.SendReliable(b)
	conn.listening = true
}

// noArgs checks that a command that reads no arguments got an object —
// the dispatcher already did; it exists so each such handler states it.
func noArgs(json.RawMessage) {}

func invalidArgs(command, format string, args ...any) *protocol.Error {
	return protocol.Errorf(protocol.InvalidArguments, "%s: %s", command, fmt.Sprintf(format, args...))
}
