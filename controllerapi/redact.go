// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// The texts the argument log puts in place of a value: a value the model
// marks secret (matter.js's isSecret, schema.Field.Secret and
// schema.Attribute.Secret), and a value the model cannot judge.
const (
	redacted = "[redacted]"
	unjudged = "[unjudged]"
)

// argMembers are the args members each handler reads besides its target
// (cmdDeviceCommand, cmdWriteAttribute): wire names of this server's
// protocol, not Matter ids. Any other member is logged "[unjudged]".
var argMembers = map[string][]string{
	"device_command":  {"node_id", "endpoint_id", "cluster_id", "command_name", "timed_request_timeout_ms", "interaction_timeout_ms"},
	"write_attribute": {"node_id", "attribute_path", "timed_request_timeout_ms"},
}

// sdpCredential is an SDP line whose value is a credential, ice-ufrag or
// ice-pwd, matched without regard to case — matterjs-server's
// SDP_CREDENTIAL_LINES (packages/ws-client/src/logging-redaction.ts:72-76).
var sdpCredential = regexp.MustCompile(`(?im)^(a=(?:ice-ufrag|ice-pwd):)[^\r\n]*`)

// loggedArgs is the args member of fabricserver.ws.request at Debug, for
// the two commands whose arguments the model describes: device_command
// (the payload judged by the request fields of its command) and
// write_attribute (the value judged by its attribute). The members the
// handler reads besides (argMembers) are logged as given; every other
// command logs no argument (ok false): their arguments are outside the
// snapshot, and the server keeps no table of names. The judgement fails
// closed — an unknown cluster, command or attribute, a value of another
// shape than the model's, a struct the tables do not resolve, a member
// the model does not define and a top-level member no handler reads are
// logged as "[unjudged]" — the credential lines of an SDP string are
// masked, and the raw frame is never logged.
func loggedArgs(command string, raw json.RawMessage) (args any, ok bool) {
	var (
		target string
		t      *schema.Type
		secret bool
	)
	switch command {
	case "device_command":
		target = "payload"
		if cmd := commandTarget(raw); cmd != nil {
			t = structOf(cmd.Fields)
		}
	case "write_attribute":
		target = "value"
		if a := writeTarget(raw); a != nil {
			t, secret = &a.Type, a.Secret
		}
	default:
		return nil, false
	}
	var top map[string]json.RawMessage
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, true
	}
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return unjudged, true
	}
	out := make(map[string]any, len(top))
	for k, v := range top {
		val := decodeAny(v)
		// The args decoder matches a member's name without regard to case
		// (encoding/json), so every spelling of the target is judged.
		if !strings.EqualFold(k, target) {
			out[k] = unjudged
			for _, m := range argMembers[command] {
				if strings.EqualFold(k, m) {
					out[k] = judgeValue(schema.Type{}, false, val)
				}
			}
			continue
		}
		switch {
		case t == nil:
			out[k] = unjudged
		case val == nil && target == "payload":
			out[k] = nil // the empty payload
		default:
			out[k] = judgeValue(*t, secret, val)
		}
	}
	return out, true
}

// commandTarget is the command a device_command's args name, nil when
// they name none the model knows.
func commandTarget(raw json.RawMessage) *schema.Command {
	var a struct {
		ClusterID   *uint32 `json:"cluster_id"`
		CommandName *string `json:"command_name"`
	}
	if protocol.DecodeArgs("device_command", raw, &a) != nil || a.ClusterID == nil || a.CommandName == nil {
		return nil
	}
	return schema.ClusterByID(*a.ClusterID).CommandByName(*a.CommandName)
}

// writeTarget is the attribute a write_attribute's args name, nil when
// they name none the model knows.
func writeTarget(raw json.RawMessage) *schema.Attribute {
	wp := writePathOf(raw)
	if wp == nil {
		return nil
	}
	return schema.ClusterByID(*wp.Cluster).Attribute(*wp.Element)
}

// writePathOf is the path of one attribute a write_attribute's args name,
// nil for none.
func writePathOf(raw json.RawMessage) *wirePath {
	var args struct {
		AttributePath *string `json:"attribute_path"`
	}
	if protocol.DecodeArgs("write_attribute", raw, &args) != nil || args.AttributePath == nil {
		return nil
	}
	wp, err := parsePath(*args.AttributePath, 3, true)
	if err != nil || wp.Cluster == nil || wp.Element == nil {
		return nil
	}
	return &wp
}

// judgeValue is v as the log shows it, typed t: a secret value is
// "[redacted]" (null stays null), a struct is walked by its fields — a
// member matched by every spelling the encoder accepts (memberField) — a
// list by its entry type, a scalar logged as given.
func judgeValue(t schema.Type, secret bool, v any) any {
	if v == nil {
		return nil
	}
	if secret {
		return redacted
	}
	switch t.Meta {
	case schema.MetaStruct:
		m, ok := v.(map[string]any)
		if !ok || t.Struct == nil {
			return unjudged
		}
		out := make(map[string]any, len(m))
		for k, val := range m {
			f := memberField(t.Struct, k)
			if f == nil {
				out[k] = unjudged
				continue
			}
			out[k] = judgeValue(f.Type, f.Secret, val)
		}
		return out
	case schema.MetaList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return unjudged
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = judgeValue(*t.Entry, false, e)
		}
		return out
	case schema.MetaAny, schema.MetaBool, schema.MetaUint, schema.MetaInt, schema.MetaFloat, schema.MetaDouble,
		schema.MetaString, schema.MetaBytes, schema.MetaEnum, schema.MetaBitmap:
	}
	switch x := v.(type) {
	case map[string]any, []any:
		return unjudged
	case string:
		// matterjs-server masks the SDP's credential lines in a member
		// named sdp (logging-redaction.ts:143-148); every string here.
		return sdpCredential.ReplaceAllString(x, "${1}"+redacted)
	}
	return v
}

// holdsSecret reports whether a value typed t may carry a secret: t, or
// a member or entry of it, is marked.
func holdsSecret(t schema.Type, secret bool, seen map[*schema.Struct]bool) bool {
	if secret {
		return true
	}
	switch {
	case t.Meta == schema.MetaStruct && t.Struct == nil:
		return true // a struct the tables do not resolve: fail closed
	case t.Struct != nil:
		if seen[t.Struct] {
			return false
		}
		seen[t.Struct] = true
		for i := range t.Struct.Fields {
			if holdsSecret(t.Struct.Fields[i].Type, t.Struct.Fields[i].Secret, seen) {
				return true
			}
		}
	case t.Entry != nil:
		return holdsSecret(*t.Entry, false, seen)
	}
	return false
}

// loggedDetails is an error's details as fabricserver.ws.error logs them.
// For device_command and write_attribute they are the encoder's text,
// which quotes the value it refused, so a target whose model holds a
// secret — or an attribute the model does not know, whose value is
// encoded untyped — logs "[redacted]". The wire answer is unchanged.
func loggedDetails(command string, raw json.RawMessage, details string) string {
	var t *schema.Type
	switch command {
	case "device_command":
		cmd := commandTarget(raw)
		if cmd == nil {
			return details // refused before any value is read
		}
		t = structOf(cmd.Fields)
	case "write_attribute":
		if writePathOf(raw) == nil {
			return details // refused before any value is read
		}
		a := writeTarget(raw)
		if a == nil || a.Secret {
			return redacted
		}
		t = &a.Type
	default:
		return details
	}
	if holdsSecret(*t, false, map[*schema.Struct]bool{}) {
		return redacted
	}
	return details
}
