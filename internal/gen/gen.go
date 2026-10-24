// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Generate returns the generated files by name.
func Generate() (map[string][]byte, error) {
	s, err := loadSnapshot()
	if err != nil {
		return nil, err
	}
	g := &gen{s: s, version: goFabricVersion(), byName: map[string]*sCluster{}, emitted: map[string]bool{}}
	for i := range s.Clusters {
		g.byName[s.Clusters[i].Name] = &s.Clusters[i]
	}
	src, err := g.goSource()
	if err != nil {
		return nil, err
	}
	js, err := g.jsonSchema()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"tables_gen.go": src, "schema.json": js}, nil
}

type gen struct {
	s       *snapshot
	version string
	byName  map[string]*sCluster

	vars    bytes.Buffer // the datatype variables
	emitted map[string]bool
}

// datatype finds the datatype a value names: in the cluster, then in its
// base cluster, then among the global datatypes (scope "global" first).
func (g *gen) datatype(v sValue, cl *sCluster) (*sDatatype, string) {
	if v.Scope != "global" && cl != nil {
		for c := cl; c != nil; c = g.byName[c.Base] {
			for i := range c.Datatypes {
				if c.Datatypes[i].Name == v.Type {
					return &c.Datatypes[i], fmt.Sprintf("dt%04X_%s", c.ID, ident(v.Type))
				}
			}
			if c.Base == "" {
				break
			}
		}
	}
	for i := range g.s.GlobalDatatypes {
		if g.s.GlobalDatatypes[i].Name == v.Type {
			return &g.s.GlobalDatatypes[i], "dtGlobal_" + ident(v.Type)
		}
	}
	return nil, ""
}

