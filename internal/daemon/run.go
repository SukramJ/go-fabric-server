// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric-ui/fabricui/gofabric"
	"github.com/SukramJ/go-fabric/ble/bluez"
	"github.com/SukramJ/go-fabric/ble/esphome"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/camera"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
	"github.com/SukramJ/go-fabric/controller/topology"
	"github.com/SukramJ/go-fabric/node"
	expvarmetrics "github.com/SukramJ/go-fabric/node/metrics/expvar"

	"github.com/SukramJ/go-fabric-server/bridgeapi"
	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/internal/version"
	"github.com/SukramJ/go-fabric-server/protocol"
)

// Config is the daemon's configuration: the flags, each with an
// environment variable.
type Config struct {
	Listen                  string
	Token                   string
	StoragePath             string
	MatterListen            string
	VendorID                uint
	Trust                   string
	LogLevel                string
	DefaultFabricLabel      string
	DisableAutoSubscription bool
	Controller              bool
	Bridge                  bool
	// The bridge half's node (--bridge).
	BridgeListen        string
	BridgePasscode      uint
	BridgeDiscriminator uint
	BridgeVendorID      uint
	BridgeProductID     uint
	BridgeLabel         string
	BridgeResultTimeout time.Duration
	// BridgeMDNSInterfaces are the host interfaces every device node's
	// mDNS responder announces and answers on, comma-separated
	// (--bridge-mdns-interfaces; go-fabric node.MDNS.Interfaces): empty is
	// every multicast-capable interface. One host, one set: the configured
	// node and every registry node take it.
	BridgeMDNSInterfaces string
	// The bridge node's surface policies (node.Config.Surface, go-fabric
	// ADR 0030), comma-separated: the enabled "Ahead of matter.js"
	// register IDs (--bridge-extensions), the specification level
	// (--bridge-spec-level, "" the pin's) and the controller profiles
	// (--bridge-profile).
	BridgeExtensions string
	BridgeSpecLevel  string
	BridgeProfile    string
	// NodeLimit is the most device nodes the pages may create beside the
	// --bridge-* one (--node-limit): the node registry's (nodes.json)
	// cap.
	NodeLimit   int
	ShowVersion bool
	// HealthCheck, when set, is a URL to probe instead of running: exit 0
	// when it answers 200 (the image's HEALTHCHECK; distroless has no
	// shell or curl).
	HealthCheck string
	// EnableTestNetDCL trusts the CSA test roots as well (matterjs-server's
	// --enable-test-net-dcl): --trust development.
	EnableTestNetDCL bool
	// BluetoothAdapter is the HCI id of the BLE adapter commission_with_code
	// may commission over (matterjs-server's --bluetooth-adapter); empty
	// is no BLE.
	BluetoothAdapter string
	// BluetoothProxies are the ESPHome Bluetooth proxies commission_with_code
	// may commission through (--bluetooth-proxy, repeatable;
	// github.com/SukramJ/go-fabric/ble/esphome); one BLE backend, so not
	// with BluetoothAdapter.
	BluetoothProxies []esphome.Proxy
	// Web is the address of the pages (go-fabric-ui: the node family over
	// the bridge node, the controller family over the controller); empty
	// serves none. WebAdmin lets the pages write (token mode), WebAdminSource
	// admits only requests from that address as an admin (an Ingress
	// proxy); without either the pages are read-only.
	Web            string
	WebAdmin       bool
	WebAdminSource string
}

// The migrations of the flags the node registry replaced (go-fabric-ui
// contract 0.6.0: the first node is configuration, every further node is
// registry state created in the pages).
const (
	removedBridgeNode  = "--bridge-node: create further device nodes in the pages (--web with --web-admin or --web-admin-source, the node selector's \"Create node…\"); each keeps its own port, discriminator, passcode and store"
	removedNodeDefault = "--node-default: the default node is the --bridge-* one; create further device nodes in the pages (--web, the node selector's \"Create node…\")"
)

