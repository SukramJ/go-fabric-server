// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

// LabelPushes is the number of fabric-label pushes started so far — a test
// seam for set_default_fabric_label's "unchanged label tells no node".
func (s *Server) LabelPushes() int64 { return s.labelPushes.Load() }

// Primed reports whether the node's cache entry is primed (it was
// Connected since the server started): from then on a change is sent as
// attribute_updated.
func (s *Server) Primed(nodeID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stateByNodeID(nodeID)
	return st != nil && st.primed
}
