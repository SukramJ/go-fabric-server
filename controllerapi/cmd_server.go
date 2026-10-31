// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/thread"

	"github.com/SukramJ/go-fabric-server/protocol"
)

func cmdServerInfo(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	return s.serverInfo(), nil
}

// cmdDiagnostics is {info, nodes, events} (WebSocketControllerHandler.ts:1052-1058).
func cmdDiagnostics(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	info := s.serverInfo()
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]any{}, s.history...)
	return map[string]any{"info": info, "nodes": s.nodeListLocked(false), "events": events}, nil
}

// The log level names of get_loglevel/set_loglevel (model.ts:1276-1292).
var levelNames = []struct {
	name  string
	level slog.Level
}{
	{"critical", slog.LevelError + 4},
	{"error", slog.LevelError},
	{"warning", slog.LevelWarn},
	{"notice", slog.LevelInfo + 2},
	{"info", slog.LevelInfo},
	{"debug", slog.LevelDebug},
}

func levelName(l slog.Level) string {
	for _, n := range levelNames {
		if l >= n.level {
			return n.name
		}
	}
	return "debug"
}

func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(s) {
	case "fatal":
		s = "critical"
	case "warn":
		s = "warning"
	}
	for _, n := range levelNames {
		if strings.EqualFold(n.name, s) {
			return n.level, true
		}
	}
	return 0, false
}

func (s *Server) logLevels() map[string]any {
	l := slog.LevelInfo
	if s.opts.LogLevel != nil {
		l = s.opts.LogLevel.Level()
	}
	// No file logging: the daemon logs to stderr (journald, docker logs).
	return map[string]any{"console_loglevel": levelName(l), "file_loglevel": nil}
}

func cmdGetLogLevel(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	return s.logLevels(), nil
}

func cmdSetLogLevel(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Console *string `json:"console_loglevel"`
		File    *string `json:"file_loglevel"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Console != nil {
		l, ok := parseLevel(*a.Console)
		if !ok {
			return nil, invalidArgs(c.req.Command, "console_loglevel %q is not one of critical, error, warning, notice, info, debug", *a.Console)
		}
		if s.opts.LogLevel != nil {
			s.opts.LogLevel.Set(l)
		}
	}
	if a.File != nil {
		if _, ok := parseLevel(*a.File); !ok {
			return nil, invalidArgs(c.req.Command, "file_loglevel %q is not one of critical, error, warning, notice, info, debug", *a.File)
		}
	}
	return s.logLevels(), nil
}

// --- credentials ---

func cmdSetWifiCredentials(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		SSID        *string `json:"ssid"`
		Credentials *string `json:"credentials"`
		ID          *string `json:"id"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.SSID == nil || *a.SSID == "" {
		return nil, invalidArgs(c.req.Command, "ssid is required")
	}
	creds := ""
	if a.Credentials != nil {
		creds = *a.Credentials
	}
	if err := s.cfg.setWifi(credentialID(a.ID), *a.SSID, creds); err != nil {
		return nil, invalidArgs(c.req.Command, "%v", err)
	}
	s.broadcastServerInfo()
	return struct{}{}, nil
}

func cmdSetThreadDataset(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Dataset *string `json:"dataset"`
		ID      *string `json:"id"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Dataset == nil {
		return nil, invalidArgs(c.req.Command, "requires dataset to be a hex string")
	}
	if *a.Dataset == "" || !isHex(*a.Dataset) {
		return nil, invalidArgs(c.req.Command, "Invalid Thread operational dataset: must be a non-empty hex string with even length (each byte is two hex characters)")
	}
	// The dataset must also read as MeshCoP TLVs (go-fabric thread, the
	// port of matter.js OperationalDataset.ts): matterjs-server checks the
	// hex only (WebSocketControllerHandler.ts:2173-2200), a dataset no
	// device could join is refused here (report S3, by_design candidate).
	if _, err := thread.DecodeDatasetHex(*a.Dataset); err != nil {
		return nil, invalidArgs(c.req.Command, "Invalid Thread operational dataset: %v", err)
	}
	if err := s.cfg.setThread(credentialID(a.ID), *a.Dataset); err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: %v", c.req.Command, err)
	}
	s.broadcastServerInfo()
	return struct{}{}, nil
}

func cmdRemoveWifiCredentials(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		ID *string `json:"id"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if err := s.cfg.removeWifi(credentialID(a.ID)); err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: %v", c.req.Command, err)
	}
	s.broadcastServerInfo()
	return struct{}{}, nil
}

