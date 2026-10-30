// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/surface"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// ExtensionsFlag and SpecLevelFlag are the daemon flags that set the
// node's surface policies the refusals name: the enabled "Ahead of
// matter.js" register IDs (surface.Policies.Extensions) and the
// specification level (surface.Policies.Level).
const (
	ExtensionsFlag = "--bridge-extensions"
	SpecLevelFlag  = "--bridge-spec-level"
)

// aheadRegister is where an operator reads what a register ID enables.
const aheadRegister = "go-fabric notes/parity/by_design.md, Ahead-of-matter.js register"

// surfaceHint is the tail a device's refusal by the node's surface
// policies gains: for an ahead-of-matter.js selection the node does not
// enable (surface.ErrExtensionNotEnabled), the register IDs and the
// --bridge-extensions value that enables them besides the enabled ones;
// for a device the specification level withholds (surface.ErrWithheld),
// the level and the flag that sets it. Any other error gains nothing. The
// policies are the node's: a /bridge client cannot ask for an extension
// per declaration (node.ErrPolicyMismatch), so the hint addresses the
// operator.
func (s *Server) surfaceHint(err error) string {
	if ids := refusedAhead(err); len(ids) > 0 {
		value := slices.Concat(s.opts.Surface.Extensions.IDs(), ids)
		slices.Sort(value)
		value = slices.Compact(value)
		return fmt.Sprintf("; the node does not enable %s: enable them with %s %s (%s)",
			strings.Join(ids, ", "), ExtensionsFlag, strings.Join(value, ","), aheadRegister)
	}
	var w *surface.WithheldError
	if errors.As(err, &w) {
		return fmt.Sprintf("; the node composes at specification level %s (%s), which withholds endpoint %q",
			s.opts.Surface.EffectiveLevel(), SpecLevelFlag, w.Endpoint)
	}
	return ""
}

// refusedAhead returns the register IDs of the elements err's surface
// refusal names (surface.RefusedError's decisions, their
// Reference.Ahead), sorted, each once; nil when err is no refusal of an
// extension that is not enabled.
func refusedAhead(err error) []string {
	var r *surface.RefusedError
	if !errors.Is(err, surface.ErrExtensionNotEnabled) || !errors.As(err, &r) {
		return nil
	}
	var ids []string
	for _, d := range r.Decisions {
		if d.Verdict == surface.Refuse && d.Reference.Ahead != "" {
			ids = append(ids, d.Reference.Ahead)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// cmdSurface is the node's surface report (go-fabric node.Node.Surface):
// the policies it composes under, the effective surface, every decision
// and the withheld elements, encoded by surface.Report's own MarshalJSON.
func cmdSurface(_ context.Context, s *Server, _ *call) (any, *protocol.Error) {
	n := s.node.Load()
	if n == nil {
		return nil, protocol.Errorf(protocol.NodeNotReady, "surface: the bridge node is not started")
	}
	return n.Surface(), nil
}
