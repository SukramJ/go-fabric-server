// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package protocol is the wire of go-fabric-server: the envelope of every
// frame, the error codes, the schema numbers, the JSON codec and the
// per-connection send path with its backpressure model. It knows no
// Matter: the controller half (controllerapi) and the bridge half
// (bridgeapi, S2) fill the envelope.
//
// The envelope is matterjs-server's (the python-matter-server contract,
// read map §1.1-1.6 at matterjs-server 0bd06f22): a request is
// {message_id, command, args}, a success {message_id, result}, a failure
// {message_id, error_code, details, status?, cluster_status?, reason?}, an
// event {event, data}. The server_info frame a connection receives first
// is the bare object, without an envelope.
//
// Numbers: Go encodes uint64 as plain decimal digits, so 64-bit ids need
// no special serializer (matterjs-server's toBigIntAwareJson,
// packages/ws-client/src/json-utils.ts:21-52, does the same by hand); the
// decoder keeps every number as a json.Number and [Uint64] parses an id
// from its literal, so an id above 2^53 never passes through a float.
package protocol
