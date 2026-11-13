// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/node"
)

// testRadio is the first node's peripheral in go-fabric's in-memory model
// (bletest.Radio: the node's production BLE path, served to the centrals
// of its peripheral), with the Close the daemon calls at its stop.
type testRadio struct {
	*bletest.Radio
	closed atomic.Int32
}

func (r *testRadio) Close() error { r.closed.Add(1); return nil }

func newTestRadio() *testRadio {
	return &testRadio{Radio: (&bletest.Peripheral{Address: "fabric-server-node"}).Radio()}
}

// testWiFi is the first node's Wi-Fi driver: joins every network
// (wifiDriver, proxy_test.go) and counts its Close.
type testWiFi struct {
	*wifiDriver
	closed atomic.Int32
}

func (w *testWiFi) Close() error { w.closed.Add(1); return nil }

// stubNodeRadio replaces newPeripheral and newWiFiDriver for one test and
// records the adapter and interface names they were asked for.
func stubNodeRadio(t *testing.T, p contract.BLEPeripheral, perr error, w wifiPort, werr error) (adapters, ifaces *[]string) {
	t.Helper()
	var a, i []string
	prevP, prevW := newPeripheral, newWiFiDriver
	newPeripheral = func(name string) (contract.BLEPeripheral, error) {
		a = append(a, name)
		if perr != nil {
			return nil, perr
		}
		return p, nil
	}
	newWiFiDriver = func(_ context.Context, iface string, _ *slog.Logger) (wifiPort, error) {
		i = append(i, iface)
		if werr != nil {
			return nil, werr
		}
		return w, nil
	}
	t.Cleanup(func() { newPeripheral, newWiFiDriver = prevP, prevW })
	return &a, &i
}

// eventually polls cond until it holds or 20 s pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// serving reports whether a node serves the Matter GATT service on r and
// advertises on it — an uncommissioned node with its own window open.
func serving(r *bletest.Radio) bool {
	s, _ := r.Serving()
	_, adv := r.Advertising()
	return s && adv
}

// TestNodeRadioFlags: both flags parse, each also from its environment
// variable, a flag over its variable; both are empty by default.
func TestNodeRadioFlags(t *testing.T) {
	c, err := ParseFlags(nil, io.Discard)
	if err != nil || c.BridgeBluetoothAdapter != "" || c.BridgeWiFiInterface != "" {
		t.Fatalf("defaults %q %q %v", c.BridgeBluetoothAdapter, c.BridgeWiFiInterface, err)
	}
	c, err = ParseFlags([]string{"--bridge-bluetooth-adapter", "0", "--bridge-wifi-interface", "wlan0"}, io.Discard)
	if err != nil || c.BridgeBluetoothAdapter != "0" || c.BridgeWiFiInterface != "wlan0" {
		t.Fatalf("flags %q %q %v", c.BridgeBluetoothAdapter, c.BridgeWiFiInterface, err)
	}
	t.Setenv("FABRIC_SERVER_BRIDGE_BLUETOOTH_ADAPTER", "hci1")
	t.Setenv("FABRIC_SERVER_BRIDGE_WIFI_INTERFACE", "wlp2s0")
	c, err = ParseFlags(nil, io.Discard)
	if err != nil || c.BridgeBluetoothAdapter != "hci1" || c.BridgeWiFiInterface != "wlp2s0" {
		t.Fatalf("env %q %q %v", c.BridgeBluetoothAdapter, c.BridgeWiFiInterface, err)
	}
	c, err = ParseFlags([]string{"--bridge-bluetooth-adapter", "hci2"}, io.Discard)
	if err != nil || c.BridgeBluetoothAdapter != "hci2" || c.BridgeWiFiInterface != "wlp2s0" {
		t.Fatalf("flag over env %q %q %v", c.BridgeBluetoothAdapter, c.BridgeWiFiInterface, err)
	}
}

