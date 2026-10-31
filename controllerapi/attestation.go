// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"log/slog"
	"strings"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// testCertificateHint is matterjs-server's refusal of a test certificate
// (ControllerCommandHandler.ts:306-308 at 0bd06f22), naming this server's
// option.
const testCertificateHint = "This device uses a test/development certificate. To commission it, enable the test-net DCL option of the server (--enable-test-net-dcl, the add-on's enable_test_net_dcl) — only do this if you trust the vendor."

// attestationPolicy is matterjs-server's onAttestationFailure
// (ControllerCommandHandler.ts:2041-2052 at 0bd06f22): every finding is
// logged with its level, then attestationRefusalOf decides. A refusal
// rejects the commissioning with that text (the reason of
// attestation.Policy.Decide), which reaches the client as the error's
// details with reason attestation-rejected; none proceeds.
func attestationPolicy(log *slog.Logger) attestation.Policy {
	return attestation.Policy{Decide: func(findings []attestation.Finding) (bool, string, error) {
		for _, f := range findings {
			log.Info("fabricserver.attestation.finding", slog.String("level", string(f.Level)), slog.String("check", string(f.Check)), slog.String("message", f.Message))
		}
		if refusal := attestationRefusalOf(findings); refusal != "" {
			log.Warn("fabricserver.attestation.refused", slog.String("refusal", refusal))
			return false, refusal, nil
		}
		log.Info("fabricserver.attestation.accepted", slog.Int("findings", len(findings)))
		return true, "", nil
	}}
}

// attestationRefusalOf is matterjs-server's attestationRefusalOf
// (ControllerCommandHandler.ts:305-314 at 0bd06f22): why device
// attestation refuses a device, or "" when no finding stops it. A test
// certificate the trust store does not accept gets the actionable hint;
// any other error-level finding is named with its check; findings of the
// info and warning levels (a test Certification Declaration, a
// provisional one) do not stop the commissioning.
func attestationRefusalOf(findings []attestation.Finding) string {
	var errs []string
	for _, f := range findings {
		if f.Check == attestation.TrustedAsTestCertificate {
			return testCertificateHint
		}
		if f.Level == attestation.LevelError {
			errs = append(errs, string(f.Check)+": "+f.Message)
		}
	}
	if len(errs) == 0 {
		return ""
	}
	return "Device attestation refused: " + strings.Join(errs, "; ")
}
