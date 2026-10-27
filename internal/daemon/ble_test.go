// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ble"

	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// fakeCentral stands in for the BlueZ central: no BLE in CI.
type fakeCentral struct{ ble.Central }

// stubCentral replaces newCentral for one test and records the adapter
// names it was asked for.
func stubCentral(t *testing.T, c ble.Central, err error) *[]string {
	t.Helper()
	var asked []string
	prev := newCentral
	newCentral = func(name string) (ble.Central, error) {
		asked = append(asked, name)
		return c, err
	}
	t.Cleanup(func() { newCentral = prev })
	return &asked
}

// TestBluetoothAdapterWired: --bluetooth-adapter opens the BlueZ central
// on the adapter it names and the controller half's Options.BLE carries it.
func TestBluetoothAdapterWired(t *testing.T) {
	fake := &fakeCentral{}
	asked := stubCentral(t, fake, nil)

	if c, err := adapterCentral(""); c != nil || err != nil {
		t.Fatalf("no adapter: %v %v", c, err)
	}
	if len(*asked) != 0 {
		t.Fatalf("no adapter opened %v", *asked)
	}
	for _, adapter := range []string{"0", "hci1"} {
		c, err := adapterCentral(adapter)
		if err != nil || c != fake {
			t.Fatalf("%s: %v %v", adapter, c, err)
		}
	}
	if got := *asked; len(got) != 2 || got[0] != "hci0" || got[1] != "hci1" {
		t.Fatalf("adapters %v, want [hci0 hci1]", got)
	}

	c, err := adapterCentral("0")
	if err != nil {
		t.Fatal(err)
	}
	opts := controllerOptions(Config{StoragePath: t.TempDir()}, slog.New(slog.DiscardHandler), new(slog.LevelVar), c)
	if opts.BLE != fake {
		t.Fatalf("Options.BLE = %v, want the adapter's central", opts.BLE)
	}
	if _, err := controllerapi.New(opts); err != nil {
		t.Fatal(err)
	}

	stubCentral(t, nil, errors.New("bluez: no such adapter"))
	if _, err := adapterCentral("7"); err == nil || err.Error() != "--bluetooth-adapter 7: bluez: no such adapter" {
		t.Fatalf("refusal: %v", err)
	}
}
