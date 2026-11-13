// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/ble/bluez"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/wifi/wpasupplicant"
)

// The first node's host ports (brief S13, go-fabric ADR 0031 and its
// docs/guides/first-boot-without-network.md): the BLE peripheral of
// --bridge-bluetooth-adapter (node.Network.BLE) and the Wi-Fi driver of
// --bridge-wifi-interface (node.Network.WiFi.Driver). Both belong to the
// configured node of the --bridge-* flags only — the registry's nodes are
// built without them (docs/by_design.md B24) — and live as long as the
// daemon: an erase hands the same ones to the node it brings back.

// newPeripheral opens the BlueZ peripheral of --bridge-bluetooth-adapter;
// a variable so a test can stand in for the system bus (no BLE in CI).
var newPeripheral = bluez.NewPeripheral

// wifiPort is a Wi-Fi driver the daemon closes at its stop
// (wpasupplicant.Driver).
type wifiPort interface {
	contract.WiFiDriver
	Close() error
}

// newWiFiDriver opens the wpa_supplicant driver of --bridge-wifi-interface;
// a variable so a test can stand in for wpa_supplicant.
var newWiFiDriver = func(ctx context.Context, iface string, log *slog.Logger) (wifiPort, error) {
	d, err := wpasupplicant.New(ctx, wpasupplicant.Options{Interface: iface, Logger: log})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// adapterName is a Bluetooth adapter flag's BlueZ name: matterjs-server's
// HCI id ("0" is hci0), or an adapter name passed as it is.
func adapterName(adapter string) string {
	if _, err := strconv.ParseUint(adapter, 10, 8); err == nil {
		return "hci" + adapter
	}
	return adapter
}

// publicTestPasscode is the setup passcode every Matter test device uses
// (connectedhomeip's and matter.js's examples), --bridge-passcode's
// default.
const publicTestPasscode = 20202021

// checkNodeRadio refuses the first node's radio flags before anything
// opens: either without --bridge (there is no first node to give them);
// --bridge-bluetooth-adapter with the public test passcode (anyone in
// Bluetooth range could commission the node: docs/by_design.md B27); and
// --bridge-bluetooth-adapter on the adapter --bluetooth-adapter gives the
// controller's central, which is opened only with --controller (one
// adapter in both roles is not proven: docs/by_design.md B26).
func checkNodeRadio(c Config) error {
	if !c.Bridge {
		var errs []error
		if c.BridgeBluetoothAdapter != "" {
			errs = append(errs, errors.New("--bridge-bluetooth-adapter needs --bridge: it is the first device node's radio"))
		}
		if c.BridgeWiFiInterface != "" {
			errs = append(errs, errors.New("--bridge-wifi-interface needs --bridge: it is the first device node's Wi-Fi"))
		}
		return errors.Join(errs...)
	}
	if c.BridgeBluetoothAdapter != "" && c.BridgePasscode == publicTestPasscode {
		return fmt.Errorf("--bridge-bluetooth-adapter with --bridge-passcode %d, the public test passcode: anyone in Bluetooth range could commission the node while it has no fabric; set --bridge-passcode to a passcode of your own", publicTestPasscode)
	}
	if c.Controller && c.BridgeBluetoothAdapter != "" && c.BluetoothAdapter != "" &&
		adapterName(c.BridgeBluetoothAdapter) == adapterName(c.BluetoothAdapter) {
		return fmt.Errorf("--bridge-bluetooth-adapter %s and --bluetooth-adapter %s name the same adapter (%s): the device node's peripheral and the controller's central need one adapter each",
			c.BridgeBluetoothAdapter, c.BluetoothAdapter, adapterName(c.BluetoothAdapter))
	}
	return nil
}

// nodeRadio is the first node's host ports: its BLE peripheral and its
// Wi-Fi driver, either nil when its flag is not given. One value for the
// daemon's life: every run of the first node — the first, and each an
// erase brings back — is built on it. Both are reusable across node runs:
// the peripheral serves again on a later Serve (ble/bluez
// peripheral_linux.go Serve: ErrServing only while one runs), and the
// driver holds no per-node state. A node's Stop ends its Serve and its
// advertisement and waits for both — but only until the stop's context
// ends (go-fabric bridge/ble.go StopBLE; a timeout is only logged,
// matter.ble.stop, and Stop still returns nil). So an erase whose budget
// ran out does not build the replacement on the radio
// (nodeLifecycle.erase); a replacement whose Serve still meets the old one
// is off the radio with go-fabric's Warn matter.ble.serve only.
type nodeRadio struct {
	ble  contract.BLEPeripheral
	wifi wifiPort
	once sync.Once
}

// openNodeRadio opens the ports the flags name, logging one line per port
// (never a credential: the Wi-Fi credential arrives inside PASE and goes
// to wpa_supplicant only). A port that does not open stops the start (fail
// closed), naming its flag; a peripheral already open is closed again.
func openNodeRadio(ctx context.Context, c Config, log *slog.Logger) (*nodeRadio, error) {
	r := &nodeRadio{}
	if c.BridgeBluetoothAdapter != "" {
		name := adapterName(c.BridgeBluetoothAdapter)
		p, err := newPeripheral(name) //nolint:contextcheck // the constructor takes no context; the radio lives until close
		if err != nil {
			return nil, fmt.Errorf("--bridge-bluetooth-adapter %s: %w", c.BridgeBluetoothAdapter, err)
		}
		r.ble = p
		log.Info("fabricserver.node_ble", slog.String("adapter", name))
	}
	if c.BridgeWiFiInterface != "" {
		d, err := newWiFiDriver(ctx, c.BridgeWiFiInterface, log)
		if err != nil {
			r.close()
			return nil, fmt.Errorf("--bridge-wifi-interface %s: %w", c.BridgeWiFiInterface, err)
		}
		r.wifi = d
		log.Info("fabricserver.node_wifi", slog.String("interface", c.BridgeWiFiInterface))
	}
	return r, nil
}

// close releases both ports, once; after the first node's last run
// stopped.
func (r *nodeRadio) close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if c, ok := r.ble.(io.Closer); ok {
			_ = c.Close()
		}
		if r.wifi != nil {
			_ = r.wifi.Close()
		}
	})
}

// apply gives the ports to the node of s: the first node only
// (fabricui.DefaultNodeKey). A registry node's network stays as it is.
func (r *nodeRadio) apply(s nodeSpec, network *node.Network) {
	if r == nil || s.Key != fabricui.DefaultNodeKey {
		return
	}
	if r.ble != nil {
		network.BLE = r.ble
	}
	if r.wifi != nil {
		network.WiFi = &node.WiFi{Driver: r.wifi}
	}
}