func ident(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func bitsOf(primitive string) int {
	n, err := strconv.Atoi(strings.TrimLeft(primitive, "uint"))
	if err != nil {
		return 0
	}
	return n
}

func metaOf(v sValue) string {
	switch v.Metatype {
	case "boolean":
		return "MetaBool"
	case "integer":
		if strings.HasPrefix(v.Primitive, "int") {
			return "MetaInt"
		}
		return "MetaUint"
	case "float":
		if v.Primitive == "double" {
			return "MetaDouble"
		}
		return "MetaFloat"
	case "string":
		return "MetaString"
	case "bytes":
		return "MetaBytes"
	case "enum":
		return "MetaEnum"
	case "bitmap":
		return "MetaBitmap"
	case "object":
		return "MetaStruct"
	case "array":
		return "MetaList"
	}
	return "MetaAny"
}

// typeExpr is the Go literal of v's type.
func (g *gen) typeExpr(v sValue, cl *sCluster) string {
	meta := metaOf(v)
	var b strings.Builder
	fmt.Fprintf(&b, "Type{Name: %q, Meta: %s", v.Type, meta)
	switch meta {
	case "MetaUint", "MetaInt", "MetaEnum", "MetaBitmap":
		if n := bitsOf(v.Primitive); n > 0 {
			fmt.Fprintf(&b, ", Bits: %d", n)
		}
	}
	switch meta {
	case "MetaStruct", "MetaEnum", "MetaBitmap":
		if dt, name := g.datatype(v, cl); dt != nil {
			g.emitDatatype(dt, name, cl)
			field := map[string]string{"MetaStruct": "Struct", "MetaEnum": "Enum", "MetaBitmap": "Bitmap"}[meta]
			fmt.Fprintf(&b, ", %s: %s", field, name)
		}
	case "MetaList":
		if v.Entry != nil {
			fmt.Fprintf(&b, ", Entry: &%s", g.typeExpr(*v.Entry, cl))
		}
	}
	b.WriteString("}")
	return b.String()
}

func (g *gen) fieldsExpr(fs []sField, cl *sCluster) string {
	if len(fs) == 0 {
		return "nil"
	}
	var b strings.Builder
	b.WriteString("[]Field{\n")
	for _, f := range fs {
		fmt.Fprintf(&b, "{ID: %d, Name: %q, Property: %q, Type: %s", f.ID, f.Name, camelize(f.Name), g.typeExpr(f.sValue, cl))
		if f.Quality.Nullable {
			b.WriteString(", Nullable: true")
		}
		if f.Conformance.Text == "M" {
			b.WriteString(", Mandatory: true")
		}
		b.WriteString("},\n")
	}
	b.WriteString("}")
	return b.String()
}

// emitDatatype writes the variable of a datatype once.
func (g *gen) emitDatatype(dt *sDatatype, name string, cl *sCluster) {
	if g.emitted[name] {
		return
	}
	g.emitted[name] = true
	switch dt.Metatype {
	case "object":
		// The fields may name other datatypes, which are written before
		// this one; Go orders the initialisation of package variables.
		body := g.fieldsExpr(dt.Fields, cl)
		fmt.Fprintf(&g.vars, "var %s = &Struct{Name: %q, Fields: %s}\n\n", name, dt.Name, body)
	case "enum":
		fmt.Fprintf(&g.vars, "var %s = &Enum{Name: %q, Values: []EnumValue{\n", name, dt.Name)
		for _, f := range dt.Fields {
			fmt.Fprintf(&g.vars, "{Value: %d, Name: %q},\n", f.ID, f.Name)
		}
		g.vars.WriteString("}}\n\n")
	case "bitmap":
		fmt.Fprintf(&g.vars, "var %s = &Bitmap{Name: %q, Members: []BitmapMember{\n", name, dt.Name)
		for _, f := range dt.Fields {
			bit, width := f.ID, uint32(1)
			switch {
			case f.Constraint.Value != nil:
				bit = uint32(*f.Constraint.Value) //nolint:gosec // a bit number of the snapshot
			case f.Constraint.Min != nil && f.Constraint.Max != nil:
				lo, lok := f.Constraint.Min.(float64)
				hi, hok := f.Constraint.Max.(float64)
				if lok && hok && hi >= lo {
					bit, width = uint32(lo), uint32(hi-lo+1)
				}
			}
			fmt.Fprintf(&g.vars, "{Name: %q, Bit: %d, Width: %d},\n", f.Name, bit, width)
		}
		g.vars.WriteString("}}\n\n")
	}
}

func (g *gen) goSource() ([]byte, error) {
	var cl bytes.Buffer
	globals := map[uint32]string{}
	for i := range g.s.Clusters {
		c := &g.s.Clusters[i]
		fmt.Fprintf(&cl, "&Cluster{ID: %d, Name: %q, Revision: %d,\n", c.ID, c.Name, c.Revision)
		if len(c.Features) > 0 {
			cl.WriteString("Features: []Feature{\n")
			for _, f := range c.Features {
				fmt.Fprintf(&cl, "{Name: %q, Title: %q, Bit: %d},\n", f.Name, f.Effective.Title, f.Bit)
			}
			cl.WriteString("},\n")
		}
		cl.WriteString("Attributes: []Attribute{\n")
		for _, a := range c.Attributes {
			if a.ID >= 0xF000 {
				globals[a.ID] = a.Name
			}
			fmt.Fprintf(&cl, "{ID: %d, Name: %q, Property: %q, Type: %s", a.ID, a.Name, camelize(a.Name), g.typeExpr(a.Effective, c))
			if a.Effective.Quality.Nullable {
				cl.WriteString(", Nullable: true")
			}
			if strings.Contains(a.Effective.Access.RW, "W") {
				cl.WriteString(", Writable: true")
			}
			if a.Effective.Access.Timed {
				cl.WriteString(", Timed: true")
			}
			cl.WriteString("},\n")
		}
		cl.WriteString("},\n")
		if len(c.Commands) > 0 {
			cl.WriteString("Commands: []Command{\n")
			for _, x := range c.Commands {
				timed := ""
				if x.Effective.Access.Timed {
					timed = " Timed: true,"
				}
				fmt.Fprintf(&cl, "{ID: %d, Name: %q, Property: %q, Request: %t, Response: %q,%s Fields: %s},\n",
					x.ID, x.Name, camelize(x.Name), x.Direction == "request", x.Response, timed, g.fieldsExpr(x.Effective.Fields, c))
			}
			cl.WriteString("},\n")
		}
		if len(c.Events) > 0 {
			cl.WriteString("Events: []Event{\n")
			for _, e := range c.Events {
				fmt.Fprintf(&cl, "{ID: %d, Name: %q, Property: %q, Priority: %q, Fields: %s},\n", e.ID, e.Name, camelize(e.Name), e.Priority, g.fieldsExpr(e.Effective.Fields, c))
			}
			cl.WriteString("},\n")
		}
		cl.WriteString("},\n")
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/gen from go-fabric's parity/schema.json. DO NOT EDIT.\n\n")
	b.WriteString("package schema\n\n")
	fmt.Fprintf(&b, "// Provenance of the tables.\nconst (\n")
	fmt.Fprintf(&b, "\t// GoFabricVersion is the go-fabric module the tables were generated from.\n\tGoFabricVersion = %q\n", g.version)
	fmt.Fprintf(&b, "\t// MatterRevision is the Matter revision of the snapshot.\n\tMatterRevision = %q\n", g.s.Matter.Revision)
	fmt.Fprintf(&b, "\t// SourceCommit is the matter.js commit the snapshot was extracted at.\n\tSourceCommit = %q\n)\n\n", g.s.Matter.SourceCommit)
	b.WriteString("var clusters = []*Cluster{\n")
	b.Write(cl.Bytes())
	b.WriteString("}\n\n")
	b.WriteString("var deviceTypes = []DeviceType{\n")
	for _, d := range g.s.DeviceTypes {
		fmt.Fprintf(&b, "{ID: %d, Name: %q, Revision: %d},\n", d.ID, d.Name, d.Revision)
	}
	b.WriteString("}\n\n")
	ids := make([]uint32, 0, len(globals))
	for id := range globals {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	b.WriteString("var globalAttributes = []GlobalAttribute{\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "{ID: %d, Name: %q},\n", id, globals[id])
	}
	b.WriteString("}\n\n")
	b.Write(g.vars.Bytes())
	out, err := format.Source(b.Bytes())
	if err != nil {
		return b.Bytes(), fmt.Errorf("format: %w", err)
	}
	return out, nil
}

// --- JSON Schema ---

func (g *gen) jsonSchema() ([]byte, error) {
	defs := map[string]any{}
	for i := range g.s.Clusters {
		c := &g.s.Clusters[i]
		props := map[string]any{}
		for _, a := range c.Attributes {
			s := g.valueSchema(a.Effective, c, defs)
			s["title"] = a.Name
			if a.Effective.Quality.Nullable {
				s = nullable(s)
			}
			props[strconv.FormatUint(uint64(a.ID), 10)] = s
		}
		defs[fmt.Sprintf("cluster.%d", c.ID)] = map[string]any{
			"title":                c.Name,
			"description":          fmt.Sprintf("Values of the %s cluster (0x%04X, revision %d), keyed by attribute id.", c.Name, c.ID, c.Revision),
			"type":                 "object",
			"properties":           props,
			"additionalProperties": true,
		}
	}
	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://github.com/SukramJ/go-fabric-server/protocol/schema/schema.json",
		"title":   "go-fabric-server node data values",
		"description": "The attribute values of a node's data (MatterNodeData.attributes) for every cluster of the Matter " + g.s.Matter.Revision +
			" model: the node data keys a value by \"endpoint/cluster/attribute\"; $defs/cluster.<cluster id> types the values of one cluster by attribute id. " +
			"Structs are keyed by field id, bytes are base64, enums and bitmaps numbers, epoch times Unix-based. A value of a cluster or attribute outside this model is sent as decoded from the TLV.",
		"x-go-fabric-version":    g.version,
		"x-matter-revision":      g.s.Matter.Revision,
		"x-matter-source-commit": g.s.Matter.SourceCommit,
		"type":                   "object",
		"additionalProperties":   true,
		"$defs":                  defs,
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func nullable(s map[string]any) map[string]any {
	title := s["title"]
	delete(s, "title")
	out := map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
	if title != nil {
		out["title"] = title
	}
	return out
}

func (g *gen) valueSchema(v sValue, cl *sCluster, defs map[string]any) map[string]any {
	switch metaOf(v) {
	case "MetaBool":
		return map[string]any{"type": "boolean"}
	case "MetaUint":
		s := map[string]any{"type": "integer"}
		if strings.HasPrefix(v.Type, "epoch-") || strings.HasPrefix(v.Type, "posix-") {
			s["description"] = v.Type + ", sent as Unix time"
			return s
		}
		if n := bitsOf(v.Primitive); n > 0 && n < 64 {
			s["minimum"], s["maximum"] = 0, uint64(1)<<n-1
		} else if n == 64 {
			s["minimum"], s["maximum"] = 0, uint64(math.MaxUint64)
		}
		return s
	case "MetaInt":
		s := map[string]any{"type": "integer"}
		if n := bitsOf(strings.TrimPrefix(v.Primitive, "int")); n > 0 && n <= 64 {
			s["minimum"], s["maximum"] = -(int64(1) << (n - 1)), int64(uint64(1)<<(n-1)-1) //nolint:gosec // n <= 64
		}
		return s
	case "MetaFloat", "MetaDouble":
		return map[string]any{"type": "number"}
	case "MetaString":
		return map[string]any{"type": "string"}
	case "MetaBytes":
		return map[string]any{"type": "string", "contentEncoding": "base64"}
	case "MetaEnum", "MetaBitmap", "MetaStruct":
		dt, name := g.datatype(v, cl)
		if dt == nil {
			if metaOf(v) == "MetaStruct" {
				return map[string]any{"type": "object"}
			}
			return map[string]any{"type": "integer"}
		}
		key := strings.TrimPrefix(name, "dt")
		if _, ok := defs[key]; !ok {
			defs[key] = map[string]any{} // reserve against recursion
			defs[key] = g.datatypeSchema(dt, cl, defs)
		}
		return map[string]any{"$ref": "#/$defs/" + key}
	case "MetaList":
		s := map[string]any{"type": "array"}
		if v.Entry != nil {
			s["items"] = g.valueSchema(*v.Entry, cl, defs)
		}
		return s
	}
	return map[string]any{}
}

func (g *gen) datatypeSchema(dt *sDatatype, cl *sCluster, defs map[string]any) map[string]any {
	switch dt.Metatype {
	case "enum":
		names := map[string]any{}
		for _, f := range dt.Fields {
			names[strconv.FormatUint(uint64(f.ID), 10)] = f.Name
		}
		return map[string]any{"title": dt.Name, "type": "integer", "x-enum-names": names}
	case "bitmap":
		bits := map[string]any{}
		for _, f := range dt.Fields {
			bits[f.Name] = f.ID
			if f.Constraint.Value != nil {
				bits[f.Name] = *f.Constraint.Value
			}
		}
		return map[string]any{"title": dt.Name, "type": "integer", "minimum": 0, "x-bitmap-bits": bits}
	}
	props := map[string]any{}
	var required []string
	for _, f := range dt.Fields {
		s := g.valueSchema(f.sValue, cl, defs)
		s["title"] = f.Name
		if f.Quality.Nullable {
			s = nullable(s)
		}
		id := strconv.FormatUint(uint64(f.ID), 10)
		props[id] = s
		if f.Conformance.Text == "M" {
			required = append(required, id)
		}
	}
	s := map[string]any{"title": dt.Name, "type": "object", "properties": props, "additionalProperties": true}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
