// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
)

// nodeSpec is one device node of the bridge half: the node of the
// --bridge-* flags under the key "default", or a node of the registry
// (nodes.json, registry.go). The key is the node's identity
// (docs/by_design.md B23): it names the node in the pages' selector, in
// /bridge/<key> and in its store file.
type nodeSpec struct {
	Key, Listen, Label                           string
	Passcode, Discriminator, VendorID, ProductID uint
	// Store is the node's store file under --storage-path, Serial its
	// SerialNumber (an input of the UniqueID the facade derives, so it
	// differs per node).
	Store, Serial string
}

// nodeKey is a registry node's key: go-fabric-ui's NodeEntry.Key pattern
// (^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$), narrowed to 28 bytes so the
// SerialNumber "FSB-<key>" stays within BasicInformation's "max 32"
// (go-fabric parity/schema.json, BasicInformation SerialNumber, id 15).
var nodeKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,27}$`)

// pageNames are the node family's page names, which go-fabric-ui's
// NodeEntry.Key must not be (its doc at v0.6.0).
var pageNames = []string{"status", "pair", "fabrics", "selection", "endpoints", "endpoint", "surface", "diagnostics", "topology"}

// primaryNode is the node of the --bridge-* flags, the default node:
// key "default", store bridge.db, serial FABRIC-SERVER-BRIDGE.
func primaryNode(c Config) nodeSpec {
	return nodeSpec{
		Key: fabricui.DefaultNodeKey, Listen: c.BridgeListen, Label: c.BridgeLabel,
		Passcode: c.BridgePasscode, Discriminator: c.BridgeDiscriminator,
		VendorID: c.BridgeVendorID, ProductID: c.BridgeProductID,
		Store: "bridge.db", Serial: "FABRIC-SERVER-BRIDGE",
	}
}

// registrySpec is the spec of a registry node: its own key, label,
// listen address, discriminator and passcode; the vendor and product of
// the --bridge-* node; the store bridge-<key>.db and the serial FSB-<key>.
func registrySpec(primary nodeSpec, e *registryEntry) nodeSpec {
	return nodeSpec{
		Key: e.Key, Listen: e.Listen, Label: e.Label,
		Passcode: uint(e.Passcode), Discriminator: uint(e.Discriminator),
		VendorID: primary.VendorID, ProductID: primary.ProductID,
		Store: registryStore(e.Key), Serial: "FSB-" + e.Key,
	}
}

// registryStore is a registry node's store file under --storage-path.
func registryStore(key string) string { return "bridge-" + key + ".db" }

// splitListen is a listen address's host and UDP port ("" is
// node.Network's :5540).
func splitListen(addr string) (host string, port int, err error) {
	if addr == "" {
		return "", 5540, nil
	}
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err = strconv.Atoi(p)
	if err != nil || port < 0 || port > 65535 {
		return "", 0, fmt.Errorf("port %q", p)
	}
	return host, port, nil
}

// removedFlag is a flag this release no longer has: setting it, on the
// command line or through its environment variable, stops the start with
// the flag's migration instead of flag's "flag provided but not defined".
type removedFlag string

func (removedFlag) String() string     { return "" }
func (f removedFlag) Set(string) error { return fmt.Errorf("removed: %s", string(f)) }

// bridgeHalf is one running device node: its spec, the node and the
// bridge half bound to it.
type bridgeHalf struct {
	spec nodeSpec
	node *node.Node
	srv  *bridgeapi.Server
}

// BridgeHalf is one bridge half under its node's key, for MuxNodes.
type BridgeHalf struct {
	Key    string
	Server *bridgeapi.Server
}

// bridgeRoutes are the bridge halves /bridge and /bridge/<key> serve, the
// default node's first: a registry node's half is added when the node is
// created and removed when it is deleted, while the daemon runs.
type bridgeRoutes struct {
	mu     sync.RWMutex
	halves []BridgeHalf
}

func newBridgeRoutes(halves ...BridgeHalf) *bridgeRoutes {
	return &bridgeRoutes{halves: append([]BridgeHalf(nil), halves...)}
}

// all is a copy of the halves, the default first.
func (b *bridgeRoutes) all() []BridgeHalf {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]BridgeHalf(nil), b.halves...)
}

// get is the half of key, or the default node's for "" (/bridge).
func (b *bridgeRoutes) get(key string) *bridgeapi.Server {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i, h := range b.halves {
		if h.Key == key || (key == "" && i == 0) {
			return h.Server
		}
	}
	return nil
}

func (b *bridgeRoutes) add(h BridgeHalf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.halves = append(b.halves, h)
}

func (b *bridgeRoutes) remove(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, h := range b.halves {
		if h.Key == key {
			b.halves = append(b.halves[:i:i], b.halves[i+1:]...)
			return
		}
	}
}
