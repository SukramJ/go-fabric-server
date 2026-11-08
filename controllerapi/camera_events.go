// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"github.com/SukramJ/go-fabric/controller/camera"
)

// CameraObserver is the observer the camera service reports to: pass it
// as camera.Config.Observer to camera.New, before Start binds the service
// (as Hooks goes into the controller's configuration before the
// controller exists).
func (s *Server) CameraObserver() camera.Observer {
	return camera.Observer{}
}
