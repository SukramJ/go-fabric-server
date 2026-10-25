// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// hooks are the host's hooks on one declared cluster of one device: the
// write veto when the host opted into writes, the command handlers it
// opted into. Each turns the kit's inline call into an event to the
// bridge client and waits for its result until the deadline.
type hooks struct {
	s        *Server
	device   string
	cluster  string
	writes   bool
	commands []uint32
}

// Errors a hook answers the controller with. go-fabric's facade has no
// error type that carries an IM status (report S2, Questions Q15): every
// one of them reaches the controller as FAILURE.
var (
	// errNoHost: no bridge client is listening.
	errNoHost = errors.New("bridgeapi: no bridge client is listening")
	// errTimeout: the client did not answer before the deadline.
	errTimeout = errors.New("bridgeapi: the bridge client did not answer before the deadline")
)

// hostError is the client's refusal: the status it named, if any.
type hostError struct {
	status        *int
	clusterStatus *int
	gone          bool
}

func (e *hostError) Error() string {
	switch {
	case e.gone:
		return "bridgeapi: the bridge client went away before it answered"
	case e.status == nil:
		return "bridgeapi: the host refused"
	case e.clusterStatus != nil:
		return fmt.Sprintf("bridgeapi: the host answered status 0x%02X, cluster status 0x%02X", *e.status, *e.clusterStatus)
	default:
		return fmt.Sprintf("bridgeapi: the host answered status 0x%02X", *e.status)
	}
}

// outcome is a client's result for one pending event.
type outcome struct {
	err      *hostError
	response json.RawMessage
}

// pending is one write or command event waiting for its result.
type pending struct {
	correlation uint64
	device      string
	cluster     string
	what        string // "write" or "command"
	element     string // the attribute or command name
	// decode checks a command's response; nil for a write.
	decode func(json.RawMessage) error
	done   chan outcome
}

// write is the veto of a controller's write: the write event, and the
// client's result decides.
func (h *hooks) write(ctx context.Context, attrID uint32, value any) error {
	cl, err := kitByName(h.cluster)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("0x%04X", attrID)
	if a := cl.cluster().Attribute(attrID); a != nil {
		name = a.Property
	}
	p := &pending{device: h.device, cluster: h.cluster, what: "write", element: name, done: make(chan outcome, 1)}
	data := map[string]any{"device": h.device, "cluster": h.cluster, "attribute": name, "value": value}
	_, err = h.s.await(ctx, p, "write", data)
	return err
}

// command is a command handler: the command event, and the client's
// result is the answer; decode takes the response.
func (h *hooks) command(ctx context.Context, cmdID uint32, fields any, decode func(json.RawMessage) error) error {
	cl, err := kitByName(h.cluster)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("0x%02X", cmdID)
	if c := cl.cluster().Command(cmdID); c != nil {
		name = c.Name
	}
	p := &pending{device: h.device, cluster: h.cluster, what: "command", element: name, decode: decode, done: make(chan outcome, 1)}
	data := map[string]any{"device": h.device, "cluster": h.cluster, "command": name, "fields": fields}
	o, err := h.s.await(ctx, p, "command", data)
	if err != nil {
		return err
	}
	// The response was checked when the result arrived.
	return decode(o.response)
}

// await registers p, sends the event to the listening client and waits
// for its result, the deadline or the end of ctx.
func (s *Server) await(ctx context.Context, p *pending, event string, data map[string]any) (outcome, error) {
	timeout := s.opts.ResultTimeout
	deadline := time.Now().Add(timeout)
	s.mu.Lock()
	c := s.conn
	if c == nil || !c.listening || s.closing {
		s.mu.Unlock()
		s.log.Info("fabricserver.bridge.no_host", slog.String("event", event), slog.String("device", p.device), slog.String("cluster", p.cluster), slog.String("element", p.element))
		return outcome{}, errNoHost
	}
	p.correlation = s.seq.Add(1)
	s.pending[p.correlation] = p
	data["correlation"] = p.correlation
	data["deadline"] = deadline.UTC().Format(time.RFC3339Nano)
	data["timeout_ms"] = timeout.Milliseconds()
	s.emitLocked(event, data)
	s.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case o := <-p.done:
		if o.err != nil {
			return o, o.err
		}
		return o, nil
	case <-timer.C:
	case <-ctx.Done():
	}
	s.mu.Lock()
	_, still := s.pending[p.correlation]
	delete(s.pending, p.correlation)
	if still {
		s.emitLocked("result_timeout", map[string]any{
			"correlation": p.correlation, "device": p.device, "cluster": p.cluster, p.attributeOrCommand(): p.element,
		})
	}
	s.mu.Unlock()
	if !still {
		// The result arrived together with the deadline.
		o := <-p.done
		if o.err != nil {
			return o, o.err
		}
		return o, nil
	}
	s.log.Info("fabricserver.bridge.result_timeout", slog.Uint64("correlation", p.correlation), slog.String("device", p.device), slog.String("cluster", p.cluster), slog.String("element", p.element))
	return outcome{}, errTimeout
}

func (p *pending) attributeOrCommand() string {
	if p.what == "write" {
		return "attribute"
	}
	return "command"
}

// failPendingLocked answers every pending event of a client that went
// away: no result will come.
func (s *Server) failPendingLocked() {
	for id, p := range s.pending {
		delete(s.pending, id)
		p.done <- outcome{err: &hostError{gone: true}}
	}
}
