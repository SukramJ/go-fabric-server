// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	// The pure-Go SQLite driver the controller's database runs on, as
	// go-fabric's reference controller opens it (examples/reference-controller/cli/stack.go:20-21).
	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric-server/controllerapi"
	"github.com/SukramJ/go-fabric-server/internal/version"
	"github.com/SukramJ/go-fabric-server/protocol"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
	expvarmetrics "github.com/SukramJ/go-fabric/node/metrics/expvar"
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
	ShowVersion             bool
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
	fs.BoolVar(&c.Bridge, "bridge", false, "run the bridge half (/bridge; arrives with S2)")
	fs.BoolVar(&c.ShowVersion, "version", false, "print the version and exit")
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
	if c.Bridge {
		return errors.New("--bridge: the bridge half arrives with go-fabric-server S2")
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
	if c.Controller {
		mode, err := attestation.ParseTrustMode(c.Trust)
		if err != nil || mode == attestation.None {
			return fmt.Errorf("--trust %q, want production or development", c.Trust)
		}
		snap, err := snapshot.Embedded()
		if snap == nil {
			return fmt.Errorf("the embedded DCL snapshot: %w", err)
		}
		db, err := sql.Open("sqlite", "file:"+filepath.Join(c.StoragePath, "fabric-server.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
		if err != nil {
			return err
		}
		defer db.Close()
		api, err = controllerapi.New(controllerapi.Options{
			Version: version.Version, GoFabricVersion: version.GoFabric(), StoragePath: c.StoragePath,
			DisableAutoSubscription: c.DisableAutoSubscription, DefaultFabricLabel: c.DefaultFabricLabel,
			Logger: log, LogLevel: lvl, Metrics: expvarmetrics.New("fabricserver."),
		})
		if err != nil {
			return err
		}
		cfg := controller.Config{
			DB: db, Attestation: attestation.Trust(mode, snap, nil), Listen: c.MatterListen,
			VendorID: uint16(c.VendorID), Label: api.FabricLabel(), Logger: log, //nolint:gosec // checked above
		}
		// go-fabric's controller takes one set of hooks for every node
		// (controller.Config.Node.Hooks); the per-node form is
		// client.RegistryConfig.HooksFor, which controller.Config does not
		// expose (report S1, Questions Q2).
		cfg.Node.Hooks = api.Hooks("")
		ctrl, err = controller.New(ctx, cfg)
		if err != nil {
			return fmt.Errorf("controller: %w", err)
		}
		defer func() { _ = ctrl.Close(context.WithoutCancel(ctx)) }()
		if err := api.Start(ctx, ctrl); err != nil {
			return err
		}
	}

	ln, err := listen.Open(ctx)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: Mux(api, c.Token, version.Version), ReadHeaderTimeout: 10 * time.Second}
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
	_ = srv.Shutdown(shutdown)
	if listen.Network == "unix" {
		_ = os.Remove(listen.Address)
	}
	log.Info("fabricserver.stopped")
	return nil
}
