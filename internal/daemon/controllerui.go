// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric-ui/fabricui/gofabric"
	"github.com/SukramJ/go-fabric/controller"

	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// controllerSource is the controller pages' source (go-fabric-ui v0.2.1,
// docs/embedding.md "The controller family"): go-fabric-ui's adapter over
// the daemon's controller, with the controller half's credential store,
// pairing template and fabric label, so the pages and /ws see one list,
// one policy and one label. hooks are the adapter's, wired into
// controller.Config.HooksFor before controller.New.
func controllerSource(ctrl *controller.Controller, api *controllerapi.Server, hooks *gofabric.Hooks, log *slog.Logger) fabricui.ControllerSource {
	src := gofabric.ControllerOf(ctrl, gofabric.ControllerOptions{
		Hooks:       hooks,
		Credentials: credentialStore{api},
		Pair:        api.PairOptions(),
		Logger:      log,
	})
	return labelled{ControllerSource: src, api: api}
}

// labelled is the adapter with the controller half's fabric label: a
// label set on the pages is stored in server.json as set_default_fabric_label
// stores it, so the next start does not push the old one back, and a
// pinned --default-fabric-label refuses it (409).
type labelled struct {
	fabricui.ControllerSource
	api *controllerapi.Server
}

func (l labelled) SetFabricLabel(ctx context.Context, label string) error {
	if err := l.api.SetFabricLabel(ctx, label); err != nil {
		if errors.Is(err, controllerapi.ErrLabelPinned) {
			return fmt.Errorf("%w: --default-fabric-label pins the label", fabricui.ErrConflict)
		}
		return err
	}
	return nil
}

// credentialStore is the controller half's settings store (server.json:
// set_wifi_credentials, set_thread_dataset, get_all_credentials) as
// go-fabric-ui's credential store: one list for the pages and /ws.
type credentialStore struct{ api *controllerapi.Server }

var _ gofabric.Credentials = credentialStore{}

func (c credentialStore) List(context.Context) ([]fabricui.Credential, error) {
	cs := c.api.Credentials()
	out := make([]fabricui.Credential, 0, len(cs))
	for _, e := range cs {
		out = append(out, fabricui.Credential{Kind: fabricui.CredentialKind(e.Kind), ID: e.ID, Name: e.Name})
	}
	return out, nil
}

func (c credentialStore) SetWiFi(_ context.Context, id, ssid, passphrase string) error {
	return credentialError(c.api.SetWiFiCredential(id, ssid, passphrase))
}

func (c credentialStore) SetThread(_ context.Context, id string, dataset []byte) error {
	return credentialError(c.api.SetThreadDataset(id, dataset))
}

func (c credentialStore) Remove(_ context.Context, kind fabricui.CredentialKind, id string) error {
	return credentialError(c.api.RemoveCredential(string(kind), id))
}

func (c credentialStore) WiFi(id string) (ssid, passphrase string, ok bool) {
	return c.api.WiFiCredential(id)
}

func (c credentialStore) Thread(id string) ([]byte, bool) { return c.api.ThreadDataset(id) }

// credentialError maps the store's errors to the port's kinds.
func credentialError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, controllerapi.ErrInvalidCredential):
		return fmt.Errorf("%w: %w", fabricui.ErrInvalidCredential, err)
	case errors.Is(err, controllerapi.ErrUnknownCredential):
		return fmt.Errorf("%w: %w", fabricui.ErrUnknownCredential, err)
	}
	return err
}
