// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"encoding/json"
	"testing"
)

// TestAvailableAtCommissioning: go-fabric starts a commissioned node before
// the commissioning returns (controller/client/fabricops.go:277 at
// v0.28.0), so it is Reconnecting or Connected — available in the answer,
// as matterjs-server answers (WebSocketControllerHandler.ts:1348-1357,
// ControllerCommandHandler.ts:2106-2120 at 0bd06f22) (#81). node_added goes
// out with the node's first cache entry, measured here while the node is
// not yet started (Disconnected, available false); a node_updated with
// available true follows.
func TestAvailableAtCommissioning(t *testing.T) {
	e := newEnv(t, envOptions{})
	payload, err := e.dev.Node().SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.client.CommissionWithCode(e.ctx, payload.QR, true)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Available {
		t.Fatalf("commissioning answer: available false")
	}
	raw := e.waitEvent(e.client, "node_added", nil)
	var added struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("node_added %s: %v", raw, err)
	}
	if !added.Available {
		e.waitEvent(e.client, "node_updated", func(raw json.RawMessage) bool {
			var u struct {
				Available bool `json:"available"`
			}
			return json.Unmarshal(raw, &u) == nil && u.Available
		})
	}
	e.waitPrimed(n.NodeID)
}
