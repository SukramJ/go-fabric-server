// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgeapi

import (
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/node"
)

// SetBatchAdd replaces node.Batch.Add for one test; the returned function
// restores it.
func SetBatchAdd(f func(bt *node.Batch, ep node.Endpoint) error) (restore func()) {
	prev := batchAdd
	batchAdd = f
	return func() { batchAdd = prev }
}

// SetBatchReplace replaces node.Batch.Replace for one test; the returned
// function restores it.
func SetBatchReplace(f func(bt *node.Batch, key device.SourceKey, ep node.Endpoint) error) (restore func()) {
	prev := batchReplace
	batchReplace = f
	return func() { batchReplace = prev }
}