// env returns the environment variable name of a flag.
func env(flagName string) string {
	return "FABRIC_SERVER_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// ParseFlags reads the flags; an environment variable FABRIC_SERVER_<FLAG>
// supplies a flag that is not given.
func ParseFlags(args []string, stderr io.Writer) (Config, error) {
	var c Config
	fs := flag.NewFlagSet("fabric-server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.Listen, "listen", "", "the API listener: host:port, or unix:/path (default unix:$XDG_RUNTIME_DIR/fabric-server.sock)")
	fs.StringVar(&c.Token, "token", "", "the bearer token every request must carry (required on a TCP listener beyond loopback)")
	fs.StringVar(&c.StoragePath, "storage-path", defaultStorage(), "the directory of the controller's database and the server's settings")
	fs.StringVar(&c.MatterListen, "matter-listen", "", "the controller's UDP address for Matter (default [::]:0)")
	fs.UintVar(&c.VendorID, "vendor-id", 0, "our fabric's vendor id on the first run (default 0xFFF1)")
	fs.StringVar(&c.Trust, "trust", "production", "the attestation roots: production (the DCL snapshot) or development (plus the CSA test roots)")
	fs.StringVar(&c.LogLevel, "log-level", "info", "the log level: critical, error, warning, notice, info or debug")
	fs.StringVar(&c.DefaultFabricLabel, "default-fabric-label", "", "pin the fabric label; set_default_fabric_label then has no effect")
	fs.BoolVar(&c.DisableAutoSubscription, "disable-auto-subscription", false, "pair new nodes without the server's own subscription; enables subscribe and unsubscribe")
	fs.BoolVar(&c.Controller, "controller", true, "run the controller half (/ws)")
	fs.BoolVar(&c.Bridge, "bridge", false, "run the bridge half (/bridge): a bridge node whose devices a client declares")
	fs.StringVar(&c.BridgeListen, "bridge-listen", "", "the bridge node's UDP address for Matter (default :5540)")
	fs.UintVar(&c.BridgePasscode, "bridge-passcode", 20202021, "the bridge node's setup passcode (the public test value by default: change it before pairing for real)")
	fs.UintVar(&c.BridgeDiscriminator, "bridge-discriminator", 3840, "the bridge node's discriminator")
	fs.UintVar(&c.BridgeVendorID, "bridge-vendor-id", 0xFFF1, "the bridge node's vendor id (the CSA test vendor by default)")
	fs.UintVar(&c.BridgeProductID, "bridge-product-id", 0x8001, "the bridge node's product id")
	fs.StringVar(&c.BridgeLabel, "bridge-label", "fabric-server bridge", "the bridge node's NodeLabel")
	fs.StringVar(&c.BridgeMDNSInterfaces, "bridge-mdns-interfaces", "", "the host interfaces the device nodes announce and answer mDNS on, name[,name...] (a dual-homed host behind an mDNS reflector names its LAN interface); empty is every multicast-capable interface")
	fs.DurationVar(&c.BridgeResultTimeout, "bridge-result-timeout", 5*time.Second, "how long a write or command event waits for the bridge client's result")
	fs.StringVar(&c.BridgeExtensions, "bridge-extensions", "", "the ahead-of-matter.js register IDs the bridge node enables, id[,id...] (go-fabric notes/parity/by_design.md); a declaration selecting a feature of one not enabled is refused; known: "+strings.Join(extensionList(), ", "))
	fs.StringVar(&c.BridgeSpecLevel, "bridge-spec-level", "", "the specification level the bridge node composes at (default the pin's): "+levelList())
	fs.StringVar(&c.BridgeProfile, "bridge-profile", "", "the controller profiles the bridge node composes for, name[,name...]: "+profileList())
	fs.IntVar(&c.NodeLimit, "node-limit", defaultNodeLimit, "the most device nodes the pages may create beside the --bridge-* one (the node registry, nodes.json)")
	fs.Var(removedFlag(removedBridgeNode), "bridge-node", "removed: "+removedBridgeNode)
	fs.Var(removedFlag(removedNodeDefault), "node-default", "removed: "+removedNodeDefault)
	fs.BoolVar(&c.ShowVersion, "version", false, "print the version and exit")
	fs.StringVar(&c.HealthCheck, "health-check", "", "probe this /health URL and exit 0 when healthy, instead of running")
	fs.BoolVar(&c.EnableTestNetDCL, "enable-test-net-dcl", false, "trust the CSA test roots besides the production DCL (as --trust development)")
	fs.StringVar(&c.BluetoothAdapter, "bluetooth-adapter", "", "the HCI id of the BLE adapter (0 for hci0) commission_with_code may commission over; empty is no BLE (Linux, BlueZ)")
	fs.Var((*proxyList)(&c.BluetoothProxies), "bluetooth-proxy", "an ESPHome Bluetooth proxy commission_with_code may commission through: host[:port][,key] (port 6053, key the base64 API encryption key); repeatable; not with --bluetooth-adapter")
	fs.StringVar(&c.Web, "web", "", "serve the pages (go-fabric-ui) on this address — the bridge node's with --bridge, the controller's with --controller; read-only unless --web-admin or --web-admin-source")
	fs.BoolVar(&c.WebAdmin, "web-admin", false, "let the pages write, show the pairing code and use the controller's admin routes (the page's token mode)")
	fs.StringVar(&c.WebAdminSource, "web-admin-source", "", "admit page requests from this IP address only, as an admin (an Ingress proxy: 172.30.32.2 in a Home Assistant add-on)")
	set := map[string]bool{}
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var errs []error
	fs.VisitAll(func(f *flag.Flag) {
		if set[f.Name] {
			return
		}
		if v, ok := os.LookupEnv(env(f.Name)); ok {
			if err := fs.Set(f.Name, v); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", env(f.Name), err))
			}
		}
	})
	return c, errors.Join(errs...)
}

// defaultStorage is $STATE_DIRECTORY (systemd's StateDirectory), else
// $XDG_STATE_HOME/fabric-server, else ./data.
func defaultStorage() string {
	if d := os.Getenv("STATE_DIRECTORY"); d != "" {
		return strings.Split(d, ":")[0]
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "fabric-server")
	}
	return "data"
}

