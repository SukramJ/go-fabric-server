// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func writeKey(t *testing.T, path string, b byte) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Repeat(string("0123456789abcdef"[b%16]), 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSealingKeyFile(t *testing.T) {
	dir := t.TempDir()
	if s, err := openSealing(Config{}, discard()); s != nil || err != nil {
		t.Fatalf("no flag: %v %v", s, err)
	}
	if _, err := openSealing(Config{SealKeyPreviousFile: filepath.Join(dir, "p")}, discard()); err == nil || !strings.Contains(err.Error(), "needs --seal-key-file") {
		t.Fatalf("previous alone: %v", err)
	}
	// A missing file is created: 64 hex digits, 0600.
	path := filepath.Join(dir, "seal.key")
	s, err := openSealing(Config{SealKeyFile: path}, discard())
	if err != nil || s == nil || !s.created || s.sealer() == nil {
		t.Fatalf("create: %+v %v", s, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created file: %v %v", info, err)
	}
	b, _ := os.ReadFile(path)
	if len(bytes.TrimSpace(b)) != 64 {
		t.Fatalf("created key %q", b)
	}
	// Read again: not created, the same key seals what the first opens.
	s2, err := openSealing(Config{SealKeyFile: path}, discard())
	if err != nil || s2.created {
		t.Fatalf("reopen: %+v %v", s2, err)
	}
	sealed, err := s.sealer().Seal([]byte("x"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := s2.sealer().Open(sealed, []byte("aad")); err != nil || string(plain) != "x" {
		t.Fatalf("the reread key opens: %q %v", plain, err)
	}
	// Refused: group or others may read; not 64 hex digits; a previous
	// key that is missing or the same.
	loose := filepath.Join(dir, "loose.key")
	writeKey(t, loose, 1)
	if err := os.Chmod(loose, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := openSealing(Config{SealKeyFile: loose}, discard()); err == nil || !strings.Contains(err.Error(), "readable by group or others") {
		t.Fatalf("mode 0640: %v", err)
	}
	bad := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(bad, []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSealing(Config{SealKeyFile: bad}, discard()); err == nil || !strings.Contains(err.Error(), "64 hex digits") {
		t.Fatalf("short key: %v", err)
	}
	if _, err := openSealing(Config{SealKeyFile: path, SealKeyPreviousFile: filepath.Join(dir, "gone")}, discard()); err == nil || !strings.Contains(err.Error(), "--seal-key-previous-file") {
		t.Fatalf("missing previous: %v", err)
	}
	if _, err := openSealing(Config{SealKeyFile: path, SealKeyPreviousFile: path}, discard()); err == nil || !strings.Contains(err.Error(), "nothing to rotate") {
		t.Fatalf("same previous: %v", err)
	}
}

// ctrlRig opens the controller's database as Run does — controller.OpenDB,
// resealController, Config.Sealer, explain — on a fabrictest site.
type ctrlRig struct {
	t    *testing.T
	site *fabrictest.Site
	path string
}

func (r *ctrlRig) open(seal *sealing, keep bool, use func(*controller.Controller)) error {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, statErr := os.Stat(r.path)
	db, err := controller.OpenDB(r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := seal.resealController(ctx, db, r.path, statErr == nil); err != nil {
		return err
	}
	cfg := r.site.ControllerConfig(fabrictest.ControllerOptions{Logger: discard()})
	cfg.DB, cfg.KeepOnboardingCodes, cfg.Sealer = db, keep, seal.sealer()
	c, err := controller.New(ctx, cfg)
	if err := seal.explain(err); err != nil {
		return err
	}
	if use != nil {
		use(c)
	}
	return c.Close(ctx)
}

// stored is the database file and its -wal, as the disk holds them.
func (r *ctrlRig) stored() []byte {
	var out []byte
	for _, p := range []string{r.path, r.path + "-wal"} {
		b, err := os.ReadFile(p)
		if err == nil {
			out = append(out, b...)
		}
	}
	return out
}

func mustSealing(t *testing.T, c Config) *sealing {
	t.Helper()
	s, err := openSealing(c, discard())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSealedControllerDatabase: a database with a kept onboarding code,
// written unsealed, is sealed and scrubbed by the first start with
// --seal-key-file (go-fabric controller/controller.go:168-170 at v0.28.0);
// without the key the start fails ErrNoSealer, under another key ErrUnseal
// with the file unchanged, and a key file the failing start created is
// removed again; --seal-key-previous-file rotates, twice harmlessly.
func TestSealedControllerDatabase(t *testing.T) {
	dir := t.TempDir()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: discard()})
	r := &ctrlRig{t: t, site: site, path: filepath.Join(dir, "fabric-server.db")}
	dev := site.AddDevice(fabrictest.Topology{})
	payload, err := dev.Node().SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	code := payload.Manual
	if err := r.open(nil, true, func(c *controller.Controller) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()}); err != nil {
			t.Fatalf("pair: %v", err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(r.stored(), []byte(code)) {
		t.Fatal("the unsealed database does not hold the kept code in clear: the scrub check below would prove nothing")
	}

	keyA := filepath.Join(dir, "a.key")
	writeKey(t, keyA, 1)
	if err := r.open(mustSealing(t, Config{SealKeyFile: keyA}), true, nil); err != nil {
		t.Fatalf("first sealed start: %v", err)
	}
	if bytes.Contains(r.stored(), []byte(code)) {
		t.Fatal("the kept code is still in clear in the file after the sealed start")
	}
	if err := r.open(mustSealing(t, Config{SealKeyFile: keyA}), true, nil); err != nil {
		t.Fatalf("sealed restart: %v", err)
	}
	err = r.open(nil, true, nil)
	if !errors.Is(err, controller.ErrNoSealer) || !strings.Contains(err.Error(), "set --seal-key-file") {
		t.Fatalf("start without the key: %v", err)
	}

	// Another key: refused, nothing written; the key file this start
	// created is removed again (Q2).
	before := r.stored()
	keyNew := filepath.Join(dir, "new.key")
	err = r.open(mustSealing(t, Config{SealKeyFile: keyNew}), true, nil)
	if !errors.Is(err, controller.ErrUnseal) || !strings.Contains(err.Error(), "removed again") {
		t.Fatalf("start under a new key: %v", err)
	}
	if _, serr := os.Stat(keyNew); !os.IsNotExist(serr) {
		t.Fatalf("the created key file stays: %v", serr)
	}
	if !bytes.Equal(before, r.stored()) {
		t.Fatal("a start under another key changed the file")
	}
	keyB := filepath.Join(dir, "b.key")
	writeKey(t, keyB, 2)
	err = r.open(mustSealing(t, Config{SealKeyFile: keyB}), true, nil)
	if !errors.Is(err, controller.ErrUnseal) || strings.Contains(err.Error(), "removed again") {
		t.Fatalf("start under another existing key: %v", err)
	}
	if _, serr := os.Stat(keyB); serr != nil {
		t.Fatalf("an existing key file was removed: %v", serr)
	}

	// Rotation A -> B, then the same flags again (Reseal answers ErrUnseal,
	// the open under B decides), then A no longer opens.
	rot := Config{SealKeyFile: keyB, SealKeyPreviousFile: keyA}
	for i := range 2 {
		if err := r.open(mustSealing(t, rot), true, nil); err != nil {
			t.Fatalf("rotation run %d: %v", i+1, err)
		}
	}
	if err := r.open(mustSealing(t, Config{SealKeyFile: keyA}), true, nil); !errors.Is(err, controller.ErrUnseal) {
		t.Fatalf("the old key after the rotation: %v", err)
	}
	if bytes.Contains(r.stored(), []byte(code)) {
		t.Fatal("the kept code in clear after the rotation")
	}
}

// TestResealNeverSealed pins what Reseal answers on a store that was never
// sealed (brief S16 §4, the note's step-5 unverified case). Measured: a
// controller's database, which holds its CA in clear, is ErrUnseal (the
// godoc's "a secret in clear", controller/reseal.go:29-31 at v0.28.0), and
// the open under the current key then seals it; an uncommissioned node's
// store is nil.
func TestResealNeverSealed(t *testing.T) {
	dir := t.TempDir()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: discard()})
	r := &ctrlRig{t: t, site: site, path: filepath.Join(dir, "fabric-server.db")}
	if err := r.open(nil, false, nil); err != nil {
		t.Fatal(err)
	}
	keyA, keyB := filepath.Join(dir, "a.key"), filepath.Join(dir, "b.key")
	writeKey(t, keyA, 1)
	writeKey(t, keyB, 2)
	seal := mustSealing(t, Config{SealKeyFile: keyB, SealKeyPreviousFile: keyA})
	db, err := controller.OpenDB(r.path)
	if err != nil {
		t.Fatal(err)
	}
	err = controller.Reseal(t.Context(), db, seal.previous, seal.current)
	_ = db.Close()
	if !errors.Is(err, controller.ErrUnseal) {
		t.Fatalf("controller.Reseal on a never-sealed database: %v, want ErrUnseal", err)
	}
	if err := r.open(seal, false, nil); err != nil {
		t.Fatalf("rotation flags on a never-sealed database: %v", err)
	}

	// A device node's store, never sealed and uncommissioned.
	nodePath := filepath.Join(dir, "bridge.db")
	cfg, err := bridgeConfig(Config{StoragePath: dir}, primaryNode(Config{BridgePasscode: 20202021, BridgeDiscriminator: 3840, BridgeVendorID: 0xFFF1, BridgeProductID: 0x8001}), nil, nil, discard())
	if err != nil {
		t.Fatal(err)
	}
	n, err := node.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = n.Stop(t.Context())
	ndb := openNodeDB(t, nodePath)
	err = node.Reseal(t.Context(), ndb, seal.previous, seal.current)
	_ = ndb.Close()
	// Measured: nil — a node store without a secret in clear moves to
	// the new key.
	if err != nil {
		t.Fatalf("node.Reseal on a never-sealed, uncommissioned store: %v, want nil", err)
	}
	if err := seal.resealNode(t.Context(), nodePath); err != nil {
		t.Fatalf("resealNode on a never-sealed store: %v", err)
	}
}

func openNodeDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "_pragma=foreign_keys(1)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// TestSealedNodeStore: a device node's store under bridgeConfig's sealer,
// commissioned, erased and commissioned again, then reopened with the key,
// another key and none — the last is the measurement of the note's F9 for
// a commissioned node (go-fabric's own test opens an uncommissioned one,
// node/facade_reseal_test.go:91-98 at v0.28.0). Rotation of the file,
// then a second run of the same flags, is resealNode's.
func TestSealedNodeStore(t *testing.T) {
	dir := t.TempDir()
	keyA, keyB := filepath.Join(dir, "a.key"), filepath.Join(dir, "b.key")
	writeKey(t, keyA, 1)
	writeKey(t, keyB, 2)
	sealA := mustSealing(t, Config{SealKeyFile: keyA})
	c := Config{StoragePath: dir, BridgePasscode: 20202021, BridgeDiscriminator: 3840, BridgeVendorID: 0xFFF1, BridgeProductID: 0x8001}
	spec := primaryNode(c)
	cfgFor := func(seal *sealing) node.Config {
		t.Helper()
		cfg, err := bridgeConfig(c, spec, nil, seal, discard())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Network.Listen = "[::1]:0"
		return cfg
	}
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}, Logger: discard()})
	dev := site.AddNode(cfgFor(sealA))
	ctrl := site.AddController(fabrictest.ControllerOptions{Logger: discard()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := ctrl.Commission(ctx, dev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission: %v", err)
	}
	if err := dev.FactoryReset(ctx); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if _, err := ctrl.Commission(ctx, dev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission after the erase: %v", err)
	}
	if err := dev.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	opens := func(seal *sealing) error {
		n, err := node.New(ctx, cfgFor(seal))
		if err := seal.explain(err); err != nil {
			return err
		}
		return n.Stop(ctx)
	}
	if err := opens(mustSealing(t, Config{SealKeyFile: keyA})); err != nil {
		t.Fatalf("reopen under the key: %v", err)
	}
	if err := opens(mustSealing(t, Config{SealKeyFile: keyB})); !errors.Is(err, node.ErrUnseal) {
		t.Fatalf("reopen under another key: %v, want ErrUnseal", err)
	}
	// F9, measured: node.New without a sealer on a commissioned sealed
	// store. go-fabric's godoc: such a store "reads a sealed secret as
	// store.ErrNoSealer and writes none" (node/config.go:813-815).
	// Measured: New fails ErrNoSealer, loading the group key sets.
	if err := opens(nil); !errors.Is(err, node.ErrNoSealer) || !strings.Contains(err.Error(), "set --seal-key-file") {
		t.Fatalf("node.New without a sealer on a commissioned sealed store: %v, want ErrNoSealer", err)
	}

	// Rotation A -> B per file, the second run with the same flags too.
	path := cfgFor(nil).Store.Path
	for i := range 2 {
		rot := mustSealing(t, Config{SealKeyFile: keyB, SealKeyPreviousFile: keyA})
		if err := rot.resealNode(ctx, path); err != nil {
			t.Fatalf("resealNode run %d: %v", i+1, err)
		}
	}
	if err := opens(mustSealing(t, Config{SealKeyFile: keyB})); err != nil {
		t.Fatalf("open under the new key: %v", err)
	}
	if err := opens(mustSealing(t, Config{SealKeyFile: keyA})); !errors.Is(err, node.ErrUnseal) {
		t.Fatalf("the old key after the rotation: %v", err)
	}
}
