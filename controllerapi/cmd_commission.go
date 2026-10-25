// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"

	"github.com/SukramJ/go-fabric-server/protocol"
)

// maxDiscoveryTimeout is matterjs-server's MAX_DISCOVERY_TIMEOUT_S, 48
// hours (WebSocketControllerHandler.ts:1895-1896).
const maxDiscoveryTimeout = 48 * time.Hour

// discoveryTimeout reads discovery_timeout_s: above 0, at most 48 hours,
// null as not given (docs/websockets_api.md:1582-1586).
func discoveryTimeout(command string, raw json.RawMessage) (time.Duration, *protocol.Error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil || v <= 0 || time.Duration(v*float64(time.Second)) > maxDiscoveryTimeout {
		return 0, invalidArgs(command, "discovery_timeout_s must be a number above 0 and at most 172800")
	}
	return time.Duration(v * float64(time.Second)), nil
}

// parseCode decodes a pairing code with go-fabric's decoder
// (controller.ParsePairingCode, matter.js's PairingCodeSchema). A code it
// refuses is InvalidArguments with reason invalid-payload and the details
// matterjs-server gives (packages/ws-controller/src/controller/onboardingPayload.ts:40-52
// at a7922e85: "Invalid pairing code: <message>").
func parseCode(code string) (controller.PairingCode, *protocol.Error) {
	pc, err := controller.ParsePairingCode(code)
	if err != nil {
		return pc, protocol.Errorf(protocol.InvalidArguments, "Invalid pairing code: %v", err).WithReason(protocol.ReasonInvalidPayload)
	}
	return pc, nil
}

// commissionContext bounds a commissioning: it outlives the connection that
// asked for it (a half-done commissioning would leave the device with a
// fail-safe armed) and ends after timeout.
func (s *Server) commissionContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = s.opts.PairTimeout
	}
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

// pairOptions are the pairing options of the server's mode: with
// --disable-auto-subscription a new node gets no subscription of the
// server's; the attestation policy is the server's (--enable-test-net-dcl
// accepts the findings of a test certificate, report S3 D4).
func (s *Server) pairOptions() controller.PairOptions {
	o := controller.PairOptions{OnAttestationFailure: s.opts.Attestation}
	if s.opts.DisableAutoSubscription {
		off := false
		o.AutoSubscribe = &off
	}
	return o
}

// bleNetwork picks the stored credentials a BLE commissioning provisions:
// the ids given, else the default entries; an id given that is not stored
// is InvalidArguments (matterjs-server WebSocketControllerHandler.ts:1311-1358,
// "Unknown wifi_credentials_id: <id>").
func (s *Server) bleNetwork(command string, wifiID, threadID *string) (*commissioner.WiFiNetwork, *commissioner.ThreadNetwork, *protocol.Error) {
	w, t, missing := s.cfg.credentials(wifiID, threadID)
	if missing != "" {
		return nil, nil, invalidArgs(command, "%s", missing)
	}
	var wn *commissioner.WiFiNetwork
	if w != nil && w.SSID != "" {
		wn = &commissioner.WiFiNetwork{SSID: w.SSID, Credentials: w.Credentials}
	}
	var tn *commissioner.ThreadNetwork
	if t != nil && t.Dataset != "" {
		b, err := hex.DecodeString(t.Dataset)
		if err == nil {
			tn = &commissioner.ThreadNetwork{OperationalDataset: b}
		}
	}
	return wn, tn, nil
}

