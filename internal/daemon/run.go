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
	"time"

	"github.com/SukramJ/go-fabric-ui/fabricui"
	"github.com/SukramJ/go-fabric-ui/fabricui/gofabric"
	"github.com/SukramJ/go-fabric/ble/bluez"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
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
	ShowVersion         bool
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
	// Web is the address of the pages (go-fabric-ui: the node family over
	// the bridge node, the controller family over the controller); empty
	// serves none. WebAdmin lets the pages write (token mode), WebAdminSource
	// admits only requests from that address as an admin (an Ingress
	// proxy); without either the pages are read-only.
	Web            string
	WebAdmin       bool
	WebAdminSource string
}

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
	fs.DurationVar(&c.BridgeResultTimeout, "bridge-result-timeout", 5*time.Second, "how long a write or command event waits for the bridge client's result")
	fs.BoolVar(&c.ShowVersion, "version", false, "print the version and exit")
	fs.StringVar(&c.HealthCheck, "health-check", "", "probe this /health URL and exit 0 when healthy, instead of running")
	fs.BoolVar(&c.EnableTestNetDCL, "enable-test-net-dcl", false, "trust the CSA test roots besides the production DCL (as --trust development)")
	fs.StringVar(&c.BluetoothAdapter, "bluetooth-adapter", "", "the HCI id of the BLE adapter (0 for hci0) commission_with_code may commission over; empty is no BLE (Linux, BlueZ)")
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
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: lvl}))
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
		central, err := bleCentral(c.BluetoothAdapter)
		if err != nil {
			return err
		}
		if cl, ok := central.(io.Closer); ok {
			defer func() { _ = cl.Close() }()
		}
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
		ctrl, err = controller.New(ctx, cfg)
		if err != nil {
			return fmt.Errorf("controller: %w", err)
		}
		defer func() {
			// The pages' hooks stop their node_updated timers before the
			// controller goes (gofabric.Hooks.Close, go-fabric-ui v0.2.0).
			if pageHooks != nil {
				pageHooks.Close()
			}
			_ = ctrl.Close(context.WithoutCancel(ctx))
		}()
		if err := api.Start(ctx, ctrl); err != nil {
			return err
		}
		if pageHooks != nil {
			ctrlPages = controllerSource(ctrl, api, pageHooks, log)
		}
	}

	var br *bridgeapi.Server
	var bridgeNode *node.Node
	ev := fabricui.NewEvents()
	if c.Bridge {
		br, bridgeNode, err = startBridge(ctx, c, log)
		if err != nil {
			return err
		}
		defer func() { _ = bridgeNode.Stop(context.WithoutCancel(ctx)) }()
	}
	if c.Web != "" {
		stop, err := serveWeb(ctx, c, bridgeNode, ctrlPages, ev, log)
		if err != nil {
			return err
		}
		defer stop()
	}

	ln, err := listen.Open(ctx)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: MuxWith(api, br, c.Token, version.Version), ReadHeaderTimeout: 10 * time.Second}
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
	if br != nil {
		br.Shutdown(shutdown)
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

// bridgeConfig is the bridge node's configuration from the flags: its
// commissioning identity, its store under --storage-path, the daemon's
// logger.
func bridgeConfig(c Config, log *slog.Logger) (node.Config, error) {
	// Widths only: node.New validates the values (Config.Validate).
	if c.BridgePasscode > math.MaxUint32 || c.BridgeDiscriminator > math.MaxUint16 || c.BridgeVendorID > math.MaxUint16 || c.BridgeProductID > math.MaxUint16 {
		return node.Config{}, errors.New("--bridge-passcode, --bridge-discriminator, --bridge-vendor-id or --bridge-product-id is wider than its field")
	}
	return node.Config{
		Network: node.Network{Listen: c.BridgeListen},
		Commissioning: node.Commissioning{
			Passcode: uint32(c.BridgePasscode), Discriminator: uint16(c.BridgeDiscriminator), //nolint:gosec // checked above
			PBKDFIterations: 1000, Salt: bridgeSalt,
		},
		ProductDescription: node.ProductDescription{VendorID: uint16(c.BridgeVendorID), ProductID: uint16(c.BridgeProductID)}, //nolint:gosec // checked above
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric-server", ProductName: "fabric-server bridge", NodeLabel: c.BridgeLabel,
			SerialNumber: "FABRIC-SERVER-BRIDGE", SoftwareVersionString: version.Version,
		},
		Store:  node.Store{Path: filepath.Join(c.StoragePath, "bridge.db")},
		Logger: log,
	}, nil
}

// startBridge builds the bridge node with the bridge half's observers,
// starts it and binds the half to it.
func startBridge(ctx context.Context, c Config, log *slog.Logger) (*bridgeapi.Server, *node.Node, error) {
	cfg, err := bridgeConfig(c, log)
	if err != nil {
		return nil, nil, err
	}
	br := bridgeapi.New(bridgeapi.Options{
		Version: version.Version, GoFabricVersion: version.GoFabric(), ResultTimeout: c.BridgeResultTimeout,
		Logger: log, Metrics: expvarmetrics.New("fabricserver.bridge."),
	})
	// The node pages' event stream observes the node itself (go-fabric-ui
	// v0.1.2: the handler's Node.Observe), so only the half's observers go
	// into the config.
	cfg.Observers = br.Observers()
	n, err := node.New(cfg) //nolint:contextcheck // the node's constructor takes no context
	if err != nil {
		return nil, nil, fmt.Errorf("bridge node: %w", err)
	}
	if err := n.Start(ctx); err != nil {
		_ = n.Stop(context.WithoutCancel(ctx))
		return nil, nil, fmt.Errorf("bridge node: %w", err)
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

// bleCentral is the BlueZ central of --bluetooth-adapter
// (github.com/SukramJ/go-fabric/ble/bluez); nil without the flag. The flag
// takes matterjs-server's HCI id ("0" is hci0); an adapter name (hci1) is
// passed as it is. Opening fails without system-bus rights to org.bluez or
// on a platform without BlueZ.
func bleCentral(adapter string) (ble.Central, error) {
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
// of --bluetooth-adapter, which commission_with_code commissions over.
func controllerOptions(c Config, log *slog.Logger, lvl *slog.LevelVar, central ble.Central) controllerapi.Options {
	return controllerapi.Options{
		Version: version.Version, GoFabricVersion: version.GoFabric(), StoragePath: c.StoragePath,
		DisableAutoSubscription: c.DisableAutoSubscription, DefaultFabricLabel: c.DefaultFabricLabel,
		Logger: log, LogLevel: lvl, Metrics: expvarmetrics.New("fabricserver."), BLE: central,
	}
}