// TestRunRefusesNodeRadio: either flag without --bridge, and the
// controller's adapter named again in either spelling, stop the start
// before anything opens; a port that does not open stops it naming its
// flag, and a peripheral already open is closed again.
func TestRunRefusesNodeRadio(t *testing.T) {
	ctx := context.Background()
	central := stubCentral(t, &fakeCentral{}, nil)
	adapters, ifaces := stubNodeRadio(t, newTestRadio(), nil, &testWiFi{wifiDriver: &wifiDriver{}}, nil)
	base := func() Config { return Config{Listen: "127.0.0.1:0", LogLevel: "info", StoragePath: t.TempDir()} }
	for _, tc := range []struct {
		edit func(*Config)
		want string
	}{
		{func(c *Config) { c.BridgeBluetoothAdapter = "0" }, "--bridge-bluetooth-adapter needs --bridge"},
		{func(c *Config) { c.BridgeWiFiInterface = "wlan0" }, "--bridge-wifi-interface needs --bridge"},
		{func(c *Config) {
			c.Bridge, c.Controller, c.Trust, c.BluetoothAdapter, c.BridgeBluetoothAdapter = true, true, "production", "0", "hci0"
		}, "--bridge-bluetooth-adapter hci0 and --bluetooth-adapter 0 name the same adapter (hci0)"},
		{func(c *Config) {
			c.Bridge, c.Controller, c.Trust, c.BluetoothAdapter, c.BridgeBluetoothAdapter = true, true, "production", "hci1", "1"
		}, "--bridge-bluetooth-adapter 1 and --bluetooth-adapter hci1 name the same adapter (hci1)"},
	} {
		c := base()
		tc.edit(&c)
		if err := Run(ctx, c, io.Discard); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%+v: %v, want %q", c, err, tc.want)
		}
	}
	if len(*central) != 0 || len(*adapters) != 0 || len(*ifaces) != 0 {
		t.Fatalf("a refused start opened central %v, peripheral %v, wifi %v", *central, *adapters, *ifaces)
	}
	if err := checkNodeRadio(Config{Bridge: true, BridgeBluetoothAdapter: "hci1", BluetoothAdapter: "0"}); err != nil {
		t.Fatalf("two adapters: %v", err)
	}

	// The peripheral does not open: its flag, nothing else opened.
	adapters, ifaces = stubNodeRadio(t, nil, errors.New("bluez: no such adapter"), nil, nil)
	c := base()
	c.Bridge, c.BridgeBluetoothAdapter, c.BridgeWiFiInterface = true, "1", "wlan0"
	if err := Run(ctx, c, io.Discard); err == nil || err.Error() != "--bridge-bluetooth-adapter 1: bluez: no such adapter" {
		t.Fatalf("peripheral: %v", err)
	}
	if !slices.Equal(*adapters, []string{"hci1"}) || len(*ifaces) != 0 {
		t.Fatalf("peripheral: asked %v %v", *adapters, *ifaces)
	}

	// The driver does not open: its flag, and the open peripheral closed.
	radio := newTestRadio()
	stubNodeRadio(t, radio, nil, nil, errors.New("wpasupplicant: no interface wlan9"))
	c = base()
	c.Bridge, c.BridgeBluetoothAdapter, c.BridgeWiFiInterface = true, "hci0", "wlan9"
	if err := Run(ctx, c, io.Discard); err == nil || err.Error() != "--bridge-wifi-interface wlan9: wpasupplicant: no interface wlan9" {
		t.Fatalf("wifi: %v", err)
	}
	if radio.closed.Load() != 1 {
		t.Fatalf("the peripheral closed %d times, want 1", radio.closed.Load())
	}
}

// TestBridgeConfigRadio: bridgeConfig hands the radio's peripheral and
// driver to the first node and to no registry node; each port alone
// reaches the node alone; without a radio the network is as before.
func TestBridgeConfigRadio(t *testing.T) {
	c, err := ParseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	c.StoragePath = t.TempDir()
	r, w := newTestRadio(), &testWiFi{wifiDriver: &wifiDriver{}}
	primary := primaryNode(c)
	second := registrySpec(primary, &registryEntry{Key: "second", Label: "Second", Listen: "[::1]:0", Discriminator: 3841, Passcode: 20202022})
	network := func(s nodeSpec, radio *nodeRadio) node.Network {
		t.Helper()
		cfg, err := bridgeConfig(c, s, radio, nil)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Network
	}

	both := &nodeRadio{ble: r, wifi: w}
	if n := network(primary, both); n.BLE != r || n.WiFi == nil || n.WiFi.Driver != w {
		t.Fatalf("first node: %+v", n)
	}
	if n := network(second, both); n.BLE != nil || n.WiFi != nil {
		t.Fatalf("registry node got the radio: %+v", n)
	}
	if n := network(primary, &nodeRadio{ble: r}); n.BLE != r || n.WiFi != nil {
		t.Fatalf("BLE alone: %+v", n)
	}
	if n := network(primary, &nodeRadio{wifi: w}); n.BLE != nil || n.WiFi == nil || n.WiFi.Driver != w {
		t.Fatalf("Wi-Fi alone: %+v", n)
	}
	if n := network(primary, nil); n.BLE != nil || n.WiFi != nil {
		t.Fatalf("no radio: %+v", n)
	}
}

