// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric-server/protocol"
)

func TestEncodeUint64AndNoEscaping(t *testing.T) {
	b, err := protocol.Encode(protocol.Event{Event: "node_removed", Data: uint64(18446744069414584320)})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"event":"node_removed","data":18446744069414584320}` {
		t.Fatalf("%s", b)
	}
	b = protocol.MustEncode(map[string]string{"x": "<&>"})
	if string(b) != `{"x":"<&>"}` {
		t.Fatalf("%s", b)
	}
	r := protocol.MustEncode(protocol.Result{MessageID: "1"})
	if string(r) != `{"message_id":"1","result":null}` {
		t.Fatalf("%s", r)
	}
	e := protocol.MustEncode(protocol.Errorf(protocol.NodeCommissionFailed, "x").WithReason(protocol.ReasonTimeout))
	if string(e) != `{"message_id":"","error_code":1,"details":"x","reason":"timeout"}` {
		t.Fatalf("%s", e)
	}
	cs := 3
	e = protocol.MustEncode(protocol.Errorf(protocol.UnknownError, "y").WithStatus(129, &cs))
	if string(e) != `{"message_id":"","error_code":0,"details":"y","status":129,"cluster_status":3}` {
		t.Fatalf("%s", e)
	}
}

func TestDecodeRequest(t *testing.T) {
	cases := []struct {
		in, args string
		code     protocol.ErrorCode
		id       string
	}{
		{in: `{"message_id":"1","command":"server_info"}`, args: "{}", id: "1"},
		{in: `{"message_id":"2","command":"get_node","args":null}`, args: "{}", id: "2"},
		{in: `{"message_id":"3","command":"get_node","args":{"node_id":1}}`, args: `{"node_id":1}`, id: "3"},
		{in: `{"message_id":4,"command":"x"}`, args: "{}", id: "4"},
		{in: `{"message_id":"5","command":"get_node","args":[1]}`, code: protocol.InvalidArguments, id: "5"},
		{in: `{"message_id":"6","command":"get_node","args":"x"}`, code: protocol.InvalidArguments, id: "6"},
		{in: `{"message_id":"7"}`, code: protocol.InvalidArguments, id: "7"},
		{in: `[1,2]`, code: protocol.InvalidArguments},
		{in: `{"message_id":"8","command":"a"} trailing`, code: protocol.InvalidArguments},
		{in: `{"message_id":true,"command":"a"}`, code: protocol.InvalidArguments},
	}
	for _, c := range cases {
		req, err := protocol.DecodeRequest([]byte(c.in))
		if c.code != 0 || err != nil {
			if err == nil || err.ErrorCode != c.code || err.MessageID != c.id {
				t.Fatalf("%s: %+v", c.in, err)
			}
			continue
		}
		if req.MessageID != c.id || string(req.Args) != c.args {
			t.Fatalf("%s: %+v", c.in, req)
		}
	}
}

func TestDecodeArgsAndUint64(t *testing.T) {
	var a struct {
		NodeID *protocol.Uint64 `json:"node_id"`
		Any    any              `json:"any"`
	}
	if err := protocol.DecodeArgs("get_node", json.RawMessage(`{"node_id":18446744073709551615,"any":9007199254740993,"extra":1}`), &a); err != nil {
		t.Fatal(err)
	}
	if uint64(*a.NodeID) != 18446744073709551615 {
		t.Fatalf("%d", *a.NodeID)
	}
	if n, ok := a.Any.(json.Number); !ok || n.String() != "9007199254740993" {
		t.Fatalf("%v", a.Any)
	}
	for _, bad := range []string{`{"node_id":1e3}`, `{"node_id":-1}`, `{"node_id":1.5}`, `{"node_id":"1"}`, `{"node_id":18446744073709551616}`} {
		if err := protocol.DecodeArgs("get_node", json.RawMessage(bad), &a); err == nil || err.ErrorCode != protocol.InvalidArguments {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}

func TestListenAndToken(t *testing.T) {
	for _, c := range []struct {
		in       string
		network  string
		loopback bool
	}{
		{"127.0.0.1:5580", "tcp", true},
		{"[::1]:5580", "tcp", true},
		{"localhost:5580", "tcp", true},
		{":5580", "tcp", false},
		{"0.0.0.0:5580", "tcp", false},
		{"unix:/run/x.sock", "unix", true},
		{"", "unix", true},
	} {
		l, err := protocol.ParseListen(c.in)
		if err != nil || l.Network != c.network || l.Loopback() != c.loopback {
			t.Fatalf("%q: %+v %v", c.in, l, err)
		}
		if err := protocol.CheckListen(l, ""); (err == nil) != c.loopback {
			t.Fatalf("%q without a token: %v", c.in, err)
		}
		if err := protocol.CheckListen(l, "t"); err != nil {
			t.Fatalf("%q with a token: %v", c.in, err)
		}
	}
	if _, err := protocol.ParseListen("nonsense"); err == nil {
		t.Fatal("a listener needs host:port or unix:")
	}
	if _, err := protocol.ParseListen("unix:"); err == nil {
		t.Fatal("unix: needs a path")
	}
	h := protocol.RequireToken("tok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, c := range []struct {
		header, query string
		code          int
	}{
		{"", "", 401}, {"Bearer bad", "", 401}, {"Bearer tok", "", 204}, {"", "tok", 204}, {"Basic tok", "", 401},
	} {
		r := httptest.NewRequest(http.MethodGet, "/ws?token="+c.query, http.NoBody)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.code {
			t.Fatalf("%+v: %d", c, w.Code)
		}
	}
	open := protocol.RequireToken("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	w := httptest.NewRecorder()
	open.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ws", http.NoBody))
	if w.Code != 204 {
		t.Fatal("no token, no check")
	}
}

func FuzzDecodeRequest(f *testing.F) {
	for _, s := range []string{
		`{"message_id":"1","command":"server_info"}`,
		`{"message_id":"1","command":"get_node","args":{"node_id":1}}`,
		`{"message_id":1,"command":"x","args":null}`,
		`{"command":"x","args":[1]}`,
		`[]`, `"x"`, `{`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		req, err := protocol.DecodeRequest(b)
		if err != nil {
			// A failure always encodes.
			if _, e := protocol.Encode(err); e != nil {
				t.Fatal(e)
			}
			return
		}
		if req.Command == "" || len(req.Args) == 0 || req.Args[0] != '{' {
			t.Fatalf("accepted %q as %+v", b, req)
		}
		var a struct {
			NodeID *protocol.Uint64 `json:"node_id"`
		}
		_ = protocol.DecodeArgs(req.Command, req.Args, &a)
	})
}

func FuzzParseUint64(f *testing.F) {
	for _, s := range []string{"0", "18446744073709551615", "1e3", "-1", " 7 ", "18446744073709551616"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := protocol.ParseUint64([]byte(s))
		if err != nil {
			return
		}
		if strings.TrimSpace(s) == "" {
			t.Fatalf("%q parsed as %d", s, v)
		}
	})
}
