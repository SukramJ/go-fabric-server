// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

// LabelPushes is the number of fabric-label pushes started so far — a test
// seam for set_default_fabric_label's "unchanged label tells no node".
func (s *Server) LabelPushes() int64 { return s.labelPushes.Load() }
