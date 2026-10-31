// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// attestationRefusalOf is matterjs-server's (ControllerCommandHandler.ts:305-314).
func TestAttestationRefusalOf(t *testing.T) {
	info := attestation.Finding{Level: attestation.LevelInfo, Check: attestation.CertificationTypeTest, Message: "Certification Declaration has certification_type=0 (test)"}
	warn := attestation.Finding{Level: attestation.LevelWarning, Check: attestation.CertificationTypeProvisional, Message: "provisional"}
	errA := attestation.Finding{Level: attestation.LevelError, Check: attestation.PaaNotTrusted, Message: "no PAA"}
	errB := attestation.Finding{Level: attestation.LevelError, Check: attestation.ValidationError, Message: "bad"}
	test := attestation.Finding{Level: attestation.LevelError, Check: attestation.TrustedAsTestCertificate, Message: "test root"}
	for _, c := range []struct {
		name     string
		findings []attestation.Finding
		want     string
	}{
		{"none", nil, ""},
		{"info and warning proceed", []attestation.Finding{info, warn}, ""},
		{"errors are named with their check", []attestation.Finding{info, errA, errB}, "Device attestation refused: PaaNotTrusted: no PAA; ValidationError: bad"},
		{"a test certificate gets the hint", []attestation.Finding{errA, test}, testCertificateHint},
	} {
		if got := attestationRefusalOf(c.findings); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
