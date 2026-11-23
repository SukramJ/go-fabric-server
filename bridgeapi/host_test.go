// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	fsclient "github.com/SukramJ/go-fabric-server/client"
	"github.com/SukramJ/go-fabric-server/protocol"
)

// errLabelStore is the refusal of refusingLabels.
var errLabelStore = errors.New("the label store is full")

// refusingLabels is a node.EndpointState (the seam of node.Config) whose
// store refuses every bridged NodeLabel and holds nothing else.
type refusingLabels struct{}

func (refusingLabels) SaveNodeLabel(context.Context, device.SourceKey, string) error {
	return errLabelStore
}

func (refusingLabels) LoadNodeLabel(context.Context, device.SourceKey) (string, error) {
	return "", nil
}

func (refusingLabels) LoadScenes(context.Context, device.SourceKey) ([]byte, error) {
	return nil, nil
}

func (refusingLabels) SaveScenes(context.Context, device.SourceKey, []byte) error { return nil }

// TestHostLabelRefused: a node whose store refuses the label answers host
// with UnknownError carrying go-fabric's refusal; nothing of the statement
// is applied (the label goes first), so node_info keeps the device's name
// and its reachability.
func TestHostLabelRefused(t *testing.T) {
	e := newEnvWith(t, bridgeapi.Options{}, func(cfg *node.Config) { cfg.EndpointState = refusingLabels{} })
	if _, err := e.host.Declare(e.ctx, doc(`{"devices":[{"key":"a","type":"OnOffLight","name":"Lamp"}]}`)); err != nil {
		t.Fatal(err)
	}
	label, off := "Kitchen", false
	err := e.host.Host(e.ctx, fsclient.HostStatement{Device: "a", Label: &label, Reachable: &off})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.ErrorCode != protocol.UnknownError ||
		!strings.HasPrefix(perr.Details, "host: ") || !strings.Contains(perr.Details, errLabelStore.Error()) {
		t.Fatalf("host: %v", err)
	}
	ni, err := e.host.NodeInfo(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	devs := ni["devices"].([]any)
	if len(devs) != 1 {
		t.Fatalf("node_info devices %v", devs)
	}
	if d := devs[0].(map[string]any); d["name"] != "Lamp" || d["reachable"] != true {
		t.Errorf("node_info device %v, want name Lamp, reachable", d)
	}
}
