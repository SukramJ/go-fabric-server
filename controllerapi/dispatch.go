// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
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

// handlers is the command set — the dispatch of matterjs-server's
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
		"set_acl_entry":                 cmdSetACLEntry,
		"set_node_binding":              cmdSetNodeBinding,
		"get_vendor_names":              cmdGetVendorNames,
		"get_icd_state":                 cmdGetICDState,
		"register_icd":                  cmdRegisterICD,
		"unregister_icd":                cmdUnregisterICD,
		"resync_icd":                    cmdResyncICD,
		"get_network_topology":          cmdGetNetworkTopology,
		"import_test_node":              cmdImportTestNode,
	}
}

// planned names why a command of matterjs-server's dispatch is not
// answered (concept v8 C.2): the go-fabric work it waits for, or never.
var planned = map[string]string{
	"check_node_update":             otaRoute,
	"update_node":                   otaRoute,
	"initiate_ota_upload":           otaRoute,
	"get_thread_border_routers":     threadRoute,
	"get_thread_diagnostics":        threadRoute,
	"camera_get_capabilities":       "never",
	"camera_start_stream":           "never",
	"camera_stop_stream":            "never",
	"camera_snapshot":               "never",
	"camera_release_stream":         "never",
	"camera_provide_answer":         "never",
	"camera_provide_ice_candidates": "never",
	"send_webrtc_provider_command":  "never",
}

// The go-fabric routes the stubs wait for (brief S3 §3).
const (
	otaRoute    = "the OTA provider on go-fabric's controller (milestone 5 phase 25: a software update route on client.Node)"
	threadRoute = "a Thread border router route in go-fabric (milestone 6, ADR 0027: the OTBR client and ThreadNetworkDiagnostics)"
)

// gap is the answer of a command whose route go-fabric's controller
// facade does not offer: InvalidCommand naming what is missing. Such a
// command is not in capabilities. None is left since go-fabric v0.12.1;
// the hook stays for the next.
type gap struct {
	needs string
}

func (s *Server) gapOf(string) *gap { return nil }

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
		if why, ok := planned[c.req.Command]; ok {
			if why == "never" {
				return nil, protocol.Errorf(protocol.InvalidCommand, "not implemented in go-fabric-server %s; not planned (camera and WebRTC are out of scope)", s.opts.Version)
			}
			return nil, protocol.Errorf(protocol.InvalidCommand, "not implemented in go-fabric-server %s; needs %s", s.opts.Version, why)
		}
		return nil, protocol.Errorf(protocol.InvalidCommand, "Unknown command: %s", c.req.Command)
	}
	if g := s.gapOf(c.req.Command); g != nil {
		return nil, protocol.Errorf(protocol.InvalidCommand, "not implemented in go-fabric-server %s; needs %s in go-fabric", s.opts.Version, g.needs)
	}
	if tn, ok, perr := s.testNodeFor(c); ok {
		if perr != nil {
			return nil, perr
		}
		return testCommands[c.req.Command](s, c, tn)
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

func invalidArgs(command, format string, args ...any) *protocol.Error {
	return protocol.Errorf(protocol.InvalidArguments, "%s: %s", command, fmt.Sprintf(format, args...))
}