var levels = map[string]slog.Level{
	"critical": slog.LevelError + 4, "error": slog.LevelError, "warning": slog.LevelWarn,
	"notice": slog.LevelInfo + 2, "info": slog.LevelInfo, "debug": slog.LevelDebug,
}

// Run runs the daemon until ctx ends.
//
//nolint:funlen // the composition root builds everything in one place, in order
func Run(ctx context.Context, c Config, stderr io.Writer) error {
	if c.ShowVersion {
		_, _ = fmt.Fprintln(stderr, version.String())
		return nil
	}
	if c.HealthCheck != "" {
		return probe(ctx, c.HealthCheck, c.Token)
	}
	lvl := new(slog.LevelVar)
	l, ok := levels[strings.ToLower(c.LogLevel)]
	if !ok {
		return fmt.Errorf("--log-level %q, want critical, error, warning, notice, info or debug", c.LogLevel)
	}
	lvl.Set(l)
	log, ring := newLogger(stderr, lvl)
	ops := operations{ring: ring, started: time.Now()}
	listen, err := protocol.ParseListen(c.Listen)
	if err != nil {
		return err
	}
	if err := protocol.CheckListen(listen, c.Token); err != nil {
		return err
	}
	if err := os.MkdirAll(c.StoragePath, 0o700); err != nil {
		return fmt.Errorf("--storage-path: %w", err)
	}
	if c.VendorID > 0xFFFF {
		return fmt.Errorf("--vendor-id %d is above 0xFFFF", c.VendorID)
	}
	log.Info("fabricserver.start", slog.String("version", version.Version), slog.String("go_fabric", version.GoFabric()), slog.String("listen", listen.String()))

	var api *controllerapi.Server
	var ctrl *controller.Controller
	var ctrlPages fabricui.ControllerSource
	if c.Controller {
		trust, err := trustStore(c)
		if err != nil {
			return err
		}
		central, err := bleCentral(c, log)
		if err != nil {
			return err
		}
		// The central goes before the controller (registered again after
		// the controller's close below, so it runs first); once, also when
		// the start fails before the controller exists.
		closeCentral := sync.OnceFunc(func() {
			if cl, ok := central.(io.Closer); ok {
				_ = cl.Close()
			}
		})
		defer closeCentral()
		// The controller's database through go-fabric's opener (Q11
		// closed in v0.12.1): no SQLite driver of our own.
		db, err := controller.OpenDB(filepath.Join(c.StoragePath, "fabric-server.db"))
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		api, err = controllerapi.New(controllerOptions(c, log, lvl, central))
		if err != nil {
			return err
		}
		cfg := controller.Config{
			DB: db, Attestation: trust, Listen: c.MatterListen,
			VendorID: uint16(c.VendorID), Label: api.FabricLabel(), Logger: log, //nolint:gosec // checked above
			// One hook set per node (controller.Config.HooksFor, Q2
			// closed in v0.12.1): node_event names its node.
			HooksFor: api.Hooks,
		}
		// The controller pages' stream: go-fabric-ui's hooks chained
		// after the half's (Hooks.Wrap: each of api.Hooks(id) runs, then
		// the stream's; Event stays the half's), never instead of them.
		var pageHooks *gofabric.Hooks
		if c.Web != "" {
			pageHooks = gofabric.NewHooks()
			cfg.HooksFor = pageHooks.Wrap(cfg.HooksFor)
		}
		// The network graph of the nodes (go-fabric controller/topology,
		// matterjs-server's NetworkTopologyService): built with or without
		// --web, get_network_topology needs it. Its change hooks wrap the
		// chain composed above, so both halves' hooks run first.
		var changes topology.Changes
		changes.Install(&cfg)
		// The controller side of cameras, always on, as matterjs-server
		// composes its camera controller endpoint unconditionally
		// (ControllerNode.ts:147, as go-fabric controller/serve.go:55-59
		// cites it; brief S8 §1, owner 2026-11-08): the controller serves
		// the Camera Controller endpoint with its requestor, and opens
		// outgoing TCP, which camera.New requires and which falls back to
		// UDP for a peer without TCP (controller/controller.go:177-181).
		// The cameras' reports wrap the chain composed above, so its hooks
		// run first.
		cfg.Camera = &controller.CameraConfig{}
		cfg.TCP = controller.TCPConfig{Outgoing: true}
		var reports camera.Reports
		reports.Install(&cfg)
		ctrl, err = controller.New(ctx, cfg)
		if err != nil {
			return fmt.Errorf("controller: %w", err)
		}
		topo := topology.New(ctrl, topology.Config{Changes: &changes})
		var cam *camera.Service
		defer func() {
			// The camera service ends the sessions the closing sockets did
			// not release (api.Shutdown closes every socket first), then
			// the graph stops before the pages' hooks and the controller
			// (Service.Close detaches it from the hooks and cancels a
			// refresh in flight); the pages' hooks stop their
			// node_updated timers and their topology subscription before
			// the controller goes (gofabric.Hooks.Close).
			if cam != nil {
				_ = cam.Close(context.WithoutCancel(ctx))
			}
			topo.Close()
			if pageHooks != nil {
				pageHooks.Close()
			}
			_ = ctrl.Close(context.WithoutCancel(ctx))
		}()
		defer closeCentral()
		cam, err = camera.New(ctx, ctrl, camera.Config{Reports: &reports, Observer: api.CameraObserver(), Logger: log})
		if err != nil {
			return fmt.Errorf("camera: %w", err)
		}
		if err := api.Start(ctx, ctrl, controllerapi.Services{Topology: topo, Camera: cam}); err != nil {
			return err
		}
		if pageHooks != nil {
			ctrlPages = controllerSource(ctrl, topo, api, pageHooks, log)
		}
	}

	// The device nodes: the default one of the --bridge-* flags, then
	// the node registry's (nodes.json), each its own node, store, mDNS
	// advertiser and bridge half (startBridge), started, erased and ended
	// by one lifecycle. The registry's nodes stop before the default one;
	// each stop stops the half's current node, the one an erase brought
	// back if any.
	var def *bridgeHalf
	var reg *nodeRegistry
	var life *nodeLifecycle
	routes := newBridgeRoutes()
	if c.Bridge {
		if c.NodeLimit < 0 {
			return fmt.Errorf("--node-limit %d is negative", c.NodeLimit)
		}
		spec := primaryNode(c)
		life = newNodeLifecycle(ctx, func(ctx context.Context, s nodeSpec) (liveNode, error) {
			br, n, err := startBridge(ctx, c, s, log)
			if err != nil {
				return liveNode{}, err
			}
			return liveNode{node: n, srv: br}, nil
		}, routes, log)
		def, err = life.startHalf(spec)
		if err != nil {
			return err
		}
		defer def.stop(context.WithoutCancel(ctx))
		reg, err = openRegistry(registryOptions{dir: c.StoragePath, limit: c.NodeLimit, primary: spec, life: life})
		if err != nil {
			return err
		}
		reg.startAll()
		defer reg.stopAll(context.WithoutCancel(ctx))
	}
	ev := fabricui.NewEvents()
	if c.Web != "" {
		stop, err := serveWeb(ctx, c, life, def, reg, ctrlPages, ev, ops, log)
		if err != nil {
			return err
		}
		defer stop()
	}

	ln, err := listen.Open(ctx)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: muxRoutes(api, routes, c.Token, version.Version), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	log.Info("fabricserver.listening", slog.String("listen", listen.String()))
	select {
	case <-ctx.Done():
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if api != nil {
		api.Shutdown(shutdown)
	}
	for _, h := range routes.all() {
		h.Server.Shutdown(shutdown)
	}
	_ = srv.Shutdown(shutdown)
	if listen.Network == "unix" {
		_ = os.Remove(listen.Address)
	}
	log.Info("fabricserver.stopped")
	return nil
}

// bridgeSalt is the PBKDF salt of the bridge node's passcode verifier, a
// constant of the daemon as of any host (go-fabric's
// docs/guides/embed-in-a-daemon.md §2).
var bridgeSalt = []byte("go-fabric-server bridge node")

// The BasicInformation names of every device node.
const (
	bridgeVendorName  = "go-fabric-server"
	bridgeProductName = "fabric-server bridge"
)

// bridgeConfig is a device node's configuration: its commissioning
// identity from its spec (the --bridge-* flags or its registry entry),
// its store under --storage-path, the daemon's logger, the
// surface policies and the mDNS interfaces every node shares
// (surfacePolicies, mdnsInterfaces).
func bridgeConfig(c Config, s nodeSpec, log *slog.Logger) (node.Config, error) {
	// Widths only: node.New validates the values (Config.Validate).
	if s.Passcode > math.MaxUint32 || s.Discriminator > math.MaxUint16 || s.VendorID > math.MaxUint16 || s.ProductID > math.MaxUint16 {
		return node.Config{}, errors.New("--bridge-passcode, --bridge-discriminator, --bridge-vendor-id or --bridge-product-id is wider than its field")
	}
	policies, err := surfacePolicies(c)
	if err != nil {
		return node.Config{}, err
	}
	return node.Config{
		Network: node.Network{Listen: s.Listen, MDNS: node.MDNS{Interfaces: mdnsInterfaces(c.BridgeMDNSInterfaces)}},
		Commissioning: node.Commissioning{
			Passcode: uint32(s.Passcode), Discriminator: uint16(s.Discriminator), //nolint:gosec // checked above
			PBKDFIterations: 1000, Salt: bridgeSalt,
		},
		ProductDescription: node.ProductDescription{VendorID: uint16(s.VendorID), ProductID: uint16(s.ProductID)}, //nolint:gosec // checked above
		BasicInformation: node.BasicInformation{
			VendorName: bridgeVendorName, ProductName: bridgeProductName, NodeLabel: s.Label,
			SerialNumber: s.Serial, SoftwareVersionString: version.Version,
		},
		Store:   node.Store{Path: filepath.Join(c.StoragePath, s.Store)},
		Logger:  log,
		Surface: policies,
	}, nil
}

// mdnsInterfaces is --bridge-mdns-interfaces as a list, each name
// trimmed; nil for none. An empty name stays, so node.Config.Validate
// refuses it ("eth0,," is a typo, not every interface).
func mdnsInterfaces(flag string) []string {
	if strings.TrimSpace(flag) == "" {
		return nil
	}
	var out []string
	for name := range strings.SplitSeq(flag, ",") {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// startBridge builds one device node with its bridge half's observers,
// starts it and binds the half to it.
func startBridge(ctx context.Context, c Config, s nodeSpec, log *slog.Logger) (*bridgeapi.Server, *node.Node, error) {
	if s.Key != fabricui.DefaultNodeKey {
		log = log.With(slog.String("node", s.Key))
	}
	cfg, err := bridgeConfig(c, s, log)
	if err != nil {
		return nil, nil, err
	}
	br := bridgeapi.New(bridgeapi.Options{
		Version: version.Version, GoFabricVersion: version.GoFabric(), ResultTimeout: c.BridgeResultTimeout,
		Logger: log, Metrics: expvarmetrics.New(bridgeMetricsPrefix(s.Key)),
		// The node's policies: the half builds every declared device under
		// them (node.ErrPolicyMismatch otherwise).
		Surface: cfg.Surface, Node: s.Key,
	})
	// The node pages' event stream observes the node itself (go-fabric-ui
	// v0.1.2: the handler's Node.Observe), so only the half's observers go
	// into the config.
	cfg.Observers = br.Observers()
	n, err := node.New(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", nodeName(s.Key), err)
	}
	if err := n.Start(ctx); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, nil, fmt.Errorf("%s: %w", nodeName(s.Key), err)
	}
	if err := br.Start(ctx, n); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, nil, err
	}
	// The pairing codes carry the passcode: never in the log (brief S3,
	// hard rule 4). A client reads them with pairing_codes on /bridge, an
	// admin on the node pages.
	log.Info("fabricserver.bridge.ready")
	return br, n, nil
}

// nodeName names a device node in a start error: "bridge node" for the
// default node, as before several nodes, "bridge node <key>" for another.
func nodeName(key string) string {
	if key == fabricui.DefaultNodeKey {
		return "bridge node"
	}
	return "bridge node " + key
}

// bridgeMetricsPrefix is a bridge half's expvar prefix: the default
// node's as before several nodes, another's under its key (expvar names
// are process-wide; one prefix would merge their counters).
func bridgeMetricsPrefix(key string) string {
	if key == fabricui.DefaultNodeKey {
		return "fabricserver.bridge."
	}
	return "fabricserver.bridge." + key + "."
}

// probe is --health-check: GET url (with the token) and want 200.
func probe(ctx context.Context, url, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: %s", resp.Status)
	}
	return nil
}

