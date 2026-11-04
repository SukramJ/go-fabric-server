// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric/node"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
)

// nodeSpec is one device node of the bridge half: the node of the
// --bridge-* flags under the key "default", or a --bridge-node entry.
// The key is the node's identity (docs/by_design.md B23): it names the
// node in the pages' selector, in /bridge/<key> and in its store file.
type nodeSpec struct {
	Key, Listen, Label                           string
	Passcode, Discriminator, VendorID, ProductID uint
	// Store is the node's store file under --storage-path, Serial its
	// SerialNumber (an input of the UniqueID the facade derives, so it
	// differs per node).
	Store, Serial string
}

// specList is --bridge-node: each use appends its entries; ";" separates
// several in one value (FABRIC_SERVER_BRIDGE_NODE carries them all).
// The entries are resolved by bridgeNodes, after every flag is read.
type specList []string

func (l *specList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ";")
}

func (l *specList) Set(v string) error {
	for e := range strings.SplitSeq(v, ";") {
		if e = strings.TrimSpace(e); e != "" {
			*l = append(*l, e)
		}
	}
	return nil
}

// nodeKey is a --bridge-node key: go-fabric-ui's NodeEntry.Key pattern
// (^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$), narrowed to 28 bytes so the
// SerialNumber "FSB-<key>" stays within BasicInformation's "max 32"
// (go-fabric parity/schema.json, BasicInformation SerialNumber, id 15).
var nodeKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,27}$`)

// pageNames are the node family's page names, which go-fabric-ui's
// NodeEntry.Key must not be (its doc at 2f4a922).
var pageNames = []string{"status", "pair", "fabrics", "selection", "endpoints", "endpoint", "surface", "diagnostics", "topology"}

// primaryNode is the node of the --bridge-* flags, as before several
// nodes: key "default", store bridge.db, serial FABRIC-SERVER-BRIDGE.
func primaryNode(c Config) nodeSpec {
	return nodeSpec{
		Key: fabricui.DefaultNodeKey, Listen: c.BridgeListen, Label: c.BridgeLabel,
		Passcode: c.BridgePasscode, Discriminator: c.BridgeDiscriminator,
		VendorID: c.BridgeVendorID, ProductID: c.BridgeProductID,
		Store: "bridge.db", Serial: "FABRIC-SERVER-BRIDGE",
	}
}

// parseNodeSpec reads one --bridge-node entry,
// key,listen=ADDR[,discriminator=N][,passcode=N][,vendor-id=N][,product-id=N][,label=TEXT];
// a field left out takes the --bridge-* node's value, the label the key.
func parseNodeSpec(entry string, primary nodeSpec) (nodeSpec, error) {
	fields := strings.Split(entry, ",")
	key := strings.TrimSpace(fields[0])
	switch {
	case !nodeKey.MatchString(key):
		return nodeSpec{}, fmt.Errorf("--bridge-node %q: the key must match %s", entry, nodeKey)
	case key == fabricui.DefaultNodeKey:
		return nodeSpec{}, fmt.Errorf("--bridge-node %q: %q is the --bridge-* node's key", entry, key)
	case slices.Contains(pageNames, key):
		return nodeSpec{}, fmt.Errorf("--bridge-node %q: %q is a page name of the node pages", entry, key)
	}
	n := primary
	n.Key, n.Label, n.Listen = key, key, ""
	n.Store, n.Serial = "bridge-"+key+".db", "FSB-"+key
	uints := map[string]*uint{"passcode": &n.Passcode, "discriminator": &n.Discriminator, "vendor-id": &n.VendorID, "product-id": &n.ProductID}
	for _, f := range fields[1:] {
		name, value, ok := strings.Cut(strings.TrimSpace(f), "=")
		switch {
		case !ok:
			return nodeSpec{}, fmt.Errorf("--bridge-node %q: %q is not name=value", entry, f)
		case name == "listen":
			n.Listen = value
		case name == "label":
			n.Label = value
		case uints[name] != nil:
			v, err := strconv.ParseUint(value, 0, 32)
			if err != nil {
				return nodeSpec{}, fmt.Errorf("--bridge-node %q: %s: %w", entry, name, err)
			}
			*uints[name] = uint(v)
		default:
			return nodeSpec{}, fmt.Errorf("--bridge-node %q: unknown field %q (listen, discriminator, passcode, vendor-id, product-id, label)", entry, name)
		}
	}
	if n.Listen == "" {
		return nodeSpec{}, fmt.Errorf("--bridge-node %q: listen= is required (the --bridge-* node holds the default :5540)", entry)
	}
	return n, nil
}

// bridgeNodes are the device nodes the daemon runs, the default node
// first: the --bridge-* node and every --bridge-node entry, validated
// before any starts. Without --bridge there are none.
func bridgeNodes(c Config) ([]nodeSpec, error) {
	if !c.Bridge {
		switch {
		case len(c.BridgeNodes) > 0:
			return nil, errors.New("--bridge-node needs --bridge")
		case c.NodeDefault != "" && c.NodeDefault != fabricui.DefaultNodeKey:
			return nil, errors.New("--node-default needs --bridge")
		}
		return nil, nil
	}
	primary := primaryNode(c)
	nodes := []nodeSpec{primary}
	for _, e := range c.BridgeNodes {
		n, err := parseNodeSpec(e, primary)
		if err != nil {
			return nil, err
		}
		for _, o := range nodes {
			switch {
			case o.Key == n.Key:
				return nil, fmt.Errorf("--bridge-node %q: the key %q is configured twice", e, n.Key)
			case o.Discriminator == n.Discriminator:
				return nil, fmt.Errorf("--bridge-node %q: discriminator %d is node %q's as well; a commissioner tells the nodes apart by it", e, n.Discriminator, o.Key)
			case samePort(o.Listen, n.Listen):
				return nil, fmt.Errorf("--bridge-node %q: listen %q takes node %q's port", e, n.Listen, o.Key)
			}
		}
		nodes = append(nodes, n)
	}
	if c.NodeDefault != "" {
		i := slices.IndexFunc(nodes, func(n nodeSpec) bool { return n.Key == c.NodeDefault })
		if i < 0 {
			return nil, fmt.Errorf("--node-default %q is no configured node", c.NodeDefault)
		}
		def := nodes[i]
		nodes = append([]nodeSpec{def}, slices.Delete(nodes, i, i+1)...)
	}
	return nodes, nil
}

// samePort reports whether two listen addresses name one fixed UDP port
// ("" is node.Network's :5540; port 0 is an ephemeral one and never
// collides).
func samePort(a, b string) bool {
	port := func(addr string) string {
		if addr == "" {
			return "5540"
		}
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			return addr
		}
		return p
	}
	pa, pb := port(a), port(b)
	return pa != "0" && pa == pb
}

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

// nodeEntries are the pages' device nodes, the default first
// (fabricui.Options.Nodes with Options.Node nil: the first entry is the
// default node).
func nodeEntries(halves []bridgeHalf) []fabricui.NodeEntry {
	out := make([]fabricui.NodeEntry, 0, len(halves))
	for i := range halves {
		h := &halves[i]
		out = append(out, fabricui.NodeEntry{Key: h.spec.Key, Label: h.spec.Label, Kind: "bridge", Source: h.node})
	}
	return out
}
