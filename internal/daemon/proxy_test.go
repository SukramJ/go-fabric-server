// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/ble/esphome"
	"github.com/SukramJ/go-fabric/ble/esphome/esphometest"
	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/btp"

	"github.com/SukramJ/go-fabric-server/controllerapi"
)

// testKey is an API encryption key: base64 of 32 bytes.
var testKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

// stubProxyCentral replaces newProxyCentral for one test and records the
// proxies it was asked for.
func stubProxyCentral(t *testing.T, c ble.Central, err error) *[][]esphome.Proxy {
	t.Helper()
	var asked [][]esphome.Proxy
	prev := newProxyCentral
	newProxyCentral = func(proxies []esphome.Proxy, _ esphome.Options) (ble.Central, error) {
		asked = append(asked, proxies)
		return c, err
	}
	t.Cleanup(func() { newProxyCentral = prev })
	return &asked
}

// TestBluetoothProxyFlag: --bluetooth-proxy appends one proxy per use,
// host[:port][,key]; an empty address is refused; the flag's text names
// no key.
func TestBluetoothProxyFlag(t *testing.T) {
	c, err := ParseFlags([]string{"--bluetooth-proxy", "proxy-office.local"}, io.Discard)
	if err != nil || len(c.BluetoothProxies) != 1 || c.BluetoothProxies[0] != (esphome.Proxy{Addr: "proxy-office.local"}) {
		t.Fatalf("one: %+v %v", c.BluetoothProxies, err)
	}
	c, err = ParseFlags([]string{"--bluetooth-proxy", "proxy-office.local," + testKey, "--bluetooth-proxy", "10.0.0.42:6053"}, io.Discard)
	if err != nil || len(c.BluetoothProxies) != 2 ||
		c.BluetoothProxies[0] != (esphome.Proxy{Addr: "proxy-office.local", Key: testKey}) ||
		c.BluetoothProxies[1] != (esphome.Proxy{Addr: "10.0.0.42:6053"}) {
		t.Fatalf("several: %+v %v", c.BluetoothProxies, err)
	}
	if got := (*proxyList)(&c.BluetoothProxies).String(); got != "proxy-office.local 10.0.0.42:6053" || strings.Contains(got, testKey) {
		t.Fatalf("String %q", got)
	}
	if _, err := ParseFlags([]string{"--bluetooth-proxy", ",key"}, io.Discard); err == nil || !strings.Contains(err.Error(), "empty proxy address") {
		t.Fatalf("bad spec: %v", err)
	}
	// The environment supplies one proxy when the flag is not given.
	t.Setenv("FABRIC_SERVER_BLUETOOTH_PROXY", "proxy-env.local")
	c, err = ParseFlags(nil, io.Discard)
	if err != nil || len(c.BluetoothProxies) != 1 || c.BluetoothProxies[0].Addr != "proxy-env.local" {
		t.Fatalf("env: %+v %v", c.BluetoothProxies, err)
	}
}

// TestBluetoothProxyWired: --bluetooth-proxy opens the proxies' central and
// the controller half's Options.BLE carries it; both backends is a start-up
// error; a proxy's refusal names the flag.
func TestBluetoothProxyWired(t *testing.T) {
	fake := &fakeCentral{}
	asked := stubProxyCentral(t, fake, nil)
	adapters := stubCentral(t, &fakeCentral{}, nil)
	log := slog.New(slog.DiscardHandler)
	proxies := []esphome.Proxy{{Addr: "a.local", Key: testKey}, {Addr: "b.local:6054"}}

	c, err := bleCentral(Config{BluetoothProxies: proxies}, log)
	if err != nil || c != fake {
		t.Fatalf("proxies: %v %v", c, err)
	}
	if len(*asked) != 1 || len((*asked)[0]) != 2 || (*asked)[0][0] != proxies[0] || len(*adapters) != 0 {
		t.Fatalf("asked %v, adapters %v", *asked, *adapters)
	}
	opts := controllerOptions(Config{StoragePath: t.TempDir()}, log, new(slog.LevelVar), c)
	if opts.BLE != fake {
		t.Fatalf("Options.BLE = %v, want the proxies' central", opts.BLE)
	}
	if _, err := controllerapi.New(opts); err != nil {
		t.Fatal(err)
	}

	if _, err := bleCentral(Config{BluetoothProxies: proxies, BluetoothAdapter: "0"}, log); err == nil || !strings.Contains(err.Error(), "one BLE backend") {
		t.Fatalf("both backends: %v", err)
	}
	if len(*asked) != 1 || len(*adapters) != 0 {
		t.Fatalf("both backends opened %v %v", *asked, *adapters)
	}

	stubProxyCentral(t, nil, esphome.ErrInvalidKey)
	if _, err := bleCentral(Config{BluetoothProxies: proxies}, log); !errors.Is(err, esphome.ErrInvalidKey) || !strings.HasPrefix(err.Error(), "--bluetooth-proxy: ") {
		t.Fatalf("refusal: %v", err)
	}
}