// trustStore is the attestation roots of --trust and --enable-test-net-dcl:
// production is controller.ProductionTrust (Q10 closed in v0.12.1),
// development adds the CSA test roots to the embedded DCL snapshot.
func trustStore(c Config) (attestation.PAAStore, error) {
	trust := c.Trust
	if c.EnableTestNetDCL {
		trust = "development"
	}
	mode, err := attestation.ParseTrustMode(trust)
	if err != nil || mode == attestation.None {
		return nil, fmt.Errorf("--trust %q, want production or development", c.Trust)
	}
	if mode == attestation.Production {
		return controller.ProductionTrust()
	}
	snap, err := snapshot.Embedded()
	if snap == nil {
		return nil, fmt.Errorf("the embedded DCL snapshot: %w", err)
	}
	return attestation.Trust(mode, snap, nil), nil
}

// newCentral opens a BlueZ central; a variable so a test can stand in
// for the system bus (no BLE in CI).
var newCentral = bluez.NewCentral

// newProxyCentral opens the central over ESPHome Bluetooth proxies; a
// variable so a test can stand in for the proxies' sockets.
var newProxyCentral = func(proxies []esphome.Proxy, opts esphome.Options) (ble.Central, error) {
	c, err := esphome.NewCentral(proxies, opts)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// proxyList is --bluetooth-proxy: each use appends one proxy, parsed by
// esphome.ParseProxy (host[:port][,key]).
type proxyList []esphome.Proxy

// String names the proxies by address only: never a key.
func (l *proxyList) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(proxyAddrs(*l), " ")
}