// cmdCommissionWithCode pairs the device a QR or manual code names
// (controller.Controller.Pair) and answers its node data. Over DNS-SD by
// default; over BLE (controller.PairOptions.BLE, go-fabric v0.12.1) when
// the server has an adapter (--ble-adapter), network_only is false, the
// code does not rule BLE out (a QR code's discovery capabilities without
// the BLE bit do) and a stored Wi-Fi or Thread credential can put the
// device on its network — matterjs-server takes BLE whenever it is
// enabled and network_only is false (ControllerCommandHandler.ts:1965-2115).
func cmdCommissionWithCode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Code              *string         `json:"code"`
		NetworkOnly       bool            `json:"network_only"`
		WifiCredentialsID *string         `json:"wifi_credentials_id"`
		ThreadDatasetID   *string         `json:"thread_dataset_id"`
		DiscoveryTimeoutS json.RawMessage `json:"discovery_timeout_s"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Code == nil {
		return nil, invalidArgs(c.req.Command, "code is required")
	}
	pc, perr := parseCode(*a.Code)
	if perr != nil {
		return nil, perr
	}
	timeout, perr := discoveryTimeout(c.req.Command, a.DiscoveryTimeoutS)
	if perr != nil {
		return nil, perr
	}
	wifi, thread, perr := s.bleNetwork(c.req.Command, a.WifiCredentialsID, a.ThreadDatasetID)
	if perr != nil {
		return nil, perr
	}
	opts := s.pairOptions()
	if s.opts.BLE != nil && !a.NetworkOnly && (pc.Manual || pc.Capabilities&capabilityBLE != 0) && (wifi != nil || thread != nil) {
		opts.BLE, opts.WiFi, opts.Thread = s.opts.BLE, wifi, thread
	}
	pctx, cancel := s.commissionContext(ctx, timeout)
	defer cancel()
	n, err := s.b().Pair(pctx, *a.Code, opts)
	if err != nil {
		return nil, commissionError(err)
	}
	return s.commissioned(n)
}

// commissioned is the answer of a commissioning: the new node's data.
func (s *Server) commissioned(n *client.Node) (any, *protocol.Error) {
	d, ok := s.nodeDataOf(n)
	if !ok {
		return nil, protocol.Errorf(protocol.NodeCommissionFailed, "the commissioned node has no peer address").WithReason(protocol.ReasonOther)
	}
	return d, nil
}

// cmdCommissionOnNetwork finds the device on the network by filter
// (0 none, 1 short discriminator, 2 long discriminator, 3 vendor id, 4
// device type; docs/websockets_api.md:1605-1610), and, with ip_addr, the
// one announcing that address, then commissions it with setup_pin_code
// through the registry (client.Registry.ForDevice and Commission).
func cmdCommissionOnNetwork(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		SetupPinCode      *uint32         `json:"setup_pin_code"`
		FilterType        *int            `json:"filter_type"`
		Filter            *uint64         `json:"filter"`
		IPAddr            *string         `json:"ip_addr"`
		DiscoveryTimeoutS json.RawMessage `json:"discovery_timeout_s"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.SetupPinCode == nil {
		return nil, invalidArgs(c.req.Command, "setup_pin_code is required")
	}
	id := discovery.AnyCommissionable()
	ft := 0
	if a.FilterType != nil {
		ft = *a.FilterType
	}
	if ft != 0 && a.Filter == nil {
		return nil, invalidArgs(c.req.Command, "filter_type %d needs a filter", ft)
	}
	switch ft {
	case 0:
	case 1:
		id = discovery.ByShortDiscriminator(uint8(*a.Filter)) //nolint:gosec // a 4-bit discriminator
	case 2:
		id = discovery.ByLongDiscriminator(uint16(*a.Filter)) //nolint:gosec // a 12-bit discriminator
	case 3:
		id = discovery.ByVendor(uint16(*a.Filter)) //nolint:gosec // a vendor id
	case 4:
		id = discovery.ByDeviceType(uint32(*a.Filter)) //nolint:gosec // a device type id
	default:
		return nil, invalidArgs(c.req.Command, "filter_type %d is not one of 0 to 4", ft)
	}
	var want netip.Addr
	if a.IPAddr != nil && *a.IPAddr != "" {
		ip, err := netip.ParseAddr(*a.IPAddr)
		if err != nil {
			return nil, invalidArgs(c.req.Command, "ip_addr %q is not an IP address", *a.IPAddr)
		}
		want = ip.WithZone("")
	}
	timeout, perr := discoveryTimeout(c.req.Command, a.DiscoveryTimeoutS)
	if perr != nil {
		return nil, perr
	}
	pctx, cancel := s.commissionContext(ctx, timeout)
	defer cancel()
	found, err := s.discoverUntil(pctx, id, want, s.discoverWindow(timeout))
	if err != nil {
		return nil, commissionError(err)
	}
	var dev *client.Device
	for i := range found {
		if !want.IsValid() || announces(&found[i], want) {
			dev = &found[i]
			break
		}
	}
	if dev == nil {
		return nil, commissionError(controller.ErrNoDevice)
	}
	reg := s.b().Registry()
	n, err := reg.ForDevice(pctx, *dev)
	if err != nil {
		return nil, commissionError(err)
	}
	po := s.pairOptions()
	o := client.CommissionOptions{Options: commissioner.Options{Passcode: *a.SetupPinCode, OnAttestationFailure: po.OnAttestationFailure}, AutoSubscribe: po.AutoSubscribe}
	if err := reg.Commission(pctx, n, o); err != nil {
		return nil, commissionError(err)
	}
	return s.commissioned(n)
}

