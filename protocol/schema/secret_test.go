// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/parity"

	"github.com/SukramJ/go-fabric-server/protocol/schema"
)

// The parts of the snapshot the secret marks sit in (parity/schema.json).
type markedField struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Secret bool   `json:"isSecret"`
}

type markedDatatype struct {
	Name   string        `json:"name"`
	Fields []markedField `json:"fields"`
}

type markedSnapshot struct {
	Clusters []struct {
		ID         uint32 `json:"id"`
		Name       string `json:"name"`
		Attributes []struct {
			ID        uint32 `json:"id"`
			Name      string `json:"name"`
			Effective struct {
				Secret bool `json:"isSecret"`
			} `json:"effective"`
		} `json:"attributes"`
		Commands []struct {
			ID        uint32 `json:"id"`
			Name      string `json:"name"`
			Direction string `json:"direction"`
			Effective struct {
				Fields []markedField `json:"fields"`
			} `json:"effective"`
		} `json:"commands"`
		Datatypes []markedDatatype `json:"datatypes"`
	} `json:"clusters"`
	GlobalDatatypes []markedDatatype `json:"globalDatatypes"`
	Definitions     []struct {
		Name      string           `json:"name"`
		Datatypes []markedDatatype `json:"datatypes"`
	} `json:"definitions"`
}

// structsOf collects the structs reachable from t by name, each once.
func structsOf(t *schema.Type, into map[string][]*schema.Struct, seen map[*schema.Struct]bool) {
	if t == nil {
		return
	}
	if s := t.Struct; s != nil && !seen[s] {
		seen[s] = true
		into[s.Name] = append(into[s.Name], s)
		for i := range s.Fields {
			structsOf(&s.Fields[i].Type, into, seen)
		}
	}
	structsOf(t.Entry, into, seen)
}

// clusterStructs are the structs reachable from c's attributes, commands
// and events, by name.
func clusterStructs(c *schema.Cluster) map[string][]*schema.Struct {
	into, seen := map[string][]*schema.Struct{}, map[*schema.Struct]bool{}
	for i := range c.Attributes {
		structsOf(&c.Attributes[i].Type, into, seen)
	}
	for i := range c.Commands {
		for j := range c.Commands[i].Fields {
			structsOf(&c.Commands[i].Fields[j].Type, into, seen)
		}
	}
	for i := range c.Events {
		for j := range c.Events[i].Fields {
			structsOf(&c.Events[i].Fields[j].Type, into, seen)
		}
	}
	return into
}

func fieldByID(fs []schema.Field, id uint32) *schema.Field {
	for i := range fs {
		if fs[i].ID == id {
			return &fs[i]
		}
	}
	return nil
}

// structSecret reports whether a struct named name among structs has the
// field id marked Secret.
func structSecret(structs map[string][]*schema.Struct, name string, id uint32) (found, marked bool) {
	for _, s := range structs[name] {
		if f := fieldByID(s.Fields, id); f != nil {
			found = true
			marked = marked || f.Secret
		}
	}
	return found, marked
}