func (l *proxyList) Set(spec string) error {
	p, err := esphome.ParseProxy(spec)
	if err != nil {
		return err
	}
	*l = append(*l, p)
	return nil
}

func proxyAddrs(proxies []esphome.Proxy) []string {
	addrs := make([]string, len(proxies))
	for i, p := range proxies {
		addrs[i] = p.Addr
	}
	return addrs
}

// bleCentral is the BLE central commission_with_code commissions over:
// the ESPHome proxies' of --bluetooth-proxy, the BlueZ adapter's of
// --bluetooth-adapter, or nil without either; both is a start-up error
// (one BLE backend; matterjs-server instead prefers --ble-proxy with a
// warning, MatterServer.ts:88-91, but its proxy is a different mechanism).
func bleCentral(c Config, log *slog.Logger) (ble.Central, error) {
	if len(c.BluetoothProxies) > 0 {
		if c.BluetoothAdapter != "" {
			return nil, errors.New("--bluetooth-adapter and --bluetooth-proxy: one BLE backend, not both")
		}
		log.Info("fabricserver.ble_proxies", slog.Any("proxies", proxyAddrs(c.BluetoothProxies)))
		central, err := newProxyCentral(c.BluetoothProxies, esphome.Options{Logger: log})
		if err != nil {
			return nil, fmt.Errorf("--bluetooth-proxy: %w", err)
		}
		return central, nil
	}
	return adapterCentral(c.BluetoothAdapter)
}

