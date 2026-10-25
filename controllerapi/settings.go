// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controllerapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/thread"
)

// defaultCredentialID is the reserved entry pre-schema-12 callers use
// (docs/websockets_api.md:1452-1465).
const defaultCredentialID = "default"

// defaultFabricLabel is matterjs-server's default label
// (packages/ws-controller/src/server/ConfigStorage.ts:58).
const defaultFabricLabel = "HomeAssistant"

type wifiCredential struct {
	ID          string `json:"id"`
	SSID        string `json:"ssid"`
	Credentials string `json:"credentials"`
}

type threadCredential struct {
	ID      string `json:"id"`
	Dataset string `json:"dataset"`
}

// settingsData is server.json: the server's own settings, which are not
// the controller's (the fabric, the CA and the nodes live in the
// controller's database) — matterjs-server's ConfigStorage
// (ConfigStorage.ts:29-61).
type settingsData struct {
	FabricLabel string             `json:"fabric_label"`
	Wifi        []wifiCredential   `json:"wifi"`
	Thread      []threadCredential `json:"thread"`
}

type settingsStore struct {
	path string
	mu   sync.Mutex
	data settingsData
}

func openSettings(dir string) (*settingsStore, error) {
	s := &settingsStore{data: settingsData{FabricLabel: defaultFabricLabel}}
	if dir == "" {
		return s, nil
	}
	s.path = filepath.Join(dir, "server.json")
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("settings: %w", err)
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("settings %s: %w", s.path, err)
	}
	if s.data.FabricLabel == "" {
		s.data.FabricLabel = defaultFabricLabel
	}
	return s, nil
}

// saveLocked writes server.json atomically, readable by the owner only:
// it holds the Wi-Fi passwords and Thread datasets.
func (s *settingsStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil { //nolint:gosec // the operator's --storage-path
		return err
	}
	return os.Rename(tmp, s.path) //nolint:gosec // as above
}

func (s *settingsStore) label() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.FabricLabel
}

// normalizeLabel is matterjs-server's label rule (WebSocketControllerHandler.ts:201,
// :1227-1262): trimmed, empty is "HomeAssistant", at most 32 characters
// (commissioner.MaxLabelLength).
func normalizeLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return defaultFabricLabel
	}
	if r := []rune(label); len(r) > commissioner.MaxLabelLength {
		label = string(r[:commissioner.MaxLabelLength])
	}
	return label
}

func (s *settingsStore) setLabel(label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.FabricLabel = normalizeLabel(label)
	return s.saveLocked()
}

// defaults are the default entries, nil when unset.
func (s *settingsStore) defaults() (*wifiCredential, *threadCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var w *wifiCredential
	var t *threadCredential
	for i := range s.data.Wifi {
		if s.data.Wifi[i].ID == defaultCredentialID {
			c := s.data.Wifi[i]
			w = &c
		}
	}
	for i := range s.data.Thread {
		if s.data.Thread[i].ID == defaultCredentialID {
			c := s.data.Thread[i]
			t = &c
		}
	}
	return w, t
}

// setWifi stores an entry. A blank password keeps the stored one for an
// unchanged SSID only (docs/websockets_api.md:1460-1465).
// credentials returns the stored Wi-Fi and Thread entries of the ids
// given (nil or empty: the default entries, absent ones nil); missing
// names an id given that is not stored.
func (s *settingsStore) credentials(wifiID, threadID *string) (w *wifiCredential, t *threadCredential, missing string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	find := func(id *string, isWifi bool) (int, bool) {
		want := credentialID(id)
		if isWifi {
			for i := range s.data.Wifi {
				if s.data.Wifi[i].ID == want {
					return i, true
				}
			}
		} else {
			for i := range s.data.Thread {
				if s.data.Thread[i].ID == want {
					return i, true
				}
			}
		}
		return 0, false
	}
	if i, ok := find(wifiID, true); ok {
		c := s.data.Wifi[i]
		w = &c
	} else if wifiID != nil && *wifiID != "" {
		return nil, nil, "Unknown wifi_credentials_id: " + *wifiID
	}
	if i, ok := find(threadID, false); ok {
		c := s.data.Thread[i]
		t = &c
	} else if threadID != nil && *threadID != "" {
		return nil, nil, "Unknown thread_dataset_id: " + *threadID
	}
	return w, t, ""
}

