// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"io"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/logring"

	"github.com/SukramJ/go-fabric-server/internal/version"
)

// exposureReason is fabricui.Options.ExposureReason: the daemon serves no
// selection, a /bridge client declares what a node exposes (issue #34,
// scope 4; docs/by_design.md B11).
const exposureReason = "the server keeps no host item model; selection is the client's"

// newLogger is the daemon's logger: go-fabric's log ring in front of the
// stderr text handler, both gated by lvl — the one level source that /ws
// get_loglevel/set_loglevel (controllerapi.Options.LogLevel) and the
// pages' Operation page (operations) read and move.
func newLogger(stderr io.Writer, lvl *slog.LevelVar) (*slog.Logger, *logring.Handler) {
	ring := logring.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: lvl}), logring.Options{Level: lvl})
	return slog.New(ring), ring
}

// operations is go-fabric-ui's Operations port over the daemon's log
// ring: the level, the ring's records and their stream, and what the
// daemon is.
type operations struct {
	ring    *logring.Handler
	started time.Time
}

var _ fabricui.Operations = operations{}

func (o operations) LogLevel() slog.Level { return o.ring.Level() }

func (o operations) SetLogLevel(l slog.Level) error {
	o.ring.SetLevel(l)
	return nil
}

func (o operations) Log(since uint64, limit int) []fabricui.LogRecord {
	recs := o.ring.Records(since, limit)
	out := make([]fabricui.LogRecord, len(recs))
	for i, r := range recs {
		out[i] = logRecord(r)
	}
	return out
}

func (o operations) ObserveLog(fn func(fabricui.LogRecord)) func() {
	if fn == nil {
		return func() {}
	}
	return o.ring.Observe(func(r logring.Record) { fn(logRecord(r)) })
}

// Info is server_info's versions: version.GoFabric's "unknown" (no build
// information) is the port's empty GoFabric.
func (o operations) Info() fabricui.OperationInfo {
	gf := version.GoFabric()
	if gf == "unknown" {
		gf = ""
	}
	return fabricui.OperationInfo{
		Version: version.Version, GoFabric: gf,
		StartedAt: o.started, Uptime: time.Since(o.started),
	}
}

// logRecord maps a ring record one to one: the attributes as text
// (slog.Value.String) under the ring's dotted group keys.
func logRecord(r logring.Record) fabricui.LogRecord {
	attrs := make(map[string]string, len(r.Attrs))
	for _, a := range r.Attrs {
		attrs[a.Key] = a.Value.String()
	}
	return fabricui.LogRecord{Seq: r.Seq, Time: r.Time, Level: r.Level, Message: r.Message, Attrs: attrs}
}
