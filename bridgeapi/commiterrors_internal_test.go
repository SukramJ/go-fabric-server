// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"errors"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/surface"
)

// TestCommitErrors: a Batch's error as go-fabric v0.28.0 returns it
// (node/batch_error.go, node.BatchError) — a declaration recorded withheld
// at the commit, a Forget refused at the commit (ErrUnknownKey), a
// duplicate key, a call's store work, a store error of no call and the
// reassembly's — each sorted into its class, also when wrapped; an error
// of no call (ErrStopped) is other.
func TestCommitErrors(t *testing.T) {
	w := &surface.WithheldError{Endpoint: "w"}
	dup := fmt.Errorf("%w: d", node.ErrDuplicateKey)
	forget := fmt.Errorf("%w: f", node.ErrUnknownKey)
	attach := errors.New("the attach failed")
	store := errors.New("the store refused")
	reassembly := errors.New("the reassembly failed")
	be := &node.BatchError{
		Calls: []*node.BatchCallError{
			{Op: node.BatchReplace, Key: device.StringKey("w"), Err: w},
			{Op: node.BatchForget, Key: device.StringKey("f"), Err: forget},
			{Op: node.BatchAdd, Key: device.StringKey("d"), Err: dup},
			{Op: node.BatchAdd, Key: device.StringKey("a"), Applied: true, Err: attach},
		},
		Store: store, Reassembly: reassembly,
	}
	withheld, refusals, other := commitErrors(fmt.Errorf("declare: %w", be))
	if len(withheld) != 1 || withheld["w"] != w {
		t.Errorf("withheld %v", withheld)
	}
	if len(refusals) != 2 || !errors.Is(refusals[0], node.ErrUnknownKey) || !errors.Is(refusals[1], node.ErrDuplicateKey) {
		t.Errorf("refusals %v", refusals)
	}
	if len(other) != 3 || !errors.Is(other[0], attach) || !errors.Is(other[1], store) || !errors.Is(other[2], reassembly) {
		t.Errorf("other %v", other)
	}
	// The text a device's answer carries is the call's, as before.
	if got := refusals[1].Error(); got != "node: batch Add d: "+dup.Error() {
		t.Errorf("refusal text %q", got)
	}
	if w, r, o := commitErrors(nil); len(w) != 0 || r != nil || o != nil {
		t.Errorf("nil: %v %v %v", w, r, o)
	}
	if w, r, o := commitErrors(node.ErrStopped); len(w) != 0 || r != nil || len(o) != 1 || !errors.Is(o[0], node.ErrStopped) {
		t.Errorf("ErrStopped: %v %v %v", w, r, o)
	}
}