// TestNodeRadioPairingCode: the first node's QR code names BLE discovery
// beside the IP network with --bridge-bluetooth-adapter, and the IP
// network only without it (go-fabric node/credentials.go pairingCodes).
func TestNodeRadioPairingCode(t *testing.T) {
	ctx := context.Background()
	c, err := ParseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for _, withBLE := range []bool{false, true} {
		c.StoragePath = t.TempDir()
		var radio *nodeRadio
		if withBLE {
			radio = &nodeRadio{ble: newTestRadio()}
		}
		cfg, err := bridgeConfig(c, primaryNode(c), radio, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		n, err := node.New(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := n.SetupPayload()
		_ = n.Stop(ctx)
		if err != nil {
			t.Fatal(err)
		}
		code, err := controller.ParsePairingCode(payload.QR)
		if err != nil {
			t.Fatal(err)
		}
		// Bit 1 BLE, bit 2 the IP network (controller.PairingCode).
		if got := code.Capabilities&0x02 != 0; got != withBLE || code.Capabilities&0x04 == 0 {
			t.Fatalf("BLE %v: capabilities %#x", withBLE, code.Capabilities)
		}
	}
}

// TestEraseKeepsRadio: the first node, started by the daemon's starter,
// serves on the radio while it has no fabric; an erase stops it and the
// node it brings back serves on the same peripheral and driver again; the
// lifecycle never closes them, the daemon's close does, once.
func TestEraseKeepsRadio(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the daemon's node on the host network: linux only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := ParseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	c.StoragePath, c.BridgeListen, c.BridgeResultTimeout = t.TempDir(), "[::1]:0", time.Second
	log := slog.New(slog.DiscardHandler)
	r, w := newTestRadio(), &testWiFi{wifiDriver: &wifiDriver{}}
	radio := &nodeRadio{ble: r, wifi: w}
	life := newNodeLifecycle(ctx, nodeStarter(c, radio, log), newBridgeRoutes(), log)
	def, err := life.startHalf(primaryNode(c))
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			def.stop(context.WithoutCancel(ctx))
		}
	}()
	eventually(t, "the first node on the radio", func() bool { return serving(r.Radio) })
	first := def.live().node
	ads, _ := r.Advertisements()

	if err := life.erase(ctx, def); err != nil {
		t.Fatal(err)
	}
	if def.live().node == first {
		t.Fatal("the erase kept the node")
	}
	eventually(t, "the erased node back on the radio", func() bool {
		after, stops := r.Advertisements()
		return serving(r.Radio) && len(after) > len(ads) && stops > 0
	})
	if r.closed.Load() != 0 || w.closed.Load() != 0 {
		t.Fatalf("the erase closed the radio (%d) or the driver (%d)", r.closed.Load(), w.closed.Load())
	}

	def.stop(context.WithoutCancel(ctx))
	stopped = true
	if s, _ := r.Serving(); s {
		t.Fatal("the stopped node still serves on the radio")
	}
	radio.close()
	radio.close()
	if r.closed.Load() != 1 || w.closed.Load() != 1 {
		t.Fatalf("closed radio %d, driver %d times, want 1", r.closed.Load(), w.closed.Load())
	}
}

// lockedBuffer is a log sink the daemon writes from several goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestRunNodeRadio: Run opens both ports once, logs one line per port
// (the adapter's BlueZ name, the interface), puts the first node on the
// radio, and closes both once at its stop.
func TestRunNodeRadio(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the daemon's node on the host network: linux only")
	}
	r, w := newTestRadio(), &testWiFi{wifiDriver: &wifiDriver{}}
	adapters, ifaces := stubNodeRadio(t, r, nil, w, nil)
	c, err := ParseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	c.Listen, c.StoragePath, c.Controller, c.Bridge = "127.0.0.1:0", t.TempDir(), false, true
	c.BridgeListen, c.BridgeResultTimeout = "[::1]:0", time.Second
	c.BridgeBluetoothAdapter, c.BridgeWiFiInterface = "0", "wlan0"
	var out lockedBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, &out) }()
	eventually(t, "the first node on the radio", func() bool {
		select {
		case err := <-done:
			t.Fatalf("Run ended: %v", err)
		default:
		}
		return serving(r.Radio)
	})
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not stop")
	}
	if !slices.Equal(*adapters, []string{"hci0"}) || !slices.Equal(*ifaces, []string{"wlan0"}) {
		t.Fatalf("opened %v %v", *adapters, *ifaces)
	}
	if r.closed.Load() != 1 || w.closed.Load() != 1 {
		t.Fatalf("closed radio %d, driver %d times, want 1", r.closed.Load(), w.closed.Load())
	}
	if s, _ := r.Serving(); s {
		t.Fatal("the stopped daemon's node still serves on the radio")
	}
	log := out.String()
	for _, want := range []string{"fabricserver.node_ble", "hci0", "fabricserver.node_wifi", "wlan0"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log has no %q:\n%s", want, log)
		}
	}
}