func (s *settingsStore) setWifi(id, ssid, credentials string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Wifi {
		if e := &s.data.Wifi[i]; e.ID == id {
			if credentials == "" {
				if e.SSID != ssid || e.Credentials == "" {
					return errors.New("a blank password is accepted only for the unchanged SSID of a stored entry")
				}
				credentials = e.Credentials
			}
			e.SSID, e.Credentials = ssid, credentials
			return s.saveLocked()
		}
	}
	if credentials == "" {
		return errors.New("credentials must not be empty for a new entry")
	}
	s.data.Wifi = append(s.data.Wifi, wifiCredential{ID: id, SSID: ssid, Credentials: credentials})
	return s.saveLocked()
}

func (s *settingsStore) setThread(id, dataset string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Thread {
		if s.data.Thread[i].ID == id {
			s.data.Thread[i].Dataset = dataset
			return s.saveLocked()
		}
	}
	s.data.Thread = append(s.data.Thread, threadCredential{ID: id, Dataset: dataset})
	return s.saveLocked()
}

// removeWifi removes a named entry; the default entry is cleared, not
// removed.
func (s *settingsStore) removeWifi(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Wifi[:0]
	for _, e := range s.data.Wifi {
		if e.ID != id {
			out = append(out, e)
		}
	}
	s.data.Wifi = out
	return s.saveLocked()
}

func (s *settingsStore) removeThread(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Thread[:0]
	for _, e := range s.data.Thread {
		if e.ID != id {
			out = append(out, e)
		}
	}
	s.data.Thread = out
	return s.saveLocked()
}

// summaries are get_all_credentials' lists, the default entry always
// first and present (docs/websockets_api.md:1502-1506).
func (s *settingsStore) summaries() (wifi, threads []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wifi = []map[string]any{{"id": defaultCredentialID, "ssid": ""}}
	threads = []map[string]any{{"id": defaultCredentialID}}
	ws := append([]wifiCredential(nil), s.data.Wifi...)
	sort.SliceStable(ws, func(i, j int) bool { return ws[i].ID < ws[j].ID })
	for _, e := range ws {
		if e.ID == defaultCredentialID {
			wifi[0]["ssid"] = e.SSID
			continue
		}
		wifi = append(wifi, map[string]any{"id": e.ID, "ssid": e.SSID})
	}
	ts := append([]threadCredential(nil), s.data.Thread...)
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].ID < ts[j].ID })
	for _, e := range ts {
		m := threadSummary(e)
		if e.ID == defaultCredentialID {
			threads[0] = m
			continue
		}
		threads = append(threads, m)
	}
	return wifi, threads
}

// threadSummary is a get_all_credentials Thread entry: the id, and the
// dataset's network name and extended PAN id (uppercase hex) when it
// decodes (controller/thread.DecodeDatasetHex, go-fabric's port of
// matter.js OperationalDataset.ts) — matterjs-server's
// WebSocketControllerHandler.ts:2250-2302. The secrets are never returned.
func threadSummary(e threadCredential) map[string]any {
	m := map[string]any{"id": e.ID}
	ds, err := thread.DecodeDatasetHex(e.Dataset)
	if err != nil {
		return m
	}
	if ds.NetworkName != nil {
		m["networkName"] = *ds.NetworkName
	}
	if len(ds.ExtPANID) > 0 {
		m["extPanId"] = strings.ToUpper(hex.EncodeToString(ds.ExtPANID))
	}
	return m
}
