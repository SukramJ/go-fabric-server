// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
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

// wellFormedCode is the outer form of a pairing code
// (docs/websockets_api.md:1628): a QR payload starting with "MT:" or an
// 11- or 21-digit manual code (dashes and spaces aside).
func wellFormedCode(code string) bool {
	if strings.HasPrefix(code, "MT:") {
		return len(code) > 3
	}
	digits := 0
	for _, r := range code {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-' || r == ' ':
		default:
			return false
		}
	}
	return digits == 11 || digits == 21
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
// server's.
func (s *Server) pairOptions() controller.PairOptions {
	var o controller.PairOptions
	if s.opts.DisableAutoSubscription {
		off := false
		o.AutoSubscribe = &off
	}
	return o
}

// cmdCommissionWithCode pairs the device a QR or manual code names
// (controller.Controller.Pair) and answers its node data. The stored
// Wi-Fi and Thread credentials are not handed to the commissioner: Pair
// finds the device over DNS-SD, where it is on the network already (BLE
// is not on the facade's Pair route; report S1, Left out).
func cmdCommissionWithCode(ctx context.Context, s *Server, c *call) (any, *protocol.Error) {
	var a struct {
		Code               *string         `json:"code"`
		NetworkOnly        bool            `json:"network_only"`
		WifiCredentialsID  *string         `json:"wifi_credentials_id"`
		ThreadDatasetID    *string         `json:"thread_dataset_id"`
		DiscoveryTimeoutS  json.RawMessage `json:"discovery_timeout_s"`
	}
	if err := c.args(&a); err != nil {
		return nil, err
	}
	if a.Code == nil {
		return nil, invalidArgs(c.req.Command, "code is required")
	}
	if !wellFormedCode(*a.Code) {
		return nil, invalidArgs(c.req.Command, "the pairing code cannot be decoded").WithReason(protocol.ReasonInvalidPayload)
	}
	timeout, perr := discoveryTimeout(c.req.Command, a.DiscoveryTimeoutS)
	if perr != nil {
		return nil, perr
	}
	pctx, cancel := s.commissionContext(ctx, timeout)
	defer cancel()
	n, err := s.b().Pair(pctx, *a.Code, s.pairOptions())
	if err != nil {
		// A code of the right form the facade still cannot decode is
		// "other": controller.Pair has no sentinel for it (Questions Q4).
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
	found, err := s.b().(Discoverer).Discover(pctx, id, s.discoverWindow(timeout))
	if err != nil {
		return nil, commissionError(err)
	}
	var dev *client.Device
	for i := range found {
		if !want.IsValid() || announces(found[i], want) {
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
	o := client.CommissionOptions{Options: commissioner.Options{Passcode: *a.SetupPinCode}, AutoSubscribe: s.pairOptions().AutoSubscribe}
	if err := reg.Commission(pctx, n, o); err != nil {
		return nil, commissionError(err)
	}
	return s.commissioned(n)
}

func announces(d client.Device, ip netip.Addr) bool {
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

// cmdParseOnboardingPayload is answered by the dispatcher's gap check
// (Questions Q4).
func cmdParseOnboardingPayload(context.Context, *Server, *call) (any, *protocol.Error) {
	return nil, protocol.Errorf(protocol.InvalidCommand, "parse_onboarding_payload is not available")
}

// discoverWindowList is how long discover listens before it answers.
const discoverWindowList = 5 * time.Second

// cmdDiscover lists the commissionable devices that answer within a few
// seconds — CommissionableNodeData (model.ts:1825-1842).
func cmdDiscover(ctx context.Context, s *Server, _ *call) (any, *protocol.Error) {
	found, err := s.b().(Discoverer).Discover(ctx, discovery.AnyCommissionable(), discoverWindowList)
	if err != nil {
		return nil, protocol.Errorf(protocol.UnknownError, "discover: %v", err)
	}
	out := make([]map[string]any, 0, len(found))
	for _, d := range found {
		out = append(out, commissionableData(d))
	}
	return out, nil
}

func commissionableData(d client.Device) map[string]any {
	m := map[string]any{
		"instance_name":      d.InstanceID,
		"long_discriminator": d.D,
		"commissioning_mode": d.CM,
	}
	if d.Hostname != "" {
		m["host_name"] = d.Hostname
	}
	addrs := []string{}
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