// TestRunRefusesBothBackends: Run stops before it opens anything when both
// BLE backends are given.
func TestRunRefusesBothBackends(t *testing.T) {
	asked := stubProxyCentral(t, &fakeCentral{}, nil)
	err := Run(context.Background(), Config{
		Listen: "127.0.0.1:0", LogLevel: "info", Controller: true, Trust: "production", StoragePath: t.TempDir(),
		BluetoothAdapter: "0", BluetoothProxies: []esphome.Proxy{{Addr: "a.local"}},
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "one BLE backend") || len(*asked) != 0 {
		t.Fatalf("%v (opened %v)", err, *asked)
	}
}

// wifiDriver is a Wi-Fi driver that joins every network it is given.
type wifiDriver struct {
	mu       sync.Mutex
	connects []string
}

func (w *wifiDriver) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w *wifiDriver) ConnectNetwork(_ context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connects = append(w.connects, string(ssid)+"/"+string(credentials))
	return 0, "", 0
}

// proxyCentral is the daemon's central (esphome.NewCentral, as
// newProxyCentral opens it) over go-fabric's fake proxy, with the
// encryption key, in front of the site device's BLE side: its BTP sessions
// are served by the device's bridge (go-fabric ble/esphome's
// commission_test.go is the pattern).
func proxyCentral(t *testing.T, dev *fabrictest.Device) ble.Central {
	t.Helper()
	p := &bletest.Peripheral{
		Address: "C0:FF:EE:00:0B:1E", Discriminator: dev.Discriminator(), VendorID: 0xFFF1, ProductID: 0x8001,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			srv, err := bridgetest.ServeConn(dev.Bridge(), conn)
			if err != nil {
				t.Error(err)
				return nil, nil
			}
			return srv.Deliver, srv.Close
		},
	}
	f, err := esphometest.New(esphometest.Options{Key: testKey}, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	c, err := newProxyCentral([]esphome.Proxy{{Addr: f.Addr(), Key: testKey}}, esphome.Options{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.(io.Closer).Close() })
	return c
}

// TestCommissionThroughProxy: the daemon's composition with the proxies'
// central — server_info and the pages' status report BLE, and
// commission_with_code pairs a Wi-Fi device over BLE through the proxy
// (PASE over BTP, the network provisioned, CASE on the site's network).
func TestCommissionThroughProxy(t *testing.T) {
	drv := &wifiDriver{}
	e := newPagesEnvWith(t, Config{WebAdmin: true}, func(cfg *node.Config) {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv}
	}, func(dev *fabrictest.Device) ble.Central { return proxyCentral(t, dev) })

	var info struct {
		BluetoothEnabled bool `json:"bluetooth_enabled"`
		BLEProxyEnabled  bool `json:"ble_proxy_enabled"`
	}
	if err := e.ws.Call(e.ctx, "server_info", nil, &info); err != nil || !info.BluetoothEnabled || info.BLEProxyEnabled {
		t.Fatalf("server_info %+v, %v", info, err)
	}
	var st struct {
		Capabilities struct {
			BLE bool `json:"ble"`
		} `json:"capabilities"`
	}
	e.json(http.MethodGet, "/controller/status", "", http.StatusOK, &st)
	if !st.Capabilities.BLE {
		t.Fatalf("status %+v", st)
	}

	if err := e.ws.Call(e.ctx, "set_wifi_credentials", map[string]any{"ssid": "lab", "credentials": "secret12"}, nil); err != nil {
		t.Fatal(err)
	}
	_, code, err := e.dev.Node().PairingCodes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.ws.CommissionWithCode(e.ctx, code, false)
	if err != nil {
		t.Fatalf("commission_with_code through the proxy: %v", err)
	}
	waitWSEvent(t, e.ws, "node_added")
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.connects) != 1 || drv.connects[0] != "lab/secret12" || n.NodeID == 0 {
		t.Fatalf("driver connects %q, node %d", drv.connects, n.NodeID)
	}
}
