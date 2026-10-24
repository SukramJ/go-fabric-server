// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package protocol

import (
	"encoding/json"
	"fmt"
)

// Schema numbers of the controller half (/ws): the matterjs-server
// contract, packages/ws-controller/src/server/WebSocketControllerHandler.ts:114
// (SCHEMA_VERSION = 14) and :119 (MIN_SUPPORTED_SCHEMA_VERSION = 11).
const (
	SchemaVersion             = 14
	MinSupportedSchemaVersion = 11
)

// ErrorCode is the error_code of a failure — matterjs-server's
// ServerErrorCode (packages/ws-controller/src/types/WebSocketMessageTypes.ts:73-110),
// which keeps python-matter-server's numbers 0-11 and adds its own from
// 100.
type ErrorCode int

// The error codes, with matterjs-server's names.
const (
	UnknownError             ErrorCode = 0
	NodeCommissionFailed     ErrorCode = 1
	NodeInterviewFailed      ErrorCode = 2
	NodeNotReady             ErrorCode = 3
	NodeNotResolving         ErrorCode = 4
	NodeNotExists            ErrorCode = 5
	VersionMismatch          ErrorCode = 6
	SDKStackError            ErrorCode = 7
	InvalidArguments         ErrorCode = 8
	InvalidCommand           ErrorCode = 9
	UpdateCheckError         ErrorCode = 10
	UpdateError              ErrorCode = 11
	IcdMultiAdmin            ErrorCode = 100
	OtaUploadError           ErrorCode = 101
	CameraStreamIncompatible ErrorCode = 102
	CameraResourceExhausted  ErrorCode = 103
)

// Commissioning failure reasons — the reason field of a
// NodeCommissionFailed error (docs/websockets_api.md:82-90 of
// matterjs-server; CommissionFailureReason, ws-client model.ts:1757).
const (
	ReasonNotDiscovered       = "not-discovered"
	ReasonPasscodeMismatch    = "passcode-mismatch" //nolint:gosec // a reason name, not a credential
	ReasonInvalidPayload      = "invalid-payload"
	ReasonAttestationRejected = "attestation-rejected"
	ReasonTimeout             = "timeout"
	ReasonOther               = "other"
)

// Request is a client's command. Args is never nil after [DecodeRequest]:
// an absent or null args is the empty object (docs/websockets_api.md:50-54).
type Request struct {
	MessageID string          `json:"message_id"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
}

// Result is a success response. A nil Result encodes as "result": null,
// the answer of a command without a result (interview_node, remove_node).
type Result struct {
	MessageID string `json:"message_id"`
	Result    any    `json:"result"`
}

// Error is a failure response; it is also the error a handler returns.
// Status and ClusterStatus carry a Matter status the device answered with,
// Reason why commissioning failed (docs/websockets_api.md:73-90).
type Error struct {
	MessageID     string    `json:"message_id"`
	ErrorCode     ErrorCode `json:"error_code"`
	Details       string    `json:"details"`
	Status        *int      `json:"status,omitempty"`
	ClusterStatus *int      `json:"cluster_status,omitempty"`
	Reason        *string   `json:"reason,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("error %d: %s", e.ErrorCode, e.Details)
}

// Errorf returns an *Error of code with a formatted Details.
func Errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{ErrorCode: code, Details: fmt.Sprintf(format, args...)}
}

// WithReason returns e with Reason set.
func (e *Error) WithReason(reason string) *Error {
	e.Reason = &reason
	return e
}

// WithStatus returns e with the Matter status (and the cluster status,
// when not nil) set.
func (e *Error) WithStatus(status int, cluster *int) *Error {
	e.Status, e.ClusterStatus = &status, cluster
	return e
}

// Event is a server push: {event, data} (docs/websockets_api.md:2002-2012).
type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}
