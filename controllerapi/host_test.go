// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// TestHostCredentials: the host side of the credential store — the list
// by name, the set and remove rules, the secrets for a commissioning, and
// the store persisted for the next start.
func TestHostCredentials(t *testing.T) {
	dir := t.TempDir()
	s, err := controllerapi.New(controllerapi.Options{StoragePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Credentials(); len(got) != 0 {
		t.Fatalf("empty store lists %+v", got)
	}
	if err := s.SetWiFiCredential("home", "", "pass"); !errors.Is(err, controllerapi.ErrInvalidCredential) {
		t.Fatalf("blank ssid: %v", err)
	}
	if err := s.SetWiFiCredential("home", "HomeNet", ""); !errors.Is(err, controllerapi.ErrInvalidCredential) {
		t.Fatalf("new entry without a passphrase: %v", err)
	}
	if err := s.SetWiFiCredential("home", "HomeNet", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWiFiCredential("home", "HomeNet", ""); err != nil {
		t.Fatalf("blank passphrase for the unchanged SSID: %v", err)
	}
	if ssid, pass, ok := s.WiFiCredential("home"); !ok || ssid != "HomeNet" || pass != "secret" {
		t.Fatalf("wifi home: %q %q %v", ssid, pass, ok)
	}
	if _, _, ok := s.WiFiCredential("other"); ok {
		t.Fatal("an unknown wifi id is found")
	}
	if err := s.SetThreadDataset("mesh", nil); !errors.Is(err, controllerapi.ErrInvalidCredential) {
		t.Fatalf("empty dataset: %v", err)
	}
	if err := s.SetThreadDataset("mesh", []byte{0x0e}); !errors.Is(err, controllerapi.ErrInvalidCredential) {
		t.Fatalf("truncated dataset: %v", err)
	}
	ds, _ := hex.DecodeString("0e080000000000010000000300000f35060004001fffe0020811111111222222220708fd000000000000000510001122334455667788" +
		"99aabbccddeeff030e4f70656e54687265616444656d6f010212340410445f2b5ca6f2a93a55ce570a70efeecb0c0402a0f7f8")
	if err := s.SetThreadDataset("mesh", ds); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.ThreadDataset("mesh"); !ok || hex.EncodeToString(got) != hex.EncodeToString(ds) {
		t.Fatalf("thread mesh: %v", ok)
	}
	want := []controllerapi.Credential{{Kind: "wifi", ID: "home", Name: "HomeNet"}, {Kind: "thread", ID: "mesh", Name: "OpenThreadDemo"}}
	if got := s.Credentials(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("credentials %+v", got)
	}

	// The store is server.json: a second server on the directory lists it.
	s2, err := controllerapi.New(controllerapi.Options{StoragePath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Credentials(); len(got) != 2 {
		t.Fatalf("after a restart %+v", got)
	}

	for _, c := range []struct{ kind, id string }{{"wifi", "home"}, {"thread", "mesh"}} {
		if err := s.RemoveCredential(c.kind, c.id); err != nil {
			t.Fatal(err)
		}
		if err := s.RemoveCredential(c.kind, c.id); !errors.Is(err, controllerapi.ErrUnknownCredential) {
			t.Fatalf("second remove of %s %s: %v", c.kind, c.id, err)
		}
	}
	if err := s.RemoveCredential("zigbee", "x"); !errors.Is(err, controllerapi.ErrUnknownCredential) {
		t.Fatalf("unknown kind: %v", err)
	}
	if got := s.Credentials(); len(got) != 0 {
		t.Fatalf("after remove %+v", got)
	}
}

// TestHostFabricLabel: the host's label is the command's — normalized and
// stored; refused while --default-fabric-label pins it.
func TestHostFabricLabel(t *testing.T) {
	s, err := controllerapi.New(controllerapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFabricLabel(context.Background(), "  Pages  "); err != nil || s.FabricLabel() != "Pages" {
		t.Fatalf("label %q, %v", s.FabricLabel(), err)
	}
	if err := s.SetFabricLabel(context.Background(), ""); err != nil || s.FabricLabel() != "HomeAssistant" {
		t.Fatalf("empty label %q, %v", s.FabricLabel(), err)
	}
	p, err := controllerapi.New(controllerapi.Options{DefaultFabricLabel: "Pinned"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetFabricLabel(context.Background(), "Pages"); !errors.Is(err, controllerapi.ErrLabelPinned) || p.FabricLabel() != "Pinned" {
		t.Fatalf("pinned: %q, %v", p.FabricLabel(), err)
	}
	q, err := controllerapi.New(controllerapi.Options{DisableAutoSubscription: true})
	if err != nil {
		t.Fatal(err)
	}
	if po := q.PairOptions(); po.AutoSubscribe == nil || *po.AutoSubscribe || po.BLE != nil {
		t.Fatalf("pair options %+v", po)
	}
}