func announces(d *client.Device, ip netip.Addr) bool {
	for _, a := range d.Addresses {
		if a.IP.WithZone("") == ip {
			return true
		}
	}
	return false
}

// discoverWindow is how long a discovery listens: the caller's
// discovery_timeout_s, else 3 minutes for a commissioning — matter.js's
// default search (docs/websockets_api.md:1584).
func (s *Server) discoverWindow(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return 3 * time.Minute
}

// capabilityBLE is the BLE bit of a QR payload's discovery capabilities
// (controller.PairingCode.Capabilities: bit 0 SoftAP, bit 1 BLE, bit 2 on
// the IP network).
const capabilityBLE = 1 << 1

// cmdParseOnboardingPayload decodes a QR or manual pairing code without
// contacting a device (controller.ParsePairingCode) — matterjs-server's
// #handleParseOnboardingPayload (WebSocketControllerHandler.ts:1360-1390):
// a QR code is {type: "qr", payloads: [{version, vendor_id, product_id,
// flow_type, discovery_capabilities, discriminator, setup_pin_code}]}, a
// manual code {type: "manual", short_discriminator, setup_pin_code,
// vendor_id, product_id}; vendor_id and product_id are left out where the
// code says 0 (docs/websockets_api.md, python client models.py:912-944).
// go-fabric's decoder reads one QR payload; a concatenated QR code ("*"
// between payloads) is refused as matter.js's single-payload decode
// refuses it.
func cmdParseOnboardingPayload(_ context.Context, _ *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Code *string `json:"code"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Code == nil {
		return nil, invalidArgs(c.req.Command, "requires code as a string")
	}
	pc, perr := parseCode(*a.Code)
	if perr != nil {
		return nil, perr
	}
	if pc.Manual {
		return manualPayload{
			Type: "manual", ShortDiscriminator: pc.Discriminator, SetupPinCode: pc.Passcode,
			VendorID: nonZero(pc.VendorID), ProductID: nonZero(pc.ProductID),
		}, nil
	}
	return qrResult{Type: "qr", Payloads: []qrPayload{{
		Version: pc.Version, VendorID: nonZero(pc.VendorID), ProductID: nonZero(pc.ProductID),
		FlowType: pc.Flow, DiscoveryCapabilities: pc.Capabilities, Discriminator: pc.Discriminator, SetupPinCode: pc.Passcode,
	}}}, nil
}

// qrResult and manualPayload are QrOnboardingPayloadResult and
// ManualOnboardingPayloadResult (python client models.py:912-944).
type qrResult struct {
	Type     string      `json:"type"`
	Payloads []qrPayload `json:"payloads"`
}

type qrPayload struct {
	Version               uint8   `json:"version"`
	VendorID              *uint16 `json:"vendor_id,omitempty"`
	ProductID             *uint16 `json:"product_id,omitempty"`
	FlowType              uint8   `json:"flow_type"`
	DiscoveryCapabilities uint8   `json:"discovery_capabilities"`
	Discriminator         uint16  `json:"discriminator"`
	SetupPinCode          uint32  `json:"setup_pin_code"`
}

type manualPayload struct {
	Type               string  `json:"type"`
	ShortDiscriminator uint16  `json:"short_discriminator"`
	SetupPinCode       uint32  `json:"setup_pin_code"`
	VendorID           *uint16 `json:"vendor_id,omitempty"`
	ProductID          *uint16 `json:"product_id,omitempty"`
}

func nonZero(v uint16) *uint16 {
	if v == 0 {
		return nil
	}
	return &v
}

// discoverWindowList is how long discover listens before it answers:
// matterjs-server's 3 seconds (ControllerCommandHandler.ts:2135-2185).
const discoverWindowList = 3 * time.Second

// cmdDiscover lists the commissionable devices that answer within the
// window (controller.Controller.Discover) — CommissionableNodeData
// (model.ts:1825-1842), each device once.
func cmdDiscover(ctx context.Context, s *Server, _ *call) (any, *protocol.Error) {
	found, err := s.b().Discover(ctx, discovery.AnyCommissionable(), discoverWindowList)
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "discover: %v", err)
	}
	out := make([]map[string]any, 0, len(found))
	for i := range found {
		out = append(out, commissionableData(&found[i]))
	}
	return out, nil
}

// discoverRound is one search of discoverUntil.
const discoverRound = 2 * time.Second

// discoverUntil searches for the device id selects (and, when want is
// valid, that announces want) in rounds until one answers or window
// passes: controller.Controller.Discover returns what a whole search
// found, while matter.js's search for one device ends with the first
// answer (CommissioningController.commissionNode,
// CommissioningController.ts:462-500 at 862ad75f), so the server searches
// in short rounds (report S3, Q21).
func (s *Server) discoverUntil(ctx context.Context, id discovery.Identifier, want netip.Addr, window time.Duration) ([]client.Device, error) {
	deadline := time.Now().Add(window)
	for {
		round := min(discoverRound, time.Until(deadline))
		if round <= 0 {
			return nil, nil
		}
		found, err := s.b().Discover(ctx, id, round)
		if err != nil {
			return nil, err
		}
		for i := range found {
			if !want.IsValid() || announces(&found[i], want) {
				return found, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
}

func commissionableData(d *client.Device) map[string]any {
	m := map[string]any{
		"instance_name":      d.InstanceID,
		"long_discriminator": d.D,
		"commissioning_mode": d.CM,
	}
	if d.Hostname != "" {
		m["host_name"] = d.Hostname
	}
	addrs := make([]string, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		addrs = append(addrs, a.IP.String())
		if _, ok := m["port"]; !ok && a.Port != 0 {
			m["port"] = a.Port
		}
	}
	m["addresses"] = addrs
	if d.VP != nil {
		parts := strings.SplitN(*d.VP, "+", 2)
		if v, ok := discovery.ParseTxtDecimal(parts[0], 0xFFFF); ok {
			m["vendor_id"] = v
		}
		if len(parts) == 2 {
			if p, ok := discovery.ParseTxtDecimal(parts[1], 0xFFFF); ok {
				m["product_id"] = p
			}
		}
	}
	if d.DT != nil {
		m["device_type"] = *d.DT
	}
	if d.DN != nil {
		m["device_name"] = *d.DN
	}
	if d.PI != nil {
		m["pairing_instruction"] = *d.PI
	}
	if d.PH != nil {
		m["pairing_hint"] = *d.PH
	}
	if d.SII != nil {
		m["mrp_retry_interval_idle"] = d.SII.Milliseconds()
	}
	if d.SAI != nil {
		m["mrp_retry_interval_active"] = d.SAI.Milliseconds()
	}
	if d.RI != nil {
		m["rotating_id"] = *d.RI
	}
	return m
}
