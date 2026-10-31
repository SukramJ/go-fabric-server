// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// The server's policy is matterjs-server's: a test certificate the trust
// store does not accept is refused with the hint that names the option,
// as attestation-rejected; the site device's test Certification
// Declaration alone (every other test's commissioning) is accepted.
func TestAttestationRefusesUntrustedTestCertificate(t *testing.T) {
	production := attestation.TestStore()
	production.AcceptTest = false
	e := newEnv(t, envOptions{configure: func(c *controller.Config) { c.Attestation = production }})
	qr, _, err := e.dev.Node().PairingCodes()
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.client.CommissionWithCode(e.ctx, qr, true)
	pe := wantCode(t, err, protocol.NodeCommissionFailed, "test/development certificate")
	if pe.Reason == nil || *pe.Reason != protocol.ReasonAttestationRejected {
		t.Fatalf("reason %v (%s)", pe.Reason, pe.Details)
	}
	if !strings.Contains(pe.Details, "enable_test_net_dcl") {
		t.Fatalf("the refusal names the option: %s", pe.Details)
	}
}