// TestSecretMarks: every isSecret mark of the snapshot's clusters — on a
// command's field, an attribute, a cluster or global datatype's field —
// is the table's Secret, and so is a mark of a datatype of the
// snapshot's top-level definitions; the tables hold no Secret beyond the
// marks: their count is the snapshot's (read from it, never a constant).
func TestSecretMarks(t *testing.T) {
	var snap markedSnapshot
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	marks := 0
	all := map[string][]*schema.Struct{}
	allSeen := map[*schema.Struct]bool{}
	for _, c := range schema.Clusters() {
		for name, ss := range clusterStructs(c) {
			for _, s := range ss {
				if !allSeen[s] {
					allSeen[s] = true
					all[name] = append(all[name], s)
				}
			}
		}
	}
	for _, sc := range snap.Clusters {
		c := schema.ClusterByID(sc.ID)
		if c == nil {
			t.Fatalf("cluster %s (%d) is not in the tables", sc.Name, sc.ID)
		}
		for _, a := range sc.Attributes {
			if !a.Effective.Secret {
				continue
			}
			marks++
			if ta := c.Attribute(a.ID); ta == nil || !ta.Secret {
				t.Errorf("%s.%s: the table's attribute is not Secret", sc.Name, a.Name)
			}
		}
		for _, x := range sc.Commands {
			for _, f := range x.Effective.Fields {
				if !f.Secret {
					continue
				}
				marks++
				var cmd *schema.Command
				switch x.Direction {
				case "request":
					cmd = c.Command(x.ID)
				case "response":
					cmd = c.Response(x.ID)
				default:
					if cmd = c.CommandByName(x.Name); cmd == nil {
						cmd = c.ResponseByName(x.Name)
					}
				}
				if cmd == nil {
					t.Errorf("%s.%s: no command in the tables", sc.Name, x.Name)
					continue
				}
				if tf := fieldByID(cmd.Fields, f.ID); tf == nil || !tf.Secret {
					t.Errorf("%s.%s.%s: the table's field is not Secret", sc.Name, x.Name, f.Name)
				}
			}
		}
		structs := clusterStructs(c)
		for _, dt := range sc.Datatypes {
			for _, f := range dt.Fields {
				if !f.Secret {
					continue
				}
				marks++
				if found, marked := structSecret(structs, dt.Name, f.ID); !found || !marked {
					t.Errorf("%s.%s.%s: the table's field is not Secret (found %v)", sc.Name, dt.Name, f.Name, found)
				}
			}
		}
	}
	for _, dt := range snap.GlobalDatatypes {
		for _, f := range dt.Fields {
			if !f.Secret {
				continue
			}
			marks++
			if found, marked := structSecret(all, dt.Name, f.ID); !found || !marked {
				t.Errorf("global %s.%s: the table's field is not Secret (found %v)", dt.Name, f.Name, found)
			}
		}
	}
	// The shared definition sets (WebRtcTransportDefinitions), whose
	// datatypes a cluster names by "<set>.<datatype>"; the struct keeps
	// the datatype's own name.
	for _, set := range snap.Definitions {
		for _, dt := range set.Datatypes {
			for _, f := range dt.Fields {
				if !f.Secret {
					continue
				}
				marks++
				if found, marked := structSecret(all, dt.Name, f.ID); !found || !marked {
					t.Errorf("%s.%s.%s: the table's field is not Secret (found %v)", set.Name, dt.Name, f.Name, found)
				}
			}
		}
	}
	if marks == 0 {
		t.Fatal("the snapshot carries no isSecret mark the generator reads")
	}

	// The reverse: the tables' Secret count is the marks'. Each command's
	// and attribute's per cluster, each struct once.
	secrets := 0
	for _, c := range schema.Clusters() {
		for i := range c.Attributes {
			if c.Attributes[i].Secret {
				secrets++
			}
		}
		for i := range c.Commands {
			for j := range c.Commands[i].Fields {
				if c.Commands[i].Fields[j].Secret {
					secrets++
				}
			}
		}
		for i := range c.Events {
			for j := range c.Events[i].Fields {
				if c.Events[i].Fields[j].Secret {
					secrets++
				}
			}
		}
	}
	for _, ss := range all {
		for _, s := range ss {
			for i := range s.Fields {
				if s.Fields[i].Secret {
					secrets++
				}
			}
		}
	}
	if secrets != marks {
		t.Fatalf("the tables hold %d Secret marks, the snapshot %d", secrets, marks)
	}
}

// TestSecretInJSONSchema: schema.json marks DoorLock's
// AliroGroupResolvingKey, the snapshot's secret attribute, "x-secret".
func TestSecretInJSONSchema(t *testing.T) {
	dl := schema.ClusterByName("DoorLock")
	a := dl.AttributeByName("AliroGroupResolvingKey")
	if a == nil || !a.Secret {
		t.Fatalf("DoorLock.AliroGroupResolvingKey: %+v", a)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schema.JSONSchema(), &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Defs["cluster."+strconv.FormatUint(uint64(dl.ID), 10)].Properties[strconv.FormatUint(uint64(a.ID), 10)]
	if p["x-secret"] != true {
		t.Fatalf("schema.json DoorLock AliroGroupResolvingKey: %v", p)
	}
}
