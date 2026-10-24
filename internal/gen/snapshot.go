// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/SukramJ/go-fabric/parity"
)

// The parts of parity/schema.json the generator reads.
type snapshot struct {
	Matter struct {
		Revision     string `json:"revision"`
		SourceCommit string `json:"sourceCommit"`
	} `json:"matter"`
	Clusters        []sCluster    `json:"clusters"`
	DeviceTypes     []sDeviceType `json:"deviceTypes"`
	GlobalDatatypes []sDatatype   `json:"globalDatatypes"`
}

type sCluster struct {
	ID         uint32       `json:"id"`
	Name       string       `json:"name"`
	Revision   uint16       `json:"revision"`
	Base       string       `json:"base"`
	Attributes []sAttribute `json:"attributes"`
	Commands   []sCommand   `json:"commands"`
	Events     []sEvent     `json:"events"`
	Features   []sFeature   `json:"features"`
	Datatypes  []sDatatype  `json:"datatypes"`
}

type sAccess struct {
	RW    string `json:"rw"`
	Timed bool   `json:"timed"`
}

type sQuality struct {
	Nullable bool `json:"nullable"`
}

type sConformance struct {
	Text string `json:"text"`
}

// sValue is a typed element: an attribute's or a field's effective type, a
// list entry.
type sValue struct {
	Type        string       `json:"type"`
	Metatype    string       `json:"metatype"`
	Primitive   string       `json:"primitive"`
	Scope       string       `json:"scope"`
	Entry       *sValue      `json:"entry"`
	Quality     sQuality     `json:"quality"`
	Conformance sConformance `json:"conformance"`
	Access      sAccess      `json:"access"`
}

type sAttribute struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Effective sValue `json:"effective"`
}

type sField struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	sValue
	Constraint struct {
		Value *int `json:"value"`
		Min   any  `json:"min"`
		Max   any  `json:"max"`
	} `json:"constraint"`
}

type sCommand struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Response  string `json:"response"`
	Effective struct {
		Access sAccess  `json:"access"`
		Fields []sField `json:"fields"`
	} `json:"effective"`
}

type sEvent struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Priority  string `json:"priority"`
	Effective struct {
		Fields []sField `json:"fields"`
	} `json:"effective"`
}

type sFeature struct {
	Name      string `json:"name"`
	Bit       int    `json:"bit"`
	Effective struct {
		Title string `json:"title"`
	} `json:"effective"`
}

type sDatatype struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Metatype  string   `json:"metatype"`
	Primitive string   `json:"primitive"`
	Fields    []sField `json:"fields"`
}

type sDeviceType struct {
	ID       uint32 `json:"id"`
	Name     string `json:"name"`
	Revision uint16 `json:"revision"`
}

func loadSnapshot() (*snapshot, error) {
	var s snapshot
	if err := json.Unmarshal(parity.SchemaJSON(), &s); err != nil {
		return nil, fmt.Errorf("parity/schema.json: %w", err)
	}
	return &s, nil
}

// goFabricVersion is the version of the go-fabric module this build uses —
// the version the output is generated from.
func goFabricVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range info.Deps {
		if d.Path == "github.com/SukramJ/go-fabric" {
			if d.Replace != nil {
				return d.Replace.Version
			}
			return d.Version
		}
	}
	return "unknown"
}

// camelize is matter.js's camelize (packages/general/src/util/identifier-case.ts:21-101
// at the matter.js checkout of go-fabric's pin) for the property name of
// an element: pieces split at a lower-to-upper change and before the last
// capital of an acronym, a pluralised acronym kept whole, digits pieces of
// their own, the first piece lower-cased, every other capitalised, and
// "100Ths" written "100ths".
func camelize(name string) string {
	var pieces []string
	pieceStart, sawUpper, sawLower, i := 0, false, false, 0
	addPiece := func(to int) {
		if pieceStart < to {
			pieces = append(pieces, name[pieceStart:to])
		}
		sawLower, sawUpper = false, false
	}
	isLower := func(j int) bool { return j < len(name) && name[j] >= 'a' && name[j] <= 'z' }
	for ; i < len(name); i++ {
		ch := name[i]
		if ch == '$' {
			break
		}
		if ch >= 'A' && ch <= 'Z' {
			if sawLower {
				addPiece(i)
				pieceStart = i
			}
			sawUpper = true
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			if !sawLower && sawUpper {
				if ch == 's' && !isLower(i+1) {
					addPiece(i + 1)
					pieceStart = i + 1
					continue
				}
				addPiece(i - 1)
				pieceStart = i - 1
			}
			sawLower = true
			continue
		}
		addPiece(i)
		if ch >= '0' && ch <= '9' {
			pieces = append(pieces, string(ch))
		}
		pieceStart = i + 1
	}
	addPiece(i)
	var b strings.Builder
	for k, p := range pieces {
		first := p[:1]
		if k == 0 {
			first = strings.ToLower(first)
		} else {
			first = strings.ToUpper(first)
		}
		b.WriteString(first + strings.ToLower(p[1:]))
	}
	result := b.String()
	if j := thsIndex(result); j >= 0 {
		result = result[:j+1] + "ths" + result[j+4:]
	}
	if i < len(name) {
		result += name[i:]
	}
	return result
}

// thsIndex is the index of the digit of the first (\d)Ths match, case
// insensitive, or -1.
func thsIndex(s string) int {
	for j := 0; j+3 < len(s); j++ {
		if s[j] >= '0' && s[j] <= '9' && strings.EqualFold(s[j+1:j+4], "ths") {
			return j
		}
	}
	return -1
}
