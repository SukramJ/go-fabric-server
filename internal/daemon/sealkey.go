// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"sync"

	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/node"
)

// sealKeySize is the key's length: go-fabric's AES-256-GCM sealer takes 32
// bytes (controller/seal.go:29-36 at v0.28.0).
const sealKeySize = 32

// sealing is --seal-key-file and --seal-key-previous-file (#76): the one
// sealer of the controller's database and of every device node's store
// (controller.Config.Sealer is "the same sealer node.Store.Sealer takes",
// controller/controller.go:159-161 at v0.28.0), and the previous key the
// start reseals each store from. A nil *sealing is sealing off.
type sealing struct {
	path     string
	current  node.Sealer
	previous node.Sealer
	log      *slog.Logger

	mu sync.Mutex
	// created: this start wrote the key file; opened: a store opened under
	// it since. A first open that fails ErrUnseal under a key just created
	// means the store was sealed under a key that is gone, and the file is
	// removed again (decision Q2, brief S16).
	created, opened bool
	// resealed are the node store files Reseal ran on in this run: an
	// erase starts a node on the same file again, which needs no second
	// try.
	resealed map[string]bool
}

// openSealing reads the key files; nil without --seal-key-file. A missing
// key file is created with a new random key (go-fabric's reference
// controller does the same, examples/reference-controller/cli/sealkey.go:22-87
// at v0.28.0); the previous key's file must exist. A key file readable by
// group or others is refused.
func openSealing(c Config, log *slog.Logger) (*sealing, error) {
	if c.SealKeyFile == "" {
		if c.SealKeyPreviousFile != "" {
			return nil, errors.New("--seal-key-previous-file needs --seal-key-file, the key the stores move to")
		}
		return nil, nil
	}
	key, err := readSealKey(c.SealKeyFile)
	created := false
	if errors.Is(err, fs.ErrNotExist) {
		key, err = createSealKey(c.SealKeyFile)
		created = err == nil
	}
	if err != nil {
		return nil, fmt.Errorf("--seal-key-file: %w", err)
	}
	s := &sealing{path: c.SealKeyFile, log: log, created: created, resealed: map[string]bool{}}
	if s.current, err = controller.NewAESGCMSealer(key); err != nil {
		return nil, fmt.Errorf("--seal-key-file: %w", err)
	}
	if created {
		log.Warn("fabricserver.seal.key_created", slog.String("path", c.SealKeyFile),
			slog.String("reason", "a new key: keep a copy outside the storage directory's backup; without it the sealed stores do not open"))
	}
	if c.SealKeyPreviousFile != "" {
		prev, err := readSealKey(c.SealKeyPreviousFile)
		if err != nil {
			return nil, fmt.Errorf("--seal-key-previous-file: %w", err)
		}
		if bytes.Equal(prev, key) {
			return nil, errors.New("--seal-key-previous-file holds the key of --seal-key-file: nothing to rotate")
		}
		if s.previous, err = controller.NewAESGCMSealer(prev); err != nil {
			return nil, fmt.Errorf("--seal-key-previous-file: %w", err)
		}
	}
	return s, nil
}

// readSealKey reads a key file: 64 hex digits, surrounding white space
// ignored (the reference's format, cli/sealkey.go:54-66), owner-only.
func readSealKey(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by group or others (mode %v): chmod 600 it", path, info.Mode().Perm())
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(string(bytes.TrimSpace(b)))
	if err != nil || len(key) != sealKeySize {
		return nil, fmt.Errorf("%s: want %d bytes as %d hex digits", path, sealKeySize, 2*sealKeySize)
	}
	return key, nil
}

// createSealKey writes a new random key to path, which must not exist,
// with mode 0600.
func createSealKey(path string) ([]byte, error) {
	key := make([]byte, sealKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

// sealer is the sealer for controller.Config.Sealer and node.Store.Sealer;
// nil when sealing is off.
func (s *sealing) sealer() node.Sealer {
	if s == nil {
		return nil
	}
	return s.current
}

// resealController moves the controller's database from the previous key
// to the current one before controller.New opens it; existed is whether
// the file was there before controller.OpenDB (Reseal needs the schemas a
// New applied, controller/reseal.go:35-38).
func (s *sealing) resealController(ctx context.Context, db *sql.DB, name string, existed bool) error {
	if s == nil || s.previous == nil || !existed {
		return nil
	}
	return s.outcome(name, controller.Reseal(ctx, db, s.previous, s.current))
}

// resealNode moves a device node's store file from the previous key to
// the current one before node.New opens it, once per file and run. The
// file is opened as go-fabric's facade test opens it for node.Reseal
// (node/facade_reseal_test.go:60-61 at v0.28.0), not with
// controller.OpenDB, which would switch it to WAL.
func (s *sealing) resealNode(ctx context.Context, path string) error {
	if s == nil || s.previous == nil {
		return nil
	}
	s.mu.Lock()
	done := s.resealed[path]
	s.resealed[path] = true
	s.mu.Unlock()
	if done {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "_pragma=foreign_keys(1)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	return s.outcome(path, node.Reseal(ctx, db, s.previous, s.current))
}

// outcome maps a Reseal's result (controller/reseal.go:16-39,
// node/facade.go:132-153 at v0.28.0): nil is rotated — also a never-sealed
// store without a secret in clear (an uncommissioned node's, measured);
// ErrUnseal leaves the file as it was — already under the current key, a
// never-sealed store with a secret in clear (controller/reseal.go:29-31),
// or under neither, which the open under the current key then tells; ErrScrubIncomplete is
// rotated with the scrub owed, which the next open runs.
func (s *sealing) outcome(name string, err error) error {
	switch {
	case err == nil:
		s.log.Info("fabricserver.seal.rotated", slog.String("store", name))
		return nil
	case errors.Is(err, controller.ErrScrubIncomplete):
		s.log.Warn("fabricserver.seal.rotated", slog.String("store", name), slog.Any("error", err))
		return nil
	case errors.Is(err, controller.ErrUnseal):
		s.log.Info("fabricserver.seal.not_rotated", slog.String("store", name),
			slog.String("reason", "not sealed under --seal-key-previous-file's key; the open under --seal-key-file decides"))
		return nil
	}
	return fmt.Errorf("%s: rotating the seal key: %w", name, err)
}

// explain turns an open's sealing error into the operator's message; a
// store that opened marks the key as in use. Nothing is ever deleted but
// a key file this start created and no store opened under (Q2, Q6).
func (s *sealing) explain(err error) error {
	if s != nil && err == nil {
		s.mu.Lock()
		s.opened = true
		s.mu.Unlock()
		return nil
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, controller.ErrUnseal):
		if s == nil {
			return err
		}
		s.mu.Lock()
		remove := s.created && !s.opened
		s.created = false
		s.mu.Unlock()
		if remove {
			if rerr := os.Remove(s.path); rerr == nil {
				return fmt.Errorf("the database is sealed under a key that is not --seal-key-file's; the key file %s this start created is removed again: %w", s.path, err)
			}
		}
		return fmt.Errorf("the database is sealed under another key than --seal-key-file's (%s): %w", s.path, err)
	case errors.Is(err, controller.ErrNoSealer):
		return fmt.Errorf("the database is sealed: set --seal-key-file to its key: %w", err)
	}
	return err
}
