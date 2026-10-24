// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"fmt"
	"strconv"
	"strings"
)

// Path is a parsed "endpoint/cluster/element" path; a nil part is a
// wildcard.
type Path struct {
	Endpoint *uint16
	Cluster  *uint32
	Element  *uint32
}

// The ids that also act as wildcards — matterjs-server's pathNumbersOf
// (Converters.ts:675-690): 65535 as the endpoint, 4294967295 as the
// cluster or the element.
const (
	wildcardEndpoint = 0xFFFF
	wildcardID       = 0xFFFFFFFF
)

// ParsePath parses an attribute or event path. Lenient is read_attribute's
// rule (Converters.ts:675-725): a part that is not a decimal number reads
// as a wildcard. Strict is the rule of read, subscribe and write_attribute
// (docs/websockets_api.md:2932-2951): exactly segments parts, each a
// decimal number or "*". A number outside the id range is an error either
// way.
func ParsePath(path string, segments int, strict bool) (Path, error) {
	parts := strings.Split(path, "/")
	if strict {
		if len(parts) != segments {
			return Path{}, fmt.Errorf("path %q: want %d parts, each a number or \"*\"", path, segments)
		}
		for _, p := range parts {
			if p != "*" && !isDigits(p) {
				return Path{}, fmt.Errorf("path %q: %q is neither a number nor \"*\"", path, p)
			}
		}
	}
	var out Path
	for i := 0; i < segments && i < len(parts); i++ {
		p := parts[i]
		if !isDigits(p) {
			continue
		}
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Path{}, fmt.Errorf("invalid id in path %q: %s", path, p)
		}
		if i == 0 {
			switch {
			case v == wildcardEndpoint:
			case v > wildcardEndpoint:
				return Path{}, fmt.Errorf("invalid id in path %q: endpoint %d is out of range", path, v)
			default:
				e := uint16(v)
				out.Endpoint = &e
			}
			continue
		}
		switch {
		case v == wildcardID:
		case v > wildcardID:
			return Path{}, fmt.Errorf("invalid id in path %q: %d is out of range", path, v)
		default:
			id := uint32(v)
			if i == 1 {
				out.Cluster = &id
			} else {
				out.Element = &id
			}
		}
	}
	return out, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Key is the node data key of an attribute: "endpoint/cluster/attribute",
// decimal (Converters.ts:670-672).
func Key(endpoint uint16, cluster, attribute uint32) string {
	return strconv.FormatUint(uint64(endpoint), 10) + "/" + strconv.FormatUint(uint64(cluster), 10) + "/" + strconv.FormatUint(uint64(attribute), 10)
}

// ClusterKey is "endpoint/cluster", the key of a data version.
func ClusterKey(endpoint uint16, cluster uint32) string {
	return strconv.FormatUint(uint64(endpoint), 10) + "/" + strconv.FormatUint(uint64(cluster), 10)
}
