// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/ble/esphome"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// TestCommissionErrorProxy: a pairing that fails at an ESPHome proxy
// surfaces as NodeCommissionFailed with the module's text as it is (brief
// S5 §1), reason other.
func TestCommissionErrorProxy(t *testing.T) {
	for _, cause := range []error{esphome.ErrProxyBusy, esphome.ErrNoActiveConnections, esphome.ErrInvalidKey} {
		e := commissionError(fmt.Errorf("controller: ble: %w", cause))
		if e.ErrorCode != protocol.NodeCommissionFailed || !strings.Contains(e.Details, cause.Error()) || e.Reason == nil || *e.Reason != protocol.ReasonOther {
			t.Fatalf("%v: %+v", cause, e)
		}
	}
}
