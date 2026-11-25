// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"errors"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/surface"
)

// TestCommitErrors: the leaves of a Batch error tree as go-fabric v0.26.0
// joins them (node/aggregator.go Partition.Batch, commitPending) — a
// declaration recorded withheld at the commit, a Forget refused at the
// commit (ErrUnknownKey), a duplicate key, the store's error — each sorted
// into its class, through nested joins.
func TestCommitErrors(t *testing.T) {
	w := &surface.WithheldError{Endpoint: "w"}
	dup := fmt.Errorf("%w: d", node.ErrDuplicateKey)
	forget := fmt.Errorf("%w: f", node.ErrUnknownKey)
	store := errors.New("the store refused")
	tree := errors.Join(errors.Join(w, forget), dup, nil, fmt.Errorf("node: endpoint number reservation: %w", store))
	withheld, refusals, other := commitErrors(tree)
	if len(withheld) != 1 || withheld["w"] != w {
		t.Errorf("withheld %v", withheld)
	}
	if len(refusals) != 2 || !errors.Is(refusals[0], node.ErrUnknownKey) || !errors.Is(refusals[1], node.ErrDuplicateKey) {
		t.Errorf("refusals %v", refusals)
	}
	if len(other) != 1 || !errors.Is(other[0], store) {
		t.Errorf("other %v", other)
	}
	if w, r, o := commitErrors(nil); len(w) != 0 || r != nil || o != nil {
		t.Errorf("nil: %v %v %v", w, r, o)
	}
}
