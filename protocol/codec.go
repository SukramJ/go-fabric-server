// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Encode is the one encoder of the server: encoding/json without HTML
// escaping (JSON.stringify escapes nothing either), no trailing newline.
// uint64 values come out as plain decimal digits at full width, the form
// matterjs-server's toBigIntAwareJson writes
// (packages/ws-client/src/json-utils.ts:21-52).
func Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// MustEncode is [Encode] for values that always encode (the server's own
// types); it panics otherwise.
func MustEncode(v any) []byte {
	b, err := Encode(v)
	if err != nil {
		panic(err)
	}
	return b
}

// DecodeRequest is the one request decoder. A frame that is not a JSON
// object, or whose message_id or command has the wrong type, fails with
// InvalidArguments; an absent or null args becomes {}, and an args that is
// not an object fails with InvalidArguments naming the command
// (docs/websockets_api.md:50-54). The failure carries the message id when
// the frame stated one.
func DecodeRequest(frame []byte) (Request, *Error) {
	var raw struct {
		MessageID json.RawMessage `json:"message_id"`
		Command   json.RawMessage `json:"command"`
		Args      json.RawMessage `json:"args"`
	}
	dec := json.NewDecoder(bytes.NewReader(frame))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return Request{}, Errorf(InvalidArguments, "the message is not a JSON object: %v", err)
	}
	if dec.More() {
		return Request{}, Errorf(InvalidArguments, "the message has data after its JSON object")
	}
	var req Request
	switch id := bytes.TrimSpace(raw.MessageID); {
	case len(id) == 0 || string(id) == "null":
	case id[0] == '"':
		if err := json.Unmarshal(id, &req.MessageID); err != nil {
			return req, Errorf(InvalidArguments, "message_id: %v", err)
		}
	case isNumberLiteral(id):
		// A numeric id is answered under its own digits, so a client that
		// numbers its messages still matches the answer.
		req.MessageID = string(id)
	default:
		return req, Errorf(InvalidArguments, "message_id must be a string")
	}
	if err := json.Unmarshal(raw.Command, &req.Command); err != nil || req.Command == "" {
		e := Errorf(InvalidArguments, "the message names no command")
		e.MessageID = req.MessageID
		return req, e
	}
	args := bytes.TrimSpace(raw.Args)
	switch {
	case len(args) == 0 || string(args) == "null":
		req.Args = json.RawMessage("{}")
	case args[0] == '{':
		req.Args = json.RawMessage(args)
	default:
		e := Errorf(InvalidArguments, "%s: args must be a JSON object", req.Command)
		e.MessageID = req.MessageID
		return req, e
	}
	return req, nil
}

func isNumberLiteral(b []byte) bool {
	var n json.Number
	return json.Unmarshal(b, &n) == nil
}

// DecodeArgs decodes a request's args into dst with numbers kept exact
// (json.Number for an interface field). Unknown keys are ignored, as
// python-matter-server ignores them (parse_arguments with strict=False,
// docs/websockets_api.md:2491-2497). A failure is InvalidArguments naming
// the command.
func DecodeArgs(command string, args json.RawMessage, dst any) *Error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return Errorf(InvalidArguments, "%s: argument %s: %s is not a valid %s", command, te.Field, te.Value, te.Type)
		}
		return Errorf(InvalidArguments, "%s: %v", command, err)
	}
	return nil
}

// Uint64 is an unsigned 64-bit id argument (node_id, fabric ids): a JSON
// integer literal of up to 20 digits, parsed from its text so it never
// passes through a float. A negative, fractional or exponent form, a
// string and an out-of-range number are refused — matterjs-server refuses
// a large node_id in exponent form for the same reason
// (docs/websockets_api.md:1833-1867, "Send a large node_id as an integer
// literal").
type Uint64 uint64

// UnmarshalJSON implements json.Unmarshaler.
func (u *Uint64) UnmarshalJSON(b []byte) error {
	v, err := ParseUint64(b)
	if err != nil {
		return err
	}
	*u = Uint64(v)
	return nil
}

// ParseUint64 parses a JSON integer literal as an unsigned 64-bit number.
func ParseUint64(b []byte) (uint64, error) {
	s := string(bytes.TrimSpace(b))
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, fmt.Errorf("%q is not an unsigned integer literal", s)
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an unsigned 64-bit integer literal", s)
	}
	return v, nil
}