// adapterCentral is the BlueZ central of --bluetooth-adapter
// (github.com/SukramJ/go-fabric/ble/bluez); nil without the flag. The flag
// takes matterjs-server's HCI id ("0" is hci0); an adapter name (hci1) is
// passed as it is. Opening fails without system-bus rights to org.bluez or
// on a platform without BlueZ.
func adapterCentral(adapter string) (ble.Central, error) {
	if adapter == "" {
		return nil, nil //nolint:nilnil // no adapter is no central and no error
	}
	name := adapter
	if _, err := strconv.ParseUint(adapter, 10, 8); err == nil {
		name = "hci" + adapter
	}
	central, err := newCentral(name)
	if err != nil {
		return nil, fmt.Errorf("--bluetooth-adapter %s: %w", adapter, err)
	}
	return central, nil
}

// controllerOptions are the controller half's options; BLE is the central
// of --bluetooth-proxy or --bluetooth-adapter, which commission_with_code
// commissions over.
func controllerOptions(c Config, log *slog.Logger, lvl *slog.LevelVar, central ble.Central) controllerapi.Options {
	return controllerapi.Options{
		Version: version.Version, GoFabricVersion: version.GoFabric(), StoragePath: c.StoragePath,
		DisableAutoSubscription: c.DisableAutoSubscription, DefaultFabricLabel: c.DefaultFabricLabel,
		Logger: log, LogLevel: lvl, Metrics: expvarmetrics.New("fabricserver."), BLE: central,
	}
}
