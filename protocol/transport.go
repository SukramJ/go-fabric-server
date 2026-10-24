// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/websocket"
)

// WebSocket adapts a coder/websocket connection to the send path's
// [Socket]: text frames, Abort closes without a handshake.
func WebSocket(c *websocket.Conn) Socket { return wsSocket{c} }

type wsSocket struct{ c *websocket.Conn }

func (s wsSocket) Write(ctx context.Context, frame []byte) error {
	return s.c.Write(ctx, websocket.MessageText, frame)
}

func (s wsSocket) Abort(string) { _ = s.c.CloseNow() }

// Listen is a parsed --listen value.
type Listen struct {
	// Network is "tcp" or "unix".
	Network string
	// Address is the TCP address or the socket path.
	Address string
}

// DefaultListen is the listener when --listen is not given: the Unix
// socket fabric-server.sock in $XDG_RUNTIME_DIR, else in the temporary
// directory.
func DefaultListen() Listen {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return Listen{Network: "unix", Address: filepath.Join(dir, "fabric-server.sock")}
}

// ParseListen parses "unix:/path" or a TCP address ("127.0.0.1:5580",
// ":5580", "[::1]:5580"); empty is [DefaultListen].
func ParseListen(s string) (Listen, error) {
	switch {
	case s == "":
		return DefaultListen(), nil
	case strings.HasPrefix(s, "unix:"):
		p := strings.TrimPrefix(s, "unix:")
		if p == "" {
			return Listen{}, errors.New("listen: unix: needs a socket path")
		}
		return Listen{Network: "unix", Address: p}, nil
	}
	if _, _, err := net.SplitHostPort(s); err != nil {
		return Listen{}, fmt.Errorf("listen: %q is neither unix:/path nor host:port: %w", s, err)
	}
	return Listen{Network: "tcp", Address: s}, nil
}

// Loopback reports whether the listener is reachable from this host only:
// a Unix socket, or a TCP address on a loopback IP or "localhost".
func (l Listen) Loopback() bool {
	if l.Network == "unix" {
		return true
	}
	host, _, err := net.SplitHostPort(l.Address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Split(host, "%")[0])
	return ip != nil && ip.IsLoopback()
}

func (l Listen) String() string {
	if l.Network == "unix" {
		return "unix:" + l.Address
	}
	return l.Address
}

// Open opens the listener; a stale Unix socket file is removed first.
func (l Listen) Open(ctx context.Context) (net.Listener, error) {
	if l.Network == "unix" {
		if fi, err := os.Lstat(l.Address); err == nil && fi.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(l.Address)
		}
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, l.Network, l.Address)
	if err != nil {
		return nil, err
	}
	if l.Network == "unix" {
		_ = os.Chmod(l.Address, 0o660)
	}
	return ln, nil
}

// CheckListen enforces the authentication rule of the server: a TCP
// listener that is not loopback needs a token. It returns the error the
// daemon refuses to start with.
func CheckListen(l Listen, token string) error {
	if token == "" && !l.Loopback() {
		return fmt.Errorf("listen %s: a listener beyond loopback needs --token (or FABRIC_SERVER_TOKEN)", l)
	}
	return nil
}

// RequireToken wraps h: with a token, a request must carry it as
// "Authorization: Bearer <token>" or as the query parameter token, else it
// is answered 401. An empty token lets every request through (a loopback
// or Unix-socket listener).
func RequireToken(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
			got = strings.TrimPrefix(a, "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="fabric-server"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
