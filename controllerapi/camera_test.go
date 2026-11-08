// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"fmt"
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// cameraEndpoint is the camera's endpoint number on the site device.
func (e *env) cameraEndpoint() uint16 {
	e.t.Helper()
	ep, ok := e.camera.EndpointNumber()
	if !ok {
		e.t.Fatal("the camera has no endpoint")
	}
	return ep
}

// TestCameraOnTheSite: the env's camera is commissioned with the light,
// its AV Stream Management cluster in the node data.
func TestCameraOnTheSite(t *testing.T) {
	t.Parallel()
	e := newEnv(t, envOptions{camera: true})
	n := e.commission()
	prefix := fmt.Sprintf("%d/%d/", e.cameraEndpoint(), csdef.ClusterID)
	for k := range n.Attributes {
		if strings.HasPrefix(k, prefix) {
			return
		}
	}
	t.Fatalf("no attribute under %s", prefix)
}