func cmdRemoveThreadDataset(_ context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		ID *string `json:"id"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if err := s.cfg.removeThread(credentialID(a.ID)); err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: %v", c.req.Command, err)
	}
	s.broadcastServerInfo()
	return struct{}{}, nil
}

// cmdGetAllCredentials returns the summaries, never a secret. The Thread
// entries carry only their id: networkName and extPanId need a reading of
// the operational dataset go-fabric's facade does not export (report S1,
// Left out).
func cmdGetAllCredentials(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	wifi, threads := s.cfg.summaries()
	return map[string]any{"wifi": wifi, "thread": threads}, nil
}

func credentialID(id *string) string {
	if id == nil || *id == "" {
		return defaultCredentialID
	}
	return *id
}

func isHex(s string) bool {
	if len(s)%2 != 0 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// --- fabric label ---

// cmdSetDefaultFabricLabel stores the label (null or empty resets it to
// "HomeAssistant"; the argument itself is required,
// docs/websockets_api.md:2483-2486). The first connection that sets it
// owns it until it disconnects; a pinned --default-fabric-label ignores
// every request. The label is stored for the next start, set on our
// fabric, and pushed to every connected node in the background
// (client.Registry.SetFabricLabel, go-fabric v0.12.1; a node offline gets
// it at its next connection), as matterjs-server pushes it
// (ControllerCommandHandler.ts:1562-1631 at 0bd06f22); a newer label
// cancels the push of an older one, and the answer does not wait for it.
// A label the store and the fabric already hold tells no node
// (matterjs-server #1227, :1565-1567): Home Assistant sends its label on
// every connect.
func cmdSetDefaultFabricLabel(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Label *string `json:"label"`
	}
	raw := map[string]any{}
	if err := c.args(&raw); err != nil {
		return nil, err
	}
	if _, ok := raw["label"]; !ok {
		return nil, invalidArgs(c.req.Command, "label is required (null resets it)")
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if s.opts.DefaultFabricLabel != "" {
		s.log.Info("fabricserver.label.pinned", slog.String("label", s.opts.DefaultFabricLabel))
		return nil, nil
	}
	if !s.claimLabel(c.conn) {
		s.log.Info("fabricserver.label.owned_elsewhere", slog.String("conn", c.conn.id))
		return nil, nil
	}
	label := ""
	if a.Label != nil {
		label = *a.Label
	}
	if err := s.applyLabel(ctx, label); err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "%s: %v", c.req.Command, err)
	}
	return nil, nil
}

// applyLabel is the shared half of set_default_fabric_label and
// [Server.SetFabricLabel]: a label the store and our fabric already hold
// changes nothing and tells no node; otherwise it is stored, set on the
// fabric at once (so a repeat during the push is seen as held), and pushed
// to the connected nodes in the background.
func (s *Server) applyLabel(ctx context.Context, label string) error {
	want := normalizeLabel(label)
	if want == s.cfg.label() && s.fabricHolds(want) {
		s.log.Debug("fabricserver.label.unchanged", slog.String("label", want))
		return nil
	}
	if err := s.cfg.setLabel(want); err != nil {
		return err
	}
	if b := s.b(); b != nil {
		if f := b.Fabric(); f != nil {
			if err := f.SetLabel(want); err != nil {
				return err
			}
		}
	}
	s.pushLabel(context.WithoutCancel(ctx), want)
	return nil
}

// fabricHolds reports whether our fabric carries label; without a backend
// or a fabric nothing holds it.
func (s *Server) fabricHolds(label string) bool {
	b := s.b()
	if b == nil {
		return false
	}
	f := b.Fabric()
	return f != nil && f.CurrentLabel() == label
}

// labelPushTimeout bounds one background label push.
const labelPushTimeout = 2 * time.Minute

// pushLabel sets our fabric's label and reconciles it to the connected
// nodes in the background, cancelling a push still running.
func (s *Server) pushLabel(parent context.Context, label string) {
	b := s.b()
	if b == nil {
		return
	}
	s.labelPushes.Add(1)
	ctx, cancel := context.WithTimeout(parent, labelPushTimeout)
	s.labelMu.Lock()
	if s.labelCancel != nil {
		s.labelCancel()
	}
	s.labelCancel = cancel
	s.labelMu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		defer cancel()
		if err := b.Registry().SetFabricLabel(ctx, label); err != nil {
			s.log.Warn("fabricserver.label.push", slog.String("label", label), slog.Any("error", err))
		}
	}()
}

func cmdGetFabricLabel(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	return map[string]any{"fabric_label": s.FabricLabel()}, nil
}

// FabricLabel is the effective fabric label: the pinned one, else the
// stored one.
func (s *Server) FabricLabel() string {
	if s.opts.DefaultFabricLabel != "" {
		return s.opts.DefaultFabricLabel
	}
	return s.cfg.label()
}

func (s *Server) claimLabel(c *wsConn) bool {
	s.labelMu.Lock()
	defer s.labelMu.Unlock()
	if s.labelOwner == nil || s.labelOwner.closed.Load() {
		s.labelOwner = c
	}
	return s.labelOwner == c
}

func (s *Server) releaseLabel(c *wsConn) {
	s.labelMu.Lock()
	defer s.labelMu.Unlock()
	if s.labelOwner == c {
		s.labelOwner = nil
	}
}
