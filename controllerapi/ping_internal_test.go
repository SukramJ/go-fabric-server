// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
)

// fakePing answers the addresses in rtt and fails every other one as
// go-fabric's PingAddress fails a silent address, counting the attempts.
type fakePing struct {
	mu    sync.Mutex
	rtt   map[netip.AddrPort]time.Duration
	err   error // the error of an address not in rtt; ErrUnreachable when nil
	calls map[netip.AddrPort]int
}

func (f *fakePing) ping(_ context.Context, ap netip.AddrPort) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[netip.AddrPort]int{}
	}
	f.calls[ap]++
	if d, ok := f.rtt[ap]; ok {
		return d, nil
	}
	if f.err != nil {
		return 0, f.err
	}
	return 0, fmt.Errorf("%w: %s", client.ErrUnreachable, ap)
}

func netAddr(s string) client.NetworkAddress {
	ap := netip.MustParseAddrPort(s)
	return client.NetworkAddress{IP: ap.Addr(), Port: ap.Port()}
}

// Two addresses, one silent: each answers its own outcome, the silent one
// after every attempt, the answering one after its first; a duplicate
// address is probed once.
func TestPingAddressesTwoAddressesOneSilent(t *testing.T) {
	live := netip.MustParseAddrPort("[fd00::2]:5540")
	silent := netip.MustParseAddrPort("[fd00::1]:5540")
	f := &fakePing{rtt: map[netip.AddrPort]time.Duration{live: 1500 * time.Microsecond}}
	addrs := []client.NetworkAddress{netAddr("[fd00::2]:5540"), netAddr("[fd00::1]:5540"), netAddr("[fd00::2]:5540")}
	res := pingAddresses(t.Context(), addrs, 3, time.Second, f.ping)
	if len(res) != 2 {
		t.Fatalf("results %+v, want one per distinct address", res)
	}
	s, l := res[0], res[1]
	if s.IP != "fd00::1" || s.Port != 5540 || s.Reachable || s.RoundTripMS != nil || s.Error == "" {
		t.Fatalf("silent %+v", s)
	}
	if l.IP != "fd00::2" || !l.Reachable || l.RoundTripMS == nil || *l.RoundTripMS != 1.5 || l.Error != "" {
		t.Fatalf("live %+v", l)
	}
	if f.calls[silent] != 3 || f.calls[live] != 1 {
		t.Fatalf("attempts %v, want 3 for the silent address and 1 for the live one", f.calls)
	}
	m := pingMap(res)
	if len(m) != 2 || m["fd00::1"] || !m["fd00::2"] {
		t.Fatalf("map %v", m)
	}
}

// An IP is reachable in the map when any of its ports answered.
func TestPingMapAnyPort(t *testing.T) {
	f := &fakePing{rtt: map[netip.AddrPort]time.Duration{netip.MustParseAddrPort("10.0.0.1:5541"): time.Millisecond}}
	res := pingAddresses(t.Context(), []client.NetworkAddress{netAddr("10.0.0.1:5540"), netAddr("10.0.0.1:5541")}, 1, time.Second, f.ping)
	if len(res) != 2 || res[0].Port != 5540 || res[0].Reachable || !res[1].Reachable {
		t.Fatalf("results %+v", res)
	}
	if m := pingMap(res); len(m) != 1 || !m["10.0.0.1"] {
		t.Fatalf("map %v", m)
	}
}

// A node without a session cannot answer a further attempt: one attempt,
// and the error names it.
func TestPingAddressesNoSession(t *testing.T) {
	f := &fakePing{err: client.ErrNoSession}
	ap := netip.MustParseAddrPort("[fd00::1]:5540")
	res := pingAddresses(t.Context(), []client.NetworkAddress{netAddr(ap.String())}, 5, time.Second, f.ping)
	if len(res) != 1 || res[0].Reachable || res[0].Error != client.ErrNoSession.Error() || f.calls[ap] != 1 {
		t.Fatalf("results %+v, attempts %v", res, f.calls)
	}
}

// No address: an empty answer, as matterjs-server's for a node without one.
func TestPingAddressesNone(t *testing.T) {
	if res := pingAddresses(t.Context(), nil, 1, time.Second, (&fakePing{}).ping); len(res) != 0 {
		t.Fatalf("results %+v", res)
	}
	if m := pingMap(nil); len(m) != 0 {
		t.Fatalf("map %v", m)
	}
}

// The args: attempts 1 to 10, 1 when absent or not positive.
func TestPingArgsAttempts(t *testing.T) {
	for _, c := range []struct {
		in   *int
		want int
	}{{nil, 1}, {new(0), 1}, {new(-2), 1}, {new(3), 3}, {new(99), 10}} {
		if got := (pingArgs{Attempts: c.in}).attempts(); got != c.want {
			t.Fatalf("attempts(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
