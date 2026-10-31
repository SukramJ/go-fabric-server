// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/thread"
)

// The host side of the server: what a second surface over the same
// controller — go-fabric-ui's controller pages in the daemon — needs so
// that it and /ws see one credential list, one fabric label and one pairing
// template. Each mutation has the effect of its WebSocket command,
// server_info broadcast included.

// Credential kinds, as [Credential.Kind] names them.
const (
	CredentialWiFi   = "wifi"
	CredentialThread = "thread"
)

var (
	// ErrInvalidCredential is a credential the store refuses: a blank
	// SSID, a blank password for a new entry or a changed SSID, a dataset
	// that is not MeshCoP TLVs.
	ErrInvalidCredential = errors.New("controllerapi: invalid credential")
	// ErrUnknownCredential is a kind and id the store does not hold.
	ErrUnknownCredential = errors.New("controllerapi: no such credential")
	// ErrLabelPinned is a label change while --default-fabric-label pins
	// the label.
	ErrLabelPinned = errors.New("controllerapi: the fabric label is pinned")
)

// Credential is a stored credential's name, never its secret: Name is the
// Wi-Fi SSID or the Thread dataset's network name ("" when it names none).
type Credential struct {
	Kind, ID, Name string
}

// Credentials lists the stored credentials, Wi-Fi first, each kind by id —
// the entries get_all_credentials lists, without its placeholder default
// entries (an id-less set_wifi_credentials or set_thread_dataset stores
// the id "default", which is listed once stored).
func (s *Server) Credentials() []Credential {
	s.cfg.mu.Lock()
	ws := append([]wifiCredential(nil), s.cfg.data.Wifi...)
	ts := append([]threadCredential(nil), s.cfg.data.Thread...)
	s.cfg.mu.Unlock()
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].ID < ws[j].ID })
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	out := make([]Credential, 0, len(ws)+len(ts))
	for _, e := range ws {
		out = append(out, Credential{Kind: CredentialWiFi, ID: e.ID, Name: e.SSID})
	}
	for _, e := range ts {
		name, _ := threadSummary(e)["networkName"].(string)
		out = append(out, Credential{Kind: CredentialThread, ID: e.ID, Name: name})
	}
	return out
}

// SetWiFiCredential is set_wifi_credentials: an empty id is the default
// entry, a blank passphrase keeps the stored one of an unchanged SSID.
func (s *Server) SetWiFiCredential(id, ssid, passphrase string) error {
	if ssid == "" {
		return fmt.Errorf("%w: ssid is required", ErrInvalidCredential)
	}
	if err := s.cfg.setWifi(credentialID(&id), ssid, passphrase); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCredential, err)
	}
	s.broadcastServerInfo()
	return nil
}

// SetThreadDataset is set_thread_dataset with the dataset's bytes: it must
// read as MeshCoP TLVs (thread.DecodeDataset); it is stored as
// the hex string the command stores.
func (s *Server) SetThreadDataset(id string, dataset []byte) error {
	if len(dataset) == 0 {
		return fmt.Errorf("%w: the dataset is empty", ErrInvalidCredential)
	}
	if _, err := thread.DecodeDataset(dataset); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCredential, err)
	}
	if err := s.cfg.setThread(credentialID(&id), hex.EncodeToString(dataset)); err != nil {
		return err
	}
	s.broadcastServerInfo()
	return nil
}

// RemoveCredential is remove_wifi_credentials or remove_thread_dataset; a
// kind and id the store does not hold is [ErrUnknownCredential], where the
// commands succeed without effect.
func (s *Server) RemoveCredential(kind, id string) error {
	id = credentialID(&id)
	var err error
	switch kind {
	case CredentialWiFi:
		if _, _, ok := s.WiFiCredential(id); !ok {
			return fmt.Errorf("%w: wifi %q", ErrUnknownCredential, id)
		}
		err = s.cfg.removeWifi(id)
	case CredentialThread:
		if _, ok := s.ThreadDataset(id); !ok {
			return fmt.Errorf("%w: thread %q", ErrUnknownCredential, id)
		}
		err = s.cfg.removeThread(id)
	default:
		return fmt.Errorf("%w: kind %q", ErrUnknownCredential, kind)
	}
	if err != nil {
		return err
	}
	s.broadcastServerInfo()
	return nil
}

// WiFiCredential is the stored Wi-Fi entry of id ("" the default one), as
// a commissioning that names it provisions it.
func (s *Server) WiFiCredential(id string) (ssid, passphrase string, ok bool) {
	w, _, missing := s.cfg.credentials(&id, nil)
	if w == nil || missing != "" {
		return "", "", false
	}
	return w.SSID, w.Credentials, true
}

// ThreadDataset is the stored Thread dataset of id ("" the default one).
func (s *Server) ThreadDataset(id string) (dataset []byte, ok bool) {
	_, t, missing := s.cfg.credentials(nil, &id)
	if t == nil || missing != "" {
		return nil, false
	}
	b, err := hex.DecodeString(t.Dataset)
	if err != nil {
		return nil, false
	}
	return b, true
}

// SetFabricLabel is set_default_fabric_label for a host surface: the label
// is normalized as the command's (trimmed, empty is "HomeAssistant", at
// most 32 characters), stored for the next start, set on the fabric at
// once and pushed to the nodes in the background. It does not claim the
// label for a connection; a pinned --default-fabric-label refuses it with
// [ErrLabelPinned].
func (s *Server) SetFabricLabel(ctx context.Context, label string) error {
	if s.opts.DefaultFabricLabel != "" {
		return ErrLabelPinned
	}
	return s.applyLabel(ctx, label)
}

// PairOptions is the template of a commissioning outside /ws: the
// server's attestation policy and subscription mode, and its BLE central.
// A request's network is set on a copy.
func (s *Server) PairOptions() controller.PairOptions {
	o := s.pairOptions()
	o.BLE = s.opts.BLE
	return o
}
